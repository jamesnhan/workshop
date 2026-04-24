# Workshop Features

> **Specifications**: detailed per-area specs live in [`docs/specs/`](docs/specs/README.md).
> These are the source of truth for behavior and the contract for tests.

## Core Platform

| Feature | Description |
|---------|-------------|
| Single binary deployment | Go backend with embedded React SPA, one binary to run |
| tmux bridge | Go package wrapping tmux CLI for session/pane management |
| REST API v1 | Versioned API: health, sessions CRUD, pane listing, send-keys, capture |
| WebSocket streaming | Real-time terminal output streaming to browser |
| Owned-PTY architecture | creack/pty running tmux attach per pane for direct PTY I/O |
| Lua config engine | gopher-lua scripting for workspaces, agent presets, startup commands |
| SQLite database | Persistent storage for kanban cards, notes, recordings |
| systemd service | Auto-start on boot with session restore |

## Terminal UI

| Feature | Description |
|---------|-------------|
| xterm.js terminal | Full ANSI rendering in the browser with Nerd Font support |
| Multi-pane grid | Configurable NxM grid layout (up to 16x16) |
| Cell merge/split | Alt+Shift+HJKL to merge adjacent cells, Alt+Shift+S to split |
| Cell maximize | Alt+F to fullscreen a focused cell |
| Vim navigation | Alt+hjkl between cells, Alt+1-9 for direct focus |
| Key interception | Tab, Ctrl+C, Ctrl+V paste, Alt+Backspace, word navigation, `z` to pin hover previews |
| Key translation | xterm.js escape sequences mapped to tmux named keys |
| Pane tabs | Tab bar per cell with Alt+[/] cycling, Alt+W close, middle-click |
| Pane history | Back/forward navigation with Alt+Left/Right |
| Session persistence | Auto-save/restore layout to localStorage |
| Terminal recording | Record and replay terminal sessions (asciinema-style) |

## Navigation & Discovery

| Feature | Description |
|---------|-------------|
| Sidebar | Expandable session/pane tree with hover preview cards |
| Sidebar session actions | Hover a session row for inline rename (✎) and kill (✕) buttons — absolutely positioned, no layout shift |
| Click-to-focus session | Click a session row to jump to the cell already showing that session; chevron still toggles expansion |
| Fullscreen status highlight | When a different pane is maximized, sidebar sessions with hot statuses (yellow/red) get a colored left border + tinted background |
| Collapsed glyph strip | When sidebar is collapsed, a vertical strip of per-session letter glyphs with status-colored borders + corner dots replaces the blank sidebar |
| Collapsible sidebar | Alt+B toggle |
| Ctrl+P fuzzy finder | fzf-powered pane search with vim navigation modes |
| Command palette | Ctrl+Shift+P searchable action list with shortcuts |
| Hotkey menu | ? to see all keyboard shortcuts organized by category |
| Mode tabs | Sessions, Kanban, Graph, Agents, Activity, Docs, Chat, Settings — visual indicator |
| Git info hover preview | Hover a session's git badge in the sidebar for branch, dirty/ahead/behind counts, and the 5 most recent commits — reuses the shared HoverPreview primitive |
| Pin hover preview (z) | Press `z` while any hover preview is visible to pin it globally for screenshots; subtle green glow on all hover types (ticket, link, git commit, git info, usage, pane preview), `z` or `Escape` to unpin |
| Hover viewport clamping | Hover previews flip above the cursor when they would overflow the bottom of the screen |
| Usage progress bars | Status bar shows All Models and Sonnet Only weekly usage bars matching Claude's layout; color thresholds (blue/yellow/red); hover for tooltip with token counts, session count, rolling reset times, and "check /usage for exact" disclaimer |
| Pane output search | Full tmux scrollback, fzf fuzzy matching, 3-mode vim nav (FIND/NAV/PREVIEW) |
| Search preview | ANSI-rendered context with auto-scroll to match |
| Live search refresh | Results update every 3 seconds while panel is open |

## Workspaces

| Feature | Description |
|---------|-------------|
| Named workspaces | Save/restore layout presets |
| Workspace manager | Popover for switch, rename, duplicate, delete |
| Status bar indicator | Shows active workspace name |
| Dirty state indicator | Yellow dot on the workspace trigger when the live layout diverges from the saved snapshot (excludes focus changes — they're transient) |
| Save-on-switch prompt | Loading a different workspace while dirty prompts via themed confirm dialog to save current first |
| Command palette integration | Save/load/delete via Ctrl+Shift+P |

## AI Agent Orchestration

| Feature | Description |
|---------|-------------|
| Agent launcher | Launch AI agents with model/prompt config and trust prompt handling |
| Focused pane indicator | Status bar shows `▶ focused` label; border accent always wins over status color |
| Multi-provider | Claude, Gemini, and Codex support (auto-detected) |
| Provider-aware commands | Correct CLI flags per provider (--yolo, --full-auto, etc.) |
| Trust prompt handling | Auto-dismiss trust/folder prompts for all providers |
| Agent dashboard | Monitor all agents with status (working/idle/needs_input/done/error) |
| Chibi avatars | Animated per-state agent visuals with color variants |
| Session audit | Show/hide control sessions in sidebar |
| Idle detection | Provider-specific prompt pattern matching (Claude/Gemini/Codex) |
| Auto approval detection | Background pane monitor scans every 3s for permission/trust dialogs and auto-sets yellow status, even on unfocused tabs |
| Auto-attach new sessions | Sessions created via sidebar/Ctrl+P land as the active tab; background agent launches land as inactive tabs without stealing focus |
| Agent presets | Named specialist roles (reviewer, tester, security, planner, refactorer, architect) with pre-configured provider, model, and system prompt |
| Supervisor pattern | Any Claude Code session in a Workshop pane acts as a supervisor: delegates work via the native Task tool (isolation: worktree), records results via kanban/activity MCP tools |
| Bypass permissions handling | Trust prompt auto-dismissal for `--dangerously-skip-permissions` mode (Down+Enter to select "Yes, I accept") |
| Approval gates | `request_approval` MCP tool blocks until user approves/denies in the Activity tab; dedicated ApprovalHub survives WS reconnects |

## Activity Feed

| Feature | Description |
|---------|-------------|
| Activity log | SQLite-backed log of agent actions across all panes — file writes, commands, decisions, errors, status updates |
| REST API | `POST /activity` to record events, `GET /activity` with pane/project/action_type filters and `tree=true` mode |
| WebSocket streaming | New activity entries broadcast to all connected clients in real-time |
| Activity view | Dedicated "Activity" tab with filterable, scrollable feed; icons per action type |
| Execution tree view | Activities support `parent_id` for nesting subagent work under parent entries; collapsible tree with child counts |
| Approval queue | Pending approvals render at the top of the Activity view with markdown-rendered details, diff display, and approve/deny buttons |
| `report_activity` MCP tool | Agents self-report significant actions; auto-tags pane target; supports `parent_id` for tree nesting |
| Compaction timeline | Collapsible section in Activity view showing Claude Code context window compaction events with token counts, trigger type, and session slug — parsed from `~/.claude/` session JSONL |
| Telemetry | `workshop_activity_events_total` and `workshop_approval_requests_total` counters |

## Inter-Agent Channels

| Feature | Description |
|---------|-------------|
| Channel pubsub | Named channels with publish/subscribe semantics for inter-pane (inter-agent) messaging |
| Native delivery | `claude/channel` MCP capability — messages arrive as `<channel>` tags in the receiver's context, no input pollution |
| Compat delivery | Fallback that types messages into the receiver's input as `[channel:X from:Y] body`, works with every Claude Code version |
| Auto mode | Default — uses native if a listener is registered for the target pane, falls back to compat seamlessly |
| Per-target listeners | Each Claude Code MCP subprocess detects its `$TMUX_PANE`, registers as a long-poll listener, and emits `notifications/claude/channel` on inbound messages |
| Persistent history | Messages and subscriptions stored in SQLite — late subscribers can query past messages |
| Project-scoped channels | Optional `project` tag namespaces channels per repo/workspace |
| Settings toggle | Delivery mode (auto/compat/native) configurable in Settings → Channels |

## Project Tracking (Kanban)

| Feature | Description |
|---------|-------------|
| Kanban board | Dynamic columns driven by per-project workflow config, with drag-and-drop |
| Structured workflows | Per-project workflow definitions (columns + allowed transitions) stored in SQLite; `GET/PUT /workflows` API; default 4-column fallback |
| Transition validation | `MoveCard` checks the project's workflow before allowing column changes; invalid transitions return HTTP 400 with details |
| Invalid drop highlighting | Dragging a card over a column that isn't an allowed transition shows red dashed outline + faded background |
| Card types & priorities | bug/feature/task/chore, P0-P3 priority levels |
| Card notes | Timestamped notes for tracking progress |
| Project filtering | Auto-filter by active session context |
| Drag-and-drop polish | Visual drag preview, drop indicators, reorder within columns |
| Position densification | `MoveCard` re-densifies positions 0..N-1 on every move, so drop-in-place is a true no-op and reorders don't drift |
| Hierarchical subtasks | Cards can have `parent_id` for nested rendering under a parent |
| MCP integration | Create/edit/move/delete cards from Claude Code |
| Ticket autocomplete | Type `#` in kanban notes, card descriptions, or agent prompts to fuzzy-search tickets |
| Ticket lookup dialog | Press `#` in a focused terminal to pick a ticket; inserts `#id ` directly into the PTY |
| Refinement gates | Workflow transitions can require card content validation (non-empty description, checklist items) before allowing moves |

## Dependency Graph

| Feature | Description |
|---------|-------------|
| Graph view | New top-level "Graph" tab rendering kanban cards as nodes with "blocks" relationships as directed edges, built on react-flow |
| Column-by-status layout | Nodes laid out left-to-right by column: backlog → in_progress → review → done |
| Drag to connect | Drag from one node to another to create a dependency — server rejects cycles |
| Double-click to delete | Double-click an edge (with themed confirm) to remove a dependency |
| Double-click to open | Double-click a node to jump to that card in the Kanban view |
| Cycle detection | Backend BFS rejects self-loops and transitive cycles before inserting |
| Hide Done by default | Filters both nodes AND edges whose endpoints are hidden; toggle in toolbar |
| Interactive minimap | Pannable + zoomable minimap for navigating large graphs |
| Viewport persistence | Pan/zoom position saved per-project in localStorage and restored on next visit |

## Notifications

| Feature | Description |
|---------|-------------|
| Output pattern scanning | Detect task completion, permission prompts, errors |
| Browser notifications | Desktop and mobile push notifications |
| Custom patterns | User-defined regex patterns for notification triggers |
| Notification panel | In-app panel with dismiss/clear, unread badge |
| Mobile support | Permission request banner, works in background tabs |
| Themed toasts | In-app toast notifications with kind variants (info/success/warning/error), surfaceable from agents via `show_toast` |
| Themed dialogs | Reusable confirm/prompt dialog with focus trap, Enter/Escape shortcuts, danger variant — replaces browser popups |

## Documentation

| Feature | Description |
|---------|-------------|
| Markdown viewer | Docs tab with full markdown rendering |
| Pinned documents | Quick access to frequently used docs |
| Cross-view persistence | Last-open doc survives view switches — switching to Kanban/Sessions and back restores the doc you were reading |
| Live preview | Auto-updates when file changes on disk |
| Syntax highlighting | Code blocks rendered with colors |
| Filesystem browser | Browse and open any .md file |
| Copy markdown button | One-click copy of raw markdown source |
| open_doc MCP tool | Open any markdown file in the Docs view from Claude Code |
| Path traversal guard | Read/open endpoints refuse paths outside $HOME and block symlink escapes |

## Settings & Configuration

| Feature | Description |
|---------|-------------|
| Settings view | Dedicated tab for all preferences |
| Theme selector | 10 themes (Catppuccin, Tokyo Night, Workshop family) |
| Preview card size | Small/medium/large sidebar hover previews |
| CapsLock normalization | Hotkeys work regardless of CapsLock state |
| CapsLock indicator | Yellow CAPS badge in status bar |
| Notification permissions | Enable/status in settings |

## MCP Server (Claude Code Integration)

| Feature | Description |
|---------|-------------|
| 40+ MCP tools | Sessions, panes, kanban, agents, search, status, config, docs, channels, UI control, activity, approvals, presets, orchestrator, usage |
| Status indicators | set_pane_status (green/yellow/red) for agent state |
| Kanban from CLI | Create/edit/move cards without leaving the terminal |
| Agent launch | Launch multi-provider agents via MCP |
| Pane capture | Read terminal content for AI analysis |
| UI control tools | `show_toast`, `switch_view`, `focus_cell`, `focus_pane`, `assign_pane`, `open_card` for agents to drive the frontend |
| Interactive dialogs | `prompt_user` (returns typed string) and `confirm` (returns bool) — themed blocking dialogs for agents to ask the user mid-task |
| Channel tools | `channel_publish`, `channel_subscribe`, `channel_unsubscribe`, `channel_list`, `channel_messages` for inter-agent messaging |
| MCP Tool Search compatible | Tools defer-load via Claude Code 2.1.7+'s built-in search, ~85% context savings on large libraries |

## Performance

| Feature | Description |
|---------|-------------|
| WebSocket batching | 16ms debounced output flush (60fps) |
| Single writer goroutine | Serialized WebSocket writes, backpressure dropping |
| Per-buffer search locks | No global lock contention during search |
| DB indexes | Optimized queries for cards, notes, recording frames |
| React optimization | Ref-based unread tracking, memoized DOMPurify |
| Regex caching | Notification patterns precompiled, not per-chunk |
| Capture-pane search | Full scrollback indexed via tmux, not raw PTY |

## Ollama Chat (Local LLM)

| Feature | Description |
|---------|-------------|
| Multi-endpoint Ollama | Route to multiple Ollama instances (4090 desktop, M1 Max) via env var or Lua config |
| Chat UI | Streaming chat with model selector, system prompt, endpoint health badges |
| Persistent conversations | DB-backed conversation history with sidebar — survives navigation and page reloads |
| System prompt per conversation | Each conversation stores its own system prompt and model |
| Auto-continue | Target word count with automatic continuation rounds for long-form generation |
| Repetition detection | Character-level, vocabulary collapse, and trigram detectors abort degenerate output |
| Repetition trimming | Garbage text stripped between rounds and before saving; cuts to last clean sentence |
| Thinking content capture | Falls back to `thinking` field when `content` is empty (models with thinking enabled by default) |
| Unlimited tokens default | `num_predict: -1` by default — local models run until natural completion |
| System prompt passthrough | System prompts properly injected as messages for Chat API |

## Security

| Feature | Description |
|---------|-------------|
| API key authentication | `WORKSHOP_API_KEY` env var — Bearer token required on all `/api/v1/*` and `/ws` endpoints; health exempt for K8s probes |
| Frontend auth gate | Prompts for API key on first visit, validates against API, stores in localStorage |
| Lua config sandboxing | Inline code execution removed; config loading restricted to `~/.config/workshop/` with symlink-aware path validation |
| Agent model validation | Model names validated against safe character regex; shell metacharacters rejected with 422 |
| XSS protection | DOMPurify on all dangerouslySetInnerHTML sites |
| WebSocket origin check | Restricted to localhost (direct) or authenticated via token query param (K8s) |
| Error sanitization | Internal details logged, not exposed to clients |
| Bridge abstraction | No unsafe type assertions |

## Mobile

| Feature | Description |
|---------|-------------|
| Responsive grid | Single column on small screens |
| Touch controls | Touch scrolling in xterm.js terminals |
| Swipe navigation | Navigate between panes |
| Collapsible panels | Mobile-optimized layout |
| Touch drag-and-drop | Kanban cards on mobile |

## Observability (OpenTelemetry)

| Feature | Description |
|---------|-------------|
| OTel SDK bootstrap | `internal/telemetry/` package with TracerProvider + MeterProvider + LoggerProvider, gated by `WORKSHOP_OTEL_ENABLED` env var (default off = zero cost) |
| HTTP instrumentation | otelhttp wraps the REST mux — span per request with method, route, status, duration |
| Business logic tracing | Spans on channel.publish, db.MoveCard, db.AddDependency with rich attributes |
| RED metrics | Counters + gauges for channels, kanban mutations, agent launches, activity events, approval requests, agent token usage/cost |
| Structured log correlation | slog tee handler forwards every log record to Loki via OTel with trace_id/span_id for click-to-trace |
| Frontend web SDK | `@opentelemetry/sdk-trace-web` with fetch auto-instrumentation — traceparent headers chain browser → backend spans |
| MCP subprocess tracing | Every tool call produces a `mcp.<tool_name>` span with pane target, linked to backend via traceparent |
| Body scrubbing | Channel/prompt bodies over 256 chars are truncated in span attributes (configurable via `WORKSHOP_OTEL_SCRUB_BODIES`) |
| LGTM stack target | Direct OTLP/HTTP export to self-hosted Grafana LGTM (Tempo + Mimir + Loki + Grafana) on Kubernetes |

## Testing & Specs

| Feature | Description |
|---------|-------------|
| Spec-driven development | Per-area specs under `docs/specs/` are the source of truth for behavior; follows plan → spec → tests → build |
| Spec test matrices | Every spec file carries a test matrix listing what's covered and what's planned |
| Backend test suite | 253 Go tests using testify; `internal/testhelpers/` provides `TempDB`, `TempDataDir`, `NewGitRepo` fixtures |
| Frontend test suite | 51 Vitest + React Testing Library tests with jsdom environment and localStorage cleanup |
| Pre-push hook | `.githooks/pre-push` runs `make test-unit` + `make test-frontend` before every push; install once via `make install-hooks` |
| Coverage reports | `make test-cover` writes HTML coverage under `coverage/backend.html` |
| Regression guard | Bug fixes include a failing-then-passing test — #442, #443, #444, #445, #447, #330, #441, #439 all pinned |
| FK cascade fix | Missing `PRAGMA foreign_keys=ON` caught by the first kanban test — notes/deps/log now cascade correctly on card delete |

## Distribution

| Feature | Description |
|---------|-------------|
| Workshop fork | SFW open-source version (github.com/jamesnhan/workshop) |
| Provider auto-detection | Only shows installed AI CLIs |
| MIT license | Open source |
| Global personality configs | CLAUDE.md, GEMINI.md, AGENTS.md |
