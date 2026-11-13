# `send_file` tool — Design

Date: 2026-11-13
Status: approved

## Intent

The owner runs automations that produce files on disk: audio, voice
messages, photos, documents, videos and round videos. An agent, a script or a
person creates them. The automation must send these files to Telegram
chats as the owner's user account, the same way `send_message` sends text.

The tool takes a local file path. File bytes never go through MCP. This
mirrors `download_media`, which writes a file and returns its path.

## Goals

- Send one local file to one chat, as one of six kinds: `photo`,
  `document`, `audio`, `voice`, `video`, `video_note`.
- The message looks the same as one sent from the Telegram app: a voice
  message shows as a voice bubble, a video note shows as a circle.
- Optional Markdown caption and optional reply.
- Deterministic rendering: the same file and `kind` always produce the
  same kind of message. The automation depends on this.

## Non-goals

- Converting formats. The server never runs ffmpeg or any other tool.
- Albums (several files in one message).
- Stickers, GIFs, sending by URL, sending files by Telegram file id.
- Upload progress reporting.
- Audio title and performer fields.
- Idempotent sends. Each call uses a fresh random id, like
  `send_message`. If the automation retries after a timeout, the chat can
  get the file twice. Claude's call; dedup by input hash can be added
  later if duplicates become a problem.

## Anti-goals

- **Leaking local secrets.** The model reads chat messages written by
  other people. A message can tell the model to send `~/.ssh/id_rsa`. The
  tool must refuse every path outside the allowed folders.
- **Silent wrong results.** A file in the wrong format must fail with a
  clear error. It must never arrive as a broken voice bubble or as a plain
  file attachment instead of the requested kind.
- **New runtime dependencies.** No external programs, no cgo.

## Approaches

1. **One tool `send_file` with a required `kind` (chosen by the owner).**
   One place for the path check, upload and errors. An explicit `kind`
   gives a deterministic result. Guessing from the extension cannot do
   this: an `.ogg` file can be `voice` or `audio`.
2. Six tools, one per kind. Narrower schemas, but six tool descriptions
   in every session and almost all code shared. Rejected.
3. A `file` field on `send_message`. One send tool, but mixed rules:
   `text` becomes optional, and a caption is invalid for a video note.
   Rejected.

The name `kind` (not `type`) matches the `kind` field of `download_media`,
which uses the same values. `type` already means the chat type.

Format handling (decided by the owner): **no conversion, strict check.**
The server reads the needed metadata in pure Go. A wrong format fails with
an error that includes an ffmpeg command to fix it. The agent has a shell
and converts the file itself.

Rejected for format handling: converting with ffmpeg when it is on `PATH`
(runtime dependency, slower, temp files), and no check at all (silent
wrong results).

Path security (decided by the owner): **allowed folders.** Rejected:
no limit (open to the leak above) and a list of blocked paths (there is
always a secret in a path nobody listed).

## MCP tool

### `send_file`

Annotations: none (it writes to Telegram), same as `send_message`.

Description (for the model): "Send a local file to a chat as yourself.
kind is photo, document, audio, voice, video or video_note. Pass chat_id and
chat_type from list_chats. The path must be inside a folder allowed by
TELEGRAM_MCP_SEND_DIRS. voice needs an OGG/Opus file. video needs an MP4
(H.264). video_note needs a square MP4 (H.264) of at most 60 s. The server does not convert files."

Input:

```go
type SendFileInput struct {
    ChatID   int64  `json:"chat_id"   jsonschema:"id from list_chats"`
    ChatType string `json:"chat_type" jsonschema:"private, group, supergroup or channel"`
    Path     string `json:"path"      jsonschema:"absolute path of a local file inside an allowed folder"`
    Kind     string `json:"kind"      jsonschema:"photo, document, audio, voice, video or video_note"`
    Caption  string `json:"caption,omitempty"  jsonschema:"optional caption in Markdown, same syntax as send_message"`
    ReplyTo  int    `json:"reply_to,omitempty" jsonschema:"optional id of a message from read_chat to reply to"`
}
```

Validation in `mcpserver`, before any Telegram call:

- `chat_id` non-zero, valid `chat_type`. Same errors as `send_message`.
- `path` non-empty and absolute: `path must be an absolute path`.
- `kind` is one of the six values:
  `kind must be one of photo, document, audio, voice, video, video_note`.
- `caption` with `kind=video_note`: `video_note cannot have a caption`.
- `reply_to` not negative.

Output: same as `send_message`.

```go
type SendFileResult struct {
    ID   int   `json:"id"`
    Date int64 `json:"date"`
}
```

## Behaviour

1. **Path check.**
   - Resolve the path with `filepath.EvalSymlinks`. Resolve each allowed
     folder the same way, once at startup.
   - The resolved path must be inside one resolved allowed folder (a
     component-wise prefix check, not a string prefix: `/a/bc` is not
     inside `/a/b`).
   - Otherwise: `path is outside the allowed folders (TELEGRAM_MCP_SEND_DIRS)`.
     The error does not say whether the file exists.
   - Open the resolved path once. All later steps (stat, format check,
     upload) use this open handle, so the file cannot be swapped between
     the check and the upload.
   - The file must be a regular file: `path is not a regular file`.
2. **Size check.** Size from the open handle.
   - Larger than `TELEGRAM_MCP_MAX_UPLOAD_MB`: `file is N bytes, limit is M`.
   - `photo` larger than 10 MiB (Telegram's photo limit):
     `photo is N bytes, Telegram limit is 10 MiB; send it as document`.
   - Empty file: `file is empty`.
3. **Format check and metadata, per kind.** Detection uses the file
   content (magic bytes), not the extension.

   | kind | Accepted | Metadata sent | Telegram media |
   | --- | --- | --- | --- |
   | `photo` | JPEG, PNG, WebP | none | `InputMediaUploadedPhoto` |
   | `document` | any file | file name, MIME type | uploaded document, `ForceFile` |
   | `audio` | MP3, M4A (AAC), OGG (Opus or Vorbis), FLAC | duration (see note), file name | document with `DocumentAttributeAudio` |
   | `voice` | OGG with Opus | duration | document with `DocumentAttributeAudio{Voice: true}`, MIME `audio/ogg` |
   | `video` | MP4 with H.264 video; audio track optional, AAC if present | duration, width, height, file name | document with `DocumentAttributeVideo{SupportsStreaming: true}`, MIME `video/mp4` |
   | `video_note` | MP4 with H.264 video; audio track optional, AAC if present | duration, width, height | document with `DocumentAttributeVideo{RoundMessage: true}`, MIME `video/mp4` |

   Rules:
   - `video_note`: width must equal height:
     `video_note must be square, got WxH; crop with: ffmpeg -i in.mp4 -vf "crop='min(iw,ih)':'min(iw,ih)',scale=384:384" -c:v libx264 -c:a aac out.mp4`.
     Duration must be at most 60 s:
     `video_note is N s, limit is 60 s`.
   - `voice`: a non-Opus file fails with
     `voice needs OGG/Opus; convert with: ffmpeg -i <in> -c:a libopus -b:a 32k out.ogg`.
   - `video`: any width and height, no duration limit besides the size
     limit. A caption is allowed.
   - `video` and `video_note`: a non-MP4 or non-H.264 file fails with
     `<kind> needs MP4 with H.264; convert with: ffmpeg -i <in> -c:v libx264 -c:a aac out.mp4`.
   - `audio`: an unsupported format fails with
     `audio needs MP3, M4A, OGG or FLAC; send it as document instead`.
   - `photo`: an unsupported format fails with
     `photo needs JPEG, PNG or WebP; send it as document instead`.
   - `audio` duration note: if the format is accepted but the duration
     cannot be read (for example a VBR MP3 without a Xing header that
     would need a full scan), send duration 0. The Telegram player still
     works. This is Claude's call.
   - The file name sent to Telegram is the base name of the resolved path,
     for `document`, `audio` and `video`. For `document`, the MIME type comes from
     the extension (`mime.TypeByExtension`), else
     `application/octet-stream`.
4. **Peer.** Resolve the chat with the existing `resolvePeer`. The chat
   must be one of the owner's dialogs, as for `send_message`. Order: the
   path and format checks (steps 1-3) run before any Telegram call, so a
   bad file costs no network.
5. **Upload.** `gotd/td/telegram/uploader` from the open handle
   (`FromReader` with the base name, or `Upload(NewUpload(...))` with the
   known size so big files use the big-file path).
6. **Send.** `messages.sendMedia` with a random id, the caption (parsed
   with the same Markdown code as `send_message`), entities, and
   `reply_to` if set. gotd's `message` builders (`UploadedPhoto`,
   `UploadedDocument(...).Voice()`, `.Audio()`, `.RoundVideo()`) may be
   used, or the raw request. Builder's choice.
7. **Result.** Unpack the sent message: `{ "id", "date" }`.

Errors from Telegram are wrapped like the other tools, for example
`messages.sendMedia: <rpc error>`. Flood waits follow the existing rule
(≤ 60 s waited out inside the server).

## Config

| Variable | Default | Meaning |
| --- | --- | --- |
| `TELEGRAM_MCP_SEND_DIRS` | the download folder only | Folders `send_file` may read from. Separated by `os.PathListSeparator` (`:` on macOS/Linux). Relative entries are made absolute. Empty entries are ignored. |
| `TELEGRAM_MCP_MAX_UPLOAD_MB` | `100` | Largest file `send_file` sends, in MiB. Positive integer. |

- A folder in `TELEGRAM_MCP_SEND_DIRS` that does not exist is not a
  startup error. It is skipped with a warning on stderr. This is Claude's
  call: the automation may create the folder later, but `serve` resolves
  folders once at start, so a restart is then needed. The README says so.
- The default (the download folder) lets a file from `download_media` be
  sent again. It also keeps the tool safe when the variable is not set.
- When the default is used, `serve` creates the download folder at start
  (mode 0700) if it is missing. Otherwise a fresh install would skip it and
  refuse every path until a restart. Claude's call.

## Docs

- README: new `send_file` section, both env vars, and a smoke-test item
  per kind.
- README: the intro and "All tools are read-only" are already wrong
  because `send_message` exists. Fix both lines while editing.

## Testing

- Unit tests for the path check: inside, outside, `..`, symlink pointing
  outside, prefix trap (`/a/bc` vs `/a/b`), not a regular file, default
  folder.
- Unit tests for each format reader with small fixture files in
  `testdata/`: OGG Opus, OGG Vorbis, MP4 square and landscape, MP4
  with a non-H.264 codec, MP3, M4A, FLAC, JPEG, PNG, WebP, a text file.
- Handler tests with the fake `Telegram` interface, like the other tools.
- Manual test against a real account: one send per kind to Saved
  Messages. Check that the voice bubble and the circle render in the
  Telegram app on phone and desktop.

## Open points to check in the manual test

- Whether Telegram shows a waveform for a voice message sent without one.
  If it shows a flat line, computing a waveform needs Opus decoding. That
  is out of scope for this version.
- Whether Telegram rejects a video note longer than 60 s. The server
  rejects it anyway, to match the app.

## Left to the builder

- MP4/OGG/MP3/FLAC readers: hand-written box/page readers or a small
  pure-Go library. No cgo, no external programs.
- Exact MIME table for `audio`.
- Whether to use gotd `message` builders or raw `MessagesSendMediaRequest`.
- Where the path check lives (`internal/tg` or a new small package).
