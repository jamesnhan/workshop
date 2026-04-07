import { useState, useCallback, useRef, useEffect, createRef } from 'react';
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
import { WorkspaceManager } from './components/WorkspaceManager';
import { SettingsView } from './components/SettingsView';
import { ResizeHandle } from './components/ResizeHandle';
import { TicketHoverPreview } from './components/TicketHoverPreview';
import { useSettings } from './hooks/useSettings';
import { useWorkshopSocket } from './hooks/useWebSocket';
import { useNotifications } from './hooks/useNotifications';
import { get, post } from './api/client';
import type { LayoutState, PaneInfo } from './types';
import { createGrid, navigateGrid, addRow, addCol, removeRow, removeCol, mergeCells, splitCell } from './types';
import {
  loadLayout,
  restoreLayout,
  useAutoSaveLayout,
  useValidateTargets,
  saveWorkspace,
  loadWorkspace,
  deleteWorkspace,
  renameWorkspace,
  listWorkspaces,
  getActiveWorkspaceName,
  setActiveWorkspaceName,
} from './hooks/useLayoutPersistence';
import { themes, getActiveThemeName, setActiveThemeName, applyTheme } from './themes';
import './App.css';

function App() {
  const { connected, subscribe, unsubscribe, sendInput, sendResize, startRecording, stopRecording, onOutput, onStatus, onStatusClear, onReconnect } = useWorkshopSocket();
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
  const [hotkeyMenuOpen, setHotkeyMenuOpen] = useState(false);
  const [cmdPaletteOpen, setCmdPaletteOpen] = useState(false);
  const [kanbanOpen, setKanbanOpen] = useState(false);
  const [dashboardOpen, setDashboardOpen] = useState(false);
  const [docsOpen, setDocsOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [notifBannerDismissed, setNotifBannerDismissed] = useState(false);
  const [notifSettingsOpen, setNotifSettingsOpen] = useState(false);
  const [playerOpen, setPlayerOpen] = useState(false);
  const [workspaceName, setWorkspaceName] = useState<string | null>(getActiveWorkspaceName);
  const [allPanes, setAllPanes] = useState<PaneInfo[]>([]);
  // Track unread: last-output time vs last-focused time per cell
  // Initialize lastFocused to now so initial screen dump doesn't trigger unread
  const mountTime = useRef(Date.now());
  const cellLastOutput = useRef<Record<string, number>>({});
  const cellLastFocused = useRef<Record<string, number>>({});
  // Unread tracking is disabled (see TODO below) — use ref to avoid root re-renders
  const unreadTickRef = useRef(0);
  const { notifications, unreadCount, scanOutput, markSubscribed, markAllRead, dismiss, clearAll, requestPermission, permissionState } = useNotifications();
  const [capsLockOn, setCapsLockOn] = useState(false);
  const { settings, updateSettings } = useSettings();
  const [themeName, setThemeName] = useState(getActiveThemeName);
  const theme = themes[themeName] || themes['catppuccin-mocha'];

  useEffect(() => { applyTheme(theme); }, [theme]);
  useEffect(() => { requestPermission(); }, [requestPermission]);

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
    const focused = layoutRef.current.cells.find((c) => c.id === focusedId);
    scanOutput(target, data, focused?.target ?? null);
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

  // On WebSocket reconnect, clear all terminal state to avoid garbled rendering
  onReconnect(useCallback(() => {
    for (const [, ref] of viewerRefsMap.current) {
      if (ref.current) {
        // Clear terminal and let fresh PTY output repopulate
        ref.current.write('\x1b[2J\x1b[H'); // clear screen + cursor home
      }
    }
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

  useEffect(() => { refreshPanes(); }, [refreshPanes]);

  useAutoSaveLayout(layout);
  useValidateTargets(allPanes, setLayout);

  // Restore subscriptions on mount
  const hasRestored = useRef(false);
  useEffect(() => {
    if (hasRestored.current || allPanes.length === 0) return;
    hasRestored.current = true;
    for (const cell of layout.cells) {
      if (cell.target && allPanes.some((p) => p.target === cell.target)) {
        subscribePane(cell.target);
      }
    }
  }, [allPanes, layout.cells, subscribe]);

  // Assign pane to cell by ID, adding it to the cell's tab history
  const assignPaneToCell = useCallback((cellId: string, target: string) => {
    const info = allPanes.find((p) => p.target === target);
    const label = info?.windowName || target.split(':').pop() || target;

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
        fetch('/api/v1/panes/status', { method: 'DELETE', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ target: cell.target }) });
        setPaneStatuses((ps) => { const next = { ...ps }; delete next[cell.target!]; return next; });
      }
      return { ...prev, focusedId: cellId };
    });
    cellLastFocused.current[cellId] = Date.now();
    unreadTickRef.current++; // no-op while unread indicators disabled
  }, [paneStatuses]);

  // Auto-focus terminal on cell change
  useEffect(() => {
    requestAnimationFrame(() => {
      viewerRefsMap.current.get(layout.focusedId)?.current?.focus();
    });
  }, [layout.focusedId]);

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

  // Clear status on user keyboard input in focused terminal
  const paneStatusesRef = useRef(paneStatuses);
  paneStatusesRef.current = paneStatuses;
  useEffect(() => {
    const handleUserInput = () => {
      if (!document.activeElement?.classList.contains('xterm-helper-textarea')) return;
      const focusedCell = layoutRef.current.cells.find((c) => c.id === layoutRef.current.focusedId);
      const target = focusedCell?.target;
      if (target && paneStatusesRef.current[target]) {
        fetch('/api/v1/panes/status', { method: 'DELETE', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ target }) });
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

      if (key === 'Escape' && playerOpen) { setPlayerOpen(false); return; }
      if (key === 'Escape' && dashboardOpen) { setDashboardOpen(false); return; }
      if (key === 'Escape' && settingsOpen) { setSettingsOpen(false); return; }
      if (key === 'Escape' && kanbanOpen) { setKanbanOpen(false); return; }
      if (key === 'Escape' && cmdPaletteOpen) { setCmdPaletteOpen(false); return; }
      if (key === 'Escape' && switcherOpen) { setSwitcherOpen(false); return; }
      if (key === 'Escape' && hotkeyMenuOpen) { setHotkeyMenuOpen(false); return; }
      if (key === 'Escape' && notifOpen) { setNotifOpen(false); return; }

      // ? — hotkey menu (only when not typing in a terminal)
      if (e.key === '?' && !mod && !nav) {
        const active = document.activeElement;
        const inTerminal = active?.classList.contains('xterm-helper-textarea');
        if (!inTerminal) {
          e.preventDefault();
          setHotkeyMenuOpen((p) => !p);
          return;
        }
      }

      // Mod+Shift+D — agent dashboard
      if (mod && shift && key === 'd') {
        e.preventDefault(); e.stopPropagation();
        setDashboardOpen((p) => !p);
        return;
      }

      // Mod+Shift+K — kanban board
      if (mod && shift && key === 'k') {
        e.preventDefault(); e.stopPropagation();
        setKanbanOpen((p) => !p);
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
  }, [switcherOpen, openSwitcher, toggleMaximize, mergeInDirection, splitFocused, subscribe, unsubscribe, closeTab, layout.cells, layout.focusedId]);

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

  const handleLoadWorkspace = useCallback((name: string) => {
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
  }, [layout.cells, unsubscribe, subscribePane]);

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

    // Panels
    { id: 'pane-switcher', label: 'Open Pane Switcher', category: 'Panel', shortcut: 'Ctrl+P', action: () => openSwitcher() },
    { id: 'search', label: 'Search Pane Output', category: 'Panel', shortcut: 'Ctrl+Shift+F', action: () => setSearchOpen(true) },
    { id: 'notifications', label: 'Toggle Notifications', category: 'Panel', action: () => setNotifOpen((p) => !p) },
    { id: 'hotkeys', label: 'Show Keyboard Shortcuts', category: 'Panel', shortcut: '?', action: () => setHotkeyMenuOpen(true) },
    { id: 'toggle-sidebar', label: 'Toggle Sidebar', category: 'Panel', shortcut: 'Alt+B', action: () => setSidebarCollapsed((p) => !p) },
    { id: 'kanban', label: 'Open Kanban Board', category: 'Panel', shortcut: 'Ctrl+Shift+K', action: () => setKanbanOpen(true) },
    { id: 'dashboard', label: 'Open Agent Dashboard', category: 'Panel', shortcut: 'Ctrl+Shift+D', action: () => setDashboardOpen(true) },
    { id: 'docs', label: 'Open Docs', category: 'Panel', action: () => { setDocsOpen(true); setKanbanOpen(false); setDashboardOpen(false); } },
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
      if (name) { import('./api/client').then(({ post }) => post('/sessions', { name }).then(() => refreshPanes())); }
    }},
    { id: 'launch-agent', label: 'Launch Agent', category: 'Agent', action: () => {
      setSwitcherOpen(true);
    }},

    // Workspaces
    // Consensus
    { id: 'consensus', label: 'Start Consensus Run', category: 'AI', action: async () => {
      const dir = window.prompt('Working directory (required):', '~/repos/workshop');
      if (!dir) return;
      const prompt = window.prompt('Enter prompt for all agents:');
      if (!prompt) return;
      const agentCount = window.prompt('Number of agents:', '3');
      const n = parseInt(agentCount || '3') || 3;
      const agents = Array.from({ length: n }, (_, i) => ({ name: `agent-${i + 1}`, model: 'sonnet' }));
      try {
        const res = await post<{ id: string }>('/consensus', {
          prompt,
          directory: dir,
          agents,
          timeout: 600,
        });
        alert(`Consensus run started: ${res.id}\n${n} agents working in ${dir}.\nCheck sidebar for new sessions.`);
      } catch (err) {
        alert('Failed to start consensus: ' + err);
      }
    }},

    { id: 'consensus-status', label: 'Check Consensus Status', category: 'AI', action: async () => {
      try {
        const runs = await get<any[]>('/consensus');
        if (!runs || runs.length === 0) {
          alert('No consensus runs.');
          return;
        }
        const latest = runs[runs.length - 1];
        const agents = (latest.agentOutputs || []).map((a: any) =>
          `  ${a.name} (${a.model}): ${a.status}${a.needsInput ? ' ⚠️ NEEDS INPUT' : ''}`
        ).join('\n');
        const needsInput = (latest.agentOutputs || []).filter((a: any) => a.needsInput);
        let msg = `Consensus: ${latest.id}\nStatus: ${latest.status}\n\nAgents:\n${agents}`;
        if (needsInput.length > 0) {
          msg += `\n\n⚠️ ${needsInput.length} agent(s) need input! Jump to them in the sidebar.`;
        }
        alert(msg);
        // If any agent needs input, assign it to the focused cell
        if (needsInput.length > 0) {
          assignPaneToCell(layout.focusedId, needsInput[0].target);
        }
      } catch (err) {
        alert('Failed to check consensus: ' + err);
      }
    }},

    { id: 'consensus-cleanup', label: 'Cleanup Consensus Sessions', category: 'AI', action: async () => {
      try {
        const runs = await get<any[]>('/consensus');
        if (!runs || runs.length === 0) { alert('No consensus runs.'); return; }
        const latest = runs[runs.length - 1];
        if (!confirm(`Cleanup sessions for ${latest.id}?`)) return;
        await fetch(`/api/v1/consensus/${latest.id}`, { method: 'DELETE' });
        alert('Consensus sessions cleaned up.');
      } catch (err) {
        alert('Failed: ' + err);
      }
    }},

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

  const activeView = kanbanOpen ? 'kanban' : dashboardOpen ? 'dashboard' : docsOpen ? 'docs' : settingsOpen ? 'settings' : 'sessions';

  // Refocus terminal when returning to Sessions view
  useEffect(() => {
    if (activeView === 'sessions') {
      requestAnimationFrame(() => {
        viewerRefsMap.current.get(layout.focusedId)?.current?.focus();
      });
    }
  }, [activeView, layout.focusedId]);

  return (
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
            onClick={() => { setKanbanOpen(false); setDashboardOpen(false); setDocsOpen(false); setSettingsOpen(false); }}
          >
            Sessions
          </button>
          <button
            className={`mode-tab${activeView === 'kanban' ? ' active' : ''}`}
            onClick={() => { setKanbanOpen(true); setDashboardOpen(false); setDocsOpen(false); setSettingsOpen(false); }}
          >
            Kanban
          </button>
          <button
            className={`mode-tab${activeView === 'dashboard' ? ' active' : ''}`}
            onClick={() => { setDashboardOpen(true); setKanbanOpen(false); setDocsOpen(false); setSettingsOpen(false); }}
          >
            Agents
          </button>
          <button
            className={`mode-tab${activeView === 'docs' ? ' active' : ''}`}
            onClick={() => { setDocsOpen(true); setKanbanOpen(false); setDashboardOpen(false); setSettingsOpen(false); }}
          >
            Docs
          </button>
          <button
            className={`mode-tab${activeView === 'settings' ? ' active' : ''}`}
            onClick={() => { setSettingsOpen(true); setKanbanOpen(false); setDashboardOpen(false); setDocsOpen(false); }}
          >
            Settings
          </button>
        </div>
        <div className="status-bar">
          <span className={connected ? 'status-ok' : 'status-err'}>
            {connected ? 'connected' : 'disconnected'}
          </span>
          {capsLockOn && <span className="capslock-warn" title="CapsLock is on — hotkeys still work">CAPS</span>}

          {/* Context-specific status bar content */}
          {activeView === 'sessions' && (
            <>
              <WorkspaceManager
                activeWorkspace={workspaceName}
                onLoad={handleLoadWorkspace}
                onSave={(name) => { saveWorkspace(name, layout); setWorkspaceName(name); setActiveWorkspaceName(name); }}
                onDelete={handleDeleteWorkspace}
                onRename={handleRenameWorkspace}
              />
              {focusedTarget && <span className="active-target">{focusedTarget}</span>}
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
          {activeView === 'kanban' && (
            <span className="active-target">Project Tracking</span>
          )}
          {activeView === 'dashboard' && (
            <span className="active-target">Agent Monitoring</span>
          )}
          {activeView === 'docs' && (
            <span className="active-target">Documentation</span>
          )}
          {activeView === 'settings' && (
            <span className="active-target">Preferences</span>
          )}

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
        {switcherOpen && (
          <PaneSwitcher
            panes={allPanes}
            activeTargets={activeTargets}
            onSelect={(target) => assignPaneToCell(switcherCellId, target)}
            onClose={() => setSwitcherOpen(false)}
            onRefresh={refreshPanes}
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
        {/* PaneGrid always mounted (hidden when not active) to preserve terminal state */}
        <div style={{ display: activeView === 'sessions' ? 'contents' : 'none' }}>
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
            onTicketHover={(id, x, y) => setTicketHover(id ? { id, x, y } : null)}
            onTicketClick={(id) => { setKanbanOpen(true); /* TODO: focus the card */ void id; }}
          />
        </div>
        {ticketHover && <TicketHoverPreview cardId={ticketHover.id} x={ticketHover.x} y={ticketHover.y} />}
        {activeView === 'kanban' && (
          <KanbanBoard
            defaultProject={focusedTarget?.split(':')[0]}
            focusedPath={layout.cells.find((c) => c.id === layout.focusedId)?.target
              ? allPanes.find((p) => p.target === focusedTarget)?.path
              : undefined}
            onNavigateToPane={(target) => {
              setKanbanOpen(false);
              setDashboardOpen(false);
              const cell = layout.cells.find((c) => c.target === target);
              if (cell) handleFocusCell(cell.id);
              else assignPaneToCell(layout.focusedId, target);
            }}
          />
        )}
        {activeView === 'dashboard' && (
          <AgentDashboard
            onNavigateToPane={(target) => {
              setKanbanOpen(false);
              setDashboardOpen(false);
              setDocsOpen(false);
              const cell = layout.cells.find((c) => c.target === target);
              if (cell) handleFocusCell(cell.id);
              else assignPaneToCell(layout.focusedId, target);
            }}
          />
        )}
        {activeView === 'docs' && <DocsView />}
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
    </div>
  );
}

export default App;
