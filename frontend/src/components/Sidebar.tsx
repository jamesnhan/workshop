import { useEffect, useState, useCallback, useRef } from 'react';
import { get, post, del, patch } from '../api/client';
import AnsiToHtml from 'ansi-to-html';
import DOMPurify from 'dompurify';
import { ConfirmDialog, type DialogKind } from './ConfirmDialog';
import { isHoverPinned, onHoverPinChange } from '../hooks/useHoverPin';
import { GitInfoHoverPreview } from './GitInfoHoverPreview';

interface Session {
  name: string;
  windows: number;
  attached: boolean;
  hidden?: boolean;
}

interface Pane {
  id: string;
  target: string;
  windowName: string;
  command: string;
  path: string;
  width: number;
  height: number;
  active: boolean;
}

interface GitInfo {
  repoName: string;
  branch: string;
  dirty: boolean;
  ahead: number;
  behind: number;
  changed: number;
  untracked: number;
  recentLogs: string[];
}

interface GitHoverState {
  sessionName: string;
  x: number;
  y: number;
}

interface Props {
  collapsed: boolean;
  onToggleCollapse: () => void;
  onSelectPane: (target: string) => void;
  activeTargets: string[];
  paneStatuses: Record<string, { status: string; message: string }>;
  onSessionRenamed?: (oldName: string, newName: string) => void;
  /** Target of the currently fullscreened (maximized) pane, if any. */
  maximizedTarget?: string | null;
  /**
   * If any cell in the parent layout is already showing a pane from
   * `sessionName`, focus that cell and return true. Otherwise return
   * false so the sidebar falls back to its default behavior (expand
   * the row / open the collapsed sidebar).
   */
  onFocusSession?: (sessionName: string) => boolean;
  style?: React.CSSProperties;
  sfwMode?: boolean;
  nsfwProjects?: string[];
  /** Pre-fetched sessions from /init batch endpoint. Skips first /sessions fetch if provided. */
  initSessions?: { name: string; windows: number; attached: boolean }[] | null;
}

// Severity rank for aggregating a session's worst pane status.
const STATUS_RANK: Record<string, number> = { green: 1, yellow: 2, red: 3 };

const ansiConverter = new AnsiToHtml({
  fg: '#cdd6f4',
  bg: 'transparent',
});

type PreviewSize = 'small' | 'medium' | 'large';

const PREVIEW_LINES: Record<PreviewSize, number> = { small: 20, medium: 30, large: 50 };

function getPreviewSize(): PreviewSize {
  const stored = localStorage.getItem('workshop-preview-size');
  if (stored === 'small' || stored === 'medium' || stored === 'large') return stored;
  return 'medium';
}

const STATUS_COLORS: Record<string, string> = {
  green: 'var(--success)',
  yellow: 'var(--warning, #f9e2af)',
  red: 'var(--error)',
};

// Group sessions by project. We use the git repo name (already fetched for
// the badge) as the project key — for this user, session name and repo
// name converge most of the time. Sessions without gitInfo land under the
// catch-all `OTHER_GROUP`. Groups are returned sorted alphabetically with
// "Other" pinned at the bottom.
export const OTHER_GROUP = 'Other';
export interface SessionGroup {
  project: string;
  sessions: Session[];
}
export function groupSessions(
  sessions: Session[],
  gitInfo: Record<string, { repoName: string }>,
): SessionGroup[] {
  const byProject = new Map<string, Session[]>();
  for (const s of sessions) {
    const project = gitInfo[s.name]?.repoName || OTHER_GROUP;
    const bucket = byProject.get(project);
    if (bucket) bucket.push(s);
    else byProject.set(project, [s]);
  }
  return Array.from(byProject.entries())
    .map(([project, sessions]) => ({ project, sessions }))
    .sort((a, b) => {
      if (a.project === OTHER_GROUP) return 1;
      if (b.project === OTHER_GROUP) return -1;
      return a.project.localeCompare(b.project);
    });
}

const SIDEBAR_GROUPS_KEY = 'workshop:sidebar-collapsed-groups';
function loadCollapsedGroups(): Set<string> {
  try {
    const raw = localStorage.getItem(SIDEBAR_GROUPS_KEY);
    if (!raw) return new Set();
    const parsed = JSON.parse(raw);
    if (Array.isArray(parsed)) return new Set(parsed.filter((x) => typeof x === 'string'));
  } catch {}
  return new Set();
}
function saveCollapsedGroups(groups: Set<string>): void {
  try { localStorage.setItem(SIDEBAR_GROUPS_KEY, JSON.stringify(Array.from(groups))); } catch {}
}

export function Sidebar({ collapsed, onToggleCollapse, onSelectPane, activeTargets, paneStatuses, onSessionRenamed, maximizedTarget, onFocusSession, style, sfwMode = false, nsfwProjects = [], initSessions }: Props) {
  const [sessions, setSessions] = useState<Session[]>(() => (initSessions as Session[]) ?? []);
  const [panes, setPanes] = useState<Record<string, Pane[]>>({});
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [showHidden, setShowHidden] = useState(false);
  const [gitInfo, setGitInfo] = useState<Record<string, GitInfo>>({});
  const [gitHover, setGitHover] = useState<GitHoverState | null>(null);
  const [hoverTarget, setHoverTarget] = useState<string | null>(null);
  const [collapsedGroups, setCollapsedGroups] = useState<Set<string>>(() => loadCollapsedGroups());
  const toggleGroup = useCallback((project: string) => {
    setCollapsedGroups((prev) => {
      const next = new Set(prev);
      if (next.has(project)) next.delete(project);
      else next.add(project);
      saveCollapsedGroups(next);
      return next;
    });
  }, []);

  // Clear hovers when global pin is released
  useEffect(() => onHoverPinChange(() => {
    if (!isHoverPinned()) { setGitHover(null); setHoverTarget(null); setHoverPreview(null); }
  }), []);
  const [hoverPreview, setHoverPreview] = useState<string | null>(null);
  const [hoverPos, setHoverPos] = useState({ x: 0, y: 0 });
  const previewSize = getPreviewSize();
  const hoverTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Themed dialog state
  const [dialog, setDialog] = useState<{
    kind: DialogKind;
    title: string;
    message?: string;
    initialValue?: string;
    confirmLabel?: string;
    danger?: boolean;
    onConfirm: (value?: string) => void;
  } | null>(null);

  const nsfwSet = new Set(nsfwProjects.map((p) => p.toLowerCase()));
  const isNsfw = (name: string) => sfwMode && nsfwSet.has(name.toLowerCase());
  const visibleSessions = sfwMode ? sessions.filter((s) => !isNsfw(s.name)) : sessions;

  const refresh = useCallback(() => {
    const q = showHidden ? '?all=true' : '';
    get<Session[]>(`/sessions${q}`)
      .then((data) => setSessions(data ?? []))
      .catch(() => {});
  }, [showHidden]);

  // Ensure we have pane info (for the cwd) for every known session, then
  // re-fetch git info. This powers the badges without requiring the user
  // to expand each session first, and keeps ahead/behind/dirty counts
  // live instead of snapshotted at expand time.
  const gitFetchingRef = useRef(false);
  const refreshGitInfo = useCallback((sessionList: Session[]) => {
    if (gitFetchingRef.current) return; // Skip if previous batch still running
    gitFetchingRef.current = true;
    const promises = sessionList.map(async (s) => {
      let paneList = panesRef.current[s.name];
      if (!paneList) {
        try {
          paneList = (await get<Pane[]>(`/sessions/${s.name}/panes`)) ?? [];
          setPanes((prev) => ({ ...prev, [s.name]: paneList! }));
        } catch {
          return;
        }
      }
      const path = paneList[0]?.path;
      if (!path) return;
      try {
        const info = await get<GitInfo>(`/git/info?dir=${encodeURIComponent(path)}`);
        if (info) setGitInfo((prev) => ({ ...prev, [s.name]: info }));
      } catch { /* not a git repo */ }
    });
    Promise.allSettled(promises).finally(() => { gitFetchingRef.current = false; });
  }, []);

  // panesRef mirrors `panes` so refreshGitInfo can read the latest value
  // inside the interval without re-binding the callback on every change.
  const panesRef = useRef<Record<string, Pane[]>>({});
  useEffect(() => { panesRef.current = panes; }, [panes]);

  // sessionsRef lets the git polling interval read the latest sessions
  // without re-creating the interval every time sessions changes.
  const sessionsRef = useRef<Session[]>([]);
  useEffect(() => { sessionsRef.current = sessions; }, [sessions]);

  useEffect(() => {
    refresh();
    const interval = setInterval(refresh, 5000);
    return () => clearInterval(interval);
  }, [refresh]);

  // Fan out git info fetches on mount and every 5s. Uses a ref for sessions
  // to avoid tearing down the interval every time the session list updates.
  useEffect(() => {
    refreshGitInfo(sessions);
    const interval = setInterval(() => refreshGitInfo(sessionsRef.current), 5000);
    return () => clearInterval(interval);
  }, [refreshGitInfo]); // eslint-disable-line react-hooks/exhaustive-deps

  const toggleSession = async (name: string) => {
    const next = new Set(expanded);
    if (next.has(name)) {
      next.delete(name);
    } else {
      next.add(name);
      if (!panes[name]) {
        try {
          const p = await get<Pane[]>(`/sessions/${name}/panes`);
          setPanes((prev) => ({ ...prev, [name]: p ?? [] }));
          // Fetch git info from the first pane's path
          if (p && p.length > 0 && p[0].path) {
            get<GitInfo>(`/git/info?dir=${encodeURIComponent(p[0].path)}`)
              .then((info) => { if (info) setGitInfo((prev) => ({ ...prev, [name]: info })); })
              .catch(() => {});
          }
        } catch {}
      }
    }
    setExpanded(next);
  };

  const handleCreate = () => {
    setDialog({
      kind: 'prompt',
      title: 'New session',
      message: 'Enter a name for the new tmux session.',
      initialValue: '',
      confirmLabel: 'Create',
      onConfirm: async (name) => {
        setDialog(null);
        if (!name) return;
        try {
          // User-initiated: frontend should focus the new session when it attaches.
          await post('/sessions', { name, background: false });
          refresh();
        } catch {}
      },
    });
  };

  const handleRenameSession = (e: React.MouseEvent, oldName: string) => {
    e.stopPropagation();
    setDialog({
      kind: 'prompt',
      title: 'Rename session',
      message: `Rename "${oldName}" to:`,
      initialValue: oldName,
      confirmLabel: 'Rename',
      onConfirm: async (newName) => {
        setDialog(null);
        if (!newName || newName === oldName) return;
        try {
          await patch(`/sessions/${oldName}`, { newName });
          // Drop cached panes for the old name; new name will fetch on expand.
          setPanes((prev) => {
            const next = { ...prev };
            delete next[oldName];
            return next;
          });
          setExpanded((prev) => {
            const next = new Set(prev);
            if (next.has(oldName)) {
              next.delete(oldName);
              next.add(newName);
            }
            return next;
          });
          onSessionRenamed?.(oldName, newName);
          refresh();
        } catch (err) {
          setDialog({
            kind: 'confirm',
            title: 'Rename failed',
            message: err instanceof Error ? err.message : String(err),
            confirmLabel: 'OK',
            onConfirm: () => setDialog(null),
          });
        }
      },
    });
  };

  const handleDeleteSession = (e: React.MouseEvent, name: string) => {
    e.stopPropagation();
    setDialog({
      kind: 'confirm',
      title: 'Kill session?',
      message: `"${name}" and all its panes will be terminated. This cannot be undone.`,
      confirmLabel: 'Kill session',
      danger: true,
      onConfirm: async () => {
        setDialog(null);
        try {
          await del(`/sessions/${name}`);
          refresh();
        } catch (err) {
          setDialog({
            kind: 'confirm',
            title: 'Kill failed',
            message: err instanceof Error ? err.message : String(err),
            confirmLabel: 'OK',
            onConfirm: () => setDialog(null),
          });
        }
      },
    });
  };

  // Hover preview: fetch capture-pane on hover
  const handlePaneHover = (target: string, e: React.MouseEvent) => {
    if (hoverTimer.current) clearTimeout(hoverTimer.current);
    setHoverPos({ x: e.clientX, y: e.clientY });
    hoverTimer.current = setTimeout(async () => {
      try {
        const res = await get<{ output: string }>(`/sessions/${target.split(':')[0]}/capture?target=${encodeURIComponent(target)}&lines=${PREVIEW_LINES[getPreviewSize()]}`);
        setHoverTarget(target);
        setHoverPreview(res?.output ?? '');
      } catch {
        setHoverPreview(null);
      }
    }, 400);
  };

  const handlePaneLeave = () => {
    if (isHoverPinned()) return;
    if (hoverTimer.current) clearTimeout(hoverTimer.current);
    setHoverTarget(null);
    setHoverPreview(null);
  };

  // Compute the worst pane status color across a session's panes. Returns
  // one of "red" | "yellow" | "green" | null.
  const worstStatusFor = (sessionName: string): string | null => {
    const list = panes[sessionName];
    if (!list) return null;
    let best = 0;
    let kind: string | null = null;
    for (const p of list) {
      const ps = paneStatuses[p.target];
      if (!ps) continue;
      const rank = STATUS_RANK[ps.status] ?? 0;
      if (rank > best) {
        best = rank;
        kind = ps.status;
      }
    }
    return kind;
  };

  // Does this session contain the currently-fullscreened pane?
  const ownsMaximized = (sessionName: string): boolean => {
    if (!maximizedTarget) return false;
    const list = panes[sessionName];
    return !!list?.some((p) => p.target === maximizedTarget);
  };

  if (collapsed) {
    // Render a narrow status-glyph strip (#503) instead of a blank sidebar.
    return (
      <aside className="sidebar collapsed">
        <button className="sidebar-toggle" onClick={onToggleCollapse} title="Expand sidebar">›</button>
        <div className="sidebar-collapsed-strip">
          {visibleSessions.map((s) => {
            const status = worstStatusFor(s.name);
            const owns = ownsMaximized(s.name);
            const initial = s.name.charAt(0).toUpperCase();
            const title = status
              ? `${s.name} — ${status}`
              : s.name;
            return (
              <button
                key={s.name}
                className={`sidebar-glyph${status ? ' has-status' : ''}${owns ? ' owns-maximized' : ''}`}
                style={status ? { color: STATUS_COLORS[status], borderColor: STATUS_COLORS[status] } : undefined}
                title={title}
                onClick={() => {
                  // Collapsed glyph click: jump to the session if it's
                  // already visible in some cell, otherwise expand the
                  // sidebar so the user can pick a pane.
                  if (onFocusSession?.(s.name)) return;
                  onToggleCollapse();
                }}
              >
                {initial}
                {status && <span className="sidebar-glyph-dot" style={{ background: STATUS_COLORS[status] }} />}
              </button>
            );
          })}
        </div>
      </aside>
    );
  }

  return (
    <aside className="sidebar" style={style}>
      <div className="sidebar-header">
        <button className="sidebar-toggle" onClick={onToggleCollapse} title="Collapse sidebar">‹</button>
        <h2>Sessions</h2>
        <span style={{ flex: 1 }} />
        <button
          className={`btn-small${showHidden ? ' active' : ''}`}
          onClick={() => setShowHidden((p) => !p)}
          title={showHidden ? 'Hide internal sessions' : 'Show all sessions'}
        >
          {showHidden ? '👁' : '👁‍🗨'}
        </button>
        <button className="btn-small" onClick={handleCreate}>+</button>
      </div>
      {visibleSessions.length === 0 ? (
        <p className="muted">No tmux sessions</p>
      ) : (
        <ul>
          {groupSessions(visibleSessions, gitInfo).map((group) => {
            const groupCollapsed = collapsedGroups.has(group.project);
            return (
              <li key={`group:${group.project}`} className="session-group">
                <div className="session-group-header" onClick={() => toggleGroup(group.project)}>
                  <span className="expand-icon">{groupCollapsed ? '▶' : '▼'}</span>
                  <span className="session-group-name">{group.project}</span>
                  <span className="badge">{group.sessions.length}</span>
                </div>
                {!groupCollapsed && (
                  <ul className="session-group-list">
                    {group.sessions.map((s) => {
            // Session-level hot status: only highlight when a DIFFERENT
            // pane is fullscreened (#502). The user can't see the hot
            // pane directly, so we surface it in the sidebar.
            const hot = maximizedTarget && !ownsMaximized(s.name)
              ? worstStatusFor(s.name)
              : null;
            return (
            <li
              key={s.name}
              className={`session-item${s.hidden ? ' hidden-session' : ''}${hot ? ` has-hot-status hot-${hot}` : ''}`}
            >
              <div className="session-row" onClick={() => {
                // Clicking the row (but NOT the chevron) jumps to the
                // session if it's already shown in some cell. Falls back
                // to toggling expansion if not.
                if (onFocusSession?.(s.name)) return;
                toggleSession(s.name);
              }}>
                <span
                  className="expand-icon"
                  onClick={(e) => { e.stopPropagation(); toggleSession(s.name); }}
                  title={expanded.has(s.name) ? 'Collapse' : 'Expand'}
                >
                  {expanded.has(s.name) ? '▼' : '▶'}
                </span>
                <span className="session-name">{s.hidden ? `⚙ ${s.name}` : s.name}</span>
                {gitInfo[s.name] && (
                  <span
                    className={`git-badge${gitInfo[s.name].dirty ? ' dirty' : ''}`}
                    onMouseEnter={(e) => setGitHover({ sessionName: s.name, x: e.clientX, y: e.clientY })}
                    onMouseMove={(e) => setGitHover((prev) => prev && prev.sessionName === s.name ? { ...prev, x: e.clientX, y: e.clientY } : prev)}
                    onMouseLeave={() => { if (!isHoverPinned()) setGitHover((prev) => (prev?.sessionName === s.name ? null : prev)); }}
                  >
                    {gitInfo[s.name].branch}
                    {gitInfo[s.name].changed > 0 && ` ~${gitInfo[s.name].changed}`}
                    {gitInfo[s.name].ahead > 0 && ` ↑${gitInfo[s.name].ahead}`}
                    {gitInfo[s.name].behind > 0 && ` ↓${gitInfo[s.name].behind}`}
                  </span>
                )}
                <span className="badge">{s.windows}w{s.attached ? ' *' : ''}</span>
                <div className="session-actions">
                  <button
                    className="session-action-btn"
                    onClick={(e) => handleRenameSession(e, s.name)}
                    title="Rename session"
                  >
                    ✎
                  </button>
                  <button
                    className="session-action-btn danger"
                    onClick={(e) => handleDeleteSession(e, s.name)}
                    title="Kill session"
                  >
                    ✕
                  </button>
                </div>
              </div>
              {expanded.has(s.name) && (
                <>
                {gitInfo[s.name] && (
                  <div className="git-details">
                    {gitInfo[s.name].recentLogs?.slice(0, 3).map((log, i) => (
                      <div key={i} className="git-log-line">{log}</div>
                    ))}
                  </div>
                )}
                <ul className="pane-list">
                  {(panes[s.name] ?? []).map((p) => (
                    <li
                      key={p.target}
                      className={`pane-item${activeTargets.includes(p.target) ? ' active' : ''}`}
                      onClick={() => onSelectPane(p.target)}
                      onMouseEnter={(e) => handlePaneHover(p.target, e)}
                      onMouseLeave={handlePaneLeave}
                    >
                      <div className="pane-item-info">
                        {paneStatuses[p.target] && (
                          <span
                            className="pane-status-dot"
                            style={{ background: STATUS_COLORS[paneStatuses[p.target].status] }}
                            title={paneStatuses[p.target].message || paneStatuses[p.target].status}
                          />
                        )}
                        <span className="pane-target">{p.target}</span>
                        {p.windowName && <span className="pane-window-name">{p.windowName}</span>}
                      </div>
                      <div className="pane-item-meta">
                        {p.command && <span className="pane-command">{p.command}</span>}
                      </div>
                    </li>
                  ))}
                  {(panes[s.name] ?? []).length === 0 && (
                    <li className="muted pane-item">No panes</li>
                  )}
                </ul>
                </>
              )}
            </li>
            );
          })}
                  </ul>
                )}
              </li>
            );
          })}
        </ul>
      )}

      {gitHover && gitInfo[gitHover.sessionName] && (
        <GitInfoHoverPreview info={gitInfo[gitHover.sessionName]} x={gitHover.x} y={gitHover.y} pinned={isHoverPinned()} />
      )}

      <ConfirmDialog
        open={dialog !== null}
        kind={dialog?.kind ?? 'confirm'}
        title={dialog?.title ?? ''}
        message={dialog?.message}
        initialValue={dialog?.initialValue}
        confirmLabel={dialog?.confirmLabel}
        danger={dialog?.danger}
        onConfirm={(value) => dialog?.onConfirm(value)}
        onCancel={() => setDialog(null)}
      />

      {/* Hover preview card */}
      {hoverTarget && hoverPreview !== null && (
        <div
          className={`pane-hover-preview preview-${previewSize}${isHoverPinned() ? ' hover-pinned-inline' : ''}`}
          style={{
            top: Math.min(hoverPos.y, window.innerHeight - ({ small: 320, medium: 460, large: 620 }[previewSize])),
            left: 270,
          }}
        >
          <div className="pane-hover-header">{hoverTarget}</div>
          <div className="pane-hover-content-wrapper">
            <pre
              className="pane-hover-content"
              dangerouslySetInnerHTML={{ __html: DOMPurify.sanitize(ansiConverter.toHtml(hoverPreview)) }}
            />
          </div>
        </div>
      )}
    </aside>
  );
}
