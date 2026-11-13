# Roadmap

Planned work that is agreed in principle but not started. Each item moves to
a design spec under `docs/process/specs/` when work begins.

## SQLite storage for session and peer cache

**Status:** proposed, not started.

**Problem.** Claude Code starts one `telegram-mcp serve` process per session
(stdio transport), and all of them share `session.json` and `peers.json`.

- `session.json` is written by gotd's `session.FileStorage` with a plain
  `os.WriteFile`. Its lock works only inside one process, so two servers
  saving at once can corrupt the file and force a new login.
- `peers.json` is written atomically (temp file and rename), but each save
  rewrites the whole map. The last writer wins, so one server can drop
  entries another server added. The cache refills, at the cost of extra
  dialog scans.

**Proposal.**

- One SQLite file, `~/.config/telegram-mcp/state.db`, mode 0600, WAL mode,
  `busy_timeout` about 5 s.
- Driver `modernc.org/sqlite` (pure Go, no cgo). Binary size impact not
  measured yet.
- Table `session(id INTEGER PRIMARY KEY CHECK (id = 1), data BLOB)` behind
  gotd's `session.Storage` interface (`LoadSession`, `StoreSession`).
- Table `peers(type TEXT, id INTEGER, access_hash INTEGER, PRIMARY KEY(type, id))`
  behind the current `PeerCache` methods, with per-row upserts.
  `PeerCache.Save` goes away.
- One-time migration: if `state.db` has no session and `session.json`
  exists, import both JSON files. Keep the old files for rollback. No new
  login.

**Alternative considered.** A `flock` around every write of the two JSON
files. Smaller, but it fixes only the corruption risk, not the lost peer
entries.

**Touches.** `internal/tg` (storage, peer cache), `internal/config` (paths),
`logout`, README "Files written".
