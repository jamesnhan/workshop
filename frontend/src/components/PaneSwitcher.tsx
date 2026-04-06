import { useEffect, useRef, useState } from 'react';
import { get, post, del } from '../api/client';

interface PaneInfo {
  id: string;
  target: string;
  windowName: string;
  command: string;
  path: string;
  width: number;
  height: number;
  active: boolean;
}

interface Props {
  panes: PaneInfo[];
  activeTargets: string[];
  onSelect: (target: string) => void;
  onClose: () => void;
  onRefresh: () => void;
}

// Simple fuzzy match: all query chars must appear in order in the haystack
function fuzzyMatch(query: string, haystack: string): { match: boolean; score: number } {
  const q = query.toLowerCase();
  const h = haystack.toLowerCase();
  if (q.length === 0) return { match: true, score: 0 };

  let qi = 0;
  let score = 0;
  let lastMatchIdx = -1;

  for (let hi = 0; hi < h.length && qi < q.length; hi++) {
    if (h[hi] === q[qi]) {
      // Bonus for consecutive matches
      if (lastMatchIdx === hi - 1) score += 10;
      // Bonus for matching at word boundaries
      if (hi === 0 || h[hi - 1] === ':' || h[hi - 1] === '.' || h[hi - 1] === '/' || h[hi - 1] === ' ') score += 5;
      score += 1;
      lastMatchIdx = hi;
      qi++;
    }
  }

  return { match: qi === q.length, score };
}

// Build a searchable string from pane info
function searchText(p: PaneInfo): string {
  return `${p.target} ${p.windowName} ${p.command} ${p.path}`;
}

// Shorten path for display
function shortPath(path: string): string {
  const home = '/home/';
  if (path.startsWith(home)) {
    const afterHome = path.substring(home.length);
    const slashIdx = afterHome.indexOf('/');
    if (slashIdx >= 0) return '~' + afterHome.substring(slashIdx);
    return '~';
  }
  return path;
}

type Mode = 'search' | 'create' | 'agent' | 'manage';

const tabs: { key: Mode; label: string }[] = [
  { key: 'search', label: 'Search' },
  { key: 'agent', label: 'Agent' },
  { key: 'create', label: 'Session' },
  { key: 'manage', label: 'Manage' },
];

export function PaneSwitcher({ panes, activeTargets, onSelect, onClose, onRefresh }: Props) {
  const [query, setQuery] = useState('');
  const [selectedIdx, setSelectedIdx] = useState(0);
  const [mode, setMode] = useState<Mode>('search');
  const [navMode, setNavMode] = useState(false); // vim nav in search results
  const [newSessionName, setNewSessionName] = useState('');
  const [agentName, setAgentName] = useState('');
  const [agentProvider, setAgentProvider] = useState('claude');
  const [agentDir, setAgentDir] = useState('');
  const [agentPrompt, setAgentPrompt] = useState('');
  const [agentModel, setAgentModel] = useState('');
  const [agentLaunching, setAgentLaunching] = useState(false);
  const [availableProviders, setAvailableProviders] = useState<string[]>(['claude']);

  // Fetch available providers on mount
  useEffect(() => {
    get<string[]>('/agents/providers')
      .then((p) => { if (p?.length) { setAvailableProviders(p); setAgentProvider(p[0]); } })
      .catch(() => {});
  }, []);
  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLUListElement>(null);

  const filtered = query.length === 0
    ? panes
    : panes
        .map((p) => ({ pane: p, ...fuzzyMatch(query, searchText(p)) }))
        .filter((r) => r.match)
        .sort((a, b) => b.score - a.score)
        .map((r) => r.pane);

  useEffect(() => {
    inputRef.current?.focus();
  }, [mode]);

  useEffect(() => {
    setSelectedIdx(0);
    setNavMode(false);
  }, [query]);

  // Scroll selected into view
  useEffect(() => {
    const el = listRef.current?.children[selectedIdx] as HTMLElement | undefined;
    el?.scrollIntoView({ block: 'nearest' });
  }, [selectedIdx]);

  const handleKeyDown = (e: React.KeyboardEvent) => {
    // Tab / Shift+Tab to cycle Ctrl+P tabs
    if (e.key === 'Tab' && !e.ctrlKey && !e.altKey) {
      e.preventDefault();
      const currentIdx = tabs.findIndex((t) => t.key === mode);
      const nextIdx = e.shiftKey
        ? (currentIdx - 1 + tabs.length) % tabs.length
        : (currentIdx + 1) % tabs.length;
      setMode(tabs[nextIdx].key);
      setNavMode(false);
      return;
    }

    // Search tab with nav mode
    if (mode === 'search') {
      if (navMode) {
        // NAV mode: j/k to move, Enter to select, Esc/i/slash back to typing
        if (e.key === 'j' || e.key === 'ArrowDown') {
          e.preventDefault();
          setSelectedIdx((i) => Math.min(i + 1, filtered.length - 1));
        } else if (e.key === 'k' || e.key === 'ArrowUp') {
          e.preventDefault();
          setSelectedIdx((i) => Math.max(i - 1, 0));
        } else if (e.key === 'Enter' && filtered.length > 0) {
          e.preventDefault();
          onSelect(filtered[selectedIdx].target);
        } else if (e.key === 'Escape' || e.key === 'i' || e.key === '/') {
          e.preventDefault();
          setNavMode(false);
          inputRef.current?.focus();
        }
        return;
      }

      // FIND mode
      if (e.key === 'ArrowDown') {
        e.preventDefault();
        setSelectedIdx((i) => Math.min(i + 1, filtered.length - 1));
      } else if (e.key === 'ArrowUp') {
        e.preventDefault();
        setSelectedIdx((i) => Math.max(i - 1, 0));
      } else if (e.key === 'Enter' && filtered.length > 0) {
        e.preventDefault();
        // If query is empty or no filtering, just select. Otherwise enter nav mode.
        if (query.trim().length === 0) {
          onSelect(filtered[selectedIdx].target);
        } else {
          setNavMode(true);
        }
      } else if (e.key === 'Escape') {
        onClose();
      }
      return;
    }

    // Other tabs (create, agent, manage)
    if (e.key === 'Enter') {
      e.preventDefault();
      if (mode === 'create' && newSessionName.trim()) {
        handleCreateSession();
      }
    } else if (e.key === 'Escape') {
      setMode('search');
      setNavMode(false);
    }
  };

  const handleCreateSession = async () => {
    const name = newSessionName.trim();
    if (!name) return;
    try {
      await post('/sessions', { name });
      setNewSessionName('');
      setMode('search');
      onRefresh();
    } catch (err) {
      console.error('Failed to create session:', err);
    }
  };

  const handleLaunchAgent = async () => {
    setAgentLaunching(true);
    try {
      const res = await post<{ target: string }>('/agents/launch', {
        name: agentName.trim() || undefined,
        provider: agentProvider || undefined,
        directory: agentDir.trim() || undefined,
        prompt: agentPrompt.trim() || undefined,
        model: agentModel.trim() || undefined,
      });
      setAgentName('');
      setAgentProvider('claude');
      setAgentDir('');
      setAgentPrompt('');
      setAgentModel('');
      onRefresh();
      onSelect(res.target);
    } catch (err) {
      console.error('Failed to launch agent:', err);
    } finally {
      setAgentLaunching(false);
    }
  };

  const handleDeleteSession = async (sessionName: string, e: React.MouseEvent) => {
    e.stopPropagation();
    if (!confirm(`Kill session "${sessionName}"?`)) return;
    try {
      await del(`/sessions/${sessionName}`);
      onRefresh();
    } catch (err) {
      console.error('Failed to kill session:', err);
    }
  };

  // Group panes by session for the manage view
  const sessionGroups = new Map<string, PaneInfo[]>();
  for (const p of panes) {
    const session = p.target.split(':')[0];
    if (!sessionGroups.has(session)) sessionGroups.set(session, []);
    sessionGroups.get(session)!.push(p);
  }

  return (
    <div className="switcher-overlay" onClick={onClose}>
      <div className="switcher" onClick={(e) => e.stopPropagation()} onKeyDown={handleKeyDown}>
        <div className="switcher-tabs">
          {tabs.map((t) => (
            <button key={t.key} className={mode === t.key ? 'active' : ''} onClick={() => { setMode(t.key); setNavMode(false); }}>
              {t.label}
            </button>
          ))}
        </div>

        {mode === 'search' && (
          <>
            <div className="switcher-input-row">
              <input
                ref={inputRef}
                type="text"
                className="switcher-input"
                placeholder={navMode ? 'NAV: j/k, Enter select, / search' : 'Search panes...'}
                value={query}
                onChange={(e) => { setQuery(e.target.value); setNavMode(false); }}
                readOnly={navMode}
              />
              {navMode && <span className="switcher-nav-badge">NAV</span>}
            </div>
            <ul className="switcher-list" ref={listRef}>
              {filtered.map((p, i) => (
                <li
                  key={p.target}
                  className={`switcher-item${i === selectedIdx ? ' selected' : ''}${activeTargets.includes(p.target) ? ' current' : ''}`}
                  onClick={() => onSelect(p.target)}
                  onMouseEnter={() => { if (!navMode) setSelectedIdx(i); }}
                >
                  <div className="switcher-item-main">
                    <span className="switcher-target">{p.target}</span>
                    {p.windowName && <span className="switcher-window">{p.windowName}</span>}
                  </div>
                  <div className="switcher-item-meta">
                    {p.command && <span className="switcher-command">{p.command}</span>}
                    {p.path && <span className="switcher-path">{shortPath(p.path)}</span>}
                  </div>
                </li>
              ))}
              {filtered.length === 0 && (
                <li className="switcher-item muted">No matching panes</li>
              )}
            </ul>
          </>
        )}

        {mode === 'agent' && (
          <div className="switcher-agent">
            <input
              ref={inputRef}
              type="text"
              className="switcher-input"
              placeholder="Agent name (optional, auto-generated)"
              value={agentName}
              onChange={(e) => setAgentName(e.target.value)}
            />
            <div className="switcher-row">
              <select
                className="switcher-select"
                value={agentProvider}
                onChange={(e) => {
                  setAgentProvider(e.target.value);
                  setAgentModel('');
                }}
              >
                {availableProviders.map((p) => (
                  <option key={p} value={p}>{p.charAt(0).toUpperCase() + p.slice(1)}</option>
                ))}
              </select>
              <input
                type="text"
                className="switcher-input"
                placeholder={agentProvider === 'claude' ? 'Model (opus, sonnet, haiku)' : agentProvider === 'gemini' ? 'Model (pro, flash, flash-lite)' : 'Model (gpt-5-codex, gpt-5.4)'}
                value={agentModel}
                onChange={(e) => setAgentModel(e.target.value)}
              />
            </div>
            <input
              type="text"
              className="switcher-input"
              placeholder="Working directory (default: ~)"
              value={agentDir}
              onChange={(e) => setAgentDir(e.target.value)}
            />
            <textarea
              className="switcher-textarea"
              placeholder="Initial prompt (optional — launches interactive if empty)"
              value={agentPrompt}
              onChange={(e) => setAgentPrompt(e.target.value)}
              rows={3}
            />
            <button
              className="btn-create"
              onClick={handleLaunchAgent}
              disabled={agentLaunching}
            >
              {agentLaunching ? 'Launching...' : 'Launch Agent'}
            </button>
          </div>
        )}

        {mode === 'create' && (
          <div className="switcher-create">
            <input
              ref={inputRef}
              type="text"
              className="switcher-input"
              placeholder="Session name..."
              value={newSessionName}
              onChange={(e) => setNewSessionName(e.target.value)}
              onKeyDown={handleKeyDown}
            />
            <button className="btn-create" onClick={handleCreateSession}>
              Create Session
            </button>
          </div>
        )}

        {mode === 'manage' && (
          <ul className="switcher-list">
            {Array.from(sessionGroups.entries()).map(([session, sessionPanes]) => (
              <li key={session} className="switcher-manage-item">
                <div className="switcher-manage-header">
                  <span className="switcher-session-name">{session}</span>
                  <span className="switcher-pane-count">{sessionPanes.length} pane{sessionPanes.length !== 1 ? 's' : ''}</span>
                  <button
                    className="btn-danger-small"
                    onClick={(e) => handleDeleteSession(session, e)}
                    title="Kill session"
                  >
                    x
                  </button>
                </div>
                <div className="switcher-manage-panes">
                  {sessionPanes.map((p) => (
                    <span key={p.target} className="switcher-manage-pane" onClick={() => onSelect(p.target)}>
                      {p.target} ({p.command})
                    </span>
                  ))}
                </div>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}
