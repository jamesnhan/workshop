# Workshop

A tmux session manager and AI orchestration tool. Manage multiple terminal sessions through a browser-based UI with multi-pane grids, AI agent management, kanban project tracking, and more.

## Features

- **Multi-pane terminal grid** — configurable NxM grid with cell merge/split, maximize, vim navigation
- **AI agent orchestration** — launch and monitor Claude, Gemini, and Codex agents (providers auto-detected)
- **Consensus engine** — run the same prompt through multiple agents, synthesize results
- **Kanban board** — SQLite-backed project tracking with drag-and-drop, priorities, notes
- **Pane output search** — full scrollback search with fuzzy matching and ANSI-rendered preview
- **Terminal recording/replay** — record sessions and replay them later
- **Workspaces** — save/restore named layout presets
- **Theme system** — multiple themes with terminal color support
- **Mobile responsive** — full touch support, responsive grid
- **MCP server** — integrate with Claude Code and other MCP-compatible tools

## Requirements

- Go 1.21+
- Node.js 18+
- tmux 3.0+

### Optional AI Providers

Workshop auto-detects which AI CLI tools are installed:

- **Claude Code** — `npm install -g @anthropic-ai/claude-code` (or see [claude.ai/code](https://claude.ai/code))
- **Gemini CLI** — `npm install -g @google/gemini-cli`
- **Codex CLI** — `npm install -g @openai/codex`

Only installed providers appear in the agent launcher.

## Quick Start

```bash
# Clone and build
git clone https://github.com/jamesnhan/workshop.git
cd workshop
make build

# Run
./bin/workshop

# Open in browser
open http://localhost:9090
```

## Install

```bash
make install  # installs to ~/.local/bin/workshop
```

## MCP Integration

Workshop includes an MCP server for use with Claude Code:

```json
{
  "mcpServers": {
    "workshop": {
      "command": "workshop",
      "args": ["mcp"]
    }
  }
}
```

## Development

```bash
# Backend
go run .

# Frontend (separate terminal)
cd frontend && npm run dev

# Both use hot reload
```

## Architecture

- **Backend:** Go HTTP server with embedded React SPA, SQLite, WebSocket streaming
- **Terminal:** creack/pty running `tmux attach` per pane for direct PTY I/O
- **Frontend:** React + TypeScript + xterm.js + Vite
- **Config:** Lua scripting via gopher-lua
- **Search:** Full tmux scrollback captured periodically, client-side fzf matching

## License

MIT
