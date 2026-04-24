# Workspaces & layout persistence

## Overview
Frontend-only layout persistence: the current grid/cell/tab configuration
auto-saves to localStorage, and named "workspaces" can be saved/loaded/
renamed/deleted from a popover. A dirty indicator shows when the live
layout diverges from the active workspace's saved snapshot.

## Data model

localStorage keys:
- `workshop:layout` — live layout, auto-saved with a 500ms debounce via
  `useAutoSaveLayout`
- `workshop:workspace:<name>` — named workspace snapshot (`SavedLayout`)
- `workshop:activeWorkspace` — currently-loaded workspace name (or null)

`SavedLayout` shape:
```ts
{
  gridRows, gridCols,
  cells: [{ target, tabs?, history?, historyIndex?, row, col, rowSpan, colSpan }],
  focusedIdx  // deliberately EXCLUDED from dirty comparison
}
```

## API surface

No backend. All behavior lives in `frontend/src/hooks/useLayoutPersistence.ts`
and `frontend/src/components/WorkspaceManager.tsx`.

Exported functions: `loadLayout`, `restoreLayout`, `saveWorkspace`,
`loadWorkspace`, `deleteWorkspace`, `renameWorkspace`, `listWorkspaces`,
`getActiveWorkspaceName`, `setActiveWorkspaceName`, `isWorkspaceDirty`,
`useAutoSaveLayout`, `useValidateTargets`.

## Invariants

1. The live layout is always auto-saved within 500ms of the last change.
2. `isWorkspaceDirty(name, layout)` compares the structural fingerprint
   (gridRows, gridCols, cells) and **excludes** `focusedIdx` — moving focus
   is NOT a workspace edit (#330).
3. Switching to a different workspace while the current one is dirty
   prompts to save via the themed confirm dialog.
4. Saving a workspace immediately clears its dirty state (tick counter
   forces re-memo).
5. Loading a workspace unsubscribes all current pane targets and
   resubscribes the new set.
6. Invalid targets (panes that no longer exist) are cleared from a restored
   layout via `useValidateTargets`.

## Known edge cases

- **Active workspace deleted**: resets `activeWorkspace` to null.
- **Rename in flight**: `renameWorkspace` is an atomic swap of two
  localStorage keys; losing power between them leaves an orphan.
- **Save button click inside popover**: the memoized dirty value must
  re-evaluate immediately — hence the `workspaceSaveTick` counter
  (#330 fix).

## Test matrix

Legend: ✅ covered, ◻ planned.

| # | Scenario | Unit | Integration | Status | Notes |
|---|----------|------|-------------|--------|-------|
| 1 | loadLayout returns null when absent | ✅ | | done | `loadLayout` tests |
| 2 | loadLayout returns null on corrupt JSON | ✅ | | done | |
| 3 | loadLayout roundtrips saved blob | ✅ | | done | |
| 4 | restoreLayout assigns fresh cell ids | ✅ | | done | |
| 5 | restoreLayout defaults focusedId on out-of-range | ✅ | | done | |
| 6 | restoreLayout returns fresh 1x1 on empty cells | ✅ | | done | |
| 7 | saveWorkspace + loadWorkspace roundtrip | ✅ | | done | |
| 8 | loadWorkspace returns null for missing | ✅ | | done | |
| 9 | deleteWorkspace removes key | ✅ | | done | |
| 10 | renameWorkspace moves key | ✅ | | done | |
| 11 | renameWorkspace no-op on missing source | ✅ | | done | |
| 12 | listWorkspaces returns sorted names | ✅ | | done | |
| 13 | listWorkspaces ignores unrelated keys | ✅ | | done | |
| 14 | getActiveWorkspaceName null when unset | ✅ | | done | |
| 15 | setActiveWorkspaceName roundtrip | ✅ | | done | |
| 16 | setActiveWorkspaceName(null) clears | ✅ | | done | |
| 17 | isWorkspaceDirty false with null name | ✅ | | done | #330 |
| 18 | isWorkspaceDirty false for missing workspace | ✅ | | done | |
| 19 | isWorkspaceDirty false immediately after save | ✅ | | done | |
| 20 | isWorkspaceDirty true after target change | ✅ | | done | |
| 21 | isWorkspaceDirty ignores focusedIdx | ✅ | | done | #330 regression |
| 22 | isWorkspaceDirty true on grid resize | ✅ | | done | |
| 23 | useAutoSaveLayout debounce behavior | | ◻ | planned | fake timers |
| 24 | Save → dirty flips false without remount | | ◻ | planned | tick counter, WorkspaceManager RTL |
| 25 | Switch prompts on dirty | | ◻ | planned | mock confirm dialog |
| 26 | useValidateTargets clears missing panes | | ◻ | planned | |

Unit coverage is landed in `frontend/src/hooks/useLayoutPersistence.test.ts`
(22 tests). Hook/component tests (useAutoSaveLayout, WorkspaceManager) are
planned follow-ups.
