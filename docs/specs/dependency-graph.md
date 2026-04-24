# Dependency graph

## Overview
Visual graph view of kanban card dependencies. Nodes = cards, edges = blocker
relationships. Built on react-flow with a simple column-by-status layout.

See also: [Kanban](kanban.md) for the underlying data model.

## Data model

Uses the `card_dependencies` table from the kanban spec. No additional
persistence on the backend.

Frontend state:
- `localStorage["workshop:depGraph:viewport:<project>"]` — per-project viewport
  (x, y, zoom) for #447 persistence

## API surface

See the Kanban spec for `/card-dependencies`, `POST /cards/{id}/blocks`,
`DELETE /cards/{id}/blocks/{blockerId}`.

## Invariants

1. Layout groups nodes by column in left-to-right order: backlog →
   in_progress → review → done.
2. Edges animate blocker → blocked direction.
3. Hiding Done (#445) also hides edges whose either endpoint is hidden.
4. Creating a self-loop or a cycle fails with a clear error (#443).
5. Deleting an edge requires explicit confirmation via the themed confirm
   dialog (#444).
6. Viewport position is restored from localStorage on mount when present;
   otherwise `fitView` frames the graph (#447).

## Known edge cases

- **Large graphs**: current layout is naive; rendering N×M cards with dense
  edges can overwhelm react-flow. Follow-up work will use a dagre layout.
- **Project switch**: changing the project selector remounts the graph
  (reactflow `key={project}`) so viewport persistence is per-project.
- **Edge selection**: react-flow edge selection requires a focused wrapper,
  which we don't provide — we use `onEdgeDoubleClick` instead of
  Backspace/Delete (#444).

## Test matrix

Backend cycle detection (#443) and dependency CRUD are pinned in the
Kanban spec (#481). This matrix covers the frontend component only.

Legend: ✅ covered, ◻ planned.

| # | Scenario | Unit | Integration | Status | Notes |
|---|----------|------|-------------|--------|-------|
| 1 | Hides Done cards by default (#445) | ✅ | | done | node-count asserted |
| 2 | Prunes edges with hidden endpoints (#445) | ✅ | | done | |
| 3 | Restores saved viewport on mount (#447) | ✅ | | done | defaultViewport prop |
| 4 | fitView fallback when no saved viewport | ✅ | | done | |
| 5 | onMoveEnd persists viewport to localStorage (#447) | ✅ | | done | |
| 6 | Viewport keyed per project (#447) | ✅ | | done | |
| 7 | onConnect posts blocks dep | ✅ | | done | |
| 8 | onConnect ignores self-connects | ✅ | | done | |
| 9 | Double-click edge prompts then DELETEs (#444) | ✅ | | done | mocked onConfirm |
| 10 | Double-click edge bails if confirm rejected | ✅ | | done | |
| 11 | Show Done toggle re-includes done nodes (#445) | | ◻ | planned | user-event click |
| 12 | Cycle error surfaces to toast (#443 + UI wire) | | ◻ | planned | |
| 13 | Node double-click jumps to kanban | | ◻ | planned | onOpenCard prop |

Frontend coverage landed in `frontend/src/components/DependencyGraph.test.tsx`
with a reactflow module mock that exposes the passed props via a shared
`rfProps` ref so handlers can be driven directly from tests.
