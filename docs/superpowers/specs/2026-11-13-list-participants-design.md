# `list_participants` tool — Design

Date: 2026-11-13
Status: draft, awaiting approval

## Intent

The existing tools show chats and messages, but not who is in a group.
`list_participants` returns the members of a basic group or supergroup.

It is built for a **regular member**, not an admin, so that it is useful to
a broad audience. Telegram often hides the member list from regular members.
In that case the tool still returns something useful: the people who posted
recently, clearly marked as a partial list.

## Goals

- List members of a basic group or a supergroup the owner belongs to.
- Work for a regular member. When the member list is hidden, fall back to
  recent message senders automatically, in the same call.
- Tell the caller where the list came from (`source`), so a partial list is
  never mistaken for a full one.
- Support name search (`query`) and two filters: `admins`, `bots`.
- Page through results with an opaque cursor, like `list_chats`.
- Feed `get_user`: every returned user that Telegram gives a full access hash
  for is stored in the peer cache.
- Read-only.

## Non-goals

- Broadcast channels. Only admins can list subscribers, so for a regular
  member the tool would always fail.
- Admin-only filters: kicked, banned, restricted.
- Full profile per member. The caller uses `get_user` for that.
- Deduplication across pages of the fallback (see Fallback).
- A full member list for groups that hide it. Telegram does not allow this
  for regular members.

## Anti-goals

- Returning a silent empty list. An empty result must mean "nobody matched",
  never "access was denied".
- Presenting recent senders as the full member list.
- Getting the account rate-limited. The fallback scan is capped per call;
  flood waits are obeyed by the existing middleware.
- Writing member data to logs.

## Decisions made by the owner

- Scope: `group` and `supergroup` only.
- Target user: regular member.
- When the member list is hidden: fall back to recent senders (option B).
- Fallback runs automatically inside one tool (approach 1), not through a
  caller-chosen `source` input.

## Approaches considered

1. **Automatic fallback in one tool (chosen).** The tool checks whether the
   member list is visible. If not, it scans history and returns senders with
   `source: "recent_senders"`. One call always gives a result. Agents handle
   this better than a retry-on-error flow.
2. **Caller chooses the source.** Input `source: members | recent_senders`;
   a hidden list returns an error asking to retry. More control, but two
   calls in the common case. Rejected.
3. **Error only, no fallback.** Simple, but the tool fails in many large
   public groups, which defeats the broad-audience goal. Rejected earlier.

## MCP tool

### `list_participants`

Annotations: `ReadOnlyHint: true`.

Description (for the model): "List members of a group or supergroup. Pass
chat_id and chat_type from list_chats. If the group hides its member list,
returns recent message senders instead and sets source=recent_senders; that
list is partial. Use the returned cursor for the next page. Use get_user for
full profiles."

Input (in `internal/mcpserver`):

```go
type ListParticipantsInput struct {
    ChatID   int64  `json:"chat_id"            jsonschema:"id from list_chats"`
    ChatType string `json:"chat_type"          jsonschema:"group or supergroup"`
    Query    string `json:"query,omitempty"    jsonschema:"match by name or username"`
    Filter   string `json:"filter,omitempty"   jsonschema:"all, admins or bots; default all"`
    Limit    int    `json:"limit,omitempty"    jsonschema:"page size 1-100; default 50"`
    Cursor   string `json:"cursor,omitempty"   jsonschema:"opaque cursor from a previous call"`
}
```

Validation (in `mcpserver`):

- `chat_id` must be non-zero: `"chat_id is required"`.
- `chat_type` must be `group` or `supergroup`. `private` and `channel` give
  `"list_participants supports group and supergroup only"`.
- `filter` must be empty, `all`, `admins` or `bots`.
- `limit` uses the existing `parseLimit` (default 50, max 100).
- `query` is trimmed.

### Output structure

Lives in `internal/tg/types.go`.

```go
type ParticipantSource string

const (
    SourceMembers       ParticipantSource = "members"
    SourceRecentSenders ParticipantSource = "recent_senders"
)

type Participant struct {
    ID              int64  `json:"id"`
    FirstName       string `json:"first_name"`
    LastName        string `json:"last_name,omitempty"`
    Username        string `json:"username,omitempty"`
    Bot             bool   `json:"bot,omitempty"`
    Deleted         bool   `json:"deleted,omitempty"`
    Role            string `json:"role,omitempty"`              // creator | admin | member; members source only
    JoinedAt        int64  `json:"joined_at,omitempty"`         // unix seconds; members source only, when known
    LastMessageDate int64  `json:"last_message_date,omitempty"` // unix seconds; recent_senders only
}

type ListParticipantsResult struct {
    Source       ParticipantSource `json:"source"`
    Participants []Participant     `json:"participants"`
    Total        *int              `json:"total"`       // member count Telegram reports; null for recent_senders
    NextCursor   *string           `json:"next_cursor"` // null on the last page
}
```

`Participants` is always a JSON array, never `null`.

## How it works

`Service.ListParticipants(ctx, ListParticipantsParams)` in a new file
`internal/tg/participants.go`. The peer is resolved with the existing
`resolvePeer` (cache, then dialog scan; `ErrUnknownChat` if not found).

### Step 1: Is the member list visible?

- **Supergroup:** `channels.getFullChannel`. The list is visible when
  `CanViewParticipants` is true and `ParticipantsHidden` is false.
  `Total` = `ParticipantsCount`.
- **Basic group:** `messages.getFullChat`. The list is visible when
  `Participants` is `ChatParticipants`. `ChatParticipantsForbidden` means
  hidden. `Total` = number of participants.

Visible → members source. Hidden → recent senders source.
The full-chat call runs on every call, also for later pages. This keeps the
cursor simple and catches a change in visibility between pages.

### Step 2a: Members source

**Supergroup:** `channels.getParticipants`, `Limit` = page size,
`Offset` from the cursor. The Telegram filter is:

| `filter` | `query` | Telegram filter |
|---|---|---|
| all | empty | `ChannelParticipantsRecent` |
| all | set | `ChannelParticipantsSearch{Q: query}` |
| admins | any | `ChannelParticipantsAdmins`, `query` applied locally |
| bots | any | `ChannelParticipantsBots`, `query` applied locally |

Role mapping: `ChannelParticipantCreator` → `creator`,
`ChannelParticipantAdmin` → `admin`, `ChannelParticipant` and
`ChannelParticipantSelf` → `member`. `ChannelParticipantBanned` and
`ChannelParticipantLeft` are skipped. `JoinedAt` = the `Date` field where the
type has one.

Next cursor: set when the page returned `limit` items before local
filtering; offset advances by the number of items Telegram returned. Offset
paging can shift if people join or leave between calls. That is accepted.

Telegram caps "recent" listing for very large groups (about 10k, **unsure**:
not checked in this repo). Past that cap, pages come back empty and paging
ends. `query` still finds people beyond the cap.

**Basic group:** the full list comes from Step 1. Filter and `query` are
applied locally. The page is a slice by offset. Role mapping:
`ChatParticipantCreator` → `creator`, `ChatParticipantAdmin` → `admin`,
`ChatParticipant` → `member`. `JoinedAt` = `Date`.

User details (name, username, bot, deleted) come from the `Users` list of
the response. Every response goes through `cacheAndSave`, so `get_user` works
on the returned ids.

### Step 2b: Recent senders source

- `filter=admins` first tries `channels.getParticipants` with
  `ChannelParticipantsAdmins` (supergroup only). Telegram may allow this even
  when the full list is hidden (**unsure**: settled by the manual test
  below). If it returns `CHAT_ADMIN_REQUIRED` or another access error, the
  tool returns `ErrAdminsHidden`. In that case `source` is `members`,
  because the admin list is complete. For basic groups with a hidden list,
  `filter=admins` returns `ErrAdminsHidden`.
- Otherwise the tool scans history with `messages.getHistory`, 100 messages
  per request, starting at `before_id` from the cursor (0 on the first call).
- It collects senders in order of first appearance (newest first). Only user
  senders count: `from_id` of `Message` and `MessageService` when it is a
  `PeerUser`. Channel senders (anonymous admins, "send as channel") are
  skipped. For private-style messages without `from_id`, the sender is
  skipped too.
- Deduplication is per call. `LastMessageDate` = date of that user's newest
  message seen in this call.
- `filter=bots` and `query` are applied locally to each sender (query
  matches first name, last name or username, case-insensitive substring).
- The scan stops when `limit` matching senders are collected, or 1000
  messages are scanned in this call, or history ends.
- Next cursor: the id of the oldest message scanned, unless history ended.
  A sender can appear again on a later page. The tool description tells the
  caller to deduplicate by `id`.
- Senders in large groups are often "min" users without a usable access
  hash. `PutFromResponse` already skips them, so `get_user` may fail for
  those ids with `ErrUnknownUser`. This is a known limit.

### Cursor

Opaque base64url JSON, separate from the `list_chats` cursor type:

```go
type participantsCursor struct {
    ChatID   int64             `json:"c"`
    Source   ParticipantSource `json:"s"`
    Offset   int               `json:"o,omitempty"` // members source
    BeforeID int               `json:"b,omitempty"` // recent_senders source
}
```

A cursor for a different `chat_id`, or a cursor that does not decode, is
`ErrInvalidCursor`. If the source changed since the cursor was made
(visibility changed), the tool restarts from the first page of the new
source and does not error (my call; see the list at the end).

### Errors

| Case | Error |
|---|---|
| chat not in cache or dialogs | `ErrUnknownChat` (existing) |
| `CHANNEL_PRIVATE`, `CHAT_FORBIDDEN` (owner left or was removed) | new `ErrNotAMember`: "you are not a member of this chat" |
| admins hidden in fallback | new `ErrAdminsHidden`: "the group hides its admin list from members" |
| bad cursor | `ErrInvalidCursor` (existing) |
| stale access hash (`CHANNEL_INVALID`, `PEER_ID_INVALID`) | drop the cache entry, rescan dialogs, retry once (same pattern as `get_user`) |
| other Telegram errors | wrapped with the method name, like `channels.getParticipants: …` |

## Wiring

- `mcpserver.Telegram` interface gains
  `ListParticipants(ctx, tg.ListParticipantsParams) (*tg.ListParticipantsResult, error)`.
- `ListParticipantsParams` in `types.go`: `ChatID`, `ChatType`, `Query`,
  `Filter` (typed `ParticipantFilter`: `all | admins | bots`), `Limit`,
  `Cursor`.
- Register the tool in `mcpserver.New`. Update the doc comment that lists the
  tools, and the README tool list.

## Testing

Unit tests follow the existing pattern (fake invoker in `internal/tg`, fake
`Telegram` in `internal/mcpserver`):

- Validation: missing `chat_id`, bad `chat_type` (`private`, `channel`), bad
  `filter`, bad `limit`.
- Supergroup visible: filter mapping table above; role mapping; paging and
  `next_cursor`; banned/left skipped; users stored in peer cache.
- Supergroup hidden (`ParticipantsHidden` true, and separately
  `CanViewParticipants` false): falls back; `source=recent_senders`;
  `total` null.
- Basic group visible and forbidden.
- Fallback: dedup within a call; channel senders skipped; stops at `limit`;
  stops at 1000 messages; ends when history ends; `query` and `bots` local
  filters.
- `filter=admins` in fallback: success path and `CHAT_ADMIN_REQUIRED` path.
- Cursor: wrong chat id, garbage, source change restarts.
- Errors: `CHANNEL_PRIVATE` → `ErrNotAMember`; stale hash → one retry.

Manual test before merge, against real chats:

1. A small supergroup with a visible list.
2. A large public supergroup with hidden members, with and without
   `filter=admins`. This settles the open admin question.
3. A basic group.

## Left to the builder

- Exact names of private helpers and file split inside `internal/tg`.
- Whether the history scan reuses code from `history.go` or calls
  `messages.getHistory` directly.

## Calls I made (not discussed with the owner)

- `chat_type` is a required input, for consistency with `read_chat` and
  `send_message`.
- Fallback cap: 1000 messages per call.
- No dedup across fallback pages; caller dedupes by `id`.
- `role` and `joined_at` only in the members source; `last_message_date`
  only in the fallback.
- Service messages count as activity in the fallback.
- A changed source between pages restarts paging instead of failing.
- `filter=admins` with a hidden list returns a full admin list with
  `source=members` when Telegram allows it, else an error. It never falls
  back to senders, because senders carry no role.
