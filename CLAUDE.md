# Workshop Project

Workshop is a tmux session manager and AI orchestration tool. Tech stack:
- **Backend:** Go, SQLite, WebSocket (nhooyr.io/websocket), creack/pty for owned-PTY architecture
- **Frontend:** React + TypeScript (Vite), xterm.js for terminal rendering
- **MCP Server:** Model Context Protocol tools for Claude Code integration
- **Config:** Lua-based (gopher-lua)

Key directories:
- `internal/tmux/` — tmux bridge (session/pane management, agent launcher)
- `internal/server/` — HTTP server, WebSocket handler, output buffer, recording, channel hub, UI command hub, pane monitor
- `internal/api/v1/` — REST API routes (sessions, kanban, agents, consensus, docs, git, channels, ui)
- `internal/db/` — SQLite database (cards, notes, recordings, channel subscriptions/messages)
- `internal/consensus/` — Multi-agent consensus engine
- `internal/mcp/` — MCP server tool definitions
- `internal/telemetry/` — OpenTelemetry bootstrap (traces, metrics, logs), gated by `WORKSHOP_OTEL_ENABLED`
- `internal/config/` — Lua config engine
- `internal/testhelpers/` — Test fixtures (TempDB, NewGitRepo, etc.)
- `frontend/src/` — React SPA (App.tsx, components/, hooks/)
- `docs/specs/` — Per-area behavior specs with test matrices

## Build, Test & Deploy

```bash
make build              # full build (frontend + backend, produces bin/workshop)
make install            # install to ~/.local/bin/workshop
make test               # fast backend unit tests
make test-frontend      # Vitest + RTL frontend tests
make test-cover         # backend coverage → coverage/backend.html
make install-hooks      # wire the checked-in pre-push hook (one-time per clone)

# Deploy
make deploy-local       # build + install + restart service (macOS launchctl / Linux systemd)
```

Workshop runs locally only — no K8s, no Docker, no headless mode.
On macOS, `deploy-local` uses `launchctl` to restart `com.jamesnhan.workshop`.
On Linux, it uses `systemctl --user restart workshop.service`.

Specs live under [`docs/specs/`](docs/specs/README.md) and are the source of
truth for behavior. Every feature area has a test matrix in its spec.
Workflow is **plan → spec → tests → build**; see
[`TESTING.md`](TESTING.md) for details.

## MCP tool surface (current)

Workshop's MCP server exposes tools across these categories.

**Sessions / panes**
- `list_sessions`, `list_panes`, `create_session`, `kill_session`, `rename_session`
- `send_keys`, `send_text`, `capture_pane`, `search_output`
- `split_window`, `create_window`

**Agents**
- `launch_agent` — launch claude/gemini/codex with prompt + model; supports `preset` param for specialist roles
- `orchestrate_card(id, directory?, isolation?)` — launch autonomous orchestrator that drives a card through plan→implement→test→review→PR phases. Use `isolation: "worktree"` for git worktree isolation.
- `consensus_start`, `consensus_status`, `consensus_list`, `consensus_capture`, `consensus_review`, `consensus_cleanup`

**Kanban**
- `kanban_list` — supports `limit` (default 50, 0 = all) and `offset` for pagination; response includes total count and next-page hint. **Always check the "Showing X–Y of Z" header — if Z > Y, fetch additional pages.**
- `kanban_create`, `kanban_edit`, `kanban_move`, `kanban_delete`, `kanban_add_note`
- Cards can have `parent_id` for hierarchy and `blocks/blocked_by` dependencies
- Per-project workflows define columns + allowed transitions

**Activity**
- `report_activity(action, summary, project?, metadata?, parent_id?)`

**Approvals**
- `request_approval(action, details, project?, diff?)` — blocking, waits for user approve/deny

**Status**
- `set_pane_status` (green/yellow/red), `clear_pane_status`

**UI control**
- `show_toast`, `switch_view`, `focus_cell`, `focus_pane`, `assign_pane`, `open_card`
- `prompt_user(title, message)` — blocking, returns user's typed string
- `confirm(title, message, danger?)` — blocking, returns "true" or "false"

**Channels**
- `channel_publish`, `channel_subscribe`, `channel_unsubscribe`, `channel_list`, `channel_messages`

**Docs**
- `open_doc(path)` — open a markdown file in the Docs view

**Ollama (local LLM)**
- `ollama_chat`, `ollama_generate`, `ollama_models`
- System prompt passthrough, thinking content capture, unlimited token generation
- Persistent conversations stored in SQLite

## Repository Sync

Workshop is a sanitized SFW fork of Yuna (github.com/jamesnhan/yuna).

- **Yuna** is authoritative. Workshop features must never break Yuna.
- **Workshop → Yuna**: merge freely (safe direction)
- **Yuna → Workshop**: cherry-pick only, review diffs for personal data

**Features NOT in Workshop** (gated by env vars in Yuna, simply absent here):
- API key authentication
- K8s headless mode / tmux proxy / WS proxy
- Dockerfile / K8s manifests
- Ollama endpoint env var config (Workshop uses Lua config only)
- NSFW content / Yuna persona

## Security

- **Lua config sandboxing** — `POST /config/load` restricted to `~/.config/workshop/` paths only, inline code execution removed
- **Agent model validation** — model names validated against `[a-zA-Z0-9.:/_@-]+` regex, shell metacharacters rejected (422)
- **XSS protection** — DOMPurify on all dangerouslySetInnerHTML sites
- **Path traversal guard** — docs endpoints refuse paths outside `$HOME`
