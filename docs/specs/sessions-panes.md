# Sessions & panes

## Overview
Owned-PTY tmux bridge: Workshop spawns and manages tmux sessions, wires each
pane to an owned PTY, and streams output over WebSocket to the frontend.
Panes are subscribed lazily per client.

NOT covered here: agent launching on top of panes (see `internal/api/v1/agents.go` for the REST endpoint that spawns new agent sessions; there is no dispatch/tracking layer anymore).

## Data model
- In-memory only on the backend (no DB table for panes).
- Frontend: `LayoutState` persisted to localStorage by `useLayoutPersistence`.

## API surface

REST:
- `GET /sessions`, `POST /sessions`, `DELETE /sessions/{name}`, `PATCH /sessions/{name}`
- `GET /panes`, `GET /sessions/{name}/panes`
- `POST /sessions/{name}/windows`, `POST /panes/{target}/split`
- `POST /panes/{target}/resize` body `{cols, rows}`
- `POST /panes/{target}/input`, `POST /panes/{target}/keys`
- `GET /panes/{target}/capture`
- `POST /panes/status`, `DELETE /panes/status` — status dot tracking

WebSocket:
- `subscribe` / `unsubscribe` per target
- `output` frames (server → client)
- `input`, `resize` (client → server)
- `reconnect` marker
- Pane status updates

MCP tools: `list_sessions`, `list_panes`, `create_session`, `kill_session`,
`rename_session`, `send_keys`, `send_text`, `capture_pane`, `split_window`,
`create_window`, `search_output`.

## Invariants

1. Exactly one PTY per live tmux pane owned by Workshop.
2. Subscribing a target emits a screen dump so the client sees current state.
3. Unsubscribing stops output delivery to that client but keeps the PTY alive.
4. Resize messages from a client are propagated to the PTY immediately.
5. Killing a session detaches all subscribers and cleans up PTYs.
6. Pane status is server-side; clients display a dot based on latest status.

## Known edge cases

- **Reconnect drift**: after WebSocket reconnect, pane dimensions may have
  changed. Frontend calls `forceResize()` on all viewers (#441 regression).
- **Mis-sized panes on focus**: focusing a cell forces a resize push even
  when cols/rows haven't changed, so tmux reflows if it silently drifted
  (#441 regression).
- **Subscribe race**: if a client subscribes while the initial dump is still
  streaming, it should still get a complete screen state.
- **Hidden sessions**: sessions starting with a configured prefix (e.g.
  `⚙`) are styled as hidden in the sidebar but still managed normally.

## Test matrix

Legend: ✅ covered, ◻ planned.

### Tmux bridge (`internal/tmux/`)

| # | Scenario | Unit | Status |
|---|----------|------|--------|
| 1 | ListSessions parses tab-delimited output | ✅ | done |
| 2 | ListSessions hides internal sessions (workshop-ctrl-*) | ✅ | done |
| 3 | ListSessions empty output returns nil | ✅ | done |
| 4 | ListSessions "no server running" is not an error | ✅ | done |
| 5 | ListAllSessions includes hidden flag | ✅ | done |
| 6 | CreateSession without start dir | ✅ | done |
| 7 | CreateSession with start dir adds -c flag | ✅ | done |
| 8 | CreateSession propagates tmux error + output | ✅ | done |
| 9 | KillSession passes -t flag | ✅ | done |
| 10 | KillSession error includes tmux output | ✅ | done |
| 11 | RenameSession args | ✅ | done |
| 12 | RenameWindow args | ✅ | done |
| 13 | CreateWindow with name adds -n flag | ✅ | done |
| 14 | CreateWindow without name omits -n | ✅ | done |
| 15 | SplitWindow vertical passes -v | ✅ | done |
| 16 | SplitWindow horizontal passes -h | ✅ | done |
| 17 | SendKeys appends Enter | ✅ | done |
| 18 | SendKeysLiteral passes -l flag | ✅ | done |
| 19 | SendKeysHex formats as spaced hex pairs | ✅ | done |
| 20 | CapturePane requests scrollback with -S flag | ✅ | done |
| 21 | CapturePanePlain converts \n to \r\n for xterm | ✅ | done |
| 22 | CapturePaneVisible omits -S flag | ✅ | done |
| 23 | CapturePaneAll uses -S - for full history | ✅ | done |
| 24 | ResizePane passes -x / -y flags | ✅ | done |
| 25 | ListPanes parses full tab-delimited record | ✅ | done |
| 26 | ListPanes appends trailing colon to numeric session name | ✅ | done |
| 27 | PaneTTY returns tmux output | ✅ | done |

### Planned

| # | Scenario | Layer | Status |
|---|----------|-------|--------|
| 28 | forceResize fires on unchanged dims (#441) | Frontend unit (PaneViewer) | ◻ planned (xterm mocking investment) |
| 29 | Layout persistence save/load roundtrip | Frontend unit | covered in #486 |
| 30 | Subscribe emits initial dump via WS | Integration | ◻ planned |
| 31 | Resize WS message propagates to tmux | Integration | ◻ planned |
| 32 | Output buffer replay on reconnect | Integration | ◻ planned |

Backend coverage landed in `internal/tmux/tmux_bridge_test.go` (27 tests)
alongside the existing 2 tests in `internal/tmux/tmux_test.go`. The new
`scriptedRunner` helper covers every tmux subcommand via a map of
scripted stdout + per-subcommand error flags, reusable for future
bridge additions.
