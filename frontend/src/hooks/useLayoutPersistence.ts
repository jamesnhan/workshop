import { useEffect, useRef, useCallback } from 'react';
import type { LayoutState, GridCell, PaneTab } from '../types';
import { createGrid, genCellId } from '../types';

const STORAGE_KEY = 'workshop:layout';

interface SavedLayout {
  gridRows: number;
  gridCols: number;
  cells: { target: string | null; tabs?: PaneTab[]; history?: string[]; historyIndex?: number; row: number; col: number; rowSpan: number; colSpan: number }[];
  focusedIdx: number;
}

function saveToStorage(layout: LayoutState) {
  const saved: SavedLayout = {
    gridRows: layout.gridRows,
    gridCols: layout.gridCols,
    cells: layout.cells.map((c) => ({
      target: c.target, tabs: c.tabs, history: c.history, historyIndex: c.historyIndex, row: c.row, col: c.col, rowSpan: c.rowSpan, colSpan: c.colSpan,
    })),
    focusedIdx: layout.cells.findIndex((c) => c.id === layout.focusedId),
  };
  try { localStorage.setItem(STORAGE_KEY, JSON.stringify(saved)); } catch {}
}

export function loadLayout(): SavedLayout | null {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return null;
    return JSON.parse(raw) as SavedLayout;
  } catch { return null; }
}

export function restoreLayout(saved: SavedLayout): LayoutState {
  const cells: GridCell[] = saved.cells.map((c) => ({
    id: genCellId(),
    target: c.target,
    tabs: c.tabs ?? [],
    history: c.history ?? [],
    historyIndex: c.historyIndex ?? -1,
    row: c.row,
    col: c.col,
    rowSpan: c.rowSpan,
    colSpan: c.colSpan,
  }));

  if (cells.length === 0) return createGrid(1, 1);

  const focusedId = saved.focusedIdx >= 0 && saved.focusedIdx < cells.length
    ? cells[saved.focusedIdx].id
    : cells[0].id;

  return {
    gridRows: saved.gridRows,
    gridCols: saved.gridCols,
    cells,
    focusedId,
    maximizedId: null,
  };
}

// --- Workspaces ---

const WORKSPACE_PREFIX = 'workshop:workspace:';
const ACTIVE_WORKSPACE_KEY = 'workshop:activeWorkspace';

export interface Workspace {
  name: string;
  layout: SavedLayout;
}

function layoutToSaved(layout: LayoutState): SavedLayout {
  return {
    gridRows: layout.gridRows,
    gridCols: layout.gridCols,
    cells: layout.cells.map((c) => ({
      target: c.target, tabs: c.tabs, history: c.history, historyIndex: c.historyIndex, row: c.row, col: c.col, rowSpan: c.rowSpan, colSpan: c.colSpan,
    })),
    focusedIdx: layout.cells.findIndex((c) => c.id === layout.focusedId),
  };
}

export function saveWorkspace(name: string, layout: LayoutState) {
  try {
    localStorage.setItem(WORKSPACE_PREFIX + name, JSON.stringify(layoutToSaved(layout)));
  } catch {}
}

export function loadWorkspace(name: string): LayoutState | null {
  try {
    const raw = localStorage.getItem(WORKSPACE_PREFIX + name);
    if (!raw) return null;
    return restoreLayout(JSON.parse(raw) as SavedLayout);
  } catch { return null; }
}

export function deleteWorkspace(name: string) {
  localStorage.removeItem(WORKSPACE_PREFIX + name);
}

export function renameWorkspace(oldName: string, newName: string) {
  const raw = localStorage.getItem(WORKSPACE_PREFIX + oldName);
  if (!raw) return;
  localStorage.setItem(WORKSPACE_PREFIX + newName, raw);
  localStorage.removeItem(WORKSPACE_PREFIX + oldName);
}

export function listWorkspaces(): string[] {
  const names: string[] = [];
  for (let i = 0; i < localStorage.length; i++) {
    const key = localStorage.key(i);
    if (key?.startsWith(WORKSPACE_PREFIX)) {
      names.push(key.substring(WORKSPACE_PREFIX.length));
    }
  }
  return names.sort();
}

// Fingerprint of the structural parts of a SavedLayout for dirty-checking.
// Excludes focusedIdx because moving the focus cursor is transient UI state,
// not a workspace edit.
function layoutFingerprint(saved: SavedLayout): string {
  const { gridRows, gridCols, cells } = saved;
  return JSON.stringify({ gridRows, gridCols, cells });
}

// Compare the live layout against the saved snapshot for a workspace.
// Returns true if they differ (i.e. the workspace has unsaved changes).
export function isWorkspaceDirty(name: string | null, layout: LayoutState): boolean {
  if (!name) return false;
  const raw = localStorage.getItem(WORKSPACE_PREFIX + name);
  if (!raw) return false;
  try {
    const saved = JSON.parse(raw) as SavedLayout;
    return layoutFingerprint(saved) !== layoutFingerprint(layoutToSaved(layout));
  } catch {
    return false;
  }
}

export function getActiveWorkspaceName(): string | null {
  try { return localStorage.getItem(ACTIVE_WORKSPACE_KEY); } catch { return null; }
}

export function setActiveWorkspaceName(name: string | null) {
  try {
    if (name) localStorage.setItem(ACTIVE_WORKSPACE_KEY, name);
    else localStorage.removeItem(ACTIVE_WORKSPACE_KEY);
  } catch {}
}

export function useAutoSaveLayout(layout: LayoutState) {
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(() => {
    if (timerRef.current) clearTimeout(timerRef.current);
    timerRef.current = setTimeout(() => saveToStorage(layout), 500);
    return () => { if (timerRef.current) clearTimeout(timerRef.current); };
  }, [layout]);
}

export function useValidateTargets(
  availablePanes: { target: string }[],
  setLayout: (fn: (prev: LayoutState) => LayoutState) => void
) {
  const validated = useRef(false);
  const validate = useCallback(() => {
    if (validated.current || availablePanes.length === 0) return;
    validated.current = true;
    const available = new Set(availablePanes.map((p) => p.target));
    setLayout((prev) => {
      let changed = false;
      const newCells = prev.cells.map((cell) => {
        if (cell.target && !available.has(cell.target)) {
          changed = true;
          return { ...cell, target: null };
        }
        return cell;
      });
      return changed ? { ...prev, cells: newCells } : prev;
    });
  }, [availablePanes, setLayout]);
  useEffect(() => { validate(); }, [validate]);
}
