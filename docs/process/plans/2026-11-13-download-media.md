# `download_media` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an MCP tool `download_media` that saves the attachment of one message to a local file and returns its path and metadata.

**Architecture:** Config gets a download dir and a size limit. `internal/tg` gets a pure function that turns `MessageMediaClass` into a file spec (location, kind, mime, size, name, duration, extension), and a `Service.DownloadMedia` method that fetches the message, checks size, and streams the file with `gotd/td/telegram/downloader` into a temp file that is renamed on success. `mcpserver` validates input and calls it.

**Tech Stack:** Go 1.25, `github.com/gotd/td` v0.163.0 (`tg`, `telegram/downloader`, `tgmock`), `github.com/modelcontextprotocol/go-sdk` v1.8.0.

**Spec:** `docs/process/specs/2026-11-13-download-media-design.md`

## Global Constraints

- Env `TELEGRAM_MCP_DOWNLOAD_DIR`; default `<dir of session file>/downloads`; relative paths made absolute.
- Env `TELEGRAM_MCP_MAX_DOWNLOAD_MB`; integer > 0; default `100`. Limit in bytes = MB × 1024 × 1024.
- File name is always `<chat_id>_<message_id>.<ext>`. Never use the document's own file name in the path.
- `kind` values: `voice`, `audio`, `video`, `video_note`, `photo`, `document`, `sticker`, `gif`. Must match `read_chat` placeholders.
- No file bytes in the tool result. Tool is `ReadOnlyHint: true`.
- DC migration (`FILE_MIGRATE`) is already handled by `telegram.Client.Invoke` (`gotd/td/telegram/invoke.go:70`). No code needed.
- Downloader verify stays off (the default). Use `Stream` (single thread) so tgmock sees requests in order.

## Review Focus

1. **Two calls for the same message at once.** Expected: both succeed and the final file is whole, because each call writes its own temp file and `rename` is atomic. Covered by the temp-file design in Task 3; no extra test.
2. **Interrupted download leaves a partial file that the "already exists" check then returns.** Expected: no file at the final path after a failed download. Test in Task 3.
3. **Message id that does not exist** returns `MessageEmpty`. Expected: error "message not found", not a panic. Test in Task 3.
4. **Photo whose sizes list holds only stripped/cached thumbs.** Expected: error "media cannot be downloaded". Test in Task 2.
5. **Document with a file name containing `/` or `..`.** Expected: the extension is taken only if it is a short plain suffix (`^\.[A-Za-z0-9]{1,10}$`), else fall back to MIME. Test in Task 2.

---

### Task 1: Config

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces: `Config.DownloadDir string` (absolute), `Config.MaxDownloadBytes int64`.

- [ ] **Step 1: Write failing tests**

```go
func TestLoad_DownloadDefaults(t *testing.T)    // no env → DownloadDir == filepath.Join(filepath.Dir(SessionPath), "downloads"); MaxDownloadBytes == 100<<20
func TestLoad_DownloadDirFromEnv(t *testing.T)  // "/x/dl" → "/x/dl"; "rel/dl" → filepath.IsAbs(result)
func TestLoad_MaxDownloadMB(t *testing.T)       // "5" → 5<<20; "abc", "0", "-1" → error mentioning TELEGRAM_MCP_MAX_DOWNLOAD_MB
```

Use the same `getenv` map style as existing tests in the file.

- [ ] **Step 2: Run** `go test ./internal/config/` — expect FAIL (unknown fields).
- [ ] **Step 3: Implement** in `Load`. Keep `Load`'s signature.
- [ ] **Step 4: Run** `go test ./internal/config/` — expect PASS.
- [ ] **Step 5: Commit** `feat(config): download dir and size limit`

---

### Task 2: Media → file spec (pure)

**Files:**
- Create: `internal/tg/media.go`
- Modify: `internal/tg/render.go` (make `documentPlaceholder` use the shared classifier)
- Test: `internal/tg/media_test.go`; existing `internal/tg/render_test.go` must stay green

**Interfaces:**
- Produces:

```go
var ErrNoMedia = errors.New("message has no media")
var ErrNotDownloadable = errors.New("media cannot be downloaded")

type mediaFile struct {
    Location tgapi.InputFileLocationClass
    Kind     string
    MimeType string
    Size     int64
    FileName string // original document name, "" if none
    Duration int    // seconds; 0 if unknown
    Ext      string // with leading dot
}

func mediaFileOf(media tgapi.MessageMediaClass) (mediaFile, error)

// documentKind classifies a document; render.go uses it for placeholders.
func documentKind(doc *tgapi.Document) (kind, fileName string, duration int)
```

`documentKind` returns `document` when no special attribute is set. `documentPlaceholder` keeps its exact current output (`[voice]`, `[document name.pdf]`, …).

- [ ] **Step 1: Write failing tests** (table test `TestMediaFileOf`):

| case | input | expect |
|---|---|---|
| voice | Document{ID:1, AccessHash:2, FileReference:[]byte{3}, MimeType:"audio/ogg", Size:1234, Attributes:[Audio{Voice:true, Duration:7}]} | Kind `voice`, Ext `.ogg`, Size 1234, Duration 7, Location `*InputDocumentFileLocation{ID:1, AccessHash:2, FileReference:{3}, ThumbSize:""}` |
| named pdf | Document{MimeType:"application/pdf", Attributes:[Filename{"a.PDF"}]} | Kind `document`, FileName `a.PDF`, Ext `.PDF` |
| bad name | Filename{"../../etc"} , MimeType "application/octet-stream" | Ext `.bin` |
| video note | Video{RoundMessage:true, Duration:3}, "video/mp4" | Kind `video_note`, Ext `.mp4`, Duration 3 |
| sticker | Sticker attr, "image/webp" | Kind `sticker`, Ext `.webp` |
| photo | Photo{ID:5, AccessHash:6, FileReference:{7}, Sizes:[PhotoStrippedSize{Type:"i"}, PhotoSize{Type:"m",W:320,Size:100}, PhotoSizeProgressive{Type:"y",W:1280,Sizes:[10,20,300]}]} | Kind `photo`, MimeType `image/jpeg`, Ext `.jpg`, Size 300, Location `*InputPhotoFileLocation{ID:5, AccessHash:6, FileReference:{7}, ThumbSize:"y"}` |
| photo only thumbs | Sizes:[PhotoStrippedSize, PhotoCachedSize] | `ErrNotDownloadable` |
| nil media | nil | `ErrNoMedia` |
| geo / poll / contact / webpage | | `ErrNotDownloadable` |
| DocumentEmpty / PhotoEmpty | | `ErrNotDownloadable` |

Largest photo size = largest `W*H`; `PhotoSizeProgressive` size = last of `Sizes`.

- [ ] **Step 2: Run** `go test ./internal/tg/ -run 'TestMediaFileOf|TestRender'` — expect FAIL (undefined).
- [ ] **Step 3: Implement** `media.go`. MIME → ext: use `mime.ExtensionsByType` only as a fallback after a small fixed table: `audio/ogg→.ogg`, `audio/mpeg→.mp3`, `audio/mp4→.m4a`, `video/mp4→.mp4`, `image/jpeg→.jpg`, `image/png→.png`, `image/webp→.webp`, `application/x-tgsticker→.tgs`, `video/webm→.webm`, `application/pdf→.pdf`; else `.bin`. Rewrite `documentPlaceholder` on top of `documentKind`.
- [ ] **Step 4: Run** `go test ./internal/tg/` — expect PASS (including all existing render tests).
- [ ] **Step 5: Commit** `feat(tg): classify downloadable media`

---

### Task 3: `Service.DownloadMedia`

**Files:**
- Create: `internal/tg/download.go`
- Modify: `internal/tg/service.go`, `internal/tg/types.go`
- Test: `internal/tg/download_test.go`

**Interfaces:**
- Consumes: `mediaFileOf`, `ErrNoMedia`, `ErrNotDownloadable` (Task 2); `resolvePeer` (`history.go`).
- Produces:

```go
// types.go
type DownloadMediaParams struct {
    ChatID    int64
    ChatType  ChatType
    MessageID int
}
type DownloadMediaResult struct {
    Path     string `json:"path"`
    Kind     string `json:"kind"`
    MimeType string `json:"mime_type"`
    Size     int64  `json:"size"`
    FileName string `json:"file_name,omitempty"`
    Duration int    `json:"duration,omitempty"`
}

// service.go: new fields downloadDir string, maxDownload int64
func (s *Service) SetDownloads(dir string, maxBytes int64)

// download.go
var ErrMessageNotFound = errors.New("message not found")
func (s *Service) DownloadMedia(ctx context.Context, p DownloadMediaParams) (*DownloadMediaResult, error)
```

Flow: resolve peer → fetch message (`ChannelsGetMessages` with `InputChannel` built from the `InputPeerChannel` for supergroup/channel; `MessagesGetMessages` otherwise; ids `[]InputMessageClass{&InputMessageID{ID}}`) → put users/chats in peer cache like `ReadChat` → first `*tgapi.Message` with matching ID, else `ErrMessageNotFound` → `mediaFileOf` → if `Size > maxDownload` error `"file is %d bytes, limit is %d"` → `MkdirAll(dir, 0o700)` → final path `filepath.Join(dir, fmt.Sprintf("%d_%d%s", chatID, msgID, ext))`; if it exists return it → `os.CreateTemp(dir, ".dl-*")`, `downloader.NewDownloader().Download(s.api, loc).Stream(ctx, f)`, close, rename; on any error remove the temp file. `Size` in the result is the size on disk.

- [ ] **Step 1: Write failing tests** (use `newTestService`, `t.TempDir()` for the dir, `SetDownloads(dir, 1<<20)`):

```go
TestDownloadMedia_PrivateVoice     // expect MessagesGetMessagesRequest{ID:[InputMessageID{10}]} → MessagesMessages{Messages:[Message{ID:10, Media:voice doc}]};
                                   // expect UploadGetFileRequest with InputDocumentFileLocation{ID:1} → UploadFile{Bytes:"abc"};
                                   // result Path == dir+"/7_10.ogg", Kind "voice", Size 3; file content "abc"
TestDownloadMedia_ChannelUsesChannelsGetMessages // peers.Put(ChatChannel,5,11); request is ChannelsGetMessagesRequest with InputChannel{5,11}
TestDownloadMedia_ExistingFileSkipsDownload      // pre-create 7_10.ogg; only the getMessages call is expected
TestDownloadMedia_TooLarge                        // Document Size 2<<20 with limit 1<<20 → error contains "limit"; no upload call; no file in dir
TestDownloadMedia_MessageNotFound                 // response holds MessageEmpty{ID:10} → errors.Is(err, ErrMessageNotFound)
TestDownloadMedia_NoMedia                         // Message without media → errors.Is(err, ErrNoMedia)
TestDownloadMedia_FailedDownloadLeavesNoFile      // upload.getFile returns RPC error → err != nil; os.ReadDir(dir) is empty
```

- [ ] **Step 2: Run** `go test ./internal/tg/ -run TestDownloadMedia` — expect FAIL.
- [ ] **Step 3: Implement** `download.go`, fields and `SetDownloads`.
- [ ] **Step 4: Run** `go test ./internal/tg/` — expect PASS.
- [ ] **Step 5: Commit** `feat(tg): download message media to disk`

---

### Task 4: MCP tool and wiring

**Files:**
- Modify: `internal/mcpserver/server.go`, `cmd/telegram-mcp/serve.go`, `README.md`
- Test: `internal/mcpserver/server_test.go`

**Interfaces:**
- Consumes: `tg.DownloadMediaParams`, `tg.DownloadMediaResult`, `Service.DownloadMedia`, `Service.SetDownloads` (Task 3); `Config.DownloadDir`, `Config.MaxDownloadBytes` (Task 1).
- Produces: `Telegram` interface gains `DownloadMedia(ctx, tg.DownloadMediaParams) (*tg.DownloadMediaResult, error)`.

Input type and description copy exactly from the spec section "MCP tool". Validation errors: `"chat_id is required"`, the existing `parseChatType` error, `"message_id must be positive"`.

- [ ] **Step 1: Write failing tests** following the existing handler tests in `server_test.go` (extend the fake `Telegram`):
  - `TestDownloadMedia_Validation`: chat_id 0, bad chat_type, message_id 0 and -1 → the errors above; fake not called.
  - `TestDownloadMedia_PassesParams`: valid input → fake receives `{ChatID, ChatType, MessageID}`; structured result equals fake's result.
  - Tool list test (if one exists) includes `download_media` with `ReadOnlyHint: true`.
- [ ] **Step 2: Run** `go test ./internal/mcpserver/` — expect FAIL.
- [ ] **Step 3: Implement** handler and registration. In `serve.go`, call `svc.SetDownloads(cfg.DownloadDir, cfg.MaxDownloadBytes)` after `NewService`. Add the tool and both env vars to `README.md`.
- [ ] **Step 4: Run** `go vet ./... && go test ./...` — expect PASS.
- [ ] **Step 5: Commit** `feat: download_media MCP tool`

---

### Task 5: Manual check (owner)

- [ ] Rebuild, restart the MCP server, `read_chat` a chat with a voice message, call `download_media` on its id. Expected: an `.ogg` file at the returned path that plays. Repeat for one photo and one PDF.
