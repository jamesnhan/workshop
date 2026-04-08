import { useEffect, useState, useCallback, useRef } from 'react';
import { get, post, del, patch } from '../api/client';
import AnsiToHtml from 'ansi-to-html';
import DOMPurify from 'dompurify';
import { ConfirmDialog, type DialogKind } from './ConfirmDialog';

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

interface Props {
  collapsed: boolean;
  onToggleCollapse: () => void;
  onSelectPane: (target: string) => void;
  activeTargets: string[];
  paneStatuses: Record<string, { status: string; message: string }>;
  onSessionRenamed?: (oldName: string, newName: string) => void;
  style?: React.CSSProperties;
}

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

export function Sidebar({ collapsed, onToggleCollapse, onSelectPane, activeTargets, paneStatuses, onSessionRenamed, style }: Props) {
  const [sessions, setSessions] = useState<Session[]>([]);
  const [panes, setPanes] = useState<Record<string, Pane[]>>({});
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [showHidden, setShowHidden] = useState(false);
  const [gitInfo, setGitInfo] = useState<Record<string, GitInfo>>({});
  const [hoverTarget, setHoverTarget] = useState<string | null>(null);
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

  const refresh = useCallback(() => {
    const q = showHidden ? '?all=true' : '';
    get<Session[]>(`/sessions${q}`)
      .then((data) => setSessions(data ?? []))
      .catch(() => {});
  }, [showHidden]);

  useEffect(() => {
    refresh();
    const interval = setInterval(refresh, 5000);
    return () => clearInterval(interval);
  }, [refresh]);

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
    if (hoverTimer.current) clearTimeout(hoverTimer.current);
    setHoverTarget(null);
    setHoverPreview(null);
  };

  if (collapsed) {
    return (
      <aside className="sidebar collapsed">
        <button className="sidebar-toggle" onClick={onToggleCollapse} title="Expand sidebar">›</button>
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
      {sessions.length === 0 ? (
        <p className="muted">No tmux sessions</p>
      ) : (
        <ul>
          {sessions.map((s) => (
            <li key={s.name} className={`session-item${s.hidden ? ' hidden-session' : ''}`}>
              <div className="session-row" onClick={() => toggleSession(s.name)}>
                <span className="expand-icon">{expanded.has(s.name) ? '▼' : '▶'}</span>
                <span className="session-name">{s.hidden ? `⚙ ${s.name}` : s.name}</span>
                {gitInfo[s.name] && (
                  <span className={`git-badge${gitInfo[s.name].dirty ? ' dirty' : ''}`}>
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
          ))}
        </ul>
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
          className={`pane-hover-preview preview-${previewSize}`}
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
