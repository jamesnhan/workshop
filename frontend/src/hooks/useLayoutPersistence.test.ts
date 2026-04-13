import { describe, it, expect, beforeEach } from 'vitest';
import {
  loadLayout,
  restoreLayout,
  saveWorkspace,
  loadWorkspace,
  deleteWorkspace,
  renameWorkspace,
  listWorkspaces,
  isWorkspaceDirty,
  getActiveWorkspaceName,
  setActiveWorkspaceName,
} from './useLayoutPersistence';
import { createGrid, type LayoutState } from '../types';

// Build a minimal SavedLayout blob directly in localStorage so we bypass
// the live-layout path and can pin the shape expected by loadLayout /
// restoreLayout.
function seedLiveLayout(overrides: Record<string, unknown> = {}) {
  const base = {
    gridRows: 2,
    gridCols: 2,
    cells: [
      { target: 'a:1.1', tabs: [], history: [], historyIndex: -1, row: 0, col: 0, rowSpan: 1, colSpan: 1 },
      { target: null, tabs: [], history: [], historyIndex: -1, row: 0, col: 1, rowSpan: 1, colSpan: 1 },
      { target: null, tabs: [], history: [], historyIndex: -1, row: 1, col: 0, rowSpan: 1, colSpan: 1 },
      { target: null, tabs: [], history: [], historyIndex: -1, row: 1, col: 1, rowSpan: 1, colSpan: 1 },
    ],
    focusedIdx: 0,
    ...overrides,
  };
  localStorage.setItem('workshop:layout', JSON.stringify(base));
  return base;
}

beforeEach(() => {
  // setup.ts already clears storage between tests; explicit here for safety
  localStorage.clear();
});

// --- loadLayout / restoreLayout ---

describe('loadLayout', () => {
  it('returns null when no layout is saved', () => {
    expect(loadLayout()).toBeNull();
  });

  it('returns null when the stored blob is corrupt', () => {
    localStorage.setItem('workshop:layout', '{not json');
    expect(loadLayout()).toBeNull();
  });

  it('roundtrips a saved layout', () => {
    const saved = seedLiveLayout();
    expect(loadLayout()).toEqual(saved);
  });
});

describe('restoreLayout', () => {
  it('assigns fresh cell ids and preserves positions', () => {
    const saved = seedLiveLayout();
    const layout = restoreLayout(saved);

    expect(layout.gridRows).toBe(2);
    expect(layout.gridCols).toBe(2);
    expect(layout.cells).toHaveLength(4);
    expect(layout.cells[0].target).toBe('a:1.1');
    // Every cell has a non-empty id
    for (const c of layout.cells) {
      expect(c.id).toMatch(/^cell-/);
    }
    // focusedId points at the first cell
    expect(layout.focusedId).toBe(layout.cells[0].id);
  });

  it('defaults focusedId to cell[0] when focusedIdx is out of range', () => {
    const saved = seedLiveLayout({ focusedIdx: 99 });
    const layout = restoreLayout(saved);
    expect(layout.focusedId).toBe(layout.cells[0].id);
  });

  it('returns a 1x1 fresh grid when no cells are saved', () => {
    const saved = seedLiveLayout({ gridRows: 1, gridCols: 1, cells: [], focusedIdx: 0 });
    const layout = restoreLayout(saved);
    expect(layout.gridRows).toBe(1);
    expect(layout.gridCols).toBe(1);
    expect(layout.cells).toHaveLength(1);
  });
});

// --- Workspaces CRUD ---

function makeLayout(target: string | null = 'a:1.1'): LayoutState {
  const layout = createGrid(2, 2);
  if (target) layout.cells[0].target = target;
  return layout;
}

describe('workspaces CRUD', () => {
  it('save + load roundtrips a layout', () => {
    const layout = makeLayout('session:1.1');
    saveWorkspace('main', layout);

    const loaded = loadWorkspace('main');
    expect(loaded).not.toBeNull();
    expect(loaded!.gridRows).toBe(2);
    expect(loaded!.cells[0].target).toBe('session:1.1');
  });

  it('load returns null for a missing workspace', () => {
    expect(loadWorkspace('ghost')).toBeNull();
  });

  it('delete removes the stored key', () => {
    saveWorkspace('temp', makeLayout());
    expect(loadWorkspace('temp')).not.toBeNull();
    deleteWorkspace('temp');
    expect(loadWorkspace('temp')).toBeNull();
  });

  it('rename moves the stored key under a new name', () => {
    saveWorkspace('old-name', makeLayout('foo:1.1'));
    renameWorkspace('old-name', 'new-name');

    expect(loadWorkspace('old-name')).toBeNull();
    const renamed = loadWorkspace('new-name');
    expect(renamed).not.toBeNull();
    expect(renamed!.cells[0].target).toBe('foo:1.1');
  });

  it('rename is a no-op when the source workspace does not exist', () => {
    renameWorkspace('ghost', 'new-name');
    expect(loadWorkspace('new-name')).toBeNull();
  });

  it('list returns workspace names sorted', () => {
    saveWorkspace('charlie', makeLayout());
    saveWorkspace('alpha', makeLayout());
    saveWorkspace('bravo', makeLayout());
    expect(listWorkspaces()).toEqual(['alpha', 'bravo', 'charlie']);
  });

  it('list ignores non-workspace keys in localStorage', () => {
    saveWorkspace('alpha', makeLayout());
    localStorage.setItem('unrelated', 'noise');
    localStorage.setItem('workshop:layout', 'live');
    localStorage.setItem('workshop:activeWorkspace', 'alpha');
    expect(listWorkspaces()).toEqual(['alpha']);
  });
});

// --- Active workspace name ---

describe('active workspace name', () => {
  it('get returns null when unset', () => {
    expect(getActiveWorkspaceName()).toBeNull();
  });

  it('set then get roundtrips', () => {
    setActiveWorkspaceName('main');
    expect(getActiveWorkspaceName()).toBe('main');
  });

  it('set null clears the key', () => {
    setActiveWorkspaceName('main');
    setActiveWorkspaceName(null);
    expect(getActiveWorkspaceName()).toBeNull();
  });
});

// --- isWorkspaceDirty (#330 regression guard) ---

describe('isWorkspaceDirty', () => {
  it('returns false when no workspace name is active', () => {
    const layout = makeLayout();
    expect(isWorkspaceDirty(null, layout)).toBe(false);
  });

  it('returns false when the named workspace does not exist', () => {
    const layout = makeLayout();
    expect(isWorkspaceDirty('ghost', layout)).toBe(false);
  });

  it('returns false immediately after saving the live layout', () => {
    const layout = makeLayout('a:1.1');
    saveWorkspace('main', layout);
    expect(isWorkspaceDirty('main', layout)).toBe(false);
  });

  it('returns true after a structural change', () => {
    const layout = makeLayout('a:1.1');
    saveWorkspace('main', layout);

    // Mutate a cell target — a real edit.
    const next: LayoutState = {
      ...layout,
      cells: layout.cells.map((c, i) => (i === 0 ? { ...c, target: 'b:1.1' } : c)),
    };
    expect(isWorkspaceDirty('main', next)).toBe(true);
  });

  it('ignores focus changes (regression for #330)', () => {
    const layout = makeLayout('a:1.1');
    saveWorkspace('main', layout);

    // Move focus to cell[2]. This should NOT count as dirty.
    const focused: LayoutState = { ...layout, focusedId: layout.cells[2].id };
    expect(isWorkspaceDirty('main', focused)).toBe(false);
  });

  it('returns true on grid resize', () => {
    const layout = makeLayout();
    saveWorkspace('main', layout);

    const bigger: LayoutState = { ...layout, gridRows: 3 };
    expect(isWorkspaceDirty('main', bigger)).toBe(true);
  });
});
