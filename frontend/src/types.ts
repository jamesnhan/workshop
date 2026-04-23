export interface PaneTab {
  target: string;
  label: string;       // short display name (e.g. "workshop:1.1" or window name)
}

export interface GridCell {
  id: string;          // unique cell ID
  target: string | null;
  tabs: PaneTab[];     // open tabs in this cell
  history: string[];   // ordered list of pane targets visited (for back/forward)
  historyIndex: number; // current position in history (-1 = end)
  row: number;         // 0-based grid row
  col: number;         // 0-based grid col
  rowSpan: number;     // how many rows this cell spans
  colSpan: number;     // how many cols this cell spans
}

export interface LayoutState {
  gridRows: number;
  gridCols: number;
  cells: GridCell[];
  focusedId: string;
  maximizedId: string | null;  // temporarily maximized cell
}

export interface PaneInfo {
  id: string;
  target: string;
  windowName: string;
  command: string;
  path: string;
  width: number;
  height: number;
  active: boolean;
}

export interface SessionInfo {
  name: string;
  windows: number;
  attached: boolean;
}

let nextCellId = 1;
export function genCellId(): string {
  return `cell-${nextCellId++}`;
}

// Create a simple NxM grid layout
export function createGrid(rows: number, cols: number, existing?: GridCell[]): LayoutState {
  const cells: GridCell[] = [];
  const existingByPos = new Map<string, GridCell>();
  if (existing) {
    for (const c of existing) {
      existingByPos.set(`${c.row},${c.col}`, c);
    }
  }

  for (let r = 0; r < rows; r++) {
    for (let c = 0; c < cols; c++) {
      const prev = existingByPos.get(`${r},${c}`);
      cells.push({
        id: prev?.id ?? genCellId(),
        target: prev?.target ?? null,
        tabs: prev?.tabs ?? [],
        history: prev?.history ?? [],
        historyIndex: prev?.historyIndex ?? -1,
        row: r,
        col: c,
        rowSpan: 1,
        colSpan: 1,
      });
    }
  }

  return {
    gridRows: rows,
    gridCols: cols,
    cells,
    focusedId: cells[0]?.id ?? '',
    maximizedId: null,
  };
}

// Find cell at grid position (accounting for spans)
export function cellAtPosition(cells: GridCell[], row: number, col: number): GridCell | undefined {
  return cells.find((c) =>
    row >= c.row && row < c.row + c.rowSpan &&
    col >= c.col && col < c.col + c.colSpan
  );
}

// Navigate from focused cell in a direction
export function navigateGrid(layout: LayoutState, direction: 'h' | 'j' | 'k' | 'l'): string {
  const focused = layout.cells.find((c) => c.id === layout.focusedId);
  if (!focused) return layout.focusedId;

  let targetRow = focused.row;
  let targetCol = focused.col;

  switch (direction) {
    case 'h': targetCol = focused.col - 1; break;
    case 'l': targetCol = focused.col + focused.colSpan; break;
    case 'k': targetRow = focused.row - 1; break;
    case 'j': targetRow = focused.row + focused.rowSpan; break;
  }

  // Clamp
  targetRow = Math.max(0, Math.min(layout.gridRows - 1, targetRow));
  targetCol = Math.max(0, Math.min(layout.gridCols - 1, targetCol));

  const target = cellAtPosition(layout.cells, targetRow, targetCol);
  return target?.id ?? layout.focusedId;
}

// Swap the contents (target, tabs, history) of two cells without
// changing their positions or spans. Used for rearranging the grid.
export function swapCellContents(layout: LayoutState, sourceId: string, targetId: string): LayoutState {
  const source = layout.cells.find((c) => c.id === sourceId);
  const target = layout.cells.find((c) => c.id === targetId);
  if (!source || !target || source.id === target.id) return layout;

  const newCells = layout.cells.map((c) => {
    if (c.id === sourceId) {
      return {
        ...c,
        target: target.target,
        tabs: target.tabs,
        history: target.history,
        historyIndex: target.historyIndex,
      };
    }
    if (c.id === targetId) {
      return {
        ...c,
        target: source.target,
        tabs: source.tabs,
        history: source.history,
        historyIndex: source.historyIndex,
      };
    }
    return c;
  });
  return { ...layout, cells: newCells };
}

// Merge two adjacent cells — the focused cell absorbs the target cell
export function mergeCells(layout: LayoutState, sourceId: string, targetId: string): LayoutState {
  const source = layout.cells.find((c) => c.id === sourceId);
  const target = layout.cells.find((c) => c.id === targetId);
  if (!source || !target) return layout;

  // Can only merge if they share an edge and result in a rectangle
  const newRow = Math.min(source.row, target.row);
  const newCol = Math.min(source.col, target.col);
  const newRowEnd = Math.max(source.row + source.rowSpan, target.row + target.rowSpan);
  const newColEnd = Math.max(source.col + source.colSpan, target.col + target.colSpan);
  const newRowSpan = newRowEnd - newRow;
  const newColSpan = newColEnd - newCol;

  // Check that the merged area is exactly the sum of the two cells
  const mergedArea = newRowSpan * newColSpan;
  const sourceArea = source.rowSpan * source.colSpan;
  const targetArea = target.rowSpan * target.colSpan;
  if (mergedArea !== sourceArea + targetArea) return layout; // not a clean merge

  const newCells = layout.cells
    .filter((c) => c.id !== targetId)
    .map((c) => c.id === sourceId ? {
      ...c,
      row: newRow,
      col: newCol,
      rowSpan: newRowSpan,
      colSpan: newColSpan,
    } : c);

  return { ...layout, cells: newCells };
}

// Move a tab within a cell's tabs array. Returns the cell unchanged if indices
// are out of range or identical. Does not change cell.target (the active tab).
export function reorderTab(cell: GridCell, fromIndex: number, toIndex: number): GridCell {
  if (fromIndex === toIndex) return cell;
  if (fromIndex < 0 || toIndex < 0) return cell;
  if (fromIndex >= cell.tabs.length || toIndex >= cell.tabs.length) return cell;
  const newTabs = [...cell.tabs];
  const [moved] = newTabs.splice(fromIndex, 1);
  newTabs.splice(toIndex, 0, moved);
  return { ...cell, tabs: newTabs };
}

// Split a merged cell back into individual cells
export function splitCell(layout: LayoutState, cellId: string): LayoutState {
  const cell = layout.cells.find((c) => c.id === cellId);
  if (!cell || (cell.rowSpan === 1 && cell.colSpan === 1)) return layout;

  const newCells = layout.cells.filter((c) => c.id !== cellId);
  for (let r = cell.row; r < cell.row + cell.rowSpan; r++) {
    for (let c = cell.col; c < cell.col + cell.colSpan; c++) {
      const isOriginal = r === cell.row && c === cell.col;
      newCells.push({
        id: isOriginal ? cell.id : genCellId(),
        target: isOriginal ? cell.target : null,
        tabs: isOriginal ? cell.tabs : [],
        history: isOriginal ? cell.history : [],
        historyIndex: isOriginal ? cell.historyIndex : -1,
        row: r,
        col: c,
        rowSpan: 1,
        colSpan: 1,
      });
    }
  }

  return { ...layout, cells: newCells };
}

// Add a row to the grid
export function addRow(layout: LayoutState): LayoutState {
  if (layout.gridRows >= 16) return layout;
  const newRow = layout.gridRows;
  const newCells = [...layout.cells];
  for (let c = 0; c < layout.gridCols; c++) {
    newCells.push({
      id: genCellId(),
      target: null,
      tabs: [],
      history: [],
      historyIndex: -1,
      row: newRow,
      col: c,
      rowSpan: 1,
      colSpan: 1,
    });
  }
  return { ...layout, gridRows: layout.gridRows + 1, cells: newCells };
}

// Add a column to the grid
export function addCol(layout: LayoutState): LayoutState {
  if (layout.gridCols >= 16) return layout;
  const newCol = layout.gridCols;
  const newCells = [...layout.cells];
  for (let r = 0; r < layout.gridRows; r++) {
    newCells.push({
      id: genCellId(),
      target: null,
      tabs: [],
      history: [],
      historyIndex: -1,
      row: r,
      col: newCol,
      rowSpan: 1,
      colSpan: 1,
    });
  }
  return { ...layout, gridCols: layout.gridCols + 1, cells: newCells };
}

// Remove last row (if empty)
export function removeRow(layout: LayoutState): LayoutState {
  if (layout.gridRows <= 1) return layout;
  const lastRow = layout.gridRows - 1;
  // Check no cells span into or start in the last row
  const canRemove = !layout.cells.some(
    (c) => c.row === lastRow || (c.row + c.rowSpan > lastRow && c.row < lastRow)
  );
  if (!canRemove) {
    // Just remove empty cells in the last row
    const hasTargets = layout.cells.some((c) => c.row === lastRow && c.target);
    if (hasTargets) return layout;
  }
  const newCells = layout.cells.filter((c) => c.row < lastRow && c.row + c.rowSpan <= lastRow + 1);
  // Trim any cells that span past
  const trimmed = newCells.map((c) => ({
    ...c,
    rowSpan: Math.min(c.rowSpan, lastRow - c.row),
  }));
  return { ...layout, gridRows: lastRow, cells: trimmed, focusedId: layout.focusedId };
}

// Remove last column (if empty)
export function removeCol(layout: LayoutState): LayoutState {
  if (layout.gridCols <= 1) return layout;
  const lastCol = layout.gridCols - 1;
  const hasTargets = layout.cells.some((c) => c.col === lastCol && c.target);
  if (hasTargets) return layout;
  const newCells = layout.cells.filter((c) => c.col < lastCol && c.col + c.colSpan <= lastCol + 1);
  const trimmed = newCells.map((c) => ({
    ...c,
    colSpan: Math.min(c.colSpan, lastCol - c.col),
  }));
  return { ...layout, gridCols: lastCol, cells: trimmed };
}
