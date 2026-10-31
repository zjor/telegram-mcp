# telegram-mcp

A read-only MCP server for Telegram. It runs as your own user account over MTProto, not as a bot. It has two tools: `list_chats` and `read_chat`. It cannot send, edit or delete anything.

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

## Log in (once)

```bash
TELEGRAM_API_ID=… TELEGRAM_API_HASH=… ./telegram-mcp login
```

The command asks for `Phone number (international format):`, then `Code:`, and `2FA password:` if your account has one. On success it prints `Logged in as <name>` and saves the session.

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

All tools are read-only. Each result is JSON.

### `list_chats`

List chats, newest activity first. Telegram's order is kept: pinned chats come first.

Input:

| Field | Type | Notes |
| --- | --- | --- |
| `folder` | string, optional | `main` (default) or `archive`. Each folder has its own cursor. |
| `limit` | int, optional | 1-100. Default 50. |
| `cursor` | string, optional | Opaque value from `next_cursor` of the previous call. |

Output: `{ "chats": [...], "next_cursor": string|null }`. `next_cursor` is `null` on the last page.

Pinned chats appear only on the first page. Later pages skip them. Limitation: if you have more pinned chats than the first page's `limit`, the extra pinned chats are not listed. With the default limit 50 this cannot happen, because Telegram allows far fewer pins.

Each chat:

| Field | Notes |
| --- | --- |
| `id` | int64. Pass it to `read_chat`. Stable across calls and restarts. |
| `type` | `private`, `group`, `supergroup` or `channel` |
| `title` | Display name. For private chats, first and last name. |
| `username` | Without `@`. Only present if the peer has one. |
| `last_message_date` | Unix seconds |
| `unread_count` | int |
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

Both files have mode 0600. `peers.json` is created on the first successful `list_chats` or `read_chat`.

Run only one server instance at a time. The files are not locked.

## Logout

```bash
./telegram-mcp logout
```

This ends the session on Telegram and deletes `session.json` and `peers.json`. It prints `Logged out`, or `no session` if there is nothing to remove.

## Smoke test

Run these checks yourself after the first setup.

- [ ] `TELEGRAM_API_ID=… TELEGRAM_API_HASH=… ./telegram-mcp login` prints `Logged in as <name>`. `ls -l ~/.config/telegram-mcp/session.json` shows `-rw-------`.
- [ ] Register the server with `claude mcp add telegram -e TELEGRAM_API_ID=… -e TELEGRAM_API_HASH=… -- /absolute/path/to/telegram-mcp serve`. In Claude Code, call `list_chats`: your chats appear. Call `list_chats` with `folder=archive`: archived chats appear.
- [ ] Call `read_chat` for one private chat. Call it again with `before_id` set to `next_before_id`. The messages continue further back in time.
- [ ] On an account with pinned chats, call `list_chats` with `limit=2`. Page with `cursor` until `next_cursor` is `null`. Compare the list with the Telegram app: every chat appears once, none is missing.
- [ ] Call `read_chat` on a supergroup and on a channel. Page each at least 3 times with `before_id`, until `next_before_id` is `null`.
- [ ] Restart `serve`. Call `read_chat` on an archived chat without calling `list_chats` first. It works because the peer cache is persisted.
- [ ] `ls -l ~/.config/telegram-mcp/peers.json` shows `-rw-------`.
- [ ] `TELEGRAM_API_ID=1 TELEGRAM_API_HASH=x TELEGRAM_SESSION_PATH=/tmp/none.json ./telegram-mcp serve` exits with code 3 and prints the `run "telegram-mcp login" first` hint.
- [ ] `./telegram-mcp logout` prints `Logged out` and removes both `session.json` and `peers.json`.
