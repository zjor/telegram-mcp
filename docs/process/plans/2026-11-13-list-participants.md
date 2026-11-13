# `list_participants` Tool Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a read-only MCP tool `list_participants` that lists members of a basic group or supergroup for a regular member, falling back to recent message senders when the member list is hidden.

**Architecture:** `Service.ListParticipants` resolves the peer, asks the full-chat call whether the list is visible, then runs either the members path (`channels.getParticipants` / `messages.getFullChat` participants) or the recent-senders path (`messages.getHistory` scan). An opaque cursor bound to the chat id carries the offset or `before_id`. `mcpserver` validates input and exposes the tool.

**Tech Stack:** Go 1.25, `github.com/gotd/td` v0.163.0 (`tg`, `tgerr`, `tgmock`), `github.com/modelcontextprotocol/go-sdk` v1.8.0.

**Spec:** `docs/process/specs/2026-11-13-list-participants-design.md`

## Global Constraints

- Read-only: no Telegram write calls. Tool annotation `ReadOnlyHint: true`.
- Only `group` and `supergroup`. Never call channel-subscriber APIs for broadcast channels.
- Never log member data (names, usernames). Only the tool result carries it.
- Every Telegram response with `Users`/`Chats` goes through `s.cacheAndSave`. Save failure is a `slog.Warn`.
- `Participants` is always a JSON array, never `null`. `Total` is `null` for `recent_senders`.
- Limit: default 50, max 100 (existing `parseLimit`).
- Fallback scan: 100 messages per `messages.getHistory` request, at most 1000 messages per call.
- Error texts, verbatim:
  - `chat_id is required`
  - `list_participants supports group and supergroup only`
  - `filter must be all, admins or bots`
  - `you are not a member of this chat`
  - `the group hides its admin list from members`

## Review Focus

1. Supergroup page where every item is `ChannelParticipantBanned`/`Left` → empty `participants` but `next_cursor` still set (Telegram returned a full page). Test in Task 2.
2. History message whose `from_id` is nil or `PeerChannel` (anonymous admin, send-as-channel) → skipped, no panic, no zero-id participant. Test in Task 3.
3. User in a participant list missing from the response `Users` (or `UserEmpty`) → that participant is skipped, no nil panic. Test in Task 2.
4. Cursor from chat A passed with chat B → `ErrInvalidCursor` before any Telegram call. Tests in Task 1 (decode) and Task 2 (no calls).
5. `query` with mixed case (`"aNN"`) against `"Ann"` / username `"ann_lee"` in local filtering → matches. Test in Task 3.

---

### Task 1: Types and participants cursor

**Files:**
- Modify: `internal/tg/types.go` (append)
- Create: `internal/tg/participants_cursor.go`
- Test: `internal/tg/participants_cursor_test.go`

**Interfaces:**
- Produces:
  - `ParticipantSource`, `SourceMembers`, `SourceRecentSenders`, `Participant`, `ListParticipantsResult` — copy verbatim from the spec's "Output structure".
  - `type ParticipantFilter string` with `FilterAll = "all"`, `FilterAdmins = "admins"`, `FilterBots = "bots"`.
  - `type ListParticipantsParams struct { ChatID int64; ChatType ChatType; Query string; Filter ParticipantFilter; Limit int; Cursor string }`.
  - `type participantsCursor struct` — copy verbatim from the spec's "Cursor".
  - `func encodeParticipantsCursor(c participantsCursor) string`
  - `func decodeParticipantsCursor(s string, chatID int64) (participantsCursor, error)` — returns `ErrInvalidCursor` on bad base64/JSON, unknown `Source`, or `c.ChatID != chatID`. Empty `s` returns zero cursor, nil.

- [ ] **Step 1: Write the failing tests**

```go
func TestParticipantsCursor_RoundTrip(t *testing.T) {
    in := participantsCursor{ChatID: 42, Source: SourceRecentSenders, BeforeID: 900}
    out, err := decodeParticipantsCursor(encodeParticipantsCursor(in), 42)
    // err == nil, out == in
}
func TestParticipantsCursor_Empty(t *testing.T)        // "" → zero value, nil
func TestParticipantsCursor_WrongChat(t *testing.T)    // encoded for 42, decoded for 43 → ErrInvalidCursor
func TestParticipantsCursor_Garbage(t *testing.T)      // "!!!", base64 of "{}" (no source), base64 of `{"c":42,"s":"x"}` → ErrInvalidCursor
```

- [ ] **Step 2: Run** `go test ./internal/tg -run TestParticipantsCursor` — expected: FAIL (undefined).
- [ ] **Step 3: Implement** types and cursor functions. Mirror `cursor.go` (base64url raw, JSON).
- [ ] **Step 4: Run** `go test ./internal/tg -run TestParticipantsCursor` — expected: PASS.
- [ ] **Step 5: Commit** `feat(tg): list_participants types and cursor`

---

### Task 2: Members source (visible lists)

**Files:**
- Create: `internal/tg/participants.go`
- Test: `internal/tg/participants_test.go`

**Interfaces:**
- Consumes: Task 1 types and cursor; existing `s.resolvePeer`, `s.cacheAndSave`, `s.peers.Delete`, `newTestService`.
- Produces:
  - `var ErrNotAMember = errors.New("you are not a member of this chat")`
  - `var ErrAdminsHidden = errors.New("the group hides its admin list from members")` (used in Task 3)
  - `func (s *Service) ListParticipants(ctx context.Context, p ListParticipantsParams) (*ListParticipantsResult, error)`
  - `func matchesQuery(u *tgapi.User, q string) bool` — case-insensitive substring of first name, last name, or username; empty `q` matches all. Used in Task 3.
  - `func newParticipant(u *tgapi.User) Participant` — fills ID, names, username, Bot, Deleted. Used in Task 3.
  - In this task, the hidden branch returns `errors.New("recent senders: not implemented")`; Task 3 replaces it.

Flow of `ListParticipants`: decode cursor (before any call) → `resolvePeer` → full-chat call (`channels.getFullChannel` with `InputChannel` from the `InputPeerChannel`, or `messages.getFullChat`) → `cacheAndSave` → visible? members path : fallback. If the cursor's `Source` differs from the current source, reset the cursor to zero. Map `CHANNEL_PRIVATE`, `CHAT_FORBIDDEN`, `CHAT_ID_INVALID` from any call to `ErrNotAMember`. On `CHANNEL_INVALID` or `PEER_ID_INVALID` from the full-chat call: `s.peers.Delete(p.ChatType, p.ChatID)`, `resolvePeer` again, retry the full-chat call once.

Visibility: supergroup visible ⇔ `CanViewParticipants && !ParticipantsHidden`; `Total = ParticipantsCount`. Basic group visible ⇔ `Participants` is `*tgapi.ChatParticipants`; `Total = len(list)`.

Supergroup filter mapping and role mapping exactly as the spec's table. `Offset` from cursor; `Limit = p.Limit`. `next_cursor` set iff Telegram returned `p.Limit` items; next offset = old offset + items returned. Basic group: filter (`admins` = creator+admin; `bots` = `User.Bot`) and `query` locally, then slice `[offset, offset+limit)`; `next_cursor` set iff more remain.

- [ ] **Step 1: Write the failing tests** (use `tgmock` `ExpectFunc(...).ThenResult(...)` like `users_test.go`):

```go
TestListParticipants_SupergroupRecent        // visible; filter all, no query → ChannelParticipantsRecent, Offset 0, Limit 50;
                                             // creator/admin/member roles; JoinedAt from Date; Total = ParticipantsCount; source "members"
TestListParticipants_SupergroupSearch        // query "ann" → ChannelParticipantsSearch{Q: "ann"}
TestListParticipants_SupergroupAdminsQuery   // filter admins + query "bo" → ChannelParticipantsAdmins; only "Bob" returned
TestListParticipants_SupergroupBots          // filter bots → ChannelParticipantsBots
TestListParticipants_SupergroupPaging        // Limit 2, Telegram returns 2 → NextCursor decodes to Offset 2; next call sends Offset 2;
                                             // Telegram returns 1 → NextCursor nil
TestListParticipants_SkipsBannedLeft         // page of 2 = Banned + Left, Limit 2 → Participants == []Participant{} (not nil), NextCursor set
TestListParticipants_MissingUserSkipped      // participant UserID 99 not in Users → skipped
TestListParticipants_CachesUsers             // after call, s.peers.Get(ChatPrivate, id) ok for returned users
TestListParticipants_BasicGroup              // getFullChat with ChatParticipants of 3 → roles, Total 3; Limit 2 → 2 items + cursor; next page 1 item, nil cursor
TestListParticipants_BasicGroupFilters       // admins → creator+admin only; bots → User.Bot only; query local
TestListParticipants_NotAMember              // getFullChannel returns tgerr CHANNEL_PRIVATE → errors.Is(err, ErrNotAMember)
TestListParticipants_StaleHashRetry          // first getFullChannel → CHANNEL_INVALID; dialog scan returns channel with new hash; retry uses new hash
TestListParticipants_CursorWrongChatNoCalls  // cursor for chat 1, call for chat 2 → ErrInvalidCursor, mock has no expectations
TestListParticipants_CursorSourceChanged     // recent_senders cursor on a visible group → Offset 0 request
TestMatchesQuery                             // "aNN" matches FirstName "Ann" and Username "ann_lee"; "" matches; "zz" does not
```

- [ ] **Step 2: Run** `go test ./internal/tg -run 'TestListParticipants|TestMatchesQuery'` — expected: FAIL.
- [ ] **Step 3: Implement** in `participants.go`.
- [ ] **Step 4: Run** the same command — expected: PASS. Then `go test ./...` — PASS.
- [ ] **Step 5: Commit** `feat(tg): list participants of visible groups`

---

### Task 3: Recent senders fallback

**Files:**
- Create: `internal/tg/participants_fallback.go`
- Test: `internal/tg/participants_fallback_test.go`

**Interfaces:**
- Consumes: Task 2 `ListParticipants` hidden branch, `matchesQuery`, `newParticipant`, `ErrAdminsHidden`.
- Produces: `func (s *Service) recentSenders(ctx context.Context, peer tgapi.InputPeerClass, p ListParticipantsParams, beforeID int) (*ListParticipantsResult, error)` and the admins-when-hidden path; `ListParticipants` calls them instead of the Task 2 placeholder.

Rules (from the spec, Step 2b):
- `filter=admins`: supergroup → `channels.getParticipants` with `ChannelParticipantsAdmins`; on success return `source: "members"`, `Total` = admin count, roles mapped, `query` applied locally; on `CHAT_ADMIN_REQUIRED` or `CHANNEL_PRIVATE`-class access error → `ErrAdminsHidden`. Basic group → `ErrAdminsHidden` with no call.
- Otherwise scan `messages.getHistory` (`OffsetID = beforeID`, `Limit = 100`). Count `*tgapi.Message` and `*tgapi.MessageService` with `FromID` of type `*tgapi.PeerUser`; skip everything else. First appearance wins; `LastMessageDate` = that message's date. Apply `bots` and `query` locally. Stop at `p.Limit` senders, 1000 messages, or end of history (fewer than 100 returned, or `MessagesMessages` type).
- `NextCursor`: `{ChatID, SourceRecentSenders, BeforeID: id of the oldest message scanned}` unless history ended. When stopping at `p.Limit` mid-page, `BeforeID` is the id of the message that added the last sender, so the rest of that page is scanned again next call.
- `Total` nil; `cacheAndSave` each history response.

- [ ] **Step 1: Write the failing tests**

```go
TestFallback_HiddenSupergroup          // ParticipantsHidden true → getHistory OffsetID 0 Limit 100; source "recent_senders"; Total nil
TestFallback_CannotViewParticipants    // CanViewParticipants false, ParticipantsHidden false → also falls back
TestFallback_BasicGroupForbidden       // ChatParticipantsForbidden → falls back
TestFallback_DedupNewestFirst          // msgs from 7(date 300), 8(200), 7(100) → [7 (LastMessageDate 300), 8]
TestFallback_SkipsNonUserSenders       // FromID nil and PeerChannel → skipped
TestFallback_ServiceMessageCounts      // MessageService from user 9 → included
TestFallback_StopsAtLimit              // Limit 2, page with senders 7,8,9 → [7,8]; NextCursor.BeforeID = id of 8's message
TestFallback_StopsAt1000               // 10 pages of 100 from one sender → exactly 10 getHistory calls; NextCursor set
TestFallback_HistoryEnds               // page of 3 messages → NextCursor nil
TestFallback_BotsAndQuery              // filter bots keeps only Bot users; query "aNN" keeps "Ann"
TestFallback_AdminsVisible             // hidden supergroup, filter admins → ChannelParticipantsAdmins; source "members"
TestFallback_AdminsHidden              // same, Telegram returns CHAT_ADMIN_REQUIRED → ErrAdminsHidden
TestFallback_AdminsBasicGroup          // hidden basic group, filter admins → ErrAdminsHidden, no getHistory call
```

- [ ] **Step 2: Run** `go test ./internal/tg -run TestFallback` — expected: FAIL.
- [ ] **Step 3: Implement** in `participants_fallback.go`; replace the Task 2 placeholder.
- [ ] **Step 4: Run** `go test ./internal/tg` — expected: PASS.
- [ ] **Step 5: Commit** `feat(tg): fall back to recent senders when members are hidden`

---

### Task 4: MCP tool and README

**Files:**
- Modify: `internal/mcpserver/server.go` (interface, input type, description const, `New`, handler)
- Modify: `internal/mcpserver/server_test.go` (fake gains `ListParticipants`)
- Modify: `README.md` (intro tool list line 4, new `### list_participants` section after `### get_user` / before `### Paging example`)

**Interfaces:**
- Consumes: `tg.ListParticipantsParams`, `tg.ListParticipantsResult`, `tg.ParticipantFilter` constants.
- Produces: `Telegram.ListParticipants(ctx context.Context, p tg.ListParticipantsParams) (*tg.ListParticipantsResult, error)`; `ListParticipantsInput` copied verbatim from the spec; tool description copied verbatim from the spec, plus the sentence "Senders may repeat across pages; deduplicate by id."

Handler validation order: `chat_id` → `chat_type` (only `group`/`supergroup`, else `list_participants supports group and supergroup only`; reuse `parseChatType` for unknown strings) → `filter` (`""` → `all`) → `parseLimit` → trim `query`.

- [ ] **Step 1: Write the failing tests**

```go
TestListParticipants_PassesParams        // {chat_id 5, chat_type "supergroup", query " ann ", filter "bots", limit 10, cursor "c"}
                                         // → fake gets {5, ChatSupergroup, "ann", FilterBots, 10, "c"}; structured result echoed
TestListParticipants_Defaults            // no filter/limit → FilterAll, 50
TestListParticipants_Validation          // table: missing chat_id; chat_type "private"/"channel" → exact supports-only text;
                                         // filter "kicked" → "filter must be all, admins or bots"; limit 101 → limit error; fake not called
TestListParticipants_ToolListed          // ListTools includes "list_participants" with ReadOnlyHint true
```

- [ ] **Step 2: Run** `go test ./internal/mcpserver -run TestListParticipants` — expected: FAIL.
- [ ] **Step 3: Implement** handler, register tool, update `New` doc comment; update README.
- [ ] **Step 4: Run** `go test ./... && go vet ./...` — expected: PASS, no vet output.
- [ ] **Step 5: Commit** `feat: list_participants MCP tool`

---

### Task 5: Manual check against real chats

Not code. The owner runs it, because it needs the real account.

- [ ] Build: `go build -o telegram-mcp ./cmd/...`
- [ ] Call `list_participants` on: (1) a small supergroup with a visible list; (2) a large public supergroup with hidden members, with and without `filter=admins`; (3) a basic group.
- [ ] Record in the spec under a new "Manual test results" heading: whether admins are visible when members are hidden, and where "recent" paging stopped in the large group.
