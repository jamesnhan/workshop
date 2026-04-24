# UI command hub

## Overview
Server → client command bus that lets MCP tools and the backend drive the
frontend: toasts, dialogs, view switching, pane focus, card opening.
Implemented as a dedicated WebSocket message type.

## API surface

Command types (backend → frontend):
- `show_toast {message, kind: info|success|warning|error}`
- `switch_view {view: sessions|kanban|graph|docs|agents|settings}`
- `focus_cell {cellId}`
- `focus_pane {target}`
- `assign_pane {target, cellId?}`
- `open_card {id}`
- `prompt_user {title, message, initialValue?}` — blocking, expects response
- `confirm {title, message, danger?}` — blocking, expects response

Response types (frontend → backend, for blocking commands):
- `prompt_user_response {requestId, value?, cancelled}`
- `confirm_response {requestId, value: bool, cancelled}`

MCP tools wrap these; see [MCP tool surface](mcp-tools.md).

## Invariants

1. Blocking commands have a unique requestId and time out with a cancelled
   response if no client responds within the timeout.
2. Toasts stack and auto-dismiss based on their kind's configured duration.
3. `switch_view` does not affect other view state (e.g. docsOpen stays true
   under the hood even when the active view is kanban).
4. `focus_cell` clears the cell's pane status indicator (since the user is
   now looking at it).

## Known edge cases

- **Multiple clients**: only one client can respond to a blocking command.
  Current behavior: first response wins.
- **Client disconnect mid-prompt**: request times out; MCP tool gets a
  cancelled result.
- **Stacked toasts**: nextToastId counter guarantees unique keys even on
  rapid bursts.

## Test matrix

Legend: ✅ covered, ◻ planned.

### Backend UICommandHub

| # | Scenario | Unit | Integration | Status | Notes |
|---|----------|------|-------------|--------|-------|
| 1 | Send broadcasts ui_command without id | ✅ | | done | |
| 2 | SendAndWait resolves on Resolve | ✅ | | done | |
| 3 | SendAndWait propagates cancelled response | ✅ | | done | |
| 4 | SendAndWait returns ErrUITimeout | ✅ | | done | |
| 5 | Resolve on unknown id returns false | ✅ | | done | |
| 6 | Resolve after timeout returns false | ✅ | | done | |
| 7 | Concurrent SendAndWait gets distinct ids | ✅ | | done | |

### Frontend Toast

| # | Scenario | Unit | Status | Notes |
|---|----------|------|--------|-------|
| 8 | Renders every toast in list | ✅ | done | |
| 9 | Auto-dismisses after 4 seconds | ✅ | done | fake timers |
| 10 | Click dismisses immediately | ✅ | done | |
| 11 | Applies kind-specific class | ✅ | done | |

### Frontend ConfirmDialog

| # | Scenario | Unit | Status | Notes |
|---|----------|------|--------|-------|
| 12 | Hidden when open=false | ✅ | done | |
| 13 | Confirm click fires onConfirm | ✅ | done | |
| 14 | Cancel click fires onCancel | ✅ | done | |
| 15 | Enter triggers confirm | ✅ | done | |
| 16 | Escape triggers cancel | ✅ | done | |
| 17 | Overlay click triggers cancel | ✅ | done | |
| 18 | Danger flag adds danger class | ✅ | done | |
| 19 | Prompt kind renders input with initialValue | ✅ | done | |
| 20 | Prompt confirm passes current input value | ✅ | done | |
| 21 | Prompt Enter inside input submits | ✅ | done | |

### Planned

| # | Scenario | Unit | Integration | Status | Notes |
|---|----------|------|-------------|--------|-------|
| 22 | Blocking command full MCP → client roundtrip | | ◻ | planned | needs ws test client |
| 23 | focus_cell clears status on arrival | ✅ | | ◻ planned | needs App.tsx test harness |

Backend coverage landed in `internal/server/ui_test.go`. Frontend in
`frontend/src/components/Toast.test.tsx` and
`frontend/src/components/ConfirmDialog.test.tsx`.
