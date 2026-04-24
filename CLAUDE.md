# Workshop Project

Workshop is a tmux session manager and AI orchestration tool. Tech stack:
- **Backend:** Go, SQLite, WebSocket (nhooyr.io/websocket), creack/pty for owned-PTY architecture
- **Frontend:** React + TypeScript (Vite), xterm.js for terminal rendering
- **MCP Server:** Model Context Protocol tools for Claude Code integration
- **Config:** Lua-based (gopher-lua)

Key directories:
- `internal/tmux/` — tmux bridge (session/pane management, agent launcher)
- `internal/server/` — HTTP server, WebSocket handler, output buffer, recording, channel hub, UI command hub, pane monitor
- `internal/api/v1/` — REST API routes (sessions, kanban, agents, docs, git, channels, ui)
- `internal/db/` — SQLite database (cards, notes, recordings, channel subscriptions/messages)
- `internal/mcp/` — MCP server tool definitions + channel listener loop
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

Workshop runs locally only — no K8s, no Docker, no headless mode in this fork.
On macOS, `deploy-local` uses `launchctl` to restart `com.jamesnhan.workshop`.
On Linux, it uses `systemctl --user restart workshop.service`.

Specs live under [`docs/specs/`](docs/specs/README.md) and are the source of
truth for behavior. Every feature area has a test matrix in its spec.
Workflow is **plan → spec → tests → build**; see
[`TESTING.md`](TESTING.md) for details.

A checked-in `.githooks/pre-push` runs `make test-unit` + `make test-frontend`
before every push.

## MCP tool surface (current)

Workshop's MCP server exposes tools across these categories. Use them proactively
from any Claude Code session running inside a Workshop pane.

**Sessions / panes**
- `list_sessions`, `list_panes`, `create_session`, `kill_session`, `rename_session`
- `send_keys`, `send_text`, `capture_pane`, `search_output`
- `split_window`, `create_window`

**Agents** — the MCP `launch_agent` and `orchestrate_card` tools were removed;
delegate work via Claude Code's Task tool (supports `isolation: "worktree"`
for sandboxed work) and record results through the kanban/activity MCP tools.

**Kanban**
- `kanban_list` — supports `limit` (default 50, 0 = all) and `offset` for pagination; response includes total count and next-page hint. **Always check the "Showing X–Y of Z" header — if Z > Y, fetch additional pages before concluding a card doesn't exist or a project has no tickets.**
- `kanban_create`, `kanban_edit`, `kanban_move`, `kanban_delete`, `kanban_add_note`
- Cards can have `parent_id` for hierarchy and `blocks/blocked_by` dependencies (via the `/card-dependencies` REST surface; no MCP tool yet). The dependency graph view visualizes them.
- `MoveCard` re-densifies positions so drop-in-place is a true no-op (#442). Cycles in `AddDependency` are rejected server-side (#443).
- Per-project workflows define columns + allowed transitions (`GET/PUT /workflows`). `MoveCard` validates transitions server-side.
- Refinement gates: workflow transitions can require card content (description, checklist) before allowing moves.

**Activity**
- `report_activity(action, summary, project?, metadata?, parent_id?)` — log agent actions to the activity feed with optional tree nesting

**Approvals**
- `request_approval(action, details, project?, diff?)` — **blocking**, waits for user approve/deny in Activity tab (10min timeout)

**Status**
- `set_pane_status` (green/yellow/red), `clear_pane_status`

**UI control** — drive the frontend from agents
- `show_toast(message, kind)` — non-blocking themed toast
- `switch_view(view)` — sessions/kanban/graph/docs/agents/activity/settings
- `focus_cell(cellId)` / `focus_pane(target)` — change pane focus
- `assign_pane(target, cellId?)` — put a pane in a cell
- `open_card(id)` — open the kanban view and expand a card
- `prompt_user(title, message, initialValue?)` — **blocking**, returns user's typed string
- `confirm(title, message, danger?)` — **blocking**, returns "true" or "false"

**Channels** — inter-pane / inter-agent messaging
- `channel_publish(channel, body, from?, project?)` — fan out to all subscribers
- `channel_subscribe(channel, target, project?)` / `channel_unsubscribe(channel, target)`
- `channel_list(project?)` — list active channels with subscriber/message counts
- `channel_messages(channel, limit?)` — recent message history

Channel delivery has two modes:
- **Native** (preferred when available): `claude/channel` MCP capability — messages arrive as `<channel source="workshop" from="..." channel="..." project="...">body</channel>` tags in the receiver's context. Requires Claude Code launched with `--dangerously-load-development-channels server:workshop` (already aliased).
- **Compat** (fallback): typed into the receiver's input via `send_text` as `[channel:X from:Y] body`. Works with every Claude Code version.

The `auto` delivery mode (default) picks per-target based on whether the receiving pane has registered a native listener. Set in Settings → Channels.

**Docs**
- `open_doc(path)` — open a markdown file in the Docs view
- Docs view features: full-text search across .md files, syntax-highlighted code blocks (rehype-highlight), clickable `#123` ticket references with hover preview, split pane for side-by-side comparison, pinned docs, copy to clipboard

**Session analysis** (REST only, no MCP tools)
- `GET /compactions` — parse Claude Code JSONL for compaction events (token counts, trigger, tools)
- `GET /session-usage?weekly=true` — rolling 7-day token usage per model with reset times

## Channel listener architecture

Each `workshop mcp` subprocess (one per Claude Code pane) reads `$TMUX_PANE` on
startup, resolves it to a session:window.pane target via `tmux display-message`,
and long-polls `/api/v1/channel-listen/{target}` against the central Workshop
server. When a message arrives in the NDJSON stream, the subprocess emits a
`notifications/claude/channel` notification via `SendNotificationToAllClients`
to its parent Claude Code instance.

The `WithExperimental({"claude/channel": {}})` capability declaration in
`internal/mcp/mcp.go` is what registers the listener inside Claude Code.

## Repository Sync

Workshop is a sanitized SFW fork of Yuna (github.com/jamesnhan/yuna).

- **Yuna** is authoritative. Workshop features must never break Yuna.
- **Workshop → Yuna**: merge freely (safe direction)
- **Yuna → Workshop**: cherry-pick only, review diffs for personal data

Sanitization is mechanical: `yuna` → `workshop` across Go imports, env var
prefixes (`YUNA_*` → `WORKSHOP_*`), Lua API (`yuna.X` → `workshop.X`), binary
name, and example paths. Files **not** carried in Workshop: `Dockerfile`,
`deploy/k8s/*`, `docs/deployment.md`, `rename-to-yuna.sh`. Workshop defaults
to local-only mode; the K8s/auth/headless code paths exist but no-op when
their env vars are unset.

## Security

- **Lua config sandboxing** — `POST /config/load` restricted to `~/.config/workshop/` paths only, inline code execution removed
- **Agent model validation** — model names validated against `[a-zA-Z0-9.:/_@-]+` regex, shell metacharacters rejected (422)
- **XSS protection** — DOMPurify on all dangerouslySetInnerHTML sites
- **Path traversal guard** — docs endpoints refuse paths outside `$HOME`
- **API key auth** — optional, via `WORKSHOP_API_KEY` env var (unset in local mode)
