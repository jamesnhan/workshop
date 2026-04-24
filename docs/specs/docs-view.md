# Docs view

## Overview
Frontend markdown browser + viewer. Reads `.md` files from a configurable
directory, supports pinning and persistence, and can be driven from agents
via the `open_doc` MCP tool.

## Data model

Frontend-only state (no DB):
- `localStorage["workshop:pinnedDocs"]` — array of `{path, name}`
- `localStorage["workshop:activeDoc"]` — last-open doc `{path, name}` for cross-view
  persistence (#439)

## API surface

REST:
- `GET /docs/list?dir=<path>` — recursive `.md` enumeration
- `GET /docs/read?path=<path>` — file contents, path expansion (`~/`), path
  traversal guard

WebSocket: server → client `openDoc` command with `{path}` payload (driven by
the `open_doc` MCP tool).

MCP tools: `open_doc(path)`.

## Invariants

1. `/docs/read` must refuse paths outside the configured base.
2. Active doc persists across view switches (#439).
3. Pinned list survives reload.
4. Clipboard copy (#333) copies the raw markdown source, not rendered HTML.

## Known edge cases

- **Large files**: no pagination; very large markdown files will lag the
  react-markdown renderer. Needs a size cap or lazy render.
- **Symlinks**: follow or not? Currently: follow. Decide + test.
- **Binary files with .md extension**: should reject or show error.

## Test matrix

Legend: ✅ covered, ◻ planned.

| # | Scenario | Unit | Integration | Status | Notes |
|---|----------|------|-------------|--------|-------|
| 1 | Read rejects paths outside home | ✅ | | done | existing `docs_test.go` |
| 2 | Read rejects `..` traversal | ✅ | | done | |
| 3 | Read rejects disallowed extensions | ✅ | | done | |
| 4 | Read allows file inside home | ✅ | | done | |
| 5 | Read rejects symlink escape | ✅ | | done | |
| 6 | Read 400 on missing path param | ✅ | | done | |
| 7 | Read roundtrips content + name | ✅ | | done | `docs_more_test.go` |
| 8 | Read allows full whitelisted ext set | ✅ | | done | md/txt/yaml/yml/json/lua/toml |
| 9 | Read 404 for missing allowed file | ✅ | | done | |
| 10 | Open broadcasts `open_doc` event | ✅ | | done | fake StatusManager |
| 11 | Open rejects missing path | ✅ | | done | |
| 12 | Open rejects disallowed ext | ✅ | | done | |
| 13 | Open rejects outside home + no broadcast | ✅ | | done | |
| 14 | List returns only .md files (flat + nested) | ✅ | | done | |
| 15 | List skips hidden dirs, node_modules, vendor | ✅ | | done | |
| 16 | List rejects outside home | ✅ | | done | |
| 17 | Active doc restored from localStorage on mount | ✅ | | done | #439 frontend regression |
| 18 | Empty state when no active doc saved | ✅ | | done | |
| 19 | openPath prop persists to localStorage | ✅ | | done | |
| 20 | Pin toggle survives reload | | ◻ | planned | |
| 21 | List dir input roundtrip via UI | | ◻ | planned | user-event |

Backend coverage landed in `internal/api/v1/docs_test.go` (existing) +
`internal/api/v1/docs_more_test.go` (new). Frontend coverage landed in
`frontend/src/components/DocsView.test.tsx`.
