# MCP tool surface

## Overview
The Model Context Protocol tool layer exposed by Workshop to Claude Code (and
other MCP clients). Most tools are thin wrappers over the REST API; a few
(prompt_user, confirm, channel listener) are stateful.

Individual tool behaviors are documented in the spec of the area they
belong to — this spec focuses on the contract conventions and cross-cutting
concerns.

## Contract conventions

- Every tool declares: name, description, JSONSchema params, return shape.
- Required params enforced before dispatch.
- Invalid target / missing resource → structured error (not panic).
- Tools are registered in `internal/mcp/` — one file per area.
- The `workshop mcp` subprocess connects per-client, reads `$TMUX_PANE` for its
  pane identity, and registers `claude/channel` for native channel delivery.

## Tools by area

- **Sessions/panes**: `list_sessions`, `list_panes`, `create_session`,
  `kill_session`, `rename_session`, `send_keys`, `send_text`, `capture_pane`,
  `split_window`, `create_window`, `search_output`
- **Kanban**: `kanban_list`, `kanban_create`, `kanban_edit`, `kanban_move`,
  `kanban_delete`, `kanban_add_note`
- **Status**: `set_pane_status`, `clear_pane_status`
- **UI**: `show_toast`, `switch_view`, `focus_cell`, `focus_pane`,
  `assign_pane`, `open_card`, `prompt_user`, `confirm`
- **Channels**: `channel_publish`, `channel_subscribe`, `channel_unsubscribe`,
  `channel_list`, `channel_messages`
- **Docs**: `open_doc`
- **Config**: `run_config`

## Invariants

1. Tool names are stable — renames require a deprecation cycle.
2. Blocking tools (`prompt_user`, `confirm`) return structured responses,
   not panics, on dialog cancel.
3. Tool descriptions match actual parameter behavior — the MCP schema is
   the contract.
4. Errors return structured tool errors; they never leak Go panic traces.

## Test matrix

Legend: ✅ covered, ◻ planned.

### Bridge-backed handlers (fakeBridge)

| # | Scenario | Unit | Status |
|---|----------|------|--------|
| 1 | `list_sessions` returns bridge sessions | ✅ | done |
| 2 | `list_sessions` propagates bridge error | ✅ | done |
| 3 | `list_panes` requires session param | ✅ | done |
| 4 | `list_panes` happy path | ✅ | done |
| 5 | `kill_session` requires name | ✅ | done |
| 6 | `kill_session` happy path | ✅ | done |
| 7 | `send_keys` requires target + command | ✅ | done |
| 8 | `send_keys` happy path | ✅ | done |
| 9 | `send_text` requires target + text | ✅ | done |
| 10 | `send_text` happy path | ✅ | done |
| 11 | `capture_pane` requires target | ✅ | done |
| 12 | `capture_pane` strips ANSI escapes | ✅ | done |
| 13 | `split_window` requires target | ✅ | done |
| 14 | `split_window` happy path | ✅ | done |
| 15 | `create_window` requires session | ✅ | done |
| 16 | `rename_session` requires both names | ✅ | done |

### HTTP-backed handlers (httptest stub + WORKSHOP_API_URL)

| # | Scenario | Unit | Status |
|---|----------|------|--------|
| 17 | `set_pane_status` requires target + status | ✅ | done |
| 18 | `set_pane_status` posts expected payload | ✅ | done |
| 19 | `set_pane_status` surfaces API errors | ✅ | done |
| 20 | `clear_pane_status` requires target | ✅ | done |
| 21 | `clear_pane_status` sends DELETE | ✅ | done |
| 22 | `kanban_create` requires title | ✅ | done |
| 23 | `kanban_create` posts card | ✅ | done |
| 24 | `kanban_list` forwards project filter | ✅ | done |
| 25 | `channel_publish` posts payload | ✅ | done |
| 26 | `channel_subscribe` posts payload | ✅ | done |

### Planned

| # | Scenario | Unit | Status |
|---|----------|------|--------|
| 27 | Every kanban mutation tool (edit/move/delete/add_note) contract | ◻ | planned |
| 29 | UI action tools (show_toast, switch_view, focus_*, open_card) | ◻ | planned |
| 30 | prompt_user / confirm blocking resolution | ◻ | planned |
| 31 | open_doc POSTs to /docs/open | ◻ | planned |

Backend coverage landed in `internal/mcp/mcp_test.go` (26 tests). The
test file also introduces a reusable `fakeBridge` tmux.Bridge stub for
any future tests that need a scriptable bridge without spawning tmux,
and a `withFakeAPI` helper that stands up an httptest.Server + sets
WORKSHOP_API_URL so the HTTP-backed handlers can be exercised in-process.
