# `download_media` tool — Design

Date: 2026-11-13
Status: approved

## Intent

The owner wants the raw content of message attachments, voice messages
first. Telegram's own transcription is Premium-only and of poor quality, so
the owner processes the raw files elsewhere. The tool saves the file to
local disk and returns its path. File bytes never go through MCP.

## Goals

- Download the media of one message: voice, audio, video, video note,
  photo, document, sticker, gif.
- Save it to a local directory and return path and metadata.
- Read-only for the Telegram account.

## Non-goals

- Transcription.
- Inline or base64 content in the tool result.
- Downloading a whole album in one call. Each album item is its own message.
- Profile photos, chat photos, stories.

## Anti-goals

- Pulling a huge file by accident. A size limit is checked before download.
- Writing outside the download directory. File names are built from ids,
  never from user-controlled names.

## Approaches

1. **Generic `download_media` for any attachment (chosen).** Voice is a
   `Document` with a voice attribute, so one code path covers all media.
2. Voice-only tool. Same code, less useful. Rejected.
3. Return content inline (base64 / MCP audio content). Costs many tokens and
   some clients reject it. Rejected by the owner.

## MCP tool

### `download_media`

Annotations: `ReadOnlyHint: true`.

Description (for the model): "Download the attachment of one message
(voice, audio, video, photo, document, sticker) to a local file and return
its path. Pass chat_id and chat_type from list_chats and message_id from
read_chat. The file content is not returned."

Input:

```go
type DownloadMediaInput struct {
    ChatID    int64  `json:"chat_id"    jsonschema:"id from list_chats"`
    ChatType  string `json:"chat_type"  jsonschema:"private, group, supergroup or channel"`
    MessageID int    `json:"message_id" jsonschema:"message id from read_chat"`
}
```

Validation (in `mcpserver`): `chat_id` non-zero, valid `chat_type`,
`message_id` > 0.

Output:

```go
type DownloadMediaResult struct {
    Path     string `json:"path"`                // absolute
    Kind     string `json:"kind"`                // voice, audio, video, video_note, photo, document, sticker, gif
    MimeType string `json:"mime_type"`
    Size     int64  `json:"size"`                // bytes
    FileName string `json:"file_name,omitempty"` // original name, if the document has one
    Duration int    `json:"duration,omitempty"`  // seconds, for voice/audio/video
}
```

## Behaviour

1. Resolve the peer with the existing `resolvePeer`.
2. Fetch the message by id: `channels.getMessages` for supergroups and
   channels, `messages.getMessages` otherwise. This gives a fresh file
   reference on every call.
3. Pick the file location:
   - `MessageMediaPhoto` → `InputPhotoFileLocation` with the largest size
     (`PhotoSize`, `PhotoSizeProgressive`; skip stripped/cached thumbs).
   - `MessageMediaDocument` → `InputDocumentFileLocation`, empty thumb size.
4. Check the size against the limit before downloading.
5. Build the file name `<chat_id>_<message_id>.<ext>`. Extension: from the
   document file name if present, else from the MIME type (voice
   `audio/ogg` → `.ogg`, photo → `.jpg`), else `.bin`.
6. If the file already exists, return it without downloading.
7. Otherwise download with `gotd/td/telegram/downloader` to a temp file in
   the same directory, then rename. A failed download leaves no partial file.
8. `kind` uses the same classification as `read_chat` placeholders.

Errors:

- message not found
- message has no media
- media cannot be downloaded (location, contact, poll, web page, …)
- file larger than the limit (error names size and limit)

## Config

- `TELEGRAM_MCP_DOWNLOAD_DIR`: absolute or relative path. Default
  `downloads/` next to the session file. Created on first download.
- `TELEGRAM_MCP_MAX_DOWNLOAD_MB`: integer, default 100.

Defaults for directory and limit are Claude's calls; the owner did not
object.

## Left to the builder

- Exact MIME → extension table.
- How to share the media classification with `render.go`.
