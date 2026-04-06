# Workshop

## Project Context

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
