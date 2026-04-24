# Kanban

## Overview
Project tracking surface: cards organized into columns per project, with
dependencies, notes, and activity log attached. Backed by SQLite; consumed
by the Kanban board UI, the Dependency Graph view, and the MCP tool
surface.

NOT covered here: the dependency graph's visual layout (see
[Dependency graph](dependency-graph.md)).

## Data model

### `cards` table
- `id` INTEGER PK
- `title` TEXT NOT NULL
- `description` TEXT
- `column` TEXT — one of: `backlog`, `in_progress`, `review`, `done`
- `project` TEXT — free-form project name (commonly the repo name)
- `position` INTEGER — ordering within a column, densified on every move
- `pane_target` TEXT — optional linked tmux pane
- `labels` TEXT — comma-separated
- `card_type` TEXT — one of: `bug`, `feature`, `task`, `chore`
- `priority` TEXT — one of: `P0`, `P1`, `P2`, `P3`
- `parent_id` INTEGER — 0 = root, else a parent card's id (hierarchy)
- `created_at`, `updated_at` DATETIME

### `card_dependencies` table
Directed edges: `blocker_id` blocks `blocked_id`. Unique per pair. FK cascade
on card delete.

### `card_notes`, `card_log` tables
Chronological children of a card. See relevant source files in
`internal/db/` for exact columns.

## API surface

REST (`/api/v1/`):
- `GET /cards?project=<p>` — list, ordered by (column, position, id)
- `POST /cards` — create; column defaults to `backlog`
- `GET /cards/{id}`
- `PUT /cards/{id}` — full update
- `POST /cards/{id}/move` body `{column, position}` — see densify rules
- `DELETE /cards/{id}`
- `GET /projects`
- `GET /cards/{id}/notes`, `POST /cards/{id}/notes`
- `GET /cards/{id}/log`, `GET /cards/log?project=&limit=`
- `GET /card-dependencies?project=`
- `POST /cards/{id}/blocks` body `{blockerId}`
- `DELETE /cards/{id}/blocks/{blockerId}`

MCP tools: `kanban_list`, `kanban_create`, `kanban_edit`, `kanban_move`,
`kanban_delete`, `kanban_add_note`.

## Invariants

1. `column` is always one of the four known values.
2. `card_type` and `priority` are enforced at the UI + MCP layer (Workshop's
   CLAUDE.md rule), not at the DB layer.
3. **Positions are dense**: after `MoveCard`, root cards within a column
   have positions `0..N-1` with no gaps. Child cards (non-zero parent_id)
   do not participate in column ordering.
4. `AddDependency` rejects self-loops and cycles. Dependency graph is a DAG.
5. `DeleteCard` cascades to notes, log, and dependencies (FK).
6. Updating a card with a changed `column` logs a `moved` event in
   `card_log`; other field changes log their own events.

## Known edge cases

- **Sparse positions (pre-#442)**: old rows may still have sparse positions
  from before `MoveCard` was rewritten. The first move on such a column
  re-densifies it.
- **Child cards are hidden from column lists**: `KanbanBoard.columnCards`
  filters `!parentId`, rendering children nested under their parent.
- **#313-style disappearance**: if `MoveCard` moves a card to a position
  that doesn't match the frontend's render index, the card can appear to
  vanish until a refresh. The densify fix (#442) keeps DB position ==
  render index for root cards.
- **Circular dependency attempts** (#443): BFS from `blockedID` to catch
  transitive cycles.

## Test matrix

Legend: ✅ covered, ◻ planned, 🐛 uncovered test caught a real bug.

| # | Scenario | Unit | Integration | Status | Notes |
|---|----------|------|-------------|--------|-------|
| 1 | Create card with defaults | ✅ | | done | `TestCreateCard_defaultsAndInsert` |
| 2 | Create card positions monotonically | ✅ | | done | `TestCreateCard_positionMonotonic` |
| 3 | List cards filters by project | ✅ | | done | `TestListCards_projectFilter` |
| 4 | List cards ordered by column then position | ✅ | | done | `TestListCards_orderedByColumnThenPosition` |
| 5 | Update card roundtrip | ✅ | | done | `TestUpdateCard_roundtrip` |
| 6 | Move densifies destination column (#442) | ✅ | | done | `TestMoveCard_densifiesDestination` |
| 7 | Drop-in-place is a no-op (#442) | ✅ | | done | `TestMoveCard_dropInPlaceIsNoop` |
| 8 | Cross-column move re-densifies both (#442) | ✅ | | done | `TestMoveCard_crossColumnDensifiesBoth` |
| 9 | Out-of-range position clamped | ✅ | | done | `TestMoveCard_positionClampedToRange` |
| 10 | Child cards skipped from densify (#442) | ✅ | | done | `TestMoveCard_childrenSkippedFromDensify` |
| 11 | AddDependency happy path | ✅ | | done | `TestAddDependency_happyPath` |
| 12 | AddDependency self-loop rejected | ✅ | | done | `TestAddDependency_selfLoopRejected` |
| 13 | AddDependency direct cycle rejected (#443) | ✅ | | done | `TestAddDependency_directCycleRejected` |
| 14 | AddDependency transitive cycle rejected (#443) | ✅ | | done | `TestAddDependency_transitiveCycleRejected` |
| 15 | AddDependency idempotent | ✅ | | done | `TestAddDependency_idempotent` |
| 16 | RemoveDependency | ✅ | | done | `TestRemoveDependency` |
| 17 | ListDependencies project scope | ✅ | | done | `TestListDependencies_projectScoped` |
| 18 | DeleteCard cascades notes 🐛 | ✅ | | done | `TestDeleteCard_cascadesNotes` — found missing `PRAGMA foreign_keys=ON` |
| 19 | DeleteCard cascades dependencies | ✅ | | done | `TestDeleteCard_cascadesDependencies` |
| 20 | API: full CRUD roundtrip | | ◻ | planned | ephemeral DB + http test server |
| 21 | Frontend drag-drop in place stays put | | ◻ | planned | KanbanBoard + RTL |
| 22 | Frontend drag across columns updates UI + DB | | ◻ | planned | |
| 23 | DependencyGraph: connect creates edge | | ◻ | planned | RTL + mock API |
| 24 | DependencyGraph: double-click deletes with confirm | | ◻ | planned | RTL |

Backend unit coverage is landed in `internal/db/kanban_test.go`.
Integration (API layer) and frontend component tests remain for a
follow-up ticket.
