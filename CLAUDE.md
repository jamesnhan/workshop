# Workshop Project

Workshop is a tmux session manager and AI orchestration tool. Tech stack:
- **Backend:** Go, SQLite, WebSocket (nhooyr.io/websocket), creack/pty for owned-PTY architecture
- **Frontend:** React + TypeScript (Vite), xterm.js for terminal rendering
- **MCP Server:** Model Context Protocol tools for Claude Code integration
- **Config:** Lua-based (gopher-lua)

Key directories:
- `internal/tmux/` — tmux bridge (session/pane management, agent launcher)
- `internal/server/` — HTTP server, WebSocket handler, output buffer, recording
- `internal/api/v1/` — REST API routes (sessions, kanban, agents, consensus, docs, git)
- `internal/db/` — SQLite database (cards, notes, recordings)
- `internal/consensus/` — Multi-agent consensus engine
- `internal/mcp/` — MCP server tool definitions
- `internal/config/` — Lua config engine
- `frontend/src/` — React SPA (App.tsx, components/, hooks/)

## Build & Test

```bash
go build ./...          # backend
go test ./...           # backend tests
cd frontend && npm run build  # frontend
make build              # full build (frontend + backend, produces bin/workshop)
make install            # install to ~/.local/bin/workshop
```

## Repository Sync

**IMPORTANT:** This repo (workshop) is a sanitized downstream of `github.com/jamesnhan/workshop`.

- **workshop** — Personal version (unsanitized, AUTHORITATIVE SOURCE, MUST NOT BREAK)
- **workshop** — Work version (sanitized, no personal data, downstream of workshop)

### Sync Workflow (Claude Handles Git 99.99% of Time)

**Pull features from workshop (common):**
```bash
git fetch workshop
git merge workshop/main -m "Merge features from workshop"
# Sanitize any personal references
# Fix any workshop→workshop naming
git push origin main
```

**Push features to workshop (NEVER BREAK WORKSHOP!):**
```bash
# RARE: Only push well-tested, non-breaking features
# REVIEW: Ensure it won't break workshop
git push workshop main
```

**What gets synced:**
- ✅ Code (features, bugs, UI improvements)
- ❌ Personal kanban data (SQLite stays local)
- ❌ Personal session names/data
- ❌ Personal project references

**Key principle:** Features should be in parity, but workshop is authoritative and must never be broken by workshop changes.

See SYNC.md for complete workflow documentation.
