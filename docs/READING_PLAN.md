# Workshop Codebase Reading Plan

A structured guide to reading the entire Workshop codebase from scratch.
Assumes familiarity with Go, TypeScript, React, and tmux concepts.

**Total scope:** ~22,500 lines Go + ~11,000 lines TypeScript (including tests)
**Estimated time:** 8-10 hours across 3 sessions

---

## Session 1: Foundation (3-3.5 hours)

### Phase 1: Entry Point and Server Wiring (30 min)

**Goal:** Understand how a single binary boots into either a web server or an
MCP subprocess, and how every subsystem gets wired together.

| File | Lines | Focus |
|------|-------|-------|
| `main.go` | 1-67 | The `os.Args[1]` switch on line 21-29 — two processes live in one binary |
| `internal/server/server.go` | 1-265 | The `New()` constructor is the entire wiring diagram |

**What to focus on in `server.go`:**
- Lines 108-152: `New()` builds every subsystem in order. Trace the dependency
  chain: `db` -> `bridge` -> `outputBuffer` -> `recorder` -> `statusStore` ->
  `paneMonitor` -> `uiHub` -> `channelHub` -> `API`.
  Every runtime component is created here and nowhere else.
- Lines 24-79: The adapter types (`channelHubAdapter`, `uiHubAdapter`).
  These exist solely to break an import cycle between `server` and `api/v1`.
  The pattern is: `server` owns the real implementation, `api/v1` defines a
  small interface, and these adapters bridge the two.
- Lines 200-227: The `StripPrefix` + `otelhttp` layering. OTel wrapping goes
  *inside* StripPrefix so `r.Pattern` has the real route pattern. This was a
  multi-commit bug fix — the comments explain why.
- Lines 229-234: WebSocket gets its own mux entry, separate from the API.
  In headless mode, it proxies to the desktop.
- Lines 237-243: Auth middleware wraps the entire mux as the outermost layer.

**Gotchas:**
- The `embed.FS` for the frontend is declared in `main.go` (line 18) and
  threaded all the way through `server.New()` to the SPA handler. The compiled
  binary literally contains the React app.
- `headless` mode (line 109) disables tmux, pane monitor, and uses native-only
  channel delivery. This is the K8s codepath — all tmux operations get proxied
  back to the desktop via `WORKSHOP_TMUX_PROXY_URL`.

**Checkpoint questions:**
1. If you set `WORKSHOP_HEADLESS=true`, which subsystems are skipped or degraded?
2. Why are there adapter structs instead of passing the hub directly to `apiv1.New()`?
3. What determines whether Ollama endpoints come from env vars vs Lua config?

---

### Phase 2: tmux Bridge (30 min)

**Goal:** Understand how Go talks to tmux, and why the Bridge interface exists.

| File | Lines | Focus |
|------|-------|-------|
| `internal/tmux/tmux.go` | 1-349 | The full Bridge interface and ExecBridge implementation |
| `internal/tmux/agent.go` | 1-80 | Agent launching entry point |

**What to focus on in `tmux.go`:**
- Lines 32-53: The `Bridge` interface. Every tmux operation goes through this.
  There are two implementations: `ExecBridge` (shells out to `tmux`) and
  `NoBridge` (returns errors/empty results for headless mode).
- Lines 78-82: `run()` — every tmux command goes through `CombinedOutput()`.
  No streaming, no long-running connections. Just exec-and-parse.
- Lines 84-119: `ListSessions()` — the format string
  `#{session_name}\t#{session_windows}\t...` is tmux's printf-style output.
  Lines 105-108 filter internal sessions (`workshop-ctrl-*`). The
  `ListAllSessions()` variant on line 122 includes them.
- Lines 210-241: Three different `SendKeys` methods — `SendKeys` (appends
  Enter), `SendKeysLiteral` (`-l`, no Enter), `SendKeysHex` (`-H`, raw bytes).
  The distinction matters for terminal key forwarding vs typing commands.
- Lines 316-349: `ListPanes` uses `-s` flag (list all panes across all windows
  in session) and appends `:` to the target to force session interpretation.

**What to focus on in `agent.go`:**
- Lines 24-36: `AgentConfig` struct — `Isolation: "worktree"` creates a git
  worktree for the agent. `DangerousSkipPermissions` skips trust prompts.
- Lines 48-80: `LaunchAgent()` validates model names against a regex (line 54,
  security measure), builds provider-specific commands, optionally creates
  worktrees.

**Gotchas:**
- `SendKeys` appends "Enter" automatically (line 212). `SendKeysLiteral` does
  not (line 220). Mixing these up would double-send or fail to send Enter.
- The trailing colon in `ListPanes` (line 320: `session+":"`) is critical.
  Without it, a session named "1" would be interpreted as window index 1.

**Checkpoint questions:**
1. What happens when you call `bridge.SendKeys()` in headless mode?
2. How does agent isolation via worktree work at the tmux level?
3. Why does `SendKeysHex` space-separate hex pairs instead of passing them raw?

---

### Phase 3: Database Schema and Data Model (40 min)

**Goal:** Understand every table, what data lives where, and the migration
strategy.

| File | Lines | Focus |
|------|-------|-------|
| `internal/db/db.go` | 1-400 | Schema, migrations, type definitions |

**What to focus on:**
- Lines 19-38: The `Card` struct is the core kanban entity. Note `ParentID`
  for hierarchy, `Archived` for hiding done cards, and `PaneTarget` for
  linking cards to tmux panes.
- Lines 42-77: Workflow types. `DefaultWorkflow` defines the standard
  backlog->in_progress->review->done flow. `TransitionGate` on line 49 is
  the refinement gate — it blocks column transitions unless the card has a
  description or checklist.
- Lines 79-99: `Open()` — WAL mode (line 90), foreign keys enabled (line 92),
  then `migrate()`. These two PRAGMAs are easy to miss but critical.
- Lines 106-400: The `migrate()` function. This is the entire schema.
  **No migration files** — it's a single function using `CREATE TABLE IF NOT
  EXISTS` and `ALTER TABLE ADD COLUMN` (which silently no-ops if the column
  already exists). This is a pragmatic approach for a single-user app but
  would not scale to teams.

**Table inventory (trace through migrate):**
1. `cards` — kanban cards (line 108)
2. `recordings` / `recording_frames` — terminal recordings (lines 135, 157)
3. `card_notes` — append-only notes on cards (line 148)
4. `card_log` — audit trail of card changes
5. `channel_subscriptions` / `channel_messages` — inter-pane messaging
6. `workflows` — per-project column configs
7. `card_messages` — threaded chat on cards
8. `card_dependencies` — blocker/blocked DAG
9. `activity_log` — agent action audit
10. `approval_requests` — blocking approve/deny
11. `agent_presets` — named agent configurations
12. `agent_usage` — token/cost tracking
13. `ollama_conversations` — persistent local LLM chats

**Gotchas:**
- The migration uses `d.db.Exec()` without checking errors for ALTER TABLE
  (lines 127-132). This is intentional — `ADD COLUMN` on an existing column
  returns an error, which is silently ignored.
- `card_dependencies` has a UNIQUE constraint on `(blocker_id, blocked_id)`
  but cycle detection happens at the API layer, not in SQL.
- `auto-archive` migration on line 132: existing done cards get archived on
  every startup. Idempotent but worth knowing.

**Checkpoint questions:**
1. What's the difference between `card_notes` and `card_messages`?
2. How are workflow transitions enforced — at the DB level or API level?
3. If you delete a card, what happens to its notes and dependencies?

---

### Phase 4: REST API Surface (45 min)

**Goal:** Map every route to its handler and understand the API's shape.

| File | Lines | Focus |
|------|-------|-------|
| `internal/api/v1/routes.go` | 1-262 | All route registrations and interface definitions |
| `internal/api/v1/sessions.go` | all | tmux session CRUD |
| `internal/api/v1/kanban.go` | all | Card CRUD, move logic, dependency management |
| `internal/api/v1/channels.go` | all | Channel pub/sub and native listener |

**What to focus on in `routes.go`:**
- Lines 14-55: Interface definitions. The API layer depends on interfaces, not
  concrete types. `OutputSearcher`, `Recorder`, `StatusManager`, `UIHub`,
  `ChannelHubAPI` — these are the seams where you'd swap implementations for
  testing.
- Lines 79-93: The `API` struct holds all dependencies. No globals.
- Lines 114-125: `tmuxHandler()` — a clever routing trick. If a tmux proxy is
  configured, ALL tmux-dependent routes redirect to the proxy handler. Otherwise
  they use the normal handler (which 503s in headless mode). This is how K8s
  Workshop forwards tmux operations to the desktop.
- Lines 127-261: Route registration. Note the pattern: `mux.HandleFunc("METHOD
  /path", handler)` uses Go 1.22+ method-in-pattern routing.

**Route groups to understand (skim handlers, don't deep-read):**
1. **Sessions** (131-143): CRUD + send-keys + capture — the tmux control plane
2. **Kanban** (147-164): Full CRUD + move + notes + messages + log + dependencies
3. **Channels** (200-208): Pub/sub + native listener long-poll
4. **UI Control** (210-218): Agents drive the frontend via these endpoints
5. **Ollama** (231-240): Local LLM with persistent conversations

**Gotchas:**
- `handleUIAction` (lines 210-217) takes a boolean parameter — `true` means
  "blocking" (prompt_user, confirm wait for frontend response), `false` means
  fire-and-forget (toast, switch_view).
- The channel listener endpoint (line 205: `GET /channel-listen/{target}`) is
  an NDJSON long-poll, not a WebSocket. Each MCP subprocess opens one of these.
- There are two separate `/cards/{id}/notes` and `/cards/{id}/messages`
  endpoints. Notes are agent-written context; messages are chat comments.

**Checkpoint questions:**
1. Which routes get proxied in headless mode and which run locally?
2. What's the difference between `POST /ui/show_toast` and `POST /ui/prompt_user`?
3. How does the frontend know about a new approval request?

---

### Phase 5: WebSocket and PTY Architecture (45 min)

**Goal:** Understand the owned-PTY model — the most architecturally distinctive
part of Workshop. This is how terminal output gets from tmux to the browser.

| File | Lines | Focus |
|------|-------|-------|
| `internal/server/ws.go` | 1-200 | WebSocket handler, PTY management, resize coordination |
| `internal/server/buffer.go` | 1-50 | Ring buffer for searchable output |
| `internal/server/status.go` | 1-50 | Pane status pub/sub |
| `internal/server/pane_monitor.go` | 1-50 | Background approval detection |

**What to focus on in `ws.go`:**
- Lines 31-36: `paneSession` — each subscribed pane gets its own PTY running
  `tmux attach`. This is the "owned-PTY" architecture. The Go process owns the
  master side of the PTY and reads raw terminal bytes.
- Lines 38-41: `clientSize` — multiple browser clients can connect. The
  smallest client's dimensions win (like native tmux behavior).
- Lines 42-66: `wsHandler` closure captures shared size state. `smallestSize()`
  computes the minimum across all connected clients for a pane.
- Lines 112-143: The writer goroutine pattern. A single goroutine owns all
  writes to the WebSocket. Other goroutines send to `outCh` (buffered 256).
  `wsSend` drops messages under backpressure rather than blocking.
- Lines 146-154: On connect, send current pane statuses (catch-up).
- Lines 171-181: Cleanup kills all PTY processes on disconnect.
- Lines 184-199: Background goroutine captures scrollback every 1 second for
  the search buffer. This is separate from the live PTY output stream.

**What to focus on in `buffer.go`:**
- Ring buffer per pane (lines 15-20). Stores both ANSI-stripped text (for
  search) and raw text (for rendering matches). Fixed size of 10,000 lines.

**What to focus on in `pane_monitor.go`:**
- Lines 17-26: Polls every 3 seconds, captures visible pane content, pattern-
  matches for approval/permission dialogs. Auto-sets yellow status on panes
  that are blocked waiting for user input.
- Lines 39-48: `MarkSeen` prevents duplicate `session_created` events when a
  handler already broadcast the event.

**Gotchas:**
- The PTY process is `tmux attach -t <target>` — not a direct shell. So
  resizing the PTY resizes the tmux pane, which is then seen by the process
  inside tmux.
- Backpressure handling (line 138-142): messages are DROPPED, not queued
  indefinitely. A slow client will miss output frames. The client recovers
  via the periodic scrollback capture.
- Multiple browser tabs connecting to the same pane will shrink the tmux pane
  to the smallest tab's size. When a tab disconnects, the size re-expands
  (lines 84-107).

**Checkpoint questions:**
1. What process does the PTY actually run?
2. If the WebSocket disconnects, what happens to the PTY processes?
3. How does a search query find results — does it search tmux directly or a buffer?
4. What triggers a yellow status indicator without the agent calling set_pane_status?

---

## Session 2: Application Layer (3-3.5 hours)

### Phase 6: MCP Server and Tool Registration (45 min)

**Goal:** Understand how Claude Code discovers and calls Workshop's tools.

| File | Lines | Focus |
|------|-------|-------|
| `internal/mcp/mcp.go` | 1-100 | Server setup, OTel tracing wrapper, channel listener |
| `internal/mcp/mcp.go` | 100-400 | Tool registration pattern (skim for shape) |
| `internal/mcp/mcp.go` | rest | Handler implementations (reference, don't memorize) |

**What to focus on:**
- Lines 26-48: The `traced()` wrapper. Every MCP tool handler is wrapped with
  OTel tracing. The pattern is `s.AddTool(toolDef, traced("toolName", handler))`.
  This is how all 35 tools get traced uniformly.
- Lines 50-99: `Serve()` — the MCP subprocess entry point. It creates its own
  `Bridge` (headless or exec), initializes OTel with service name `workshop-mcp`,
  resolves `$TMUX_PANE` to a session:window.pane target, and starts the channel
  listener.
- Lines 85-95: The `WithExperimental` option registers the `claude/channel`
  capability, which is how Claude Code knows this MCP server can receive
  channel notifications.
- Line 99: `runChannelListener` — a background goroutine that long-polls the
  Workshop server's `/channel-listen/{target}` endpoint and emits MCP notifications.

**Tool registration pattern (skim lines 100-400):**
Each tool follows the same template:
```go
s.AddTool(mcp.NewTool("tool_name",
    mcp.WithDescription("..."),
    mcp.WithString("param", mcp.Description("..."), mcp.Required()),
), traced("tool_name", func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    // Parse params from req.Params.Arguments
    // Call the Workshop REST API via HTTP
    // Return mcp.NewToolResultText(...)
}))
```
Note: MCP handlers call the Workshop REST API over HTTP (via `WORKSHOP_API_URL`).
They do NOT import internal packages. The MCP subprocess and the main server
are separate processes communicating over HTTP.

**Gotchas:**
- `mcpPaneTarget` (line 26) is a package-level var, not passed through context.
  This is fine because each MCP subprocess serves exactly one pane.
- The MCP subprocess does NOT share the server's DB connection. It's a separate
  process that calls the REST API. Don't look for direct DB access in handler
  code.
- In headless mode, MCP tools that need tmux are simply not registered (the
  tool list is shorter).

**Checkpoint questions:**
1. How does the MCP subprocess know which pane it belongs to?
2. Why do MCP handlers make HTTP calls instead of using Go interfaces directly?
3. What happens when a channel message arrives for this pane?

---

### Phase 7: Channel Hub and Inter-Agent Messaging (30 min)

**Goal:** Understand how agents in different panes communicate.

| File | Lines | Focus |
|------|-------|-------|
| `internal/server/channels.go` | 1-60 | Hub architecture, delivery modes |
| `internal/server/channel_delivery_sendtext.go` | all | Compat delivery strategy |
| `internal/api/v1/channels.go` | all | REST endpoints + NDJSON listener |

**What to focus on:**
- `channels.go` lines 16-43: The `ChannelHub` struct. It owns: a DB handle
  (for persistence), a delivery strategy, and a `listeners` map for native
  delivery. The `mode` field selects between compat/native/auto.
- Lines 46-60: The three delivery modes. "Auto" probes whether the target has
  a native listener registered (meaning an MCP subprocess is connected), and
  falls back to compat (send_text) if not.
- The NDJSON listener endpoint (`channel-listen/{target}`) is how MCP
  subprocesses receive messages. It's a long-lived HTTP connection that streams
  JSON objects, not WebSocket.

**Data flow for a channel message:**
1. Agent A calls `channel_publish` MCP tool
2. MCP subprocess POSTs to `/api/v1/channels/publish`
3. `ChannelHub.Publish()` persists the message, finds subscribers
4. For each subscriber: checks delivery mode
   - Native: pushes to the subscriber's listener channel (in-memory)
   - Compat: types `[channel:X from:Y] body` into the target pane via send_text
5. The MCP subprocess's `runChannelListener` goroutine reads from the NDJSON
   stream and emits a `notifications/claude/channel` MCP notification

**Checkpoint questions:**
1. What's the difference between native and compat channel delivery?
2. If an agent isn't running an MCP subprocess, can it still receive channel messages?
3. Where are channel subscriptions persisted?

---

### Phase 8: Frontend Architecture (60 min)

**Goal:** Understand the React app's structure, state management, and key
components.

| File | Lines | Focus |
|------|-------|-------|
| `frontend/src/types.ts` | 1-60 | Core types: GridCell, LayoutState, PaneInfo |
| `frontend/src/hooks/useWebSocket.ts` | all | WS connection, message dispatch |
| `frontend/src/App.tsx` | 1-100 | State declarations, hook wiring |
| `frontend/src/components/PaneViewer.tsx` | 1-60 | xterm.js integration pattern |
| `frontend/src/components/PaneGrid.tsx` | all | Grid layout engine |
| `frontend/src/components/Sidebar.tsx` | all | Session tree + hover previews |

**What to focus on in `types.ts`:**
- Lines 1-24: `GridCell` is the fundamental unit. Each cell has a `target`
  (tmux pane), `tabs` (multiple panes per cell), `history` (back/forward
  navigation), and grid position (`row`, `col`, `rowSpan`, `colSpan`).
- Lines 18-24: `LayoutState` is the entire UI state — grid dimensions, cells,
  focus, maximized cell.

**What to focus on in `useWebSocket.ts`:**
- Lines 35-37: Single WebSocket connection for the entire app.
- Lines 54-61: Connection URL includes auth token from localStorage.
- Lines 62-76: On reconnect, re-subscribes to all previously subscribed panes
  and fires `onReconnect` to clear stale terminal state.
- Lines 86-93: Exponential backoff with jitter (1s, 2s, 4s... capped at 30s).
- Lines 96-127: Message dispatch switch. Types: `output`, `pane_status`,
  `pane_status_clear`, `open_doc`, `session_created`, `activity`,
  `approval_request`, `ui_command`.

**What to focus on in `App.tsx`:**
- Lines 53-54: The WebSocket hook destructure — this is where all real-time
  state flows into the app.
- Lines 55-99: ~45 `useState` declarations. This is a lot of top-level state.
  The key pieces: `layout` (grid), `paneStatuses`, `allPanes`, `switcherOpen`,
  `kanbanOpen`, etc. Note the pattern: each "view" (kanban, docs, agents,
  activity, settings, graph, ollama) has its own `*Open` boolean.
- This component is 1568 lines. It's the god component. All keyboard shortcuts,
  view switching, and cross-component coordination live here.

**What to focus on in `PaneViewer.tsx`:**
- Lines 1-14: The `PaneViewerHandle` interface — what the parent can call on
  a terminal instance (`write`, `focus`, `searchInTerminal`, `refit`).
- Lines 39-60: Props include callbacks for ticket hover, URL hover, commit
  hover, hash key — the terminal is interactive, not just a display.

**Gotchas:**
- `App.tsx` is a single 1568-line component. All state management is
  `useState` — no Redux, no Zustand, no context providers (beyond what hooks
  provide). This is a deliberate simplicity choice but means props drill deep.
- `useWebSocket` stores handler callbacks in refs (lines 40-48) to avoid
  re-creating the WebSocket effect when handlers change. This is a standard
  but non-obvious React pattern.
- The `activity` and `approval_request` message types (lines 112-115) dispatch
  via `window.dispatchEvent` instead of callback refs — a different pattern
  used for components that aren't direct children of App.

**Checkpoint questions:**
1. What happens to terminal state when the WebSocket reconnects?
2. How does the grid layout handle merged cells?
3. Where do keyboard shortcuts get registered?
4. How does a pane's status indicator (green/yellow/red) get from the server to
   the status bar?

---

## Session 3: Specialized Systems and Integration (2-3 hours)

### Phase 10: Supporting Server Components (30 min)

**Goal:** Fill in the remaining server-side pieces.

| File | Lines | Focus |
|------|-------|-------|
| `internal/server/auth.go` | all | API key middleware |
| `internal/server/spa.go` | all | Embedded frontend serving |
| `internal/server/recorder.go` | all | Terminal recording manager |
| `internal/server/approval_hub.go` | all | Blocking approval pattern |
| `internal/server/ui.go` | all | UI command hub (fire-and-forget + blocking) |
| `internal/server/ws_proxy.go` | all | WebSocket proxy for headless mode |

**Key patterns to look for:**
- `auth.go`: How health endpoints are exempted from auth (needed for K8s probes)
- `approval_hub.go`: The blocking request/response pattern. An agent calls
  `request_approval`, the hub parks a goroutine, the frontend resolves it
  via REST, and the parked goroutine wakes up. 10-minute timeout.
- `ui.go`: Same blocking pattern but for `prompt_user`/`confirm`. The hub
  generates a unique ID, sends a WebSocket message, and waits for the
  frontend to POST back the response.
- `ws_proxy.go`: TCP-level WebSocket proxying for headless mode.

---

### Phase 11: Frontend Components (60 min)

**Goal:** Understand the major UI components. Read for shape, not every line.

| File | Lines | Skim for |
|------|-------|----------|
| `frontend/src/components/KanbanBoard.tsx` | 841 | Drag-and-drop, column rendering, card expansion |
| `frontend/src/components/AgentDashboard.tsx` | 293 | Agent status polling, chibi avatars, launch UI |
| `frontend/src/components/DocsView.tsx` | 389 | Markdown rendering, search, pinned docs |
| `frontend/src/components/ActivityView.tsx` | 335 | Activity feed, tree view, approval UI |
| `frontend/src/components/OllamaChat.tsx` | 639 | Local LLM chat interface |
| `frontend/src/components/DependencyGraph.tsx` | 312 | DAG visualization |
| `frontend/src/hooks/useLayoutPersistence.ts` | all | localStorage save/restore, workspaces |
| `frontend/src/api/client.ts` | all | HTTP client with auth header injection |

**What to look for in each component:**
- How it fetches data (usually `get()` from `api/client.ts`)
- What WebSocket events it listens for
- Whether it has local state vs relying on App.tsx props

---

### Phase 12: Configuration and Deployment (30 min)

**Goal:** Understand how Workshop runs in production.

| File | Lines | Focus |
|------|-------|-------|
| `internal/config/` | all | Lua config engine (gopher-lua) |
| `internal/telemetry/` | all | OTel bootstrap |
| `internal/ollama/ollama.go` | all | Multi-endpoint Ollama client |
| `Makefile` | all | Build targets, deploy flow |

**Checkpoint questions:**
1. What does `make deploy` actually do?
2. How does OTel get enabled/disabled?
3. Where does the Lua config file live on disk?

---

### Phase 13: Tests (30 min)

**Goal:** Understand the test patterns, not every test case.

| File | Focus |
|------|-------|
| `internal/api/v1/kanban_test.go` | How API tests set up: `testhelpers.TempDB()`, httptest patterns |
| `internal/server/channels_test.go` | How server-level tests mock the Bridge |
| `internal/mcp/handlers_test.go` | How MCP handlers are tested (HTTP round-trip) |
| `frontend/src/components/DependencyGraph.test.tsx` | Vitest + React Testing Library patterns |
| `internal/testhelpers/` | Shared fixtures: TempDB, NewGitRepo |

**What to look for:**
- `testhelpers.TempDB()` creates a throwaway SQLite for each test
- API tests use `httptest.NewRecorder()` — standard Go HTTP testing
- MCP tests spin up a real HTTP server and have the MCP handler call it
- Frontend tests use `@testing-library/react` with `vi.fn()` mocks

---

## Quick Reference: Architecture Mental Model

```
Browser (React SPA)
    |
    |--- WebSocket (/ws) --- PTY --- tmux attach --- [pane processes]
    |--- REST API (/api/v1/*)
    |
Go Server (single binary)
    |--- StatusStore (pane green/yellow/red)
    |--- OutputBuffer (ring buffer per pane for search)
    |--- ChannelHub (inter-pane pub/sub)
    |--- UICommandHub (agent-driven UI control)
    |--- ApprovalHub (blocking approve/deny)
    |--- RecordingManager (terminal recordings)
    |--- PaneMonitor (background approval detection)
    |--- SQLite (kanban, channels, activity, usage, recordings)
    |
MCP Subprocess (one per Claude Code pane)
    |--- Calls REST API over HTTP
    |--- Long-polls /channel-listen/{target} for messages
    |--- Emits claude/channel MCP notifications
```

**Key insight:** The MCP subprocess and the web server are **separate
processes** from the same binary. They communicate via HTTP. The MCP
subprocess does not have direct access to the server's in-memory state.
