# Telegram MCP Server Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Go binary `telegram-mcp` that serves two read-only MCP tools over stdio, `list_chats` and `read_chat`, backed by the owner's own Telegram account over MTProto.

**Architecture:** `internal/tg` wraps gotd: pure rendering of messages to plain text, a persisted peer cache, and two operations (`ListChats`, `ReadChat`) written against `tg.Invoker` so they are testable with `tgmock`. `internal/mcpserver` registers typed tool handlers against a small `Telegram` interface. `cmd/telegram-mcp` wires config, session storage, flood-wait middleware, and the `login` / `serve` / `logout` subcommands.

**Tech Stack:** Go 1.25, `github.com/modelcontextprotocol/go-sdk v1.8.0`, `github.com/gotd/td v0.163.0`, `github.com/gotd/contrib v0.25.0` (flood-wait middleware), `golang.org/x/term` (password prompt). Tests use stdlib `testing`, `tgmock`, and the go-sdk in-memory transport.

**Spec:** `docs/superpowers/specs/2026-10-31-telegram-mcp-design.md`

## Global Constraints

- Go `1.25` (both libraries require `go 1.25.0`). Module path `github.com/zjor/telegram-mcp` (designer's call; any path works).
- Binary name `telegram-mcp`; subcommands `login`, `serve`, `logout`. `serve --transport` accepts only `stdio`.
- Env vars: `TELEGRAM_API_ID` (int), `TELEGRAM_API_HASH`, optional `TELEGRAM_SESSION_PATH`. Default session path `~/.config/telegram-mcp/session.json`. Peer cache `peers.json` in the same directory. Files mode `0600`, directory `0700`.
- stdout is reserved for MCP. All logs go to stderr via `log/slog`.
- Read-only: no RPC that writes to Telegram except `auth.logOut` in `logout`.
- Page size: default `50`, max `100`. Out-of-range or unknown enum values are tool errors, not clamped.
- Chat `type` values: `private`, `group`, `supergroup`, `channel`. Folder values: `main`, `archive` (Telegram folder ids `0` and `1`).
- Flood waits: `floodwait.NewSimpleWaiter().WithMaxWait(60 * time.Second)` as a client middleware.
- Access hashes never appear in tool output or in cursors.
- Commit after every task. The repo starts without git; Task 1 runs `git init`.

## Review Focus

1. A dialog whose top message is missing from the `messages` array (deleted top message): `last_message_date` must be `0`, not a panic. Test in Task 4.
2. A `read_chat` page where every message is older than `since_date`: `messages` is empty and `next_before_id` is null. Test in Task 5.
3. A `MessageEmpty` in a history page (deleted message): it is skipped, not rendered, and does not break `next_before_id`. Test in Task 5.
4. A user with `Deleted` set or `UserEmpty` as a sender: `from` is `"Deleted Account"`, title likewise, no empty strings. Test in Task 4 and Task 5.
5. A cursor string that was not produced by this server (garbage or tampered): `list_chats` returns a tool error `invalid cursor`, never a panic or an `InputPeerEmpty` request. Test in Task 4.

---

## File structure

```
go.mod
cmd/telegram-mcp/main.go          # subcommand dispatch, exit codes
cmd/telegram-mcp/serve.go         # serve command
cmd/telegram-mcp/login.go         # login + logout commands, terminal authenticator
internal/config/config.go         # env + paths
internal/config/config_test.go
internal/tg/types.go              # Chat, Message, params/results, enums
internal/tg/render.go             # tg.MessageClass -> text, sender name
internal/tg/render_test.go
internal/tg/peers.go              # PeerCache (+ JSON persistence)
internal/tg/peers_test.go
internal/tg/cursor.go             # opaque list cursor
internal/tg/cursor_test.go
internal/tg/dialogs.go            # Service.ListChats
internal/tg/dialogs_test.go
internal/tg/history.go            # Service.ReadChat + peer resolution fallback
internal/tg/history_test.go
internal/tg/service.go            # Service struct, NewService
internal/tg/client.go             # gotd telegram.Client construction
internal/mcpserver/server.go      # tool registration, Telegram interface
internal/mcpserver/server_test.go
README.md
```

---

### Task 1: Scaffold, config, and command dispatch

**Files:**
- Create: `go.mod`, `.gitignore`, `cmd/telegram-mcp/main.go`, `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces:
  ```go
  package config
  type Config struct {
      APIID       int
      APIHash     string
      SessionPath string // absolute file path
  }
  func (c Config) PeersPath() string // filepath.Join(filepath.Dir(c.SessionPath), "peers.json")
  func Load(getenv func(string) string, homeDir string) (Config, error)
  ```
- `main.go` exposes `func run(args []string, stdout, stderr io.Writer) int` and `main()` calls `os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))`.

- [ ] **Step 1: Init module and git**

```bash
cd /Users/zjor/projects/telegram-mcp
go mod init github.com/zjor/telegram-mcp
git init
printf 'telegram-mcp\n*.session\n' > .gitignore
```

- [ ] **Step 2: Write the failing config tests**

`internal/config/config_test.go`:

```go
func TestLoad_Defaults(t *testing.T) {
    env := map[string]string{"TELEGRAM_API_ID": "12345", "TELEGRAM_API_HASH": "abc"}
    cfg, err := config.Load(func(k string) string { return env[k] }, "/home/u")
    // assert err == nil
    // assert cfg.APIID == 12345, cfg.APIHash == "abc"
    // assert cfg.SessionPath == "/home/u/.config/telegram-mcp/session.json"
    // assert cfg.PeersPath() == "/home/u/.config/telegram-mcp/peers.json"
}
func TestLoad_SessionPathOverride(t *testing.T) { /* TELEGRAM_SESSION_PATH=/tmp/s.json -> SessionPath == "/tmp/s.json", PeersPath() == "/tmp/peers.json" */ }
func TestLoad_MissingAPIID(t *testing.T)      { /* err mentions TELEGRAM_API_ID */ }
func TestLoad_NonNumericAPIID(t *testing.T)   { /* "abc" -> err mentions TELEGRAM_API_ID */ }
func TestLoad_MissingAPIHash(t *testing.T)    { /* err mentions TELEGRAM_API_HASH */ }
```

- [ ] **Step 3: Run tests, expect compile failure**

Run: `go test ./internal/config/ -v`
Expected: FAIL, package `config` has no `Load`.

- [ ] **Step 4: Implement `config.Load` and `PeersPath`**

Error messages name the missing variable, e.g. `TELEGRAM_API_ID is not set`.

- [ ] **Step 5: Run tests, expect pass**

Run: `go test ./internal/config/ -v`
Expected: PASS (5 tests).

- [ ] **Step 6: Implement `cmd/telegram-mcp/main.go` dispatch**

`run` switches on `args[0]`: `serve`, `login`, `logout` call `runServe`, `runLogin`, `runLogout` (stubs returning `2` and printing `not implemented` to stderr for now; later tasks fill them). No args or unknown subcommand: print usage to stderr, return `2`. Usage text lists the three subcommands and the three env vars.

- [ ] **Step 7: Build and check usage**

Run: `go build ./... && go run ./cmd/telegram-mcp; echo "exit=$?"`
Expected: usage on stderr, `exit=2`.

- [ ] **Step 8: Commit**

```bash
git add -A
git commit -m "feat: scaffold telegram-mcp with config loading and command dispatch"
```

---

### Task 2: Domain types and message rendering

**Files:**
- Create: `internal/tg/types.go`, `internal/tg/render.go`
- Test: `internal/tg/render_test.go`

**Interfaces:**
- Produces (`types.go`):
  ```go
  package tg
  type ChatType string
  const (ChatPrivate ChatType = "private"; ChatGroup ChatType = "group"; ChatSupergroup ChatType = "supergroup"; ChatChannel ChatType = "channel")
  type Folder string
  const (FolderMain Folder = "main"; FolderArchive Folder = "archive")
  const (DefaultLimit = 50; MaxLimit = 100)

  type Chat struct {
      ID              int64    `json:"id"`
      Type            ChatType `json:"type"`
      Title           string   `json:"title"`
      Username        string   `json:"username,omitempty"`
      LastMessageDate int64    `json:"last_message_date"`
      UnreadCount     int      `json:"unread_count"`
      Folder          Folder   `json:"folder"`
  }
  type Message struct {
      ID   int    `json:"id"`
      Date int64  `json:"date"`
      From string `json:"from"`
      Text string `json:"text"`
  }
  type ListChatsParams struct { Folder Folder; Limit int; Cursor string }
  type ListChatsResult struct { Chats []Chat `json:"chats"`; NextCursor *string `json:"next_cursor"` }
  type ReadChatParams struct { ChatID int64; ChatType ChatType; Limit int; BeforeID int; SinceDate int64 }
  type ReadChatResult struct { Messages []Message `json:"messages"`; NextBeforeID *int `json:"next_before_id"` }
  ```
- Produces (`render.go`), all pure:
  ```go
  // Names resolves sender/peer display names from the users/chats arrays of one RPC response.
  type Names struct{ users map[int64]*tgapi.User; chats map[int64]tgapi.ChatClass }
  func NewNames(users []tgapi.UserClass, chats []tgapi.ChatClass) *Names
  func (n *Names) PeerName(p tgapi.PeerClass) string   // "Deleted Account" for deleted/empty users, "" for nil peer
  func UserName(u *tgapi.User) string                  // "First Last" trimmed; "Deleted Account" if u.Deleted
  func RenderText(m *tgapi.Message) string
  func RenderService(m *tgapi.MessageService) string
  func RenderMessage(mc tgapi.MessageClass, names *Names, peerTitle string) (Message, bool) // false for *MessageEmpty
  ```
  The gotd package `github.com/gotd/td/tg` is imported as `tgapi` everywhere in this package to avoid clashing with the package name `tg`.

- [ ] **Step 1: Write failing render tests**

`internal/tg/render_test.go` (table tests; each row builds a `*tgapi.Message` or `*tgapi.MessageService`):

```go
func TestRenderText(t *testing.T) {
  // plain text, no media                        -> "hello"
  // MessageMediaPhoto, empty text               -> "[photo]"
  // MessageMediaPhoto, text "cap"               -> "[photo] cap"
  // MessageMediaDocument, DocumentAttributeSticker        -> "[sticker]"
  // MessageMediaDocument, DocumentAttributeAudio{Voice:true} -> "[voice]"
  // MessageMediaDocument, DocumentAttributeAudio{}        -> "[audio]"
  // MessageMediaDocument, DocumentAttributeVideo{RoundMessage:true} -> "[video_note]"
  // MessageMediaDocument, DocumentAttributeVideo{}        -> "[video]"
  // MessageMediaDocument, DocumentAttributeAnimated       -> "[gif]"
  // MessageMediaDocument, only DocumentAttributeFilename{"a.pdf"} -> "[document a.pdf]"
  // MessageMediaDocument, no attributes                   -> "[document]"
  // MessageMediaGeo / MessageMediaGeoLive                 -> "[location]"
  // MessageMediaContact                                   -> "[contact]"
  // MessageMediaPoll                                      -> "[poll]"
  // MessageMediaWebPage, text "see https://x"             -> "see https://x"  (web preview is not media)
  // MessageMediaUnsupported                               -> "[media]"
}
func TestRenderService(t *testing.T) {
  // MessageActionPinMessage          -> "[service: message pinned]"
  // MessageActionChatEditTitle{"New"}-> "[service: title changed to New]"
  // MessageActionChatAddUser         -> "[service: user added]"
  // MessageActionChatDeleteUser      -> "[service: user removed]"
  // MessageActionPhoneCall           -> "[service: call]"
  // MessageActionChatJoinedByLink    -> "[service: user joined]"
  // anything else                    -> "[service]"
}
func TestUserName(t *testing.T) {
  // {FirstName:"A", LastName:"B"} -> "A B"; {FirstName:"A"} -> "A"; {Deleted:true} -> "Deleted Account"
}
func TestNames_PeerName(t *testing.T) {
  // PeerUser present -> user name; PeerUser as UserEmpty -> "Deleted Account"; PeerUser absent -> "Deleted Account"
  // PeerChat -> Chat.Title; PeerChannel -> Channel.Title; ChatForbidden/ChannelForbidden -> their Title
}
func TestRenderMessage(t *testing.T) {
  // *MessageEmpty -> ok == false
  // Message{Out:true}                     -> From == "me"
  // Message{FromID: PeerUser(7)} with user 7 "Ann" -> From == "Ann"
  // Message{FromID: nil}, peerTitle "Chan" -> From == "Chan"
  // Message{FromID: nil, PostAuthor: "Ed"} -> From == "Ed"
  // Date is copied as int64; ID copied; Text == RenderText(m)
  // MessageService{FromID: PeerUser(7)}   -> From "Ann", Text == RenderService
}
```

- [ ] **Step 2: Run tests, expect compile failure**

Run: `go get github.com/gotd/td@v0.163.0 && go test ./internal/tg/ -v`
Expected: FAIL, undefined symbols.

- [ ] **Step 3: Implement `types.go` and `render.go`**

Document media detection: inspect `Document.Attributes` in this priority: Sticker, Audio(Voice), Audio, Video(RoundMessage), Video, Animated, Filename. `From` precedence: `Out` → `me`; `FromID` non-nil → `names.PeerName`; `PostAuthor` non-empty → it; else `peerTitle`.

- [ ] **Step 4: Run tests, expect pass**

Run: `go test ./internal/tg/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tg go.mod go.sum
git commit -m "feat(tg): domain types and plain-text message rendering"
```

---

### Task 3: Peer cache with JSON persistence

**Files:**
- Create: `internal/tg/peers.go`
- Test: `internal/tg/peers_test.go`

**Interfaces:**
- Produces:
  ```go
  type PeerCache struct { /* mu sync.Mutex; path string; entries map[string]peerEntry */ }
  type peerEntry struct { Type ChatType `json:"type"`; ID int64 `json:"id"`; AccessHash int64 `json:"access_hash,omitempty"` }
  func NewPeerCache(path string) *PeerCache
  func (c *PeerCache) Load() error                 // missing file is not an error
  func (c *PeerCache) Save() error                 // writes JSON, mode 0600, via temp file + rename; creates dir 0700
  func (c *PeerCache) Put(t ChatType, id int64, accessHash int64)
  func (c *PeerCache) Get(t ChatType, id int64) (tgapi.InputPeerClass, bool)
  func (c *PeerCache) PutFromResponse(users []tgapi.UserClass, chats []tgapi.ChatClass) // skips Min users/channels and UserEmpty
  ```
  `Get` builds `InputPeerUser{UserID, AccessHash}` for `private`, `InputPeerChat{ChatID}` for `group`, `InputPeerChannel{ChannelID, AccessHash}` for `supergroup` and `channel`. Key is `fmt.Sprintf("%s:%d", t, id)`; `supergroup` and `channel` share the key prefix `channel` so a chat found under either name resolves.

- [ ] **Step 1: Write failing tests**

```go
func TestPeerCache_PutGet(t *testing.T)            // private 7/hash 99 -> InputPeerUser{7,99}; group 5 -> InputPeerChat{5}; channel 3/hash 1 -> InputPeerChannel{3,1}; supergroup 3 -> same entry
func TestPeerCache_GetUnknown(t *testing.T)        // ok == false
func TestPeerCache_SaveLoadRoundTrip(t *testing.T) // t.TempDir(); Save; stat mode == 0600; new cache Load; Get returns same
func TestPeerCache_LoadMissingFile(t *testing.T)   // Load returns nil
func TestPeerCache_PutFromResponse(t *testing.T)   // User{ID:7,AccessHash:9}, User{ID:8,Min:true}, UserEmpty{9}, Channel{ID:3,AccessHash:4,Megagroup:true}, Chat{ID:5} -> 7 found, 8 absent, 9 absent, supergroup 3 found, group 5 found
```

- [ ] **Step 2: Run tests, expect failure**

Run: `go test ./internal/tg/ -run PeerCache -v`
Expected: FAIL, undefined `NewPeerCache`.

- [ ] **Step 3: Implement `peers.go`**

- [ ] **Step 4: Run tests, expect pass**

Run: `go test ./internal/tg/ -run PeerCache -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tg/peers.go internal/tg/peers_test.go
git commit -m "feat(tg): persisted peer cache for access hashes"
```

---

### Task 4: Cursor and `ListChats`

**Files:**
- Create: `internal/tg/cursor.go`, `internal/tg/service.go`, `internal/tg/dialogs.go`
- Test: `internal/tg/cursor_test.go`, `internal/tg/dialogs_test.go`

**Interfaces:**
- Consumes: `PeerCache`, `Names`, `UserName`, types from Tasks 2 and 3.
- Produces:
  ```go
  type cursor struct { Date int `json:"d"`; ID int `json:"i"`; PeerType ChatType `json:"t"`; PeerID int64 `json:"p"` }
  func encodeCursor(c cursor) string                 // base64.RawURLEncoding of JSON
  func decodeCursor(s string) (cursor, error)        // ErrInvalidCursor on bad base64/JSON/empty type
  var ErrInvalidCursor = errors.New("invalid cursor")
  var ErrUnknownChat  = errors.New("unknown chat; call list_chats first")

  type Service struct { api *tgapi.Client; peers *PeerCache }
  func NewService(inv tgapi.Invoker, peers *PeerCache) *Service
  func (s *Service) ListChats(ctx context.Context, p ListChatsParams) (*ListChatsResult, error)
  ```
  Behavior of `ListChats`:
  - `Limit` 0 → `DefaultLimit`. Caller validates range; service only defaults.
  - Request: `MessagesGetDialogsRequest{Limit: p.Limit, OffsetPeer: &InputPeerEmpty{}}`, `SetFolderID(0)` for `main`, `SetFolderID(1)` for `archive`. With a cursor: `OffsetDate`, `OffsetID` from it and `OffsetPeer` from `peers.Get(PeerType, PeerID)`; if not cached, `InputPeerEmpty`.
  - Response handling: `MessagesDialogs` and `MessagesDialogsSlice` are accepted; `MessagesDialogsNotModified` returns an empty result. Users/chats are fed to `peers.PutFromResponse` and `peers.Save()` (save error is logged, not returned).
  - Only `*tgapi.Dialog` entries are mapped; `DialogFolder` and `DialogCommunity` are skipped.
  - Map: `PeerUser` → `private`, title `UserName`, username from user; `PeerChat` → `group`, title from `Chat`/`ChatForbidden`; `PeerChannel` → `supergroup` if `Channel.Megagroup`, else `channel`; `ChannelForbidden` → `channel` unless `Megagroup`.
  - `LastMessageDate`: date of the message in `Messages` whose ID equals `Dialog.TopMessage` and whose `PeerID` equals the dialog peer; `0` if absent.
  - `NextCursor`: nil when response is `MessagesDialogs` (complete) or fewer than `p.Limit` dialogs were returned; otherwise built from the last returned dialog's top message date/id and its peer.

- [ ] **Step 1: Write failing cursor tests**

```go
func TestCursor_RoundTrip(t *testing.T)   // encode then decode equals input
func TestCursor_Invalid(t *testing.T)     // "not base64!", base64 of "{}", base64 of `{"t":"bogus"}` -> errors.Is(err, ErrInvalidCursor)
```

- [ ] **Step 2: Write failing ListChats tests using tgmock**

`dialogs_test.go` builds a `*tgmock.Mock` with `tgmock.New(t)`, passes it to `NewService(mock, NewPeerCache(filepath.Join(t.TempDir(), "peers.json")))`, and uses `mock.ExpectFunc(func(b bin.Encoder){ req := b.(*tgapi.MessagesGetDialogsRequest); /* assert fields */ }).ThenResult(&tgapi.MessagesDialogsSlice{...})`.

```go
func TestListChats_MapsAllPeerTypes(t *testing.T)
// dialogs: PeerUser 7 (User "Ann Lee", username "ann"), PeerChat 5 (Chat "Team"), PeerChannel 3 (Channel Megagroup "SG"), PeerChannel 4 (Channel "News")
// messages: top messages with dates 100, 200, 300, 400; unread counts 1,2,3,4
// expect 4 chats in order, types private/group/supergroup/channel, titles, username "ann" only on first, folder "main", dates, unread
// expect request: Limit 50, FolderID set to 0, OffsetPeer InputPeerEmpty
func TestListChats_ArchiveFolder(t *testing.T)        // Folder archive -> request FolderID == 1; chats carry Folder "archive"
func TestListChats_SkipsDialogFolder(t *testing.T)    // response contains DialogFolder + one Dialog -> 1 chat
func TestListChats_MissingTopMessage(t *testing.T)    // Messages empty -> LastMessageDate == 0   (Review Focus 1)
func TestListChats_DeletedUser(t *testing.T)          // PeerUser whose User has Deleted:true -> Title "Deleted Account" (Review Focus 4)
func TestListChats_NextCursor(t *testing.T)
// Limit 2, slice returns 2 dialogs -> NextCursor non-nil; decodeCursor gives Date/ID of last top message, PeerType/PeerID of last dialog
// second call with that Cursor -> request OffsetDate/OffsetID match, OffsetPeer is InputPeerUser with access hash from first response
func TestListChats_LastPage(t *testing.T)             // MessagesDialogs (not slice) -> NextCursor nil; slice with fewer than Limit -> nil
func TestListChats_InvalidCursor(t *testing.T)        // Cursor "zzz" -> errors.Is(err, ErrInvalidCursor), no RPC made (Review Focus 5)
func TestListChats_PersistsPeers(t *testing.T)        // after call, a new PeerCache loaded from the same path resolves private 7 with access hash
func TestListChats_DefaultLimit(t *testing.T)         // Limit 0 -> request Limit 50
```

- [ ] **Step 3: Run tests, expect failure**

Run: `go test ./internal/tg/ -run 'Cursor|ListChats' -v`
Expected: FAIL, undefined `NewService`.

- [ ] **Step 4: Implement `cursor.go`, `service.go`, `dialogs.go`**

`NewService` calls `tgapi.NewClient(inv)` to get `*tgapi.Client`.

- [ ] **Step 5: Run tests, expect pass**

Run: `go test ./internal/tg/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tg
git commit -m "feat(tg): list_chats over messages.getDialogs with opaque cursor"
```

---

### Task 5: `ReadChat` with paging and `since_date`

**Files:**
- Create: `internal/tg/history.go`
- Test: `internal/tg/history_test.go`

**Interfaces:**
- Consumes: `Service`, `PeerCache.Get`, `RenderMessage`, `NewNames`.
- Produces:
  ```go
  func (s *Service) ReadChat(ctx context.Context, p ReadChatParams) (*ReadChatResult, error)
  func (s *Service) resolvePeer(ctx context.Context, t ChatType, id int64) (tgapi.InputPeerClass, error) // Task 6 adds the fallback; here it only consults the cache and returns ErrUnknownChat
  ```
  Behavior of `ReadChat`:
  - `Limit` 0 → `DefaultLimit`.
  - Request: `MessagesGetHistoryRequest{Peer: resolved, OffsetID: p.BeforeID, Limit: p.Limit}`; other fields zero.
  - Accept `MessagesMessages`, `MessagesMessagesSlice`, `MessagesChannelMessages`; `MessagesMessagesNotModified` → empty result. Feed users/chats to `peers.PutFromResponse` (no Save needed here).
  - `peerTitle` for `RenderMessage`: the name of the requested peer from the response's users/chats via `Names.PeerName`; empty string if absent.
  - Iterate messages in response order (newest first). Skip `MessageEmpty`. If `p.SinceDate > 0` and message date `< SinceDate`: stop, set `cutOff = true`.
  - `NextBeforeID`: nil if `cutOff`, or no messages were rendered, or the RPC returned fewer than `p.Limit` message entries (counting `MessageEmpty`); else the ID of the oldest rendered message.
  - Does not consult `since_date` when it is `0`.

- [ ] **Step 1: Write failing tests**

```go
func TestReadChat_RendersPage(t *testing.T)
// cache has private 7 hash 9; mock expects MessagesGetHistoryRequest{Peer InputPeerUser{7,9}, OffsetID 0, Limit 50}
// returns MessagesMessagesSlice with Message{ID 30, Date 300, Out:true, "hi"}, Message{ID 20, Date 200, FromID PeerUser 7, "yo"}, users: User 7 "Ann"
// expect messages [{30,300,"me","hi"},{20,200,"Ann","yo"}], NextBeforeID nil (fewer than limit)
func TestReadChat_NextBeforeID(t *testing.T)        // Limit 2, two messages 30 and 20 -> NextBeforeID == 20; second call with BeforeID 20 -> request OffsetID 20
func TestReadChat_SinceDateCutsPage(t *testing.T)   // SinceDate 250, messages dates 300,200 -> one message, NextBeforeID nil
func TestReadChat_SinceDateDropsAll(t *testing.T)   // SinceDate 999 -> Messages empty (not nil), NextBeforeID nil (Review Focus 2)
func TestReadChat_SkipsMessageEmpty(t *testing.T)   // Limit 2, [Message 30, MessageEmpty 25] -> 1 message, NextBeforeID == 30 (Review Focus 3)
func TestReadChat_ChannelPeerTitle(t *testing.T)    // channel 3 hash 1, message FromID nil, chats: Channel 3 "News" -> From "News"
func TestReadChat_DeletedSender(t *testing.T)       // FromID PeerUser 8, users: UserEmpty 8 -> From "Deleted Account" (Review Focus 4)
func TestReadChat_UnknownPeer(t *testing.T)         // empty cache -> errors.Is(err, ErrUnknownChat) (Task 6 replaces this test's mock expectations)
func TestReadChat_ServiceMessage(t *testing.T)      // MessageService PinMessage -> Text "[service: message pinned]"
```

- [ ] **Step 2: Run tests, expect failure**

Run: `go test ./internal/tg/ -run ReadChat -v`
Expected: FAIL, undefined `ReadChat`.

- [ ] **Step 3: Implement `history.go`**

- [ ] **Step 4: Run tests, expect pass**

Run: `go test ./internal/tg/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tg/history.go internal/tg/history_test.go
git commit -m "feat(tg): read_chat over messages.getHistory with paging and since_date"
```

---

### Task 6: Peer resolution fallback by scanning dialogs

**Files:**
- Modify: `internal/tg/history.go` (`resolvePeer`)
- Test: `internal/tg/history_test.go`

**Interfaces:**
- Consumes: `ListChats` request building from Task 4. Extract from `dialogs.go` a helper `func (s *Service) fetchDialogs(ctx, folderID int, c *cursor, limit int) (dialogsPage, error)` where `dialogsPage` holds `dialogs []*tgapi.Dialog, users, chats, messages, complete bool`, and make `ListChats` use it.
- Produces: `resolvePeer` behavior:
  1. `peers.Get` hit → return it.
  2. Otherwise scan folder `0` then folder `1` with `fetchDialogs(limit 100)`, paging with the same cursor logic as `ListChats`, calling `peers.PutFromResponse` after each page, and stop as soon as `peers.Get` hits. At most `20` pages per folder (designer's cap against runaway accounts).
  3. After both folders: `peers.Save()`; if still missing → `ErrUnknownChat`.

- [ ] **Step 1: Write failing tests**

```go
func TestResolvePeer_ScansMainFolder(t *testing.T)
// empty cache; mock expects getDialogs(FolderID 0, Limit 100) -> MessagesDialogs with User 7 hash 9; then getHistory with InputPeerUser{7,9}
func TestResolvePeer_ScansArchiveAfterMain(t *testing.T)
// getDialogs(folder 0) -> complete, no user 7; getDialogs(folder 1) -> has user 7; then getHistory succeeds
func TestResolvePeer_NotFoundAnywhere(t *testing.T)
// both folders complete and empty -> errors.Is(err, ErrUnknownChat); no getHistory call
func TestResolvePeer_PagesUntilFound(t *testing.T)
// folder 0: first response is a MessagesDialogsSlice with exactly 100 dialogs built in a loop (PeerUser 1000..1099, matching users, matching top messages) and no user 7;
// second getDialogs call must carry OffsetDate/OffsetID/OffsetPeer of dialog 1099 and returns MessagesDialogs with user 7 hash 9; then getHistory with InputPeerUser{7,9}
```

Update `TestReadChat_UnknownPeer` from Task 5 to expect the two `getDialogs` calls (both returning empty `MessagesDialogs`).

- [ ] **Step 2: Run tests, expect failure**

Run: `go test ./internal/tg/ -run 'ResolvePeer|UnknownPeer' -v`
Expected: FAIL (unexpected RPC or `ErrUnknownChat` where a result is expected).

- [ ] **Step 3: Implement fallback and the `fetchDialogs` refactor**

- [ ] **Step 4: Run all tg tests, expect pass**

Run: `go test ./internal/tg/ -v`
Expected: PASS, including the Task 4 tests after the refactor.

- [ ] **Step 5: Commit**

```bash
git add internal/tg
git commit -m "feat(tg): resolve unknown peers by scanning main and archive folders"
```

---

### Task 7: MCP server and tools

**Files:**
- Create: `internal/mcpserver/server.go`
- Test: `internal/mcpserver/server_test.go`

**Interfaces:**
- Consumes: `tg.ListChatsParams`, `tg.ListChatsResult`, `tg.ReadChatParams`, `tg.ReadChatResult`, enums and limits from Task 2.
- Produces:
  ```go
  package mcpserver
  type Telegram interface {
      ListChats(ctx context.Context, p tg.ListChatsParams) (*tg.ListChatsResult, error)
      ReadChat(ctx context.Context, p tg.ReadChatParams) (*tg.ReadChatResult, error)
  }
  const Version = "0.1.0"
  func New(t Telegram) *mcp.Server   // Implementation{Name: "telegram-mcp", Version: Version}

  type ListChatsInput struct {
      Folder string `json:"folder,omitempty" jsonschema:"main or archive; default main"`
      Limit  int    `json:"limit,omitempty"  jsonschema:"page size 1-100; default 50"`
      Cursor string `json:"cursor,omitempty" jsonschema:"opaque cursor from a previous call"`
  }
  type ReadChatInput struct {
      ChatID    int64  `json:"chat_id"             jsonschema:"id from list_chats"`
      ChatType  string `json:"chat_type"           jsonschema:"private, group, supergroup or channel"`
      Limit     int    `json:"limit,omitempty"     jsonschema:"page size 1-100; default 50"`
      BeforeID  int    `json:"before_id,omitempty" jsonschema:"only messages with id lower than this"`
      SinceDate int64  `json:"since_date,omitempty" jsonschema:"unix seconds; return nothing older"`
  }
  ```
  Tools are registered with `mcp.AddTool` using `ToolHandlerFor[ListChatsInput, *tg.ListChatsResult]` and `ToolHandlerFor[ReadChatInput, *tg.ReadChatResult]`. Both tools set `Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}`.
  Validation (returned as `error` so the SDK marks `IsError`): folder not in {"", main, archive} → `folder must be main or archive`; limit < 0 or > 100 → `limit must be between 1 and 100`; chat_type not one of the four → `chat_type must be one of private, group, supergroup, channel`; `chat_id == 0` → `chat_id is required`. Empty folder → `main`; limit `0` → `50`.
  Tool descriptions (exact copy):
  - `list_chats`: `List the user's Telegram chats (private chats, groups, supergroups, channels), newest activity first. Use the returned cursor to fetch the next page. Pass folder=archive to list archived chats.`
  - `read_chat`: `Read plain-text message history of one chat, newest first. Pass before_id=next_before_id from the previous call to page backwards; stop when next_before_id is null.`

- [ ] **Step 1: Write failing tests with the in-memory transport**

Test helper: `fakeTelegram` records the last params and returns canned results or an error. `newSession(t, fake)` does `srv := New(fake)`; `ct, st := mcp.NewInMemoryTransports()`; `go srv.Run(ctx, st)`; `cs, _ := mcp.NewClient(&mcp.Implementation{Name:"test",Version:"0"}, nil).Connect(ctx, ct, nil)`; returns `cs`.

```go
func TestListTools(t *testing.T)                 // cs.ListTools -> names {"list_chats","read_chat"}, both annotations.readOnlyHint true
func TestListChats_DefaultsAndPassthrough(t *testing.T)
// CallTool("list_chats", {}) -> fake received Folder main, Limit 50, Cursor ""
// result.IsError false; result.StructuredContent (or Content[0] text JSON) decodes to the canned ListChatsResult
func TestListChats_ArchiveAndCursor(t *testing.T) // {"folder":"archive","limit":10,"cursor":"abc"} passes through
func TestListChats_InvalidFolder(t *testing.T)   // {"folder":"spam"} -> IsError true, text contains "folder must be main or archive"; fake not called
func TestListChats_LimitTooLarge(t *testing.T)   // {"limit":101} -> IsError, "limit must be between 1 and 100"
func TestReadChat_Passthrough(t *testing.T)      // {"chat_id":7,"chat_type":"private","before_id":20,"since_date":5} -> params match, Limit 50
func TestReadChat_InvalidType(t *testing.T)      // {"chat_id":7,"chat_type":"dm"} -> IsError, message text
func TestReadChat_MissingChatID(t *testing.T)    // {"chat_type":"private"} -> IsError, "chat_id is required"
func TestReadChat_BackendError(t *testing.T)     // fake returns errors.New("boom") -> IsError true, text contains "boom"
func TestReadChat_NullNextBeforeID(t *testing.T) // fake returns NextBeforeID nil -> JSON text contains `"next_before_id":null`
```

- [ ] **Step 2: Run tests, expect failure**

Run: `go get github.com/modelcontextprotocol/go-sdk@v1.8.0 && go test ./internal/mcpserver/ -v`
Expected: FAIL, undefined `New`.

- [ ] **Step 3: Implement `server.go`**

- [ ] **Step 4: Run tests, expect pass**

Run: `go test ./internal/mcpserver/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mcpserver go.mod go.sum
git commit -m "feat(mcp): list_chats and read_chat tools with input validation"
```

---

### Task 8: gotd client construction and `serve`

**Files:**
- Create: `internal/tg/client.go`, `cmd/telegram-mcp/serve.go`
- Modify: `cmd/telegram-mcp/main.go` (replace `runServe` stub)
- Test: `cmd/telegram-mcp/serve_test.go`

**Interfaces:**
- Consumes: `config.Config`, `tg.NewService`, `tg.NewPeerCache`, `mcpserver.New`.
- Produces:
  ```go
  // internal/tg/client.go
  func NewClient(cfg ClientConfig) *telegram.Client
  type ClientConfig struct { APIID int; APIHash string; SessionPath string; Logger *slog.Logger }
  ```
  `NewClient` uses `telegram.Options{SessionStorage: &session.FileStorage{Path: cfg.SessionPath}, Middlewares: []telegram.Middleware{floodwait.NewSimpleWaiter().WithMaxWait(60 * time.Second)}, NoUpdates: true}`. Session directory is created with `0700` before the client is built.

  ```go
  // cmd/telegram-mcp/serve.go
  func runServe(args []string, stderr io.Writer) int
  ```
  Behavior:
  1. Parse `--transport` (default `stdio`); any other value → stderr `unsupported transport %q; only stdio is implemented`, return `2`.
  2. `config.Load(os.Getenv, home)`; error → stderr, return `2`.
  3. If the session file does not exist → stderr `no session at <path>; run "telegram-mcp login" first`, return `3`. (Checked before any network use.)
  4. Build client, `client.Run(ctx, func(ctx) error { status := client.Auth().Status(ctx); if !status.Authorized → return error "session is not authorized; run telegram-mcp login"; peers := tg.NewPeerCache(cfg.PeersPath()); peers.Load(); svc := tg.NewService(client, peers) /* *telegram.Client implements tgapi.Invoker; *tgapi.Client does not */; return mcpserver.New(svc).Run(ctx, &mcp.StdioTransport{}) })`. Any error → stderr, return `1`.
  5. `ctx` is cancelled on SIGINT/SIGTERM via `signal.NotifyContext`.

  To keep steps 1–3 testable without network, split: `func serveConfig(args []string, getenv func(string) string, home string, stat func(string) (os.FileInfo, error)) (config.Config, string /*transport*/, error)` and have `runServe` call it then the network part.

- [ ] **Step 1: Write failing tests for `serveConfig`**

```go
func TestServeConfig_BadTransport(t *testing.T)   // ["--transport","http"] -> err contains "only stdio"
func TestServeConfig_MissingSession(t *testing.T) // stat returns os.ErrNotExist -> err contains `run "telegram-mcp login"` and the path
func TestServeConfig_OK(t *testing.T)             // stat ok -> cfg returned, transport "stdio"
func TestRunServe_ExitCodes(t *testing.T)         // runServe with bad transport returns 2; with missing session returns 3 (use env TELEGRAM_SESSION_PATH to a nonexistent temp path)
```

- [ ] **Step 2: Run tests, expect failure**

Run: `go test ./cmd/telegram-mcp/ -v`
Expected: FAIL, undefined `serveConfig`.

- [ ] **Step 3: Implement `client.go`, `serve.go`, wire `runServe`**

Run: `go get github.com/gotd/contrib@v0.25.0`

- [ ] **Step 4: Run tests and build, expect pass**

Run: `go test ./... && go build -o telegram-mcp ./cmd/telegram-mcp && TELEGRAM_API_ID=1 TELEGRAM_API_HASH=x TELEGRAM_SESSION_PATH=/nonexistent/s.json ./telegram-mcp serve; echo "exit=$?"`
Expected: tests PASS; the run prints the `run "telegram-mcp login"` message and `exit=3`.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat: serve command wiring gotd client, flood-wait middleware and stdio MCP"
```

---

### Task 9: `login` and `logout`

**Files:**
- Create: `cmd/telegram-mcp/login.go`
- Modify: `cmd/telegram-mcp/main.go` (replace stubs)
- Test: `cmd/telegram-mcp/login_test.go`

**Interfaces:**
- Consumes: `tg.NewClient`, `config.Load`.
- Produces:
  ```go
  type termAuth struct { in *bufio.Reader; out io.Writer; readPassword func() (string, error) }
  func (a termAuth) Phone(ctx) (string, error)        // prompt "Phone number (international format): "
  func (a termAuth) Code(ctx, *tgapi.AuthSentCode) (string, error) // prompt "Code: "
  func (a termAuth) Password(ctx) (string, error)     // prompt "2FA password: ", uses readPassword (term.ReadPassword on os.Stdin in production)
  func (a termAuth) AcceptTermsOfService(ctx, tos) error { return nil }
  func (a termAuth) SignUp(ctx) (auth.UserInfo, error)  // returns error "sign up is not supported; create the account in a Telegram app first"
  func runLogin(stdout, stderr io.Writer) int
  func runLogout(stdout, stderr io.Writer) int
  ```
  `runLogin`: load config; build client; `client.Run(ctx, func(ctx) error { if err := client.Auth().IfNecessary(ctx, auth.NewFlow(termAuth{...}, auth.SendCodeOptions{})); err != nil { return err }; self, _ := client.Self(ctx); fmt.Fprintf(stdout, "Logged in as %s\n", tg.UserName(self)); return nil })`. Exit `0` on success, `1` otherwise. gotd's `FileStorage` already writes the session with mode `0600`.
  `runLogout`: load config; if session file missing → print `no session` and return `0`; build client; `client.Run(... client.API().AuthLogOut(ctx) ...)`, log any RPC error to stderr but continue; remove session file and peers file; return `0`.

- [ ] **Step 1: Write failing tests for `termAuth`**

```go
func TestTermAuth_PhoneAndCode(t *testing.T)   // in = "+420123\n12345\n"; Phone -> "+420123"; Code -> "12345"; out contains both prompts; whitespace trimmed
func TestTermAuth_Password(t *testing.T)       // readPassword returns "s3cret" -> Password == "s3cret"
func TestTermAuth_SignUpRefused(t *testing.T)  // SignUp returns error
```

- [ ] **Step 2: Run tests, expect failure**

Run: `go test ./cmd/telegram-mcp/ -run TermAuth -v`
Expected: FAIL, undefined `termAuth`.

- [ ] **Step 3: Implement `login.go`, wire `runLogin` and `runLogout`**

Run: `go get golang.org/x/term@latest`

- [ ] **Step 4: Run tests and build**

Run: `go test ./... && go vet ./... && go build ./...`
Expected: PASS, no vet findings.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat: interactive login and logout commands"
```

---

### Task 10: README and manual smoke test

**Files:**
- Create: `README.md`

- [ ] **Step 1: Write README**

Sections, in this order: what it is (two tools, read-only, user account over MTProto); requirements (Go 1.25, `api_id`/`api_hash` from my.telegram.org); build (`go build -o telegram-mcp ./cmd/telegram-mcp`); env vars; `login` once; Claude Code config snippet:

```json
{
  "mcpServers": {
    "telegram": {
      "command": "/absolute/path/to/telegram-mcp",
      "args": ["serve"],
      "env": { "TELEGRAM_API_ID": "…", "TELEGRAM_API_HASH": "…" }
    }
  }
}
```

tool reference (inputs/outputs copied from the spec); paging example (call `read_chat`, pass `next_before_id` back until null); files written (`session.json`, `peers.json`, mode 0600) and the single-instance assumption; `logout`.

- [ ] **Step 2: Manual smoke test (owner runs; builder documents the checklist in README under "Smoke test")**

1. `TELEGRAM_API_ID=… TELEGRAM_API_HASH=… ./telegram-mcp login` → prints `Logged in as <name>`; `ls -l ~/.config/telegram-mcp/session.json` shows `-rw-------`.
2. `claude mcp add telegram -- /abs/path/telegram-mcp serve` (with env), then in Claude Code: call `list_chats` → chats appear; call `list_chats` with `folder=archive`.
3. Call `read_chat` for one private chat; call again with `before_id=next_before_id`; messages continue older.
4. `./telegram-mcp serve` with `TELEGRAM_SESSION_PATH=/tmp/none.json` → exit 3 with the login hint.

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs: README with setup, tool reference and smoke test"
```
