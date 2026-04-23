import { type RefObject, useState, useLayoutEffect } from 'react';
import { PaneViewer, type PaneViewerHandle } from './PaneViewer';
import { ChibiAvatar, variantFromName, type ChibiState } from './ChibiAvatar';
import { recordBreadcrumb } from '../lib/telemetry';
import type { LayoutState } from '../types';
import type { Theme } from '../themes';

interface PaneStatusInfo {
  status: string;
  message: string;
}

interface Props {
  layout: LayoutState;
  viewerRefs: Map<string, RefObject<PaneViewerHandle | null>>;
  theme: Theme;
  unreadCells: Set<string>;
  paneStatuses: Record<string, PaneStatusInfo>;
  onInput: (target: string, data: string) => void;
  onResize: (target: string, cols: number, rows: number) => void;
  onFocusCell: (cellId: string) => void;
  onAssignPane: (cellId: string) => void;
  onSwitchTab: (cellId: string, target: string) => void;
  onCloseTab: (cellId: string, target: string) => void;
  onReorderTab?: (cellId: string, fromIndex: number, toIndex: number) => void;
  onTicketHover?: (cardId: number | null, x: number, y: number) => void;
  onTicketClick?: (cardId: number) => void;
  onUrlHover?: (url: string | null, x: number, y: number) => void;
  onCommitHover?: (sha: string | null, x: number, y: number) => void;
  onHashKey?: () => void;
  agentTargets?: Set<string>;  // pane targets running agent commands (claude/gemini/codex) — # only intercepted for these
  sfwMode?: boolean;
  nsfwProjects?: string[];
}

const STATUS_COLORS: Record<string, string> = {
  green: 'var(--success)',
  yellow: 'var(--warning, #f9e2af)',
  red: 'var(--error)',
};

const STATUS_TO_CHIBI: Record<string, ChibiState> = {
  green: 'done',
  yellow: 'needs_input',
  red: 'error',
};

export function PaneGrid({
  layout, viewerRefs, theme, unreadCells, paneStatuses, onInput, onResize,
  onFocusCell, onAssignPane, onSwitchTab, onCloseTab, onReorderTab, onTicketHover, onTicketClick, onUrlHover, onCommitHover, onHashKey,
  agentTargets, sfwMode = false, nsfwProjects = [],
}: Props) {
  const { gridRows, gridCols, cells, focusedId, maximizedId } = layout;
  const [dragTab, setDragTab] = useState<{ cellId: string; fromIndex: number } | null>(null);
  const [dragOverIndex, setDragOverIndex] = useState<number | null>(null);

  const focusedTarget = cells.find((c) => c.id === focusedId)?.target ?? null;
  useLayoutEffect(() => {
    recordBreadcrumb('commit:PaneGrid', {
      cells: cells.length,
      focused: focusedTarget ?? 'none',
      maximized: maximizedId ?? 'none',
    });
  });

  const gridStyle = maximizedId ? {
    gridTemplate: '1fr / 1fr',
  } : {
    gridTemplateRows: `repeat(${gridRows}, 1fr)`,
    gridTemplateColumns: `repeat(${gridCols}, 1fr)`,
  };

  const visibleCells = maximizedId ? cells.filter((c) => c.id === maximizedId) : cells;
  const nsfwSet = new Set(nsfwProjects.map((p) => p.toLowerCase()));
  const isCellHidden = (target: string | null | undefined) =>
    sfwMode && target && nsfwSet.has(target.split(':')[0].toLowerCase());

  return (
    <div className="pane-grid" style={gridStyle}>
      {visibleCells.map((cell) => {
        const cellStyle = maximizedId ? {} : {
          gridRow: `${cell.row + 1} / span ${cell.rowSpan}`,
          gridColumn: `${cell.col + 1} / span ${cell.colSpan}`,
        };

        if (isCellHidden(cell.target)) {
          return (
            <div key={cell.id} className="pane-cell" style={cellStyle} onClick={() => onFocusCell(cell.id)}>
              <div className="pane-empty"><p>Empty</p><p className="muted">Click or Ctrl+P</p></div>
            </div>
          );
        }

        const cellStatus = cell.target ? paneStatuses[cell.target] : undefined;

        return (
          <div
            key={cell.id}
            className={`pane-cell${cell.id === focusedId ? ' focused' : ''}${unreadCells.has(cell.id) ? ' unread' : ''}${cellStatus ? ` pane-status-${cellStatus.status}` : ''}`}
            style={{
              ...cellStyle,
              // Don't override border with status color when focused — let .focused accent class win
              ...(cellStatus && cell.id !== focusedId ? { borderColor: STATUS_COLORS[cellStatus.status] } : {}),
            }}
            onClick={() => onFocusCell(cell.id)}
          >
            {/* Status bar */}
            {cellStatus && (
              <div className="pane-status-bar" style={{ background: STATUS_COLORS[cellStatus.status] }}>
                <ChibiAvatar state={STATUS_TO_CHIBI[cellStatus.status] || 'idle'} variant={variantFromName(cell.target || '')} size="sm" />
                <span>{cellStatus.message || cellStatus.status}</span>
                {cell.id === focusedId && <span className="pane-status-focused-indicator">▶ focused</span>}
              </div>
            )}

            {/* Tab bar */}
            {cell.tabs.length > 1 && (
              <div
                className="pane-tabs"
                onDragOver={(e) => {
                  // Fallback: dragging over empty space past the last tab.
                  // Tab-level handlers stop propagation so this only runs when
                  // the cursor is NOT over a specific tab.
                  if (!dragTab || dragTab.cellId !== cell.id) return;
                  e.preventDefault();
                  if (dragOverIndex !== cell.tabs.length) setDragOverIndex(cell.tabs.length);
                }}
                onDrop={(e) => {
                  e.preventDefault();
                  if (dragTab && dragTab.cellId === cell.id && dragOverIndex !== null) {
                    const toIdx = dragOverIndex > dragTab.fromIndex ? dragOverIndex - 1 : dragOverIndex;
                    if (toIdx !== dragTab.fromIndex) {
                      onReorderTab?.(cell.id, dragTab.fromIndex, toIdx);
                    }
                  }
                  setDragTab(null);
                  setDragOverIndex(null);
                }}
              >
                {cell.tabs.map((tab, tabIdx) => {
                  const tabStatus = paneStatuses[tab.target];
                  const isDragging = dragTab?.cellId === cell.id && dragTab.fromIndex === tabIdx;
                  // dragOverIndex is an INSERT position (0..tabs.length). Show a
                  // left-side indicator when it matches this tab's index, and a
                  // right-side indicator when it matches this tab's index + 1.
                  const dropBefore = dragTab?.cellId === cell.id && dragOverIndex === tabIdx;
                  const dropAfter = dragTab?.cellId === cell.id && dragOverIndex === tabIdx + 1;
                  return (
                    <div
                      key={tab.target}
                      className={`pane-tab${tab.target === cell.target ? ' active' : ''}${tabStatus ? ` tab-status-${tabStatus.status}` : ''}${isDragging ? ' dragging' : ''}${dropBefore ? ' drop-before' : ''}${dropAfter ? ' drop-after' : ''}`}
                      onClick={(e) => { e.stopPropagation(); onSwitchTab(cell.id, tab.target); }}
                      onAuxClick={(e) => {
                        if (e.button === 1) { e.stopPropagation(); onCloseTab(cell.id, tab.target); }
                      }}
                      title={tabStatus?.message || tabStatus?.status || tab.label}
                      draggable={!!onReorderTab}
                      onDragStart={(e) => {
                        e.stopPropagation();
                        setDragTab({ cellId: cell.id, fromIndex: tabIdx });
                      }}
                      onDragOver={(e) => {
                        if (!dragTab || dragTab.cellId !== cell.id) return;
                        e.preventDefault();
                        e.stopPropagation();
                        const rect = e.currentTarget.getBoundingClientRect();
                        const after = e.clientX > rect.left + rect.width / 2;
                        const insertIdx = after ? tabIdx + 1 : tabIdx;
                        if (dragOverIndex !== insertIdx) setDragOverIndex(insertIdx);
                      }}
                      onDrop={(e) => {
                        e.preventDefault();
                        e.stopPropagation();
                        if (dragTab && dragTab.cellId === cell.id && dragOverIndex !== null) {
                          // Convert insert-position to post-splice index: if dropping
                          // past the source, subtract 1 to compensate for the removal.
                          let toIdx = dragOverIndex > dragTab.fromIndex ? dragOverIndex - 1 : dragOverIndex;
                          if (toIdx !== dragTab.fromIndex) {
                            onReorderTab?.(cell.id, dragTab.fromIndex, toIdx);
                          }
                        }
                        setDragTab(null);
                        setDragOverIndex(null);
                      }}
                      onDragEnd={() => { setDragTab(null); setDragOverIndex(null); }}
                    >
                      {tabStatus && (
                        <span className="pane-tab-status-dot" style={{ background: STATUS_COLORS[tabStatus.status] }} />
                      )}
                      <span className="pane-tab-label">{tab.label}</span>
                      <button
                        className="pane-tab-close"
                        onClick={(e) => { e.stopPropagation(); onCloseTab(cell.id, tab.target); }}
                      >
                        x
                      </button>
                    </div>
                  );
                })}
              </div>
            )}

            {cell.target ? (
              <PaneViewer
                key={cell.id}
                ref={viewerRefs.get(cell.id) ?? null}
                target={cell.target}
                terminalTheme={theme.terminal}
                onData={(data) => onInput(cell.target!, data)}
                onResize={(cols, rows) => onResize(cell.target!, cols, rows)}
                onTicketHover={onTicketHover}
                onTicketClick={onTicketClick}
                onUrlHover={onUrlHover}
                onCommitHover={onCommitHover}
                onHashKey={cell.id === focusedId && cell.target && agentTargets?.has(cell.target) ? onHashKey : undefined}
              />
            ) : (
              <div className="pane-empty" onClick={() => onAssignPane(cell.id)}>
                <p>Empty</p>
                <p className="muted">Click or Ctrl+P</p>
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}
