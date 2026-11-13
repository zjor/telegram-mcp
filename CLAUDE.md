# telegram-mcp

Go MCP server for Telegram (MTProto, user account). See README.md for tools,
setup and files written.

## Process

@docs/process/conventions.md

## Docs

- `docs/roadmap.md`: agreed work that is not started. When an item starts,
  write its design spec and remove or update the roadmap entry.
- `docs/process/specs/YYYY-MM-DD-<topic>-design.md`: design specs. The
  spec is the authority for what a feature must do.
- `docs/process/plans/YYYY-MM-DD-<topic>.md`: implementation plans
  derived from a spec.

## Checks

- `go test ./...` and `go vet ./...` must pass before a commit.
- Build the server: `go build -o telegram-mcp ./cmd/telegram-mcp`.
