import { describe, it, expect } from 'vitest';
import { reorderTab, type GridCell } from './types';

function cellWithTabs(targets: string[]): GridCell {
  return {
    id: 'cell-1',
    target: targets[0] ?? null,
    tabs: targets.map((t) => ({ target: t, label: t })),
    history: [],
    historyIndex: -1,
    row: 0,
    col: 0,
    rowSpan: 1,
    colSpan: 1,
  };
}

describe('reorderTab', () => {
  it('moves a tab from one index to another', () => {
    const cell = cellWithTabs(['a', 'b', 'c', 'd']);
    const result = reorderTab(cell, 0, 2);
    expect(result.tabs.map((t) => t.target)).toEqual(['b', 'c', 'a', 'd']);
  });

  it('moves left (higher index to lower)', () => {
    const cell = cellWithTabs(['a', 'b', 'c', 'd']);
    const result = reorderTab(cell, 3, 0);
    expect(result.tabs.map((t) => t.target)).toEqual(['d', 'a', 'b', 'c']);
  });

  it('is a no-op when from === to', () => {
    const cell = cellWithTabs(['a', 'b', 'c']);
    const result = reorderTab(cell, 1, 1);
    expect(result).toBe(cell);
  });

  it('is a no-op for out-of-range indices', () => {
    const cell = cellWithTabs(['a', 'b']);
    expect(reorderTab(cell, -1, 0)).toBe(cell);
    expect(reorderTab(cell, 0, 5)).toBe(cell);
    expect(reorderTab(cell, 5, 0)).toBe(cell);
  });

  it('does not mutate the input cell', () => {
    const cell = cellWithTabs(['a', 'b', 'c']);
    const originalTabs = cell.tabs;
    reorderTab(cell, 0, 2);
    expect(cell.tabs).toBe(originalTabs);
    expect(cell.tabs.map((t) => t.target)).toEqual(['a', 'b', 'c']);
  });

  it('preserves non-tab fields on the cell', () => {
    const cell = cellWithTabs(['a', 'b']);
    cell.history = ['a', 'b'];
    cell.historyIndex = 1;
    cell.target = 'a';
    const result = reorderTab(cell, 0, 1);
    expect(result.history).toEqual(['a', 'b']);
    expect(result.historyIndex).toBe(1);
    expect(result.target).toBe('a');
    expect(result.id).toBe(cell.id);
  });

  it('keeps cell.target stable when the active tab moves', () => {
    // Active tab is 'b' (cell.target). Moving it to a new position should
    // leave cell.target pointing to 'b' — we only reordered tabs, not switched.
    const cell = cellWithTabs(['a', 'b', 'c']);
    cell.target = 'b';
    const result = reorderTab(cell, 1, 2);
    expect(result.tabs.map((t) => t.target)).toEqual(['a', 'c', 'b']);
    expect(result.target).toBe('b');
  });
});
