import { type RefObject } from 'react';
import { PaneViewer, type PaneViewerHandle } from './PaneViewer';
import { ChibiAvatar, variantFromName, type ChibiState } from './ChibiAvatar';
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
  onTicketHover?: (cardId: number | null, x: number, y: number) => void;
  onTicketClick?: (cardId: number) => void;
  onHashKey?: () => void;
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
  onFocusCell, onAssignPane, onSwitchTab, onCloseTab, onTicketHover, onTicketClick, onHashKey,
}: Props) {
  const { gridRows, gridCols, cells, focusedId, maximizedId } = layout;

  const gridStyle = maximizedId ? {
    gridTemplate: '1fr / 1fr',
  } : {
    gridTemplateRows: `repeat(${gridRows}, 1fr)`,
    gridTemplateColumns: `repeat(${gridCols}, 1fr)`,
  };

  const visibleCells = maximizedId ? cells.filter((c) => c.id === maximizedId) : cells;

  return (
    <div className="pane-grid" style={gridStyle}>
      {visibleCells.map((cell) => {
        const cellStyle = maximizedId ? {} : {
          gridRow: `${cell.row + 1} / span ${cell.rowSpan}`,
          gridColumn: `${cell.col + 1} / span ${cell.colSpan}`,
        };

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
              <div className="pane-tabs">
                {cell.tabs.map((tab) => {
                  const tabStatus = paneStatuses[tab.target];
                  return (
                    <div
                      key={tab.target}
                      className={`pane-tab${tab.target === cell.target ? ' active' : ''}${tabStatus ? ` tab-status-${tabStatus.status}` : ''}`}
                      onClick={(e) => { e.stopPropagation(); onSwitchTab(cell.id, tab.target); }}
                      onAuxClick={(e) => {
                        if (e.button === 1) { e.stopPropagation(); onCloseTab(cell.id, tab.target); }
                      }}
                      title={tabStatus?.message || tabStatus?.status || tab.label}
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
                onHashKey={cell.id === focusedId ? onHashKey : undefined}
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
