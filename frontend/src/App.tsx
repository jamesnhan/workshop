import { useState, useCallback, useRef, useEffect, createRef } from 'react';
import { AuthGate } from './components/AuthGate';
import { Sidebar } from './components/Sidebar';
import { PaneGrid } from './components/PaneGrid';
import { type PaneViewerHandle } from './components/PaneViewer';
import { PaneSwitcher } from './components/PaneSwitcher';
import { SearchPanel } from './components/SearchPanel';
import { NotificationPanel } from './components/NotificationPanel';
import { HotkeyMenu } from './components/HotkeyMenu';
import { NotificationSettings } from './components/NotificationSettings';
import { RecordingPlayer } from './components/RecordingPlayer';
import { CommandPalette, type Command } from './components/CommandPalette';
import { KanbanBoard } from './components/KanbanBoard';
import { AgentDashboard } from './components/AgentDashboard';
import { DocsView } from './components/DocsView';
import { OllamaChat } from './components/OllamaChat';
import { DependencyGraph } from './components/DependencyGraph';
import { WorkspaceManager } from './components/WorkspaceManager';
import { SettingsView } from './components/SettingsView';
import { ActivityView } from './components/ActivityView';
import { UsageBars } from './components/UsageBars';
import { isHoverPinned, setHoverPinned as setGlobalHoverPinned } from './hooks/useHoverPin';
import { ResizeHandle } from './components/ResizeHandle';
import { TicketHoverPreview } from './components/TicketHoverPreview';
import { LinkHoverPreview } from './components/LinkHoverPreview';
import { GitCommitHoverPreview } from './components/GitCommitHoverPreview';
import { TicketLookupDialog } from './components/TicketLookupDialog';
import { MobileToolbar } from './components/MobileToolbar';
import { ToastContainer, type ToastItem, type ToastKind } from './components/Toast';
import { ConfirmDialog, type DialogKind } from './components/ConfirmDialog';
import { useSettings } from './hooks/useSettings';
import { useWorkshopSocket } from './hooks/useWebSocket';
import { useNotifications } from './hooks/useNotifications';
import { useLockupWatchdog } from './hooks/useLockupWatchdog';
import { get, post, authHeaders } from './api/client';
import type { LayoutState, PaneInfo, SessionInfo } from './types';
import { createGrid, navigateGrid, addRow, addCol, removeRow, removeCol, mergeCells, splitCell, swapCellContents, reorderTab } from './types';
import {
  loadLayout,
  restoreLayout,
  useAutoSaveLayout,
  useValidateTargets,
  saveWorkspace,
  loadWorkspace,
  isWorkspaceDirty,
  deleteWorkspace,
  renameWorkspace,
  listWorkspaces,
  getActiveWorkspaceName,
  setActiveWorkspaceName,
} from './hooks/useLayoutPersistence';
import { themes, getActiveThemeName, setActiveThemeName, applyTheme } from './themes';
import './App.css';

// Run a heavy operation against each item one frame at a time, instead of
// all at once. Prevents main-thread saturation when touching N terminals
// simultaneously (e.g. on WebSocket reconnect or wake-from-sleep, where
// force-resizing all viewers + processing backlog output in one tick has
// caused complete browser tab freezes — #934).
function rafStagger<T>(items: T[], op: (item: T) => void): void {
  if (items.length === 0) return;
  let i = 0;
  const step = () => {
    if (i >= items.length) return;
    try { op(items[i]); } catch {}
    i++;
    if (i < items.length) requestAnimationFrame(step);
  };
  requestAnimationFrame(step);
}

function App() {
  const { connected, subscribe, unsubscribe, sendInput, sendResize, startRecording, stopRecording, onOutput, onStatus, onStatusClear, onReconnect, onOpenDoc, onSessionCreated, onUICommand } = useWorkshopSocket();
  const [paneStatuses, setPaneStatuses] = useState<Record<string, { status: string; message: string }>>({});
  const [layout, setLayout] = useState<LayoutState>(() => {
    const saved = loadLayout();
    return saved ? restoreLayout(saved) : createGrid(1, 1);
  });
  const [switcherOpen, setSwitcherOpen] = useState(false);
  const [searchOpen, setSearchOpen] = useState(false);
  const [notifOpen, setNotifOpen] = useState(false);
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false);
  const [sidebarWidth, setSidebarWidth] = useState(260);
  const [searchPanelWidth, setSearchPanelWidth] = useState(() => Math.min(800, window.innerWidth * 0.6));
  const [ticketHover, setTicketHover] = useState<{ id: number; x: number; y: number } | null>(null);
  const [urlHover, setUrlHover] = useState<{ url: string; x: number; y: number } | null>(null);
  const [commitHover, setCommitHover] = useState<{ sha: string; x: number; y: number } | null>(null);
  const [hoverPinned, setHoverPinned] = useState(false);
  const hoverPinnedRef = useRef(false);
  const [hotkeyMenuOpen, setHotkeyMenuOpen] = useState(false);
  const [cmdPaletteOpen, setCmdPaletteOpen] = useState(false);
  type ViewName = 'sessions' | 'kanban' | 'dashboard' | 'docs' | 'graph' | 'ollama' | 'activity' | 'settings';
  const [activeView, setActiveView] = useState<ViewName>('sessions');
  const [pendingDocPath, setPendingDocPath] = useState<string | null>(null);
  const [ticketLookupOpen, setTicketLookupOpen] = useState(false);
  const [ticketInsertTarget, setTicketInsertTarget] = useState<string | null>(null);
  const [splitView, setSplitView] = useState(() => {
    try { return localStorage.getItem('workshop-split-view') === 'true'; } catch { return false; }
  });
  const [splitRatio, setSplitRatio] = useState(() => {
    try { return parseFloat(localStorage.getItem('workshop-split-ratio') || '') || 0.4; } catch { return 0.4; }
  });
  const splitContainerRef = useRef<HTMLDivElement>(null);
  const [notifBannerDismissed, setNotifBannerDismissed] = useState(false);
  const [notifSettingsOpen, setNotifSettingsOpen] = useState(false);
  const [playerOpen, setPlayerOpen] = useState(false);
  const [workspaceName, setWorkspaceName] = useState<string | null>(getActiveWorkspaceName);
  // Bumps on workspace save so dirty-state memoization re-evaluates against
  // the fresh localStorage snapshot.
  const [workspaceSaveTick, setWorkspaceSaveTick] = useState(0);
  const [allPanes, setAllPanes] = useState<PaneInfo[]>([]);
  // Track unread: last-output time vs last-focused time per cell
  // Initialize lastFocused to now so initial screen dump doesn't trigger unread
  const mountTime = useRef(Date.now());
  const cellLastOutput = useRef<Record<string, number>>({});
  const cellLastFocused = useRef<Record<string, number>>({});
  const pendingScanData = useRef<Record<string, string>>({});
  const scanTimers = useRef<Record<string, ReturnType<typeof setTimeout>>>({});
  // Unread tracking is disabled (see TODO below) — use ref to avoid root re-renders
  const unreadTickRef = useRef(0);
  const { notifications, unreadCount, scanOutput, markSubscribed, markAllRead, dismiss, clearAll, requestPermission, permissionState } = useNotifications();
  const [capsLockOn, setCapsLockOn] = useState(false);
  // Toast notifications surfaced via show_toast UI command
  const [toasts, setToasts] = useState<ToastItem[]>([]);
  const nextToastId = useRef(1);
  const pushToast = useCallback((message: string, kind: ToastKind = 'info') => {
    const id = nextToastId.current++;
    setToasts((prev) => [...prev, { id, message, kind }]);
  }, []);
  const dismissToast = useCallback((id: number) => {
    setToasts((prev) => prev.filter((t) => t.id !== id));
  }, []);

  // Generic dialog state for UI prompt_user / confirm commands
  const [uiDialog, setUIDialog] = useState<{
    kind: DialogKind;
    title: string;
    message?: string;
    initialValue?: string;
    danger?: boolean;
    onResolve: (value: string | undefined, cancelled: boolean) => void;
  } | null>(null);

  // Imperative confirm that resolves via the custom ConfirmDialog, so child
  // components can avoid the native window.confirm.
  const confirmViaDialog = useCallback((opts: { title: string; message?: string; danger?: boolean }): Promise<boolean> => {
    return new Promise((resolve) => {
      setUIDialog({
        kind: 'confirm',
        title: opts.title,
        message: opts.message,
        danger: opts.danger,
        onResolve: (_v, cancelled) => {
          setUIDialog(null);
          resolve(!cancelled);
        },
      });
    });
  }, []);

  // Pending kanban card to open from a UI command
  const [pendingOpenCardId, setPendingOpenCardId] = useState<number | null>(null);
  const { settings, updateSettings } = useSettings();
  const [themeName, setThemeName] = useState(getActiveThemeName);
  const theme = themes[themeName] || themes['catppuccin-mocha'];

  useEffect(() => { applyTheme(theme); }, [theme]);
  useEffect(() => { requestPermission(); }, [requestPermission]);
  useEffect(() => { try { localStorage.setItem('workshop-split-view', String(splitView)); } catch {} }, [splitView]);
  useEffect(() => { try { localStorage.setItem('workshop-split-ratio', String(splitRatio)); } catch {} }, [splitRatio]);

  // Wrap subscribe to also mark the pane for notification grace period
  const subscribePane = useCallback((target: string) => {
    markSubscribed(target);
    subscribe(target);
  }, [subscribe, markSubscribed]);
  const handleSetTheme = useCallback((name: string) => {
    setThemeName(name);
    setActiveThemeName(name);
  }, []);

  // Dynamic ref map — one ref per cell ID
  const viewerRefsMap = useRef(new Map<string, React.RefObject<PaneViewerHandle | null>>());
  const getRef = useCallback((cellId: string) => {
    if (!viewerRefsMap.current.has(cellId)) {
      viewerRefsMap.current.set(cellId, createRef<PaneViewerHandle>());
    }
    return viewerRefsMap.current.get(cellId)!;
  }, []);
  // Ensure all cells have refs (in effect to avoid render-time mutation)
  useEffect(() => {
    for (const cell of layout.cells) {
      getRef(cell.id);
    }
  }, [layout.cells, getRef]);

  const layoutRef = useRef(layout);
  useEffect(() => {
    layoutRef.current = layout;
  }, [layout]);

  // Route WS output to correct terminal + scan for notifications + track unread
  const unreadThrottle = useRef(0);
  onOutput(useCallback((target: string, data: string) => {
    const focusedId = layoutRef.current.focusedId;
    pendingScanData.current[target] = (pendingScanData.current[target] ?? '') + data;
    if (!scanTimers.current[target]) {
      scanTimers.current[target] = setTimeout(() => {
        delete scanTimers.current[target];
        const buffered = pendingScanData.current[target];
        delete pendingScanData.current[target];
        if (buffered) {
          const currentFocusedId = layoutRef.current.focusedId;
          const currentFocused = layoutRef.current.cells.find((c) => c.id === currentFocusedId);
          scanOutput(target, buffered, currentFocused?.target ?? null);
        }
      }, 500);
    }
    // Track output timestamps for unfocused cells (skip grace period after subscribe)
    const now = Date.now();
    let changed = false;
    for (const cell of layoutRef.current.cells) {
      if (cell.target === target && cell.id !== focusedId) {
        // Skip if within 5s of this cell's last focus/assign (initial screen dump)
        const lastFocused = cellLastFocused.current[cell.id] ?? mountTime.current;
        if (now - lastFocused < 5000) continue;
        cellLastOutput.current[cell.id] = now;
        changed = true;
      }
    }
    if (changed && now - unreadThrottle.current > 500) {
      unreadThrottle.current = now;
      unreadTickRef.current++;
    }
    for (const cell of layoutRef.current.cells) {
      if (cell.target === target) {
        viewerRefsMap.current.get(cell.id)?.current?.write(data);
      }
    }
  }, [scanOutput]));

  useEffect(() => () => {
    for (const timer of Object.values(scanTimers.current)) {
      clearTimeout(timer);
    }
    scanTimers.current = {};
    pendingScanData.current = {};
  }, []);

  // Handle pane status updates from WS
  onStatus(useCallback((target: string, status: string, message: string) => {
    setPaneStatuses((prev) => ({ ...prev, [target]: { status, message } }));
  }, []));

  onStatusClear(useCallback((target: string) => {
    setPaneStatuses((prev) => {
      const next = { ...prev };
      delete next[target];
      return next;
    });
  }, []));

  // On WebSocket reconnect, clear all terminal state to avoid garbled rendering.
  // Stagger the writes one viewer per frame — issuing N xterm writes synchronously
  // contributed to post-wake freezes (#934). forceResize is handled by the
  // `connected` effect below; we don't duplicate it here.
  onReconnect(useCallback(() => {
    const refs = Array.from(viewerRefsMap.current.values());
    rafStagger(refs, (ref) => {
      ref.current?.write('\x1b[2J\x1b[H'); // clear screen + cursor home
    });
  }, []));

  // When WebSocket connects (first time or reconnect), force-resize all
  // terminals so the server knows each client's dimensions. On first load,
  // runFit fires before the WS is open, so the resize message is dropped.
  // Stagger across frames to avoid layout storms on wake-from-sleep (#934).
  useEffect(() => {
    if (!connected) return;
    const timer = setTimeout(() => {
      const refs = Array.from(viewerRefsMap.current.values());
      rafStagger(refs, (ref) => { ref.current?.forceResize(true); });
    }, 500);
    return () => clearTimeout(timer);
  }, [connected]);

  onOpenDoc(useCallback((path: string) => {
    setActiveView('docs');
    setPendingDocPath(path);
  }, []));

  onSessionCreated(useCallback((target: string, background: boolean) => {
    // Refresh the pane list so the new session shows up everywhere.
    refreshPanes();
    if (background) {
      // Agent/MCP-initiated: add as an inactive tab, don't steal focus.
      addBackgroundTab(target);
    } else {
      // User-initiated: make it the active tab in the focused cell.
      // Switch to sessions view first — PaneGrid is display:none on
      // other views, and browsers can't focus elements in hidden containers.
      setActiveView('sessions');
      assignPaneToCell(layoutRef.current.focusedId, target);
      // Bump focusTick to trigger the auto-focus effect (focusedId
      // doesn't change since the same cell gets a new target).
      setTimeout(() => setFocusTick((t) => t + 1), 300);
    }
  }, []));

  // UI command dispatch — agents drive the frontend via the ui_command WS
  // channel. Blocking commands (prompt_user/confirm) post their result back
  // to /api/v1/ui/response/{id}.
  onUICommand(useCallback((cmd) => {
    const respond = (value: string | undefined, cancelled: boolean) => {
      if (!cmd.id) return;
      post(`/ui/response/${cmd.id}`, { value, cancelled }).catch(() => {});
    };
    switch (cmd.action) {
      case 'show_toast': {
        const message = String(cmd.payload.message ?? '');
        const kind = (cmd.payload.kind as ToastKind) || 'info';
        if (message) pushToast(message, kind);
        break;
      }
      case 'switch_view': {
        const view = String(cmd.payload.view ?? 'sessions');
        setActiveView(view === 'agents' ? 'dashboard' : view as ViewName);
        break;
      }
      case 'focus_cell': {
        const cellId = String(cmd.payload.cellId ?? '');
        if (cellId) {
          setLayout((prev) => ({ ...prev, focusedId: cellId }));
        }
        break;
      }
      case 'focus_pane': {
        const target = String(cmd.payload.target ?? '');
        const cell = layoutRef.current.cells.find((c) => c.target === target);
        if (cell) {
          setLayout((prev) => ({ ...prev, focusedId: cell.id }));
          requestAnimationFrame(() => viewerRefsMap.current.get(cell.id)?.current?.focus());
        }
        break;
      }
      case 'assign_pane': {
        const target = String(cmd.payload.target ?? '');
        const cellId = (cmd.payload.cellId as string | undefined) ?? layoutRef.current.focusedId;
        if (target) assignPaneToCell(cellId, target);
        break;
      }
      case 'open_card': {
        const id = Number(cmd.payload.id);
        if (id > 0) {
          setActiveView('kanban');
          setPendingOpenCardId(id);
        }
        break;
      }
      case 'prompt_user': {
        setUIDialog({
          kind: 'prompt',
          title: String(cmd.payload.title ?? 'Input requested'),
          message: cmd.payload.message ? String(cmd.payload.message) : undefined,
          initialValue: cmd.payload.initialValue ? String(cmd.payload.initialValue) : '',
          onResolve: (value, cancelled) => {
            setUIDialog(null);
            respond(value, cancelled);
          },
        });
        break;
      }
      case 'confirm': {
        setUIDialog({
          kind: 'confirm',
          title: String(cmd.payload.title ?? 'Confirm'),
          message: cmd.payload.message ? String(cmd.payload.message) : undefined,
          danger: Boolean(cmd.payload.danger),
          onResolve: (_value, cancelled) => {
            setUIDialog(null);
            respond(cancelled ? 'false' : 'true', false);
          },
        });
        break;
      }
      default:
        console.warn('[workshop] unknown ui_command action:', cmd.action);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []));

  // Load all panes
  const refreshPanes = useCallback(async () => {
    try {
      const panes = await get<PaneInfo[]>('/panes');
      setAllPanes(panes ?? []);
    } catch (err) {
      console.error('Failed to load panes:', err);
    }
  }, []);

  // Batch init: fetch sessions, panes, and projects in a single round-trip.
  // Cuts page-load time dramatically over K8s/HTTPS where each request pays
  // TLS + proxy latency. Components still refresh independently after the
  // initial load.
  const [initSessions, setInitSessions] = useState<SessionInfo[] | null>(null);
  useEffect(() => {
    get<{ sessions: SessionInfo[]; panes: PaneInfo[]; projects: string[] }>('/init')
      .then((data) => {
        if (data) {
          setAllPanes(data.panes ?? []);
          setInitSessions(data.sessions ?? []);
        }
      })
      .catch(() => {
        // Fallback: fetch panes individually if init endpoint not available
        refreshPanes();
      });
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  useAutoSaveLayout(layout);
  useValidateTargets(allPanes, setLayout);
  useLockupWatchdog(layout, connected);

  // Remap all pane targets in the layout when a session is renamed.
  const handleSessionRenamed = useCallback((oldName: string, newName: string) => {
    const remap = (t: string | null) =>
      t && t.startsWith(oldName + ':') ? newName + t.slice(oldName.length) : t;
    setLayout((prev) => ({
      ...prev,
      cells: prev.cells.map((c) => ({
        ...c,
        target: remap(c.target),
        history: c.history.map((h) => remap(h) ?? h),
        tabs: c.tabs.map((tab) => {
          const nt = remap(tab.target);
          return nt && nt !== tab.target
            ? { ...tab, target: nt, label: tab.label === tab.target ? nt : tab.label }
            : tab;
        }),
      })),
    }));
    // Force a repaint/refit after the target remap lands in the DOM.
    // xterm panes need a resize tick to redraw their content for the new target.
    const refs = Array.from(viewerRefsMap.current.values());
    rafStagger(refs, (ref) => { ref.current?.refit(); });
  }, []);

  // Restore subscriptions on mount and fix stale tab labels
  const hasRestored = useRef(false);
  useEffect(() => {
    if (hasRestored.current || allPanes.length === 0) return;
    hasRestored.current = true;
    for (const cell of layout.cells) {
      if (cell.target && allPanes.some((p) => p.target === cell.target)) {
        subscribePane(cell.target);
      }
    }
    // Fix tab labels — old labels may be "claude" or "1.1" from before
    // the session-name fix. Normalize all labels to session name.
    setLayout((prev) => ({
      ...prev,
      cells: prev.cells.map((c) => ({
        ...c,
        tabs: c.tabs.map((tab) => ({
          ...tab,
          label: tab.target.split(':')[0] || tab.label,
        })),
      })),
    }));
    // Force a resize for restored panes so tmux reflows to match viewport.
    // force=true: first-mount path, no cached dims to gate against.
    setTimeout(() => {
      const refs = Array.from(viewerRefsMap.current.values());
      rafStagger(refs, (ref) => { ref.current?.forceResize(true); });
    }, 100);
  }, [allPanes, layout.cells, subscribe]);

  // Assign pane to cell by ID, adding it to the cell's tab history
  const assignPaneToCell = useCallback((cellId: string, target: string) => {
    // Use session name as tab label. windowName is often "claude"/"gemini"/"codex"
    // (set by agent launcher) which isn't useful for distinguishing tabs.
    const label = target.split(':')[0] || target;

    setLayout((prev) => {
      const newCells = prev.cells.map((c) => {
        if (c.id !== cellId && c.target === target) {
          return { ...c, target: null };
        }
        if (c.id === cellId) {
          if (c.target && c.target !== target) {
            unsubscribe(c.target);
          }
          // Add tab if not already present
          const hasTab = c.tabs.some((t) => t.target === target);
          const newTabs = hasTab ? c.tabs : [...c.tabs, { target, label }];
          // Push to history (truncate forward history if we navigated back)
          const newHistory = [...c.history.slice(0, c.historyIndex >= 0 ? c.historyIndex + 1 : c.history.length), target].slice(-50);
          return { ...c, target, tabs: newTabs.slice(-10), history: newHistory, historyIndex: newHistory.length - 1 };
        }
        return c;
      });
      return { ...prev, cells: newCells, focusedId: cellId };
    });
    cellLastFocused.current[cellId] = Date.now();
    subscribePane(target);
    setSwitcherOpen(false);
  }, [subscribe, unsubscribe, allPanes]);

  // Add a pane as an inactive background tab on a cell, without changing
  // the focused cell or the currently-active tab. Used when new sessions
  // are created elsewhere (sidebar + button, /agents/launch REST, external
  // tmux) so they surface in the UI without stealing focus.
  const addBackgroundTab = useCallback((target: string, cellId?: string) => {
    const targetCellId = cellId ?? layoutRef.current.focusedId;
    const cell = layoutRef.current.cells.find((c) => c.id === targetCellId);
    if (!cell) return;
    // If the pane is already a tab somewhere, don't duplicate it.
    if (layoutRef.current.cells.some((c) => c.tabs.some((t) => t.target === target))) {
      return;
    }
    const wasEmpty = !cell.target;
    const label = target.split(':')[0] || target;
    setLayout((prev) => ({
      ...prev,
      cells: prev.cells.map((c) => {
        if (c.id !== targetCellId) return c;
        if (wasEmpty) {
          return {
            ...c,
            target,
            tabs: [...c.tabs, { target, label }].slice(-10),
            history: [target],
            historyIndex: 0,
          };
        }
        return { ...c, tabs: [...c.tabs, { target, label }].slice(-10) };
      }),
    }));
    if (wasEmpty) subscribePane(target);
  }, [subscribePane]);

  // Switch to a tab within a cell (re-subscribes)
  const switchTab = useCallback((cellId: string, target: string) => {
    setLayout((prev) => {
      const newCells = prev.cells.map((c) => {
        if (c.id === cellId) {
          if (c.target && c.target !== target) {
            unsubscribe(c.target);
          }
          return { ...c, target };
        }
        return c;
      });
      return { ...prev, cells: newCells, focusedId: cellId };
    });
    subscribePane(target);
  }, [subscribe, unsubscribe]);

  // Close a tab in a cell
  const closeTab = useCallback((cellId: string, target: string) => {
    setLayout((prev) => {
      const newCells = prev.cells.map((c) => {
        if (c.id !== cellId) return c;
        const newTabs = c.tabs.filter((t) => t.target !== target);
        // If closing the active tab, switch to the last remaining tab
        let newTarget = c.target;
        if (c.target === target) {
          unsubscribe(target);
          newTarget = newTabs.length > 0 ? newTabs[newTabs.length - 1].target : null;
          if (newTarget) subscribePane(newTarget);
        }
        return { ...c, target: newTarget, tabs: newTabs };
      });
      return { ...prev, cells: newCells };
    });
  }, [subscribe, unsubscribe]);

  const reorderTabInCell = useCallback((cellId: string, fromIndex: number, toIndex: number) => {
    setLayout((prev) => ({
      ...prev,
      cells: prev.cells.map((c) => c.id === cellId ? reorderTab(c, fromIndex, toIndex) : c),
    }));
  }, []);

  const handleInput = useCallback((target: string, data: string) => {
    sendInput(target, data);
  }, [sendInput]);

  const resizeTimers = useRef<Record<string, ReturnType<typeof setTimeout>>>({});
  const handleResize = useCallback((target: string, cols: number, rows: number) => {
    if (resizeTimers.current[target]) clearTimeout(resizeTimers.current[target]);
    resizeTimers.current[target] = setTimeout(() => {
      sendResize(target, cols, rows);
    }, 300);
  }, [sendResize]);

  const handleFocusCell = useCallback((cellId: string) => {
    setLayout((prev) => {
      const cell = prev.cells.find((c) => c.id === cellId);
      if (cell?.target && paneStatuses[cell.target]) {
        fetch('/api/v1/panes/status', { method: 'DELETE', headers: authHeaders(), body: JSON.stringify({ target: cell.target }) });
        setPaneStatuses((ps) => { const next = { ...ps }; delete next[cell.target!]; return next; });
      }
      return { ...prev, focusedId: cellId };
    });
    cellLastFocused.current[cellId] = Date.now();
    unreadTickRef.current++; // no-op while unread indicators disabled
  }, [paneStatuses]);

  // focusTick is bumped to force the auto-focus effect to re-run even when
  // focusedId hasn't changed (e.g. new session assigned to the same cell).
  const [focusTick, setFocusTick] = useState(0);

  // Auto-focus terminal on cell change. focus() internally calls
  // notifyResizeIfChanged() so tmux gets notified only when this client's
  // dims truly changed — no forceResize() here, the container size hasn't
  // changed on a focus-switch and an unconditional push was the core of the
  // #934 amplification loop.
  useEffect(() => {
    requestAnimationFrame(() => {
      const ref = viewerRefsMap.current.get(layout.focusedId);
      ref?.current?.focus();
    });
  }, [layout.focusedId, focusTick]);

  // Switcher target cell
  const [switcherCellId, setSwitcherCellId] = useState('');
  const openSwitcher = useCallback((cellId?: string) => {
    setSwitcherCellId(cellId ?? layout.focusedId);
    setSwitcherOpen(true);
    refreshPanes();
  }, [layout.focusedId, refreshPanes]);

  // Maximize toggle
  const toggleMaximize = useCallback(() => {
    setLayout((prev) => ({
      ...prev,
      maximizedId: prev.maximizedId ? null : prev.focusedId,
    }));
  }, []);

  // Merge focused cell with neighbor in direction
  const mergeInDirection = useCallback((dir: 'h' | 'j' | 'k' | 'l') => {
    setLayout((prev) => {
      const neighborId = navigateGrid(prev, dir);
      if (neighborId === prev.focusedId) return prev;
      return mergeCells(prev, prev.focusedId, neighborId);
    });
  }, []);

  // Split focused cell back into individual cells
  const splitFocused = useCallback(() => {
    setLayout((prev) => splitCell(prev, prev.focusedId));
  }, []);

  // Swap focused cell's contents with the cell in the given direction
  const swapInDirection = useCallback((dir: 'h' | 'j' | 'k' | 'l') => {
    setLayout((prev) => {
      const neighborId = navigateGrid(prev, dir);
      if (neighborId === prev.focusedId) return prev;
      const swapped = swapCellContents(prev, prev.focusedId, neighborId);
      return { ...swapped, focusedId: neighborId };
    });
    // Refit all viewers after the swap settles in the DOM
    requestAnimationFrame(() => {
      const refs = Array.from(viewerRefsMap.current.values());
      rafStagger(refs, (ref) => { ref.current?.refit(); });
    });
  }, []);

  // Clear status on user keyboard input in focused terminal
  const paneStatusesRef = useRef(paneStatuses);
  paneStatusesRef.current = paneStatuses;
  useEffect(() => {
    const handleUserInput = () => {
      if (!document.activeElement?.classList.contains('xterm-helper-textarea')) return;
      const focusedCell = layoutRef.current.cells.find((c) => c.id === layoutRef.current.focusedId);
      const target = focusedCell?.target;
      if (target && paneStatusesRef.current[target]) {
        fetch('/api/v1/panes/status', { method: 'DELETE', headers: authHeaders(), body: JSON.stringify({ target }) });
        setPaneStatuses((ps) => { const next = { ...ps }; delete next[target]; return next; });
      }
    };
    document.addEventListener('keydown', handleUserInput);
    return () => document.removeEventListener('keydown', handleUserInput);
  }, []);

  // Global keyboard shortcuts
  useEffect(() => {
    const handleGlobalKey = (e: KeyboardEvent) => {
      const capsLock = e.getModifierState('CapsLock');
      setCapsLockOn(capsLock);

      // When capslock normalization is enabled, normalize key to lowercase
      // and compute true shift (physical shift, not capslock-induced)
      const normalize = settings.capsLockNormalization && capsLock;
      const key = normalize || settings.capsLockNormalization ? (e.key.length === 1 ? e.key.toLowerCase() : e.key) : e.key;
      const shift = normalize ? !e.shiftKey : e.shiftKey;
      // Mac uses Cmd (metaKey) where Linux/Windows uses Ctrl
      const isMac = navigator.platform.toUpperCase().indexOf('MAC') >= 0;
      const mod = isMac ? e.metaKey : e.ctrlKey; // Cmd on Mac, Ctrl on others
      const nav = isMac ? e.ctrlKey : e.altKey;   // Ctrl on Mac, Alt on others

      // Mod+P — pane switcher (Cmd+P on Mac, Ctrl+P on Linux/Win)
      if (mod && key === 'p' && !shift && !nav) {
        e.preventDefault(); e.stopPropagation();
        switcherOpen ? setSwitcherOpen(false) : openSwitcher();
        return;
      }
      // Mod+Shift+P — command palette
      if (mod && shift && key === 'p') {
        e.preventDefault(); e.stopPropagation();
        setCmdPaletteOpen((p) => !p);
        return;
      }

      // z — pin/unpin ALL hover previews globally (BG3 inspect key)
      if (key === 'z' && !mod && !nav && !shift) {
        if (isHoverPinned()) {
          // Unpin — clear all App.tsx hovers immediately
          hoverPinnedRef.current = false;
          setGlobalHoverPinned(false);
          setHoverPinned(false);
          setTicketHover(null);
          setUrlHover(null);
          setCommitHover(null);
          return;
        }
        // Pin whatever is currently on screen
        e.preventDefault();
        hoverPinnedRef.current = true;
        setGlobalHoverPinned(true);
        setHoverPinned(true);
        return;
      }
      // Escape clears all pinned hovers
      if (key === 'Escape' && isHoverPinned()) {
        hoverPinnedRef.current = false;
        setGlobalHoverPinned(false);
        setHoverPinned(false);
        setTicketHover(null);
        setUrlHover(null);
        setCommitHover(null);
        return;
      }

      if (key === 'Escape' && ticketLookupOpen) {
        e.preventDefault();
        e.stopPropagation();
        const target = ticketInsertTarget;
        // Insert a literal # if we came from a terminal (user pressed # to open,
        // Esc to cancel — they wanted to type #, not select a ticket).
        if (target) {
          handleInput(target, '#');
        }
        setTicketLookupOpen(false);
        setTicketInsertTarget(null);
        // Restore terminal focus after React unmounts the dialog.
        if (target) {
          const cell = layout.cells.find((c) => c.target === target);
          if (cell) {
            setTimeout(() => {
              viewerRefsMap.current.get(cell.id)?.current?.focus();
            }, 100);
          }
        }
        return;
      }
      if (key === 'Escape' && playerOpen) { setPlayerOpen(false); return; }
      if (key === 'Escape' && activeView !== 'sessions') { setActiveView('sessions'); return; }
      if (key === 'Escape' && cmdPaletteOpen) { setCmdPaletteOpen(false); return; }
      if (key === 'Escape' && switcherOpen) { setSwitcherOpen(false); return; }
      if (key === 'Escape' && hotkeyMenuOpen) { setHotkeyMenuOpen(false); return; }
      if (key === 'Escape' && notifOpen) { setNotifOpen(false); return; }

      // Detect whether the user is typing in a real text input (not a terminal).
      // Terminal panes use a hidden textarea (xterm-helper-textarea) for input,
      // but we still want some hotkeys (like #) to fire inside terminals.
      const active = document.activeElement;
      const inTerminal = active?.classList.contains('xterm-helper-textarea');
      const inTextInput = !inTerminal && (
        active instanceof HTMLInputElement
        || active instanceof HTMLTextAreaElement
        || active?.hasAttribute('contenteditable')
      );

      // ` — toggle split view (kanban + terminal), only when not typing
      if (e.key === '`' && !mod && !nav && !inTerminal && !inTextInput) {
        e.preventDefault();
        setSplitView((prev) => {
          if (!prev) setActiveView('kanban');
          return !prev;
        });
        return;
      }

      // ? — hotkey menu (only when not typing in any input or terminal)
      if (e.key === '?' && !mod && !nav && !inTerminal && !inTextInput) {
        e.preventDefault();
        setHotkeyMenuOpen((p) => !p);
        return;
      }

      // # — ticket lookup dialog
      // In agent terminals: handled by PaneViewer's xterm key handler → onHashKey
      //   (sets ticketInsertTarget so dialog inserts into PTY)
      // In non-agent terminals: xterm handler passes through, global handler
      //   opens dialog in clipboard mode (no ticketInsertTarget)
      // In text inputs: skip — let the user type a literal #
      if (e.key === '#' && !mod && !nav && !inTextInput) {
        // Don't intercept if xterm's onHashKey will handle it (agent sessions).
        // Check: if we're in a terminal and the focused cell has onHashKey wired,
        // let xterm handle it. Otherwise open clipboard-mode dialog.
        if (inTerminal) {
          // Let the event propagate to xterm's attachCustomKeyEventHandler.
          // If it's an agent session, xterm will call onHashKey and return false.
          // If not, xterm returns true and # types normally in the PTY.
          return;
        }
        e.preventDefault();
        setTicketLookupOpen((p) => !p);
        return;
      }

      // Mod+Shift+D — agent dashboard
      if (mod && shift && key === 'd') {
        e.preventDefault(); e.stopPropagation();
        setActiveView((p) => p === 'dashboard' ? 'sessions' : 'dashboard');
        return;
      }

      // Mod+Shift+K — kanban board
      if (mod && shift && key === 'k') {
        e.preventDefault(); e.stopPropagation();
        setActiveView((p) => p === 'kanban' ? 'sessions' : 'kanban');
        return;
      }

      // Mod+Shift+L — toggle split view (kanban + terminal)
      if (mod && shift && key === 'l') {
        e.preventDefault(); e.stopPropagation();
        setSplitView((prev) => {
          if (!prev) setActiveView('kanban');
          return !prev;
        });
        return;
      }

      // Nav+B — toggle sidebar (Alt on Linux, Ctrl on Mac)
      if (nav && !mod && !shift && key === 'b') {
        e.preventDefault(); e.stopPropagation();
        setSidebarCollapsed((p) => !p);
        return;
      }

      // Mod+Shift+F — search
      if (mod && shift && key === 'f') {
        e.preventDefault(); e.stopPropagation();
        setSearchOpen((p) => !p);
        return;
      }

      // Nav+h/j/k/l — navigate cells
      if (nav && !mod && !shift && 'hjkl'.includes(key)) {
        e.preventDefault(); e.stopPropagation();
        const newId = navigateGrid(layoutRef.current, key as 'h' | 'j' | 'k' | 'l');
        if (newId !== layoutRef.current.focusedId) handleFocusCell(newId);
        return;
      }

      // Nav+1-9 — direct cell focus
      if (nav && !mod && key >= '1' && key <= '9') {
        e.preventDefault(); e.stopPropagation();
        const idx = parseInt(key) - 1;
        const cells = layoutRef.current.cells;
        if (idx < cells.length) handleFocusCell(cells[idx].id);
        return;
      }

      // Nav+F — toggle maximize focused cell
      if (nav && !mod && !shift && key === 'f') {
        e.preventDefault(); e.stopPropagation();
        toggleMaximize();
        return;
      }

      // Nav+Shift+h/j/k/l — merge focused cell in direction
      if (nav && shift && !mod && 'hjkl'.includes(key)) {
        e.preventDefault(); e.stopPropagation();
        mergeInDirection(key as 'h' | 'j' | 'k' | 'l');
        return;
      }

      // Nav+Shift+S — split focused cell
      if (nav && shift && !mod && key === 's') {
        e.preventDefault(); e.stopPropagation();
        splitFocused();
        return;
      }

      // Nav+Shift+Arrow — swap focused cell with neighbor
      if (nav && shift && !mod && (key === 'ArrowLeft' || key === 'ArrowRight' || key === 'ArrowUp' || key === 'ArrowDown')) {
        e.preventDefault(); e.stopPropagation();
        const dirMap: Record<string, 'h' | 'j' | 'k' | 'l'> = {
          ArrowLeft: 'h',
          ArrowRight: 'l',
          ArrowUp: 'k',
          ArrowDown: 'j',
        };
        swapInDirection(dirMap[key]);
        return;
      }

      // Nav+] — next tab in focused cell
      if (nav && !mod && !shift && key === ']') {
        e.preventDefault(); e.stopPropagation();
        setLayout((prev) => {
          const cell = prev.cells.find((c) => c.id === prev.focusedId);
          if (!cell || cell.tabs.length < 2 || !cell.target) return prev;
          const idx = cell.tabs.findIndex((t) => t.target === cell.target);
          const nextIdx = (idx + 1) % cell.tabs.length;
          const nextTarget = cell.tabs[nextIdx].target;
          if (cell.target) unsubscribe(cell.target);
          subscribePane(nextTarget);
          return { ...prev, cells: prev.cells.map((c) => c.id === cell.id ? { ...c, target: nextTarget } : c) };
        });
        requestAnimationFrame(() => viewerRefsMap.current.get(layoutRef.current.focusedId)?.current?.focus());
        return;
      }

      // Nav+[ — previous tab in focused cell
      if (nav && !mod && !shift && key === '[') {
        e.preventDefault(); e.stopPropagation();
        setLayout((prev) => {
          const cell = prev.cells.find((c) => c.id === prev.focusedId);
          if (!cell || cell.tabs.length < 2 || !cell.target) return prev;
          const idx = cell.tabs.findIndex((t) => t.target === cell.target);
          const prevIdx = (idx - 1 + cell.tabs.length) % cell.tabs.length;
          const prevTarget = cell.tabs[prevIdx].target;
          if (cell.target) unsubscribe(cell.target);
          subscribePane(prevTarget);
          return { ...prev, cells: prev.cells.map((c) => c.id === cell.id ? { ...c, target: prevTarget } : c) };
        });
        requestAnimationFrame(() => viewerRefsMap.current.get(layoutRef.current.focusedId)?.current?.focus());
        return;
      }

      // Nav+Shift+[ — move current tab left (no wrap)
      // e.code is used because e.key becomes '{' when Shift is held on US layouts
      if (nav && !mod && shift && e.code === 'BracketLeft') {
        e.preventDefault(); e.stopPropagation();
        setLayout((prev) => {
          const cell = prev.cells.find((c) => c.id === prev.focusedId);
          if (!cell || cell.tabs.length < 2 || !cell.target) return prev;
          const idx = cell.tabs.findIndex((t) => t.target === cell.target);
          if (idx <= 0) return prev;
          return { ...prev, cells: prev.cells.map((c) => c.id === cell.id ? reorderTab(c, idx, idx - 1) : c) };
        });
        return;
      }

      // Nav+Shift+] — move current tab right (no wrap)
      if (nav && !mod && shift && e.code === 'BracketRight') {
        e.preventDefault(); e.stopPropagation();
        setLayout((prev) => {
          const cell = prev.cells.find((c) => c.id === prev.focusedId);
          if (!cell || cell.tabs.length < 2 || !cell.target) return prev;
          const idx = cell.tabs.findIndex((t) => t.target === cell.target);
          if (idx < 0 || idx >= cell.tabs.length - 1) return prev;
          return { ...prev, cells: prev.cells.map((c) => c.id === cell.id ? reorderTab(c, idx, idx + 1) : c) };
        });
        return;
      }

      // Nav+W — close current tab in focused cell
      if (nav && !mod && !shift && key === 'w') {
        e.preventDefault(); e.stopPropagation();
        const cell = layout.cells.find((c) => c.id === layout.focusedId);
        if (cell?.target) {
          closeTab(cell.id, cell.target);
        }
        return;
      }

      // Nav+Left — history back (only when terminal not focused, otherwise it's word nav)
      if (nav && !mod && !shift && key === 'ArrowLeft' && !document.activeElement?.classList.contains('xterm-helper-textarea')) {
        e.preventDefault(); e.stopPropagation();
        setLayout((prev) => {
          const cell = prev.cells.find((c) => c.id === prev.focusedId);
          if (!cell || cell.history.length < 2 || cell.historyIndex <= 0) return prev;
          const newIdx = cell.historyIndex - 1;
          const newTarget = cell.history[newIdx];
          if (cell.target) unsubscribe(cell.target);
          subscribePane(newTarget);
          return { ...prev, cells: prev.cells.map((c) => c.id === cell.id ? { ...c, target: newTarget, historyIndex: newIdx } : c) };
        });
        return;
      }

      // Nav+Right — history forward (only when terminal not focused)
      if (nav && !mod && !shift && key === 'ArrowRight' && !document.activeElement?.classList.contains('xterm-helper-textarea')) {
        e.preventDefault(); e.stopPropagation();
        setLayout((prev) => {
          const cell = prev.cells.find((c) => c.id === prev.focusedId);
          if (!cell || cell.historyIndex >= cell.history.length - 1) return prev;
          const newIdx = cell.historyIndex + 1;
          const newTarget = cell.history[newIdx];
          if (cell.target) unsubscribe(cell.target);
          subscribePane(newTarget);
          return { ...prev, cells: prev.cells.map((c) => c.id === cell.id ? { ...c, target: newTarget, historyIndex: newIdx } : c) };
        });
        return;
      }
    };
    // Track CapsLock by toggling on each CapsLock keydown (browser modifier state
    // timing is inconsistent across platforms, so we track it ourselves)
    const handleCapsLock = (e: KeyboardEvent) => {
      if (e.key === 'CapsLock' && e.type === 'keydown') {
        setCapsLockOn((prev) => !prev);
      }
    };
    document.addEventListener('keydown', handleGlobalKey, true);
    document.addEventListener('keydown', handleCapsLock);
    return () => { document.removeEventListener('keydown', handleGlobalKey, true); document.removeEventListener('keydown', handleCapsLock); };
  }, [switcherOpen, openSwitcher, toggleMaximize, mergeInDirection, splitFocused, swapInDirection, subscribe, unsubscribe, closeTab, layout.cells, layout.focusedId, hotkeyMenuOpen, notifOpen, ticketLookupOpen, playerOpen, activeView, cmdPaletteOpen]);

  const activeTargets = layout.cells
    .map((c) => c.target)
    .filter((t): t is string => t !== null);

  const focusedCell = layout.cells.find((c) => c.id === layout.focusedId);
  const focusedTarget = focusedCell?.target ?? null;

  // TODO: unread indicators disabled — needs investigation into why
  // cells always show as unread despite grace periods
  void unreadTickRef;
  const unreadCells = new Set<string>();

  // Workspace handlers
  const handleSaveWorkspace = useCallback(() => {
    const name = prompt('Workspace name:', workspaceName ?? '');
    if (!name) return;
    saveWorkspace(name, layout);
    setWorkspaceName(name);
    setActiveWorkspaceName(name);
  }, [layout, workspaceName]);

  const handleLoadWorkspace = useCallback(async (name: string) => {
    if (workspaceName && workspaceName !== name && isWorkspaceDirty(workspaceName, layout)) {
      const save = await confirmViaDialog({
        title: `Unsaved changes in "${workspaceName}"`,
        message: `Save changes to "${workspaceName}" before switching to "${name}"?`,
      });
      if (save) {
        saveWorkspace(workspaceName, layout);
      }
    }
    const ws = loadWorkspace(name);
    if (!ws) return;
    // Unsubscribe all current panes
    for (const cell of layout.cells) {
      if (cell.target) unsubscribe(cell.target);
    }
    setLayout(ws);
    setWorkspaceName(name);
    setActiveWorkspaceName(name);
    // Subscribe to all panes in the new workspace
    for (const cell of ws.cells) {
      if (cell.target) subscribePane(cell.target);
    }
  }, [layout, workspaceName, confirmViaDialog, unsubscribe, subscribePane]);

  const handleDeleteWorkspace = useCallback((name: string) => {
    if (!confirm(`Delete workspace "${name}"?`)) return;
    deleteWorkspace(name);
    if (workspaceName === name) {
      setWorkspaceName(null);
      setActiveWorkspaceName(null);
    }
  }, [workspaceName]);

  const handleRenameWorkspace = useCallback((oldName: string, newName: string) => {
    renameWorkspace(oldName, newName);
    if (workspaceName === oldName) {
      setWorkspaceName(newName);
      setActiveWorkspaceName(newName);
    }
  }, [workspaceName]);

  // Command palette actions
  const commands: Command[] = [
    // Layout
    { id: 'add-row', label: 'Add Row', category: 'Layout', action: () => setLayout(addRow) },
    { id: 'remove-row', label: 'Remove Row', category: 'Layout', action: () => setLayout(removeRow) },
    { id: 'add-col', label: 'Add Column', category: 'Layout', action: () => setLayout(addCol) },
    { id: 'remove-col', label: 'Remove Column', category: 'Layout', action: () => setLayout(removeCol) },
    { id: 'maximize', label: 'Toggle Maximize', category: 'Layout', shortcut: 'Alt+F', action: toggleMaximize },
    { id: 'split-cell', label: 'Split Merged Cell', category: 'Layout', shortcut: 'Alt+Shift+S', action: splitFocused },
    { id: 'merge-left', label: 'Merge Left', category: 'Layout', shortcut: 'Alt+Shift+H', action: () => mergeInDirection('h') },
    { id: 'merge-down', label: 'Merge Down', category: 'Layout', shortcut: 'Alt+Shift+J', action: () => mergeInDirection('j') },
    { id: 'merge-up', label: 'Merge Up', category: 'Layout', shortcut: 'Alt+Shift+K', action: () => mergeInDirection('k') },
    { id: 'merge-right', label: 'Merge Right', category: 'Layout', shortcut: 'Alt+Shift+L', action: () => mergeInDirection('l') },
    { id: 'swap-left', label: 'Swap Cell Left', category: 'Layout', shortcut: 'Alt+Shift+←', action: () => swapInDirection('h') },
    { id: 'swap-right', label: 'Swap Cell Right', category: 'Layout', shortcut: 'Alt+Shift+→', action: () => swapInDirection('l') },
    { id: 'swap-up', label: 'Swap Cell Up', category: 'Layout', shortcut: 'Alt+Shift+↑', action: () => swapInDirection('k') },
    { id: 'swap-down', label: 'Swap Cell Down', category: 'Layout', shortcut: 'Alt+Shift+↓', action: () => swapInDirection('j') },

    // Panels
    { id: 'pane-switcher', label: 'Open Pane Switcher', category: 'Panel', shortcut: 'Ctrl+P', action: () => openSwitcher() },
    { id: 'search', label: 'Search Pane Output', category: 'Panel', shortcut: 'Ctrl+Shift+F', action: () => setSearchOpen(true) },
    { id: 'notifications', label: 'Toggle Notifications', category: 'Panel', action: () => setNotifOpen((p) => !p) },
    { id: 'hotkeys', label: 'Show Keyboard Shortcuts', category: 'Panel', shortcut: '?', action: () => setHotkeyMenuOpen(true) },
    { id: 'toggle-sidebar', label: 'Toggle Sidebar', category: 'Panel', shortcut: 'Alt+B', action: () => setSidebarCollapsed((p) => !p) },
    { id: 'kanban', label: 'Open Kanban Board', category: 'Panel', shortcut: 'Ctrl+Shift+K', action: () => setActiveView('kanban') },
    { id: 'dashboard', label: 'Open Agent Dashboard', category: 'Panel', shortcut: 'Ctrl+Shift+D', action: () => setActiveView('dashboard') },
    { id: 'docs', label: 'Open Docs', category: 'Panel', action: () => setActiveView('docs') },
    { id: 'ticket-lookup', label: 'Ticket Lookup', category: 'Panel', shortcut: '#', action: () => setTicketLookupOpen(true) },
    { id: 'enable-notifs', label: `Notifications: ${permissionState}`, category: 'Settings', action: requestPermission },
    { id: 'notif-patterns', label: 'Notification Patterns', category: 'Settings', action: () => setNotifSettingsOpen(true) },
    { id: 'preview-size', label: `Preview Size: ${localStorage.getItem('workshop-preview-size') || 'medium'}`, category: 'Settings', action: () => {
      const sizes = ['small', 'medium', 'large'];
      const current = localStorage.getItem('workshop-preview-size') || 'medium';
      const next = sizes[(sizes.indexOf(current) + 1) % sizes.length];
      localStorage.setItem('workshop-preview-size', next);
    }},

    // Recording
    { id: 'record-start', label: 'Start Recording Focused Pane', category: 'Recording', action: () => {
      if (!focusedTarget) { alert('No pane focused'); return; }
      const name = window.prompt('Recording name:', `Recording ${focusedTarget}`);
      if (!name) return;
      startRecording(focusedTarget, name, 80, 24);
      alert(`Recording started for ${focusedTarget}`);
    }},
    { id: 'record-stop', label: 'Stop Recording Focused Pane', category: 'Recording', action: () => {
      if (!focusedTarget) { alert('No pane focused'); return; }
      stopRecording(focusedTarget);
      alert('Recording stopped');
    }},
    { id: 'record-list', label: 'View / Replay Recordings', category: 'Recording', action: () => setPlayerOpen(true) },

    // Tabs
    { id: 'close-tab', label: 'Close Current Tab', category: 'Tab', shortcut: 'Alt+W', action: () => {
      const cell = layout.cells.find((c) => c.id === layout.focusedId);
      if (cell?.target) closeTab(cell.id, cell.target);
    }},
    { id: 'next-tab', label: 'Next Tab', category: 'Tab', shortcut: 'Alt+]', action: () => {
      // trigger the same logic as Alt+]
      const cell = layout.cells.find((c) => c.id === layout.focusedId);
      if (cell && cell.tabs.length > 1 && cell.target) {
        const idx = cell.tabs.findIndex((t) => t.target === cell.target);
        switchTab(cell.id, cell.tabs[(idx + 1) % cell.tabs.length].target);
      }
    }},
    { id: 'prev-tab', label: 'Previous Tab', category: 'Tab', shortcut: 'Alt+[', action: () => {
      const cell = layout.cells.find((c) => c.id === layout.focusedId);
      if (cell && cell.tabs.length > 1 && cell.target) {
        const idx = cell.tabs.findIndex((t) => t.target === cell.target);
        switchTab(cell.id, cell.tabs[(idx - 1 + cell.tabs.length) % cell.tabs.length].target);
      }
    }},

    // Theme
    ...Object.entries(themes).map(([key, t]) => ({
      id: `theme-${key}`,
      label: `Theme: ${t.name}`,
      category: 'Theme',
      action: () => handleSetTheme(key),
    })),

    // Session
    { id: 'new-session', label: 'Create New Session', category: 'Session', action: () => {
      const name = prompt('Session name:');
      if (name) { import('./api/client').then(({ post }) => post('/sessions', { name, background: false }).then(() => refreshPanes())); }
    }},
    { id: 'launch-agent', label: 'Launch Agent', category: 'Agent', action: () => {
      setSwitcherOpen(true);
    }},

    // Workspaces

    { id: 'save-workspace', label: 'Save Current as Workspace', category: 'Workspace', action: handleSaveWorkspace },
    ...listWorkspaces().map((name) => ({
      id: `ws-load-${name}`,
      label: `Switch to: ${name}`,
      category: 'Workspace',
      action: () => handleLoadWorkspace(name),
    })),
    ...listWorkspaces().map((name) => ({
      id: `ws-delete-${name}`,
      label: `Delete: ${name}`,
      category: 'Workspace',
      action: () => handleDeleteWorkspace(name),
    })),
  ];

  const showSplit = splitView && activeView === 'kanban';

  // Refocus terminal when returning to Sessions view (or split view which also shows terminal)
  useEffect(() => {
    if (activeView === 'sessions' || showSplit) {
      requestAnimationFrame(() => {
        viewerRefsMap.current.get(layout.focusedId)?.current?.focus();
      });
    }
  }, [activeView, showSplit, layout.focusedId]);

  return (
    <AuthGate>
    <div className="app">
      {/* Mobile sidebar backdrop */}
      {!sidebarCollapsed && (
        <div className="sidebar-backdrop" onClick={() => setSidebarCollapsed(true)} />
      )}
      <Sidebar
        collapsed={sidebarCollapsed}
        onToggleCollapse={() => setSidebarCollapsed((p) => !p)}
        onSelectPane={(target) => {
          assignPaneToCell(layout.focusedId, target);
          // Auto-close sidebar only on mobile
          if (window.innerWidth < 768) setSidebarCollapsed(true);
        }}
        activeTargets={activeTargets}
        paneStatuses={paneStatuses}
        maximizedTarget={layout.maximizedId ? layout.cells.find((c) => c.id === layout.maximizedId)?.target ?? null : null}
        onFocusSession={(sessionName) => {
          // Return true if we found a cell already showing a pane from
          // this session and focused it. Prefer the maximized cell if it
          // matches, then the focused cell, then any match.
          const prefix = sessionName + ':';
          const matches = layout.cells.filter((c) => c.target?.startsWith(prefix));
          if (matches.length === 0) return false;
          const preferred =
            matches.find((c) => c.id === layout.maximizedId) ??
            matches.find((c) => c.id === layout.focusedId) ??
            matches[0];
          // If a different cell is currently maximized, move the maximize
          // to the new cell so the jump actually lands on screen instead
          // of being hidden behind the old fullscreen.
          if (layout.maximizedId && layout.maximizedId !== preferred.id) {
            setLayout((prev) => ({ ...prev, maximizedId: preferred.id }));
          }
          handleFocusCell(preferred.id);
          return true;
        }}
        onSessionRenamed={handleSessionRenamed}
        sfwMode={settings.sfwMode}
        nsfwProjects={settings.nsfwProjects}
        initSessions={initSessions}
        style={{ width: sidebarCollapsed ? undefined : sidebarWidth }}
      />
      {!sidebarCollapsed && (
        <ResizeHandle onResize={(d) => setSidebarWidth((w) => Math.min(500, Math.max(180, w + d)))} />
      )}
      <main className="main-content">
        {/* Notification permission banner — shown until user grants/denies */}
        {permissionState === 'default' && !notifBannerDismissed && (
          <div className="notif-permission-banner">
            <span>Enable notifications to get alerts when agents finish or need input</span>
            <button className="btn-create" onClick={requestPermission}>Enable</button>
            <button className="btn-small" onClick={() => setNotifBannerDismissed(true)}>Later</button>
          </div>
        )}
        <div className="mode-bar">
          <button className="mobile-menu-btn" onClick={() => setSidebarCollapsed((p) => !p)}>☰</button>
          <button
            className={`mode-tab${activeView === 'sessions' ? ' active' : ''}`}
            onClick={() => setActiveView('sessions')}
          >
            Sessions
          </button>
          <button
            className={`mode-tab${activeView === 'kanban' ? ' active' : ''}`}
            onClick={() => setActiveView('kanban')}
          >
            Kanban
          </button>
          <button
            className={`mode-tab${activeView === 'graph' ? ' active' : ''}`}
            onClick={() => setActiveView('graph')}
          >
            Graph
          </button>
          <button
            className={`mode-tab${activeView === 'dashboard' ? ' active' : ''}`}
            onClick={() => setActiveView('dashboard')}
          >
            Agents
          </button>
          <button
            className={`mode-tab${activeView === 'activity' ? ' active' : ''}`}
            onClick={() => setActiveView('activity')}
          >
            Activity
          </button>
          <button
            className={`mode-tab${activeView === 'docs' ? ' active' : ''}`}
            onClick={() => setActiveView('docs')}
          >
            Docs
          </button>
          <button
            className={`mode-tab${activeView === 'ollama' ? ' active' : ''}`}
            onClick={() => setActiveView('ollama')}
          >
            Chat
          </button>
          <button
            className={`mode-tab${activeView === 'settings' ? ' active' : ''}`}
            onClick={() => setActiveView('settings')}
          >
            Settings
          </button>
          {(activeView === 'kanban' || activeView === 'sessions') && (
            <button
              className={`mode-tab split-toggle${showSplit ? ' active' : ''}`}
              onClick={() => {
                setSplitView((prev) => {
                  if (!prev) setActiveView('kanban');
                  return !prev;
                });
              }}
              title={showSplit ? 'Exit split view (` or Ctrl+Shift+L)' : 'Split: Kanban + Terminal (` or Ctrl+Shift+L)'}
            >
              {showSplit ? '\u22A1' : '\u229F'}
            </button>
          )}
        </div>
        <div className="status-bar">
          <span className={connected ? 'status-ok' : 'status-err'}>
            {connected ? 'connected' : 'disconnected'}
          </span>
          {capsLockOn && <span className="capslock-warn" title="CapsLock is on — hotkeys still work">CAPS</span>}

          {/* Context-specific status bar content */}
          {(activeView === 'sessions' || showSplit) && (
            <>
              <WorkspaceManager
                activeWorkspace={workspaceName}
                dirty={(void workspaceSaveTick, isWorkspaceDirty(workspaceName, layout))}
                onLoad={handleLoadWorkspace}
                onSave={(name) => { saveWorkspace(name, layout); setWorkspaceName(name); setActiveWorkspaceName(name); setWorkspaceSaveTick((t) => t + 1); }}
                onDelete={handleDeleteWorkspace}
                onRename={handleRenameWorkspace}
              />
              {focusedTarget && !(settings.sfwMode && settings.nsfwProjects.some((p) => focusedTarget.toLowerCase().startsWith(p.toLowerCase() + ':'))) && <span className="active-target">{focusedTarget}</span>}
              <div className="grid-controls">
                <span className="grid-size">{layout.gridRows}x{layout.gridCols}</span>
                <button className="btn-grid" onClick={() => setLayout(addRow)} title="Add row">+R</button>
                <button className="btn-grid" onClick={() => setLayout(removeRow)} title="Remove row">-R</button>
                <button className="btn-grid" onClick={() => setLayout(addCol)} title="Add col">+C</button>
                <button className="btn-grid" onClick={() => setLayout(removeCol)} title="Remove col">-C</button>
                <button className="btn-grid" onClick={toggleMaximize} title="Maximize (Alt+F)">
                  {layout.maximizedId ? '⊡' : '⊞'}
                </button>
              </div>
            </>
          )}
          {activeView === 'kanban' && !showSplit && (
            <span className="active-target">Project Tracking</span>
          )}
          {activeView === 'dashboard' && (
            <span className="active-target">Agent Monitoring</span>
          )}
          {activeView === 'docs' && (
            <span className="active-target">Documentation</span>
          )}
          {activeView === 'ollama' && (
            <span className="active-target">Local LLM Chat</span>
          )}
          {activeView === 'settings' && (
            <span className="active-target">Preferences</span>
          )}
          {activeView === 'activity' && (
            <span className="active-target">Agent Activity</span>
          )}
          {activeView === 'graph' && (
            <span className="active-target">Dependency Graph</span>
          )}

          <div className="status-bar-right">
            <UsageBars />
            <button className="btn-switcher" onClick={() => openSwitcher()}>Ctrl+P</button>
            <button className="btn-switcher" onClick={() => setSearchOpen((p) => !p)}>Search</button>
            <button className="btn-switcher" onClick={() => setHotkeyMenuOpen((p) => !p)} title="Hotkeys (?)">?</button>
            <button className="notif-bell" onClick={() => { setNotifOpen((p) => !p); markAllRead(); }}>
              🔔{unreadCount > 0 && <span className="notif-badge">{unreadCount}</span>}
            </button>
            <select
              className="theme-select"
              value={themeName}
              onChange={(e) => handleSetTheme(e.target.value)}
            >
              {Object.entries(themes).map(([key, t]) => (
                <option key={key} value={key}>{t.name}</option>
              ))}
            </select>
          </div>
        </div>
        {switcherOpen && (
          <PaneSwitcher
            panes={allPanes}
            activeTargets={activeTargets}
            onSelect={(target) => assignPaneToCell(switcherCellId, target)}
            onClose={() => setSwitcherOpen(false)}
            onRefresh={refreshPanes}
            ticketAutocomplete={settings.ticketAutocomplete}
          />
        )}
        {cmdPaletteOpen && (
          <CommandPalette commands={commands} onClose={() => setCmdPaletteOpen(false)} />
        )}
        {playerOpen && (
          <RecordingPlayer onClose={() => setPlayerOpen(false)} />
        )}
        {notifSettingsOpen && (
          <NotificationSettings onClose={() => setNotifSettingsOpen(false)} />
        )}
        {hotkeyMenuOpen && (
          <HotkeyMenu onClose={() => setHotkeyMenuOpen(false)} />
        )}
        {ticketLookupOpen && (
          <TicketLookupDialog
            onClose={() => {
              const target = ticketInsertTarget;
              setTicketLookupOpen(false);
              setTicketInsertTarget(null);
              if (target) {
                const cell = layout.cells.find((c) => c.target === target);
                if (cell) {
                  const ref = viewerRefsMap.current.get(cell.id);
                  // Delay focus so the current keydown/keypress cycle completes before
                  // xterm gains focus — prevents Enter/Escape leaking into the PTY
                  requestAnimationFrame(() => ref?.current?.focus());
                }
              }
            }}
            onInsert={ticketInsertTarget ? (text) => handleInput(ticketInsertTarget, text) : undefined}
          />
        )}
        {notifOpen && (
          <NotificationPanel
            notifications={notifications}
            onClickNotification={(target) => {
              const cell = layout.cells.find((c) => c.target === target);
              if (cell) handleFocusCell(cell.id);
              setNotifOpen(false);
            }}
            onDismiss={dismiss}
            onClearAll={clearAll}
            onClose={() => setNotifOpen(false)}
          />
        )}
        {searchOpen && (
          <div className="search-panel-wrapper" style={{ width: searchPanelWidth }}>
            <ResizeHandle onResize={(d) => setSearchPanelWidth((w) => Math.min(window.innerWidth * 0.9, Math.max(350, w - d)))} />
            <SearchPanel
              onSelectResult={(target, searchText) => {
                const cell = layout.cells.find((c) => c.target === target);
                if (cell) {
                  handleFocusCell(cell.id);
                  viewerRefsMap.current.get(cell.id)?.current?.searchInTerminal(searchText);
                } else {
                  assignPaneToCell(layout.focusedId, target);
                }
              }}
              onClose={() => setSearchOpen(false)}
            />
          </div>
        )}
        {/* PaneGrid is always mounted to preserve terminal state.
            In split mode, kanban sits above PaneGrid as flex siblings inside main-content.
            In sessions mode, PaneGrid fills all remaining space.
            In other views, PaneGrid is hidden with display:none. */}
        {showSplit && (
          <>
            <div className="split-view-top" ref={splitContainerRef} style={{ flex: `0 0 ${splitRatio * 100}%`, overflow: 'hidden', minHeight: 100 }}>
              <KanbanBoard
                defaultProject={focusedTarget?.split(':')[0]}
                focusedPath={layout.cells.find((c) => c.id === layout.focusedId)?.target
                  ? allPanes.find((p) => p.target === focusedTarget)?.path
                  : undefined}
                onNavigateToPane={(target) => {
                  setActiveView('sessions');
                  const cell = layout.cells.find((c) => c.target === target);
                  if (cell) handleFocusCell(cell.id);
                  else assignPaneToCell(layout.focusedId, target);
                }}
                ticketAutocomplete={settings.ticketAutocomplete}
                sfwMode={settings.sfwMode}
                nsfwProjects={settings.nsfwProjects}
                openCardId={pendingOpenCardId}
                onCardOpened={() => setPendingOpenCardId(null)}
              />
            </div>
            <ResizeHandle
              direction="vertical"
              onResize={(delta) => {
                // Use main-content as the reference height for ratio calculation
                const mainEl = splitContainerRef.current?.parentElement;
                if (!mainEl) return;
                const h = mainEl.getBoundingClientRect().height;
                if (h <= 0) return;
                setSplitRatio((prev) => Math.min(0.8, Math.max(0.1, prev + delta / h)));
              }}
            />
          </>
        )}
        <div style={{ display: (activeView === 'sessions' || showSplit) ? 'contents' : 'none' }}>
          <PaneGrid
            layout={layout}
            viewerRefs={viewerRefsMap.current}
            theme={theme}
            unreadCells={unreadCells}
            paneStatuses={paneStatuses}
            onInput={handleInput}
            onResize={handleResize}
            onFocusCell={handleFocusCell}
            onAssignPane={openSwitcher}
            onSwitchTab={switchTab}
            onCloseTab={closeTab}
            onReorderTab={reorderTabInCell}
            onTicketHover={(id, x, y) => { if (!id && hoverPinnedRef.current) return; setTicketHover(id ? { id, x, y } : null); }}
            onUrlHover={(url, x, y) => { if (!url && hoverPinnedRef.current) return; setUrlHover(url ? { url, x, y } : null); }}
            onCommitHover={(sha, x, y) => { if (!sha && hoverPinnedRef.current) return; setCommitHover(sha ? { sha, x, y } : null); }}
            onTicketClick={(id) => { setActiveView('kanban'); /* TODO: focus the card */ void id; }}
            sfwMode={settings.sfwMode}
            nsfwProjects={settings.nsfwProjects}
            onHashKey={settings.terminalHashKey ? () => {
              const focusedCell = layout.cells.find((c) => c.id === layout.focusedId);
              const target = focusedCell?.target;
              if (target) {
                setTicketInsertTarget(target);
                setTicketLookupOpen(true);
              }
            } : undefined}
            agentTargets={new Set(
              allPanes
                .filter((p) => ['claude', 'gemini', 'codex'].includes(p.command) || ['claude', 'gemini', 'codex'].includes(p.windowName))
                .map((p) => p.target)
            )}
          />
        </div>
        <MobileToolbar
          visible={(activeView === 'sessions' || showSplit) && !!focusedTarget && !cmdPaletteOpen}
          onSend={(data) => { if (focusedTarget) handleInput(focusedTarget, data); }}
        />
        {ticketHover && <TicketHoverPreview cardId={ticketHover.id} x={ticketHover.x} y={ticketHover.y} pinned={hoverPinned} />}
        {urlHover && <LinkHoverPreview url={urlHover.url} x={urlHover.x} y={urlHover.y} pinned={hoverPinned} />}
        {commitHover && (() => {
          const focusedPane = allPanes.find((p) => p.target === focusedTarget);
          return focusedPane?.path ? (
            <GitCommitHoverPreview sha={commitHover.sha} repoDir={focusedPane.path} x={commitHover.x} y={commitHover.y} pinned={hoverPinned} />
          ) : null;
        })()}
        {activeView === 'kanban' && !showSplit && (
          <KanbanBoard
            defaultProject={focusedTarget?.split(':')[0]}
            focusedPath={layout.cells.find((c) => c.id === layout.focusedId)?.target
              ? allPanes.find((p) => p.target === focusedTarget)?.path
              : undefined}
            onNavigateToPane={(target) => {
              setActiveView('sessions');
              const cell = layout.cells.find((c) => c.target === target);
              if (cell) handleFocusCell(cell.id);
              else assignPaneToCell(layout.focusedId, target);
            }}
            ticketAutocomplete={settings.ticketAutocomplete}
            sfwMode={settings.sfwMode}
            nsfwProjects={settings.nsfwProjects}
            openCardId={pendingOpenCardId}
            onCardOpened={() => setPendingOpenCardId(null)}
          />
        )}
        {activeView === 'dashboard' && (
          <AgentDashboard
            onNavigateToPane={(target) => {
              setActiveView('sessions');
              const cell = layout.cells.find((c) => c.target === target);
              if (cell) handleFocusCell(cell.id);
              else assignPaneToCell(layout.focusedId, target);
            }}
            sfwMode={settings.sfwMode}
            nsfwProjects={settings.nsfwProjects}
          />
        )}
        {activeView === 'docs' && <DocsView openPath={pendingDocPath} onOpenPathConsumed={() => setPendingDocPath(null)} onTicketClick={(id) => { setActiveView('kanban'); void id; }} onTicketHover={(id, x, y) => { if (!id && hoverPinnedRef.current) return; setTicketHover(id ? { id, x, y } : null); }} />}
        {activeView === 'activity' && <ActivityView />}
        {activeView === 'ollama' && <OllamaChat />}
        {activeView === 'graph' && (
          <DependencyGraph
            defaultProject={focusedTarget?.split(':')[0]}
            onOpenCard={(id) => { setPendingOpenCardId(id); setActiveView('kanban'); }}
            onConfirm={confirmViaDialog}
          />
        )}
        {activeView === 'settings' && (
          <SettingsView
            settings={settings}
            onUpdate={updateSettings}
            themeName={themeName}
            onThemeChange={handleSetTheme}
            notificationPermission={permissionState}
            onRequestNotifications={requestPermission}
          />
        )}
      </main>
      {/* Mobile FAB for command palette */}
      <button className="mobile-fab" onClick={() => setCmdPaletteOpen(true)} title="Command Palette">
        ⌘
      </button>
      <ToastContainer toasts={toasts} onDismiss={dismissToast} />
      <ConfirmDialog
        open={uiDialog !== null}
        kind={uiDialog?.kind ?? 'confirm'}
        title={uiDialog?.title ?? ''}
        message={uiDialog?.message}
        initialValue={uiDialog?.initialValue}
        danger={uiDialog?.danger}
        onConfirm={(value) => uiDialog?.onResolve(value, false)}
        onCancel={() => uiDialog?.onResolve(undefined, true)}
      />
    </div>
    </AuthGate>
  );
}

export default App;
