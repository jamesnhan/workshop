# Orchestration Runbook

Workshop is the persona across all AI tools — Claude Code, Gemini CLI, and Codex
CLI are all configured as Workshop with full MCP access. Any of them can serve as
the primary orchestrator. Use this runbook to switch between them (during
outages, or just by preference).

## Quick reference

| Tool | MCP config location | Workshop configured? | Channel support |
|------|---------------------|-------------------|-----------------|
| Claude Code | `.claude/settings.json` | Yes (primary) | Native + compat |
| Gemini CLI | `~/.gemini/settings.json` | Yes | Compat only (send_text) |
| Codex CLI | `~/.codex/config.toml` | Yes | Compat only (send_text) |
| `curl` / REST | N/A (direct HTTP) | Always works | N/A |

## Option 1: Gemini CLI (recommended fallback)

Gemini CLI supports MCP over stdio identically to Claude Code. Workshop is already
configured in `~/.gemini/settings.json`.

```bash
# Launch from a Workshop-managed tmux pane
gemini

# Verify Workshop tools are loaded
# Type: "list your available tools" or check for kanban_list, list_sessions, etc.
```

**What works:** All MCP tools (kanban, sessions, panes, UI control, activity,
approvals, docs).

**What doesn't work:** Native channel delivery. Channels fall back to compat
mode (send_text injection). The `claude/channel` experimental capability is
Claude Code-specific.

**Gemini-specific notes:**
- No underscore in MCP server names (use `workshop`, not `workshop_server`).
- Default tool timeout ~60s; long operations may need retry.
- Gemini's shared engineering instructions live in `~/.gemini/GEMINI.md`.

## Option 2: Codex CLI

Codex supports MCP over stdio. Workshop is already configured in
`~/.codex/config.toml`.

```bash
codex
```

**What works:** All MCP tools.

**What doesn't work:**
- Native channels (same as Gemini — compat mode only).
- MCP resources (Codex only uses `tools/list` + `tools/call`).
- Startup timeout is aggressive (10s default); if Workshop server is slow to
  respond, increase `startup_timeout_sec` in config.

## Option 3: Direct REST API (no LLM needed)

Every MCP tool maps to a REST endpoint. For quick operations when no LLM is
available:

```bash
# List kanban cards
curl -s localhost:9090/api/v1/cards?limit=50 | jq .

# Create a card
curl -s -X POST localhost:9090/api/v1/cards \
  -H 'Content-Type: application/json' \
  -d '{"title":"Fix the thing","column":"backlog","project":"workshop","card_type":"bug","priority":"P1"}'

# Move a card
curl -s -X POST localhost:9090/api/v1/cards/123/move \
  -H 'Content-Type: application/json' \
  -d '{"column":"in_progress"}'

# List sessions
curl -s localhost:9090/api/v1/sessions | jq .

# List panes in a session
curl -s localhost:9090/api/v1/sessions/workshop/panes | jq .

# Capture pane output
curl -s localhost:9090/api/v1/panes/workshop:1.1/capture | jq .

# Send keys to a pane
curl -s -X POST localhost:9090/api/v1/panes/workshop:1.1/send-keys \
  -H 'Content-Type: application/json' \
  -d '{"keys":"echo hello\n"}'

# Activity log
curl -s localhost:9090/api/v1/activity?limit=20 | jq .

# Channel publish
curl -s -X POST localhost:9090/api/v1/channels/general/publish \
  -H 'Content-Type: application/json' \
  -d '{"body":"manual message","sender":"james"}'
```

## Switchover checklist

1. Confirm Workshop service is running: `systemctl --user status workshop.service`
2. Confirm API is healthy: `curl -s localhost:9090/api/v1/sessions | head -1`
3. Launch fallback tool in a Workshop tmux pane (so `$TMUX_PANE` is set)
4. Verify MCP tools load (ask the agent to list tools or run `kanban_list`)
5. Note: Channel delivery will use compat mode — agents receive messages as
   `[channel:X from:Y] body` typed into their input, not native tags

## When Claude comes back

No switchover needed — just start a new Claude Code session. The MCP subprocess
spawns fresh each time. Workshop's state (kanban, channels, activity) is all in
SQLite and survives across any number of tool switches.
