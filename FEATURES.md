# Workshop Features

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
| Key interception | Tab, Ctrl+C, Ctrl+V paste, Alt+Backspace, word navigation |
| Key translation | xterm.js escape sequences mapped to tmux named keys |
| Pane tabs | Tab bar per cell with Alt+[/] cycling, Alt+W close, middle-click |
| Pane history | Back/forward navigation with Alt+Left/Right |
| Session persistence | Auto-save/restore layout to localStorage |
| Terminal recording | Record and replay terminal sessions (asciinema-style) |

## Navigation & Discovery

| Feature | Description |
|---------|-------------|
| Sidebar | Expandable session/pane tree with hover preview cards |
| Collapsible sidebar | Alt+B toggle |
| Ctrl+P fuzzy finder | fzf-powered pane search with vim navigation modes |
| Command palette | Ctrl+Shift+P searchable action list with shortcuts |
| Hotkey menu | ? to see all keyboard shortcuts organized by category |
| Mode tabs | Sessions, Kanban, Agents, Docs, Settings — visual indicator |
| Pane output search | Full tmux scrollback, fzf fuzzy matching, 3-mode vim nav (FIND/NAV/PREVIEW) |
| Search preview | ANSI-rendered context with auto-scroll to match |
| Live search refresh | Results update every 3 seconds while panel is open |

## Workspaces

| Feature | Description |
|---------|-------------|
| Named workspaces | Save/restore layout presets |
| Workspace manager | Popover for switch, rename, duplicate, delete |
| Status bar indicator | Shows active workspace name |
| Command palette integration | Save/load/delete via Ctrl+Shift+P |

## AI Agent Orchestration

| Feature | Description |
|---------|-------------|
| Agent launcher | Launch AI agents with model/prompt config and trust prompt handling |
| Multi-provider | Claude, Gemini, and Codex support (auto-detected) |
| Provider-aware commands | Correct CLI flags per provider (--yolo, --full-auto, etc.) |
| Trust prompt handling | Auto-dismiss trust/folder prompts for all providers |
| Agent dashboard | Monitor all agents with status (working/idle/needs_input/done/error) |
| Chibi avatars | Animated per-state agent visuals with color variants |
| Consensus engine | Run same prompt through multiple agents, coordinator synthesizes |
| Mixed-provider consensus | name:provider:model spec format (e.g. deep:gemini:pro) |
| Consensus cleanup | Kill agent sessions after runs complete |
| Session audit | Show/hide consensus and control sessions in sidebar |
| Idle detection | Provider-specific prompt pattern matching (Claude/Gemini/Codex) |

## Project Tracking (Kanban)

| Feature | Description |
|---------|-------------|
| Kanban board | 4 columns (backlog/in_progress/review/done) with drag-and-drop |
| Card types & priorities | bug/feature/task/chore, P0-P3 priority levels |
| Card notes | Timestamped notes for tracking progress |
| Project filtering | Auto-filter by active session context |
| Drag-and-drop polish | Visual drag preview, drop indicators, reorder within columns |
| MCP integration | Create/edit/move/delete cards from Claude Code |

## Notifications

| Feature | Description |
|---------|-------------|
| Output pattern scanning | Detect task completion, permission prompts, errors |
| Browser notifications | Desktop and mobile push notifications |
| Custom patterns | User-defined regex patterns for notification triggers |
| Notification panel | In-app panel with dismiss/clear, unread badge |
| Mobile support | Permission request banner, works in background tabs |

## Documentation

| Feature | Description |
|---------|-------------|
| Markdown viewer | Docs tab with full markdown rendering |
| Pinned documents | Quick access to frequently used docs |
| Live preview | Auto-updates when file changes on disk |
| Syntax highlighting | Code blocks rendered with colors |
| Filesystem browser | Browse and open any .md file |

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
| 20+ MCP tools | Sessions, panes, kanban, agents, consensus, search, status, config |
| Status indicators | set_pane_status (green/yellow/red) for agent state |
| Kanban from CLI | Create/edit/move cards without leaving the terminal |
| Agent launch | Launch multi-provider agents via MCP |
| Consensus runs | Start/monitor/review multi-agent consensus |
| Pane capture | Read terminal content for AI analysis |

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

## Security

| Feature | Description |
|---------|-------------|
| XSS protection | DOMPurify on all dangerouslySetInnerHTML sites |
| WebSocket origin check | Restricted to localhost |
| Shell injection fix | Escaped command concatenation |
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

## Distribution

| Feature | Description |
|---------|-------------|
| Workshop fork | SFW open-source version (github.com/jamesnhan/workshop) |
| Provider auto-detection | Only shows installed AI CLIs |
| MIT license | Open source |
| Global personality configs | CLAUDE.md, GEMINI.md, AGENTS.md |
