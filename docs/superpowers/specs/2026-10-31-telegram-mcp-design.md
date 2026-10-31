# Telegram MCP Server — Design

Date: 2026-10-31
Status: draft, awaiting approval

## Intent

A local MCP server that lets an MCP client (Claude Code / Claude Desktop on
the owner's Mac) list the owner's Telegram chats and read their message
history as plain text. The owner uses the text as context for a personal
"second brain": conversations from private chats feed later tasks.

The server logs in as the owner's own Telegram account over MTProto. The
Telegram Bot API cannot list a user's chats or read history, so a bot is
not an option.

## Goals

- List the owner's dialogs: private chats, groups, supergroups, channels.
- Read the message history of any dialog as plain text, newest first,
  with pagination so a whole chat can be read page by page.
- Run as a single Go binary over stdio.
- Login once in a terminal; afterwards the server starts non-interactively
  from a saved session.

## Non-goals (for this version)

- Sending messages, marking as read, or any write to Telegram.
- Media download, voice transcription, reactions, polls, reply threads,
  forwards metadata.
- Search inside chats.
- HTTP / Streamable HTTP transport. The transport is selected by a flag
  so it can be added later, but only stdio is implemented now.
- Multiple accounts.
- Running more than one server instance at a time. The owner runs exactly
  one; no file locking on the session file.

## Anti-goals

- Anything that writes to or changes the owner's Telegram account.
- Getting the account rate-limited or banned. Flood waits must be obeyed.
- Leaking the session: the session file must be readable only by the
  owner (mode 0600) and must never be printed in logs or tool output.

## Decisions made by the owner

- Language: Go (toolchain 1.25 is installed).
- Transport: stdio.
- Auth: MTProto user login, not a bot.
- API credentials (`api_id`, `api_hash`) are provided by the owner.
- Message content: plain text only.
- Pagination over MTProto's native offsets.

## Decisions made by the designer (owner may override)

- MCP library: `github.com/modelcontextprotocol/go-sdk` (official).
- Telegram library: `github.com/gotd/td`.
- Credentials via env vars `TELEGRAM_API_ID`, `TELEGRAM_API_HASH`.
- Session file: `~/.config/telegram-mcp/session.json`, override with
  `TELEGRAM_SESSION_PATH`. Created with mode 0600.
- Default page size 50, maximum 100 (Telegram's per-request limit).
- Messages with no text (photo, video, voice, sticker, document, etc.)
  are rendered as a placeholder like `[photo]`, `[voice]`, `[sticker]`,
  `[document]`, followed by the caption if there is one. Service
  messages (joins, pins, title changes) are rendered as `[service]` with
  a short description where cheap to produce, otherwise just `[service]`.
- Flood waits (`FLOOD_WAIT_X`) are handled inside the server with
  gotd's built-in flood-wait middleware; the tool call blocks and
  retries. It is not surfaced to the client as an error unless the wait
  exceeds 60 s, in which case the tool returns an error naming the wait.
- Each chat object also carries `folder` (`main` or `archive`).
- Logging goes to stderr only. stdout is reserved for the MCP protocol.

## CLI

One binary, `telegram-mcp`, with subcommands:

- `telegram-mcp login`
  Interactive. Prompts for phone number, the code Telegram sends, and the
  2FA password if the account has one. Writes the session file. Exits 0
  on success. Safe to re-run; it overwrites the session.
- `telegram-mcp serve [--transport stdio]`
  Non-interactive. Loads the session; if the session file is missing or
  the session is not authorized, prints a one-line instruction to run
  `telegram-mcp login` to stderr and exits non-zero. Then serves MCP on
  stdio until stdin closes.
- `telegram-mcp logout` (optional, cheap): deletes the session file and,
  if possible, calls `auth.logOut`.

`--transport` accepts only `stdio` for now; any other value is an error.

## MCP tools

All tools are read-only. All responses are JSON text in the tool result.

### `list_chats`

Input:
- `folder` (string, optional, default `main`): `main` or `archive`.
  Telegram keeps archived dialogs in folder 1; the main list does not
  include them. Each folder has its own independent cursor.
- `limit` (int, optional, default 50, max 100)
- `cursor` (string, optional): opaque paging cursor from a previous
  call. Internally encodes MTProto's `offset_date`, `offset_id` and
  `offset_peer` so paging is exact even with pinned dialogs and equal
  timestamps.

Output: `{ "chats": [...], "next_cursor": string|null }`

Each chat:
- `id` (int64) — the value to pass to `read_chat`. Must be stable across
  calls and across restarts for the same chat. For channels/supergroups
  this is the bare channel id; for groups the bare chat id; for users the
  user id. The `type` field disambiguates.
- `type` — one of `private`, `group`, `supergroup`, `channel`.
- `title` — display name (user's first+last name for private chats).
- `username` (string, optional) — without `@`, when the peer has one.
- `last_message_date` (int64 unix seconds).
- `unread_count` (int).
- `folder` — `main` or `archive`.

Ordering: Telegram's dialog order (pinned first, then by last message
date, newest first). `next_cursor` is null when there are no more.

### `read_chat`

Input:
- `chat_id` (int64, required) — from `list_chats`.
- `chat_type` (string, required) — the `type` from `list_chats`. Needed
  because Telegram needs the access hash and peer kind to build an
  `InputPeer`; the server resolves it from its dialog cache (see below).
- `limit` (int, optional, default 50, max 100).
- `before_id` (int, optional) — return only messages with id lower than
  this. Used for paging backwards in time.
- `since_date` (int64 unix seconds, optional) — stop and return nothing
  older than this date.

Output: `{ "messages": [...], "next_before_id": int|null }`

Each message:
- `id` (int)
- `date` (int64 unix seconds)
- `from` (string) — sender display name; for channels the channel title;
  `"me"` for messages sent by the owner.
- `text` (string) — message text, or placeholder as described above.

Ordering: newest first. `next_before_id` is the id of the oldest message
returned, or null when the start of the chat has been reached or
`since_date` cut the page off.

Paging contract: calling repeatedly with `before_id = next_before_id`
until it is null reads the entire chat.

### `get_chat` (optional)

`get_chat(chat_id, chat_type)` returns the same chat object as
`list_chats` for one chat. Cheap to add; useful when the client already
knows the id. Builder may skip in the first version.

## Peer resolution

MTProto needs an access hash to address users and channels. The server
keeps a map `(type, id) -> InputPeer` filled by `list_chats` (and by
`read_chat` results). The map is persisted to `peers.json` next to the
session file (mode 0600) so a fresh server process can address chats
without re-scanning dialogs. Access hashes are account-bound and are
never exposed through tool output. If `read_chat` is called for a peer that is
not in the map, the server fetches dialogs itself (paging through
`messages.getDialogs`) until it finds the peer, then proceeds. The fallback scan
covers both the main and the archive folder. If it still cannot find it,
the tool returns an error saying the chat is unknown and suggesting
`list_chats`.

This keeps `chat_id` values simple integers for the client and avoids
exposing access hashes.

## Error handling

Tool errors are returned as MCP tool errors with a human-readable message.
Cases to cover:
- session missing / unauthorized (at startup; serve refuses to start)
- unknown chat id
- Telegram network errors (wrapped, with the RPC error code)
- flood wait > 60 s
- invalid arguments (limit out of range, unknown chat_type)

## Project layout (suggested)

```
cmd/telegram-mcp/main.go      # cobra or stdlib flag; subcommands
internal/tg/client.go         # gotd client setup, session storage, flood-wait middleware
internal/tg/dialogs.go        # list dialogs, peer cache
internal/tg/history.go        # get history, message -> plain text
internal/mcp/server.go        # MCP server, tool registration
internal/mcp/tools.go         # list_chats, read_chat handlers
```

The builder may reshape this.

## Testing

- Unit tests for message-to-text rendering (text, media placeholders,
  captions, service messages) using constructed `tg.Message` values.
- Unit tests for dialog-to-chat mapping and id/type extraction.
- Unit tests for paging logic (`next_before_id`, `since_date` cut-off).
- MCP tool handlers tested against an interface over the Telegram
  client so no network is needed.
- One manual smoke test documented in README: `login`, then `serve` under
  Claude Code, call `list_chats`, call `read_chat` twice with paging.

## Left to the builder

- Exact gotd API usage and session storage type.
- CLI library choice (stdlib `flag` is fine).
- How much of service messages to describe.
- Whether `get_chat` ships in v1.
- README content.

## Open questions resolved during brainstorming

- Transport: stdio only, flag reserved for later.
- Bot vs user: user over MTProto.
- "Read completely" means page until `next_before_id` is null.
- Content: plain text only; non-text messages get placeholders.
