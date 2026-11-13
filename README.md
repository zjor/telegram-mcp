| `TELEGRAM_MCP_DEBUG` | no | Set to `1` to log every Telegram call (method, duration, result or error) and gotd connection events to stderr. Payloads are not logged. |
# telegram-mcp

An MCP server for Telegram. It runs as your own user account over MTProto, not as a bot. It has seven tools: `list_chats`, `read_chat`, `send_message`, `send_file`, `get_user`, `list_participants` and `download_media`. It cannot edit or delete anything.

`get_user` returns the profile details of one user, found by `user_id` or `username`. Phone number and birthday appear only when Telegram shows them to you.

`list_participants` lists the members of a group or supergroup. It works as a regular member. If the group hides its member list, it returns recent message senders instead.

## Requirements

- Go 1.25
- A Telegram `api_id` and `api_hash`. Create them at https://my.telegram.org.

## Build

```bash
go build -o telegram-mcp ./cmd/telegram-mcp
```

## Environment variables

| Variable | Required | Meaning |
| --- | --- | --- |
| `TELEGRAM_API_ID` | yes | Telegram API id (a number) |
| `TELEGRAM_API_HASH` | yes | Telegram API hash |
| `TELEGRAM_SESSION_PATH` | no | Session file. Default `~/.config/telegram-mcp/session.json` |
| `TELEGRAM_MCP_DEBUG` | no | Set to `1` to log every Telegram call (method, duration, result or error) and gotd connection events to stderr. Payloads are not logged. |
| `TELEGRAM_MCP_DOWNLOAD_DIR` | no | Folder for `download_media` files. Default `downloads/` next to the session file. |
| `TELEGRAM_MCP_MAX_DOWNLOAD_MB` | no | Largest file `download_media` downloads, in MiB. Default `100`. |
| `TELEGRAM_MCP_SEND_DIRS` | no | Folders `send_file` may read from. Separated by `:` on macOS and Linux (`os.PathListSeparator`). Relative entries are made absolute. Empty entries are ignored. Default: the download folder only. |
| `TELEGRAM_MCP_MAX_UPLOAD_MB` | no | Largest file `send_file` sends, in MiB. Positive integer. Default `100`. |

A folder in `TELEGRAM_MCP_SEND_DIRS` that does not exist is skipped with a warning on stderr. It is not a startup error. `serve` resolves the folders once at start. If you create the folder later, restart `serve`. When `TELEGRAM_MCP_SEND_DIRS` is not set, `serve` creates the download folder at start (mode 0700) if it is missing.

## Log in (once)

```bash
TELEGRAM_API_ID=… TELEGRAM_API_HASH=… ./telegram-mcp login
```

The command prints a QR code. Scan it in the Telegram app on a logged-in device: Settings → Devices → Link Desktop Device. The QR refreshes itself when it expires. If your account has a cloud password, the command then asks for `2FA password:`. On success it prints `Logged in as <name>` and saves the session.

If you cannot scan, use the phone-and-code flow instead:

```bash
TELEGRAM_API_ID=… TELEGRAM_API_HASH=… ./telegram-mcp login --code
```

It asks for `Phone number (international format):`, then `Code:`, and `2FA password:` if needed. Telegram delivers the code to your other Telegram sessions, not by SMS. Telegram sometimes does not deliver codes requested through a new `api_id` at all; the QR flow does not have this problem.

## Claude Code setup

Add this to your MCP config:

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

Or use the CLI:

```bash
claude mcp add telegram -e TELEGRAM_API_ID=… -e TELEGRAM_API_HASH=… -- /absolute/path/to/telegram-mcp serve
```

`serve` runs the MCP server on stdio. It refuses to start if there is no session file.

| Exit code | Meaning |
| --- | --- |
| 2 | Bad usage or config (for example `TELEGRAM_API_ID is not set`) |
| 3 | No session file. Message: `no session at <path>; run "telegram-mcp login" first` |
| 1 | Runtime failure (for example the session is not authorized) |

## Tools

`send_message` and `send_file` send messages. All other tools are read-only. Each result is JSON.

### `list_chats`

List chats, newest activity first. Telegram's order is kept: pinned chats come first. `limit` counts unpinned chats; the first page also returns every pinned chat on top of that, each marked `"pinned": true`.

Input:

| Field | Type | Notes |
| --- | --- | --- |
| `folder` | string, optional | `main` (default) or `archive`. Each folder has its own cursor. |
| `limit` | int, optional | 1-100. Default 50. |
| `cursor` | string, optional | Opaque value from `next_cursor` of the previous call. |

Output: `{ "chats": [...], "next_cursor": string|null }`. `next_cursor` is `null` on the last page.

Pinned chats appear only on the first page. Later pages skip them.

Each chat:

| Field | Notes |
| --- | --- |
| `id` | int64. Pass it to `read_chat`. Stable across calls and restarts. |
| `type` | `private`, `group`, `supergroup` or `channel` |
| `title` | Display name. For private chats, first and last name. |
| `username` | Without `@`. Only present if the peer has one. |
| `last_message_date` | Unix seconds |
| `unread_count` | int |
| `pinned` | `true` for pinned chats. Omitted otherwise. |
| `folder` | `main` or `archive` |

### `read_chat`

Read the text history of one chat, newest first.

Input:

| Field | Type | Notes |
| --- | --- | --- |
| `chat_id` | int64, required | `id` from `list_chats`. A missing value is rejected by the input schema. `0` passes the schema and fails with `chat_id is required`. |
| `chat_type` | string, required | `type` from `list_chats` |
| `limit` | int, optional | 1-100. Default 50. |
| `before_id` | int, optional | Return only messages with an id lower than this. |
| `since_date` | int64, optional | Unix seconds. Return nothing older than this. |

Output: `{ "messages": [...], "next_before_id": int|null }`.

Each message:

| Field | Notes |
| --- | --- |
| `id` | int |
| `date` | Unix seconds |
| `from` | Sender name. The channel title for channels. `me` for your own messages. |
| `text` | The message text. For media, a placeholder (see below), followed by the caption if there is one, for example `[photo] caption`. |

Placeholders for media: `[photo]`, `[video]`, `[voice]`, `[audio]`, `[video_note]`, `[sticker]`, `[gif]`, `[document name]`, `[location]`, `[contact]`, `[poll]`, `[media]`. A document without a file name is `[document]`. Service messages appear as `[service: ...]` (for example `[service: message pinned]`). Other service messages are plain `[service]`.

`next_before_id` is the lowest message id on the page Telegram returned. Deleted and service entries count too, so it can be lower than the last `id` in `messages`. Paging ends when a call returns no messages: then `next_before_id` is `null`. It is also `null` when Telegram returned the whole chat in one call, or when `since_date` cut the page off. A page can have an empty `messages` list and a non-null `next_before_id`; keep paging.

If the chat is not in the local peer cache, the server scans your dialogs (main and archive, at most 20 pages per folder) to find it. If it still cannot find it, it returns an error that tells you to call `list_chats`.

### `send_message`

Send a text message to one chat as yourself. The message looks the same as one typed in the Telegram app.

Input:

| Field | Type | Notes |
| --- | --- | --- |
| `chat_id` | int64, required | `id` from `list_chats`. `0` fails with `chat_id is required`. |
| `chat_type` | string, required | `type` from `list_chats` |
| `text` | string, required | Message text in Markdown (see below). Blank text fails with `text is required`. |
| `reply_to` | int, optional | `id` of a message from `read_chat` to reply to. |

Output: `{ "id": int, "date": int }`, the id and Unix time of the sent message.

Markdown is CommonMark, parsed leniently: an unmatched marker is sent as plain text, never as an error.

| Syntax | Result |
| --- | --- |
| `**bold**` or `__bold__` | bold |
| `*italic*` or `_italic_` | italic |
| `~~text~~` | strikethrough |
| `\|\|text\|\|` | spoiler |
| `` `code` `` | inline code |
| ```` ```lang ```` fenced block | code block, with optional language |
| `[text](url)` | link |
| `> text` | block quote |

Underline has no Markdown syntax and is not supported.

The chat must be one of your own dialogs. An id that is not in your chat list fails with the same `list_chats` error as `read_chat`, so a message cannot go to an unknown chat.

### `send_file`

Send a local file to one chat as yourself. The file must be inside an allowed folder (see `TELEGRAM_MCP_SEND_DIRS`).

Input:

| Field | Type | Notes |
| --- | --- | --- |
| `chat_id` | int64, required | `id` from `list_chats`. `0` fails with `chat_id is required`. |
| `chat_type` | string, required | `type` from `list_chats` |
| `path` | string, required | Absolute path of the file. An empty path fails with `path is required`. A relative path fails with `path must be an absolute path`. |
| `kind` | string, required | `photo`, `document`, `audio`, `voice`, `video` or `video_note`. Any other value fails with `kind must be one of photo, document, audio, voice, video, video_note`. |
| `caption` | string, optional | Caption in Markdown, same syntax as `send_message`. Not allowed with `video_note`: `video_note cannot have a caption`. |
| `reply_to` | int, optional | `id` of a message from `read_chat` to reply to. Must not be negative. |

Output: `{ "id": int, "date": int }`, the id and Unix time of the sent message.

The server checks the file content (magic bytes), not the extension. It does not convert files.

| kind | Accepted format |
| --- | --- |
| `photo` | JPEG, PNG or WebP. Up to 10 MiB. |
| `document` | Any file. The MIME type comes from the extension. |
| `audio` | MP3, M4A (AAC), OGG (Opus or Vorbis) or FLAC |
| `voice` | OGG with Opus |
| `video` | MP4 with H.264. The audio track is optional, AAC if present. |
| `video_note` | Square MP4 with H.264, at most 60 s. The audio track is optional, AAC if present. |

Errors:

- `path is outside the allowed folders (TELEGRAM_MCP_SEND_DIRS)`. The error does not say whether the file exists.
- `path is not a regular file`
- `file is empty`
- `file is N bytes, limit is M`, when the file is larger than `TELEGRAM_MCP_MAX_UPLOAD_MB`.
- `photo is N bytes, Telegram limit is 10 MiB; send it as document`
- `photo needs JPEG, PNG or WebP; send it as document instead`
- `audio needs MP3, M4A, OGG or FLAC; send it as document instead`
- `voice needs OGG/Opus; convert with: ffmpeg -i <in> -c:a libopus -b:a 32k out.ogg`
- `<kind> needs MP4 with H.264; convert with: ffmpeg -i <in> -c:v libx264 -c:a aac out.mp4`, for `video` and `video_note`.
- `video_note must be square, got WxH; crop with: ffmpeg -i in.mp4 -vf "crop='min(iw,ih)':'min(iw,ih)',scale=384:384" -c:v libx264 -c:a aac out.mp4`
- `video_note is N s, limit is 60 s`

The path and format checks run before any Telegram call. The chat must be one of your own dialogs, as for `send_message`.

### `list_participants`

List the members of a group or supergroup. Broadcast channels are not supported.

Input:

| Field | Type | Notes |
| --- | --- | --- |
| `chat_id` | int64, required | `id` from `list_chats`. |
| `chat_type` | string, required | `group` or `supergroup`. Other types fail with `list_participants supports group and supergroup only`. |
| `query` | string, optional | Match by first name, last name or username. |
| `filter` | string, optional | `all` (default), `admins` or `bots`. |
| `limit` | int, optional | Page size 1-100. Default 50. |
| `cursor` | string, optional | `next_cursor` from the previous call. |

Output:

| Field | Notes |
| --- | --- |
| `source` | `members`: the real member list. `recent_senders`: the group hides its members, so these are people who posted recently. This list is partial. |
| `participants` | `id`, `first_name`, `last_name`, `username`, `bot`, `deleted`. With `members`: `role` (`creator`, `admin`, `member`) and `joined_at`. With `recent_senders`: `last_message_date`. |
| `total` | Member count from Telegram. `null` for `recent_senders`. |
| `next_cursor` | `null` on the last page. |

Limits:

- Telegram lets admins hide the member list. Many large public groups do this. Then you get `recent_senders`. One call scans at most 1000 messages.
- With `recent_senders`, a person can appear again on a later page. Remove duplicates by `id`.
- `filter=admins` returns the real admin list when Telegram allows it. Otherwise it fails with `the group hides its admin list from members`.
- `get_user` works on most returned ids. It can fail for some senders in large groups, because Telegram gives no access data for them.

### `download_media`

Save the attachment of one message to a local file. The tool returns the path, not the file content.

Input:

| Field | Type | Notes |
| --- | --- | --- |
| `chat_id` | int64, required | `id` from `list_chats`. `0` fails with `chat_id is required`. |
| `chat_type` | string, required | `type` from `list_chats` |
| `message_id` | int, required | `id` of a message from `read_chat`. Must be positive. |

Output:

| Field | Notes |
| --- | --- |
| `path` | Absolute path: `<download dir>/<chat_id>_<message_id>.<ext>` |
| `kind` | `voice`, `audio`, `video`, `video_note`, `photo`, `document`, `sticker` or `gif` |
| `mime_type` | For example `audio/ogg` for voice messages |
| `size` | Bytes on disk |
| `file_name` | Original file name of a document. Omitted if there is none. |
| `duration` | Seconds, for voice, audio and video. Omitted if unknown. |

Voice messages are saved as `.ogg` (Opus). Photos are saved in their largest size as `.jpg`. If the file already exists, it is returned without a new download. A failed download leaves no partial file.

Errors: `message not found`, `message has no media`, `media cannot be downloaded` (location, contact, poll, link preview), and `file is N bytes, limit is M` when the file is larger than `TELEGRAM_MCP_MAX_DOWNLOAD_MB`.

### Paging example

1. Call `read_chat` with `chat_id` and `chat_type`.
2. Read `next_before_id` in the result.
3. Call `read_chat` again with the same `chat_id` and `chat_type`, and `before_id` set to that value.
4. Repeat until `next_before_id` is `null`. You have then read the whole chat.

`list_chats` pages the same way with `cursor` and `next_cursor`.

### Errors and limits

Errors are returned as MCP tool errors with a readable message. Examples: `limit must be between 1 and 100`, `folder must be main or archive`, `chat_type must be one of private, group, supergroup, channel`.

Telegram flood waits of up to 60 s are waited out inside the server. Longer ones return a tool error.

## Files written

| File | Content |
| --- | --- |
| `session.json` | Your Telegram session. Treat it like a password. |
| `peers.json` | Cache of chat access data. It is in the same folder as the session file. |
| `downloads/` | Files saved by `download_media`. The folder has mode 0700. |

Both files have mode 0600. `peers.json` is created on the first successful `list_chats`, `read_chat`, `send_message` or `send_file`.

Run only one server instance at a time. The files are not locked.

## Logout

```bash
./telegram-mcp logout
```

This ends the session on Telegram and deletes `session.json` and `peers.json`. It prints `Logged out`, or `no session` if there is nothing to remove.

## Smoke test

Run these checks yourself after the first setup.

- [ ] `TELEGRAM_API_ID=… TELEGRAM_API_HASH=… ./telegram-mcp login` shows a QR code, and after scanning prints `Logged in as <name>`. Running `login` again prints `Logged in as <name>` at once without a QR. `ls -l ~/.config/telegram-mcp/session.json` shows `-rw-------`.
- [ ] Register the server with `claude mcp add telegram -e TELEGRAM_API_ID=… -e TELEGRAM_API_HASH=… -- /absolute/path/to/telegram-mcp serve`. In Claude Code, call `list_chats`: your chats appear. Call `list_chats` with `folder=archive`: archived chats appear.
- [ ] Call `read_chat` for one private chat. Call it again with `before_id` set to `next_before_id`. The messages continue further back in time.
- [ ] On an account with pinned chats, call `list_chats` with `limit=2`. Page with `cursor` until `next_cursor` is `null`. Compare the list with the Telegram app: every chat appears once, none is missing.
- [ ] Call `read_chat` on a supergroup and on a channel. Page each at least 3 times with `before_id`, until `next_before_id` is `null`.
- [ ] Restart `serve`. Call `read_chat` on an archived chat without calling `list_chats` first. It works because the peer cache is persisted.
- [ ] Call `send_message` to your Saved Messages chat with this `text`. The message arrives with the formatting shown. Call it again with `reply_to` set to that message's `id`: the reply quotes the first message.

  ```
  hello **bold** _italic_ ~~strike~~ ||spoiler|| `code` [link](https://example.com)
  ```
- [ ] Call `send_file` once per kind (`photo`, `document`, `audio`, `voice`, `video`, `video_note`) to your Saved Messages chat, with files inside the download folder. Each one arrives. Check that the voice message and the video circle show correctly in the Telegram app on phone and desktop.
- [ ] Call `send_file` with a path outside the allowed folders. It fails with `path is outside the allowed folders (TELEGRAM_MCP_SEND_DIRS)`.
- [ ] `ls -l ~/.config/telegram-mcp/peers.json` shows `-rw-------`.
- [ ] `TELEGRAM_API_ID=1 TELEGRAM_API_HASH=x TELEGRAM_SESSION_PATH=/tmp/none.json ./telegram-mcp serve` exits with code 3 and prints the `run "telegram-mcp login" first` hint.
- [ ] `./telegram-mcp logout` prints `Logged out` and removes both `session.json` and `peers.json`.

## Docs

| Path | What it holds |
| --- | --- |
| `docs/process/conventions.md` | How work is done in this repo, for example one git worktree per feature. |
| `docs/roadmap.md` | Work that is agreed but not started. An item gets a design spec when work begins. |
| `docs/process/specs/` | Design specs, one per feature: `YYYY-MM-DD-<topic>-design.md`. A spec is the authority for what a feature must do. |
| `docs/process/plans/` | Implementation plans made from a spec: `YYYY-MM-DD-<topic>.md`. |

`CLAUDE.md` loads the process conventions for Claude Code sessions.
