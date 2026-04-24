# Workshop Feature Specifications

Source of truth for Workshop's feature behavior. Each spec under this folder
describes one feature area with enough detail that a contributor (human or
agent) can implement, test, or refactor that area without hunting through
the codebase for tribal knowledge.

## Workflow: plan → spec → tests → build

1. **Plan** — break down what's needed in a kanban ticket.
2. **Spec** — update (or create) the relevant spec file in this folder with
   the intended behavior, data model changes, and edge cases. The spec is
   the contract the tests and implementation both answer to.
3. **Tests** — write unit + integration tests against the spec before
   touching implementation.
4. **Build** — implement until tests pass. If reality disagrees with the
   spec, update the spec in the same commit — never silently diverge.

## Spec file structure

Every spec under `docs/specs/` should follow this shape:

```markdown
# <Feature Area>

## Overview
What this area does, why it exists, and what it explicitly does NOT cover.

## Data model
- Tables, structs, localStorage keys, on-wire shapes.
- Migration notes for breaking changes.

## API surface
- REST endpoints (method + path + inputs + outputs + error cases)
- WebSocket messages
- MCP tools
- Internal Go / TS interfaces consumers depend on

## Invariants
Rules that MUST hold. The tests should pin each one.

## Known edge cases
The weird stuff — concurrency hazards, race conditions, off-by-ones,
historical bugs the current code guards against.

## Test matrix
| # | Scenario | Unit | Integration | Notes |
|---|----------|------|-------------|-------|
| 1 | ...      | ✅   |             |       |
```

## Unit vs integration vs e2e

- **Unit** — single function or component in isolation; mock collaborators;
  fast; no DB, no network, no filesystem. Lives next to the source file
  (`foo_test.go`, `Foo.test.tsx`).
- **Integration** — multiple units wired together with real collaborators
  (ephemeral SQLite, in-process HTTP server, mock tmux bridge). Lives under
  `internal/<pkg>/...` for backend or `frontend/src/test/integration/` for
  frontend.
- **E2E** — full stack, real server, real browser. Use sparingly — we rely
  on integration tests for the bulk of behavior coverage.

## Coverage expectations

- New code should have tests that pin the spec's behavior.
- We do not enforce a coverage percentage. Instead: every merged PR either
  adds tests OR explains (in the PR body) why the change doesn't need them.
- Bug fixes MUST include a regression test that fails before the fix.

## Running tests

See `TESTING.md` at the repo root for commands, CI integration, and local
tooling.

## Current specs

- [Kanban](kanban.md) — cards, dependencies, notes, log
- [Sessions & panes](sessions-panes.md) — tmux bridge, owned-PTY, subscriptions
- [Channels](channels.md) — inter-pane / inter-agent messaging
- [Docs view](docs-view.md) — markdown browser and viewer
- [Dependency graph](dependency-graph.md) — visual card dependency view
- [Workspaces & layout persistence](workspaces.md) — layout save/load/dirty
- [MCP tool surface](mcp-tools.md) — Model Context Protocol tool contracts
- [UI command hub](ui-commands.md) — toast, dialogs, view/focus control
- [Git info API](git-info.md) — repo metadata endpoint
- [Observability](observability.md) — OpenTelemetry across backend, frontend, and MCP subprocesses
