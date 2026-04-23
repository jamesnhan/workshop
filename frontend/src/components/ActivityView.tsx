import { useState, useEffect, useCallback, useRef } from 'react';
import Markdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { get, post } from '../api/client';

interface ActivityEntry {
  id: number;
  parentId: number;
  paneTarget: string;
  agentName: string;
  actionType: string;
  summary: string;
  metadata: string;
  project: string;
  createdAt: string;
  children?: ActivityEntry[];
}

interface ApprovalRequest {
  id: number;
  paneTarget: string;
  agentName: string;
  action: string;
  details: string;
  diff: string;
  project: string;
  status: string;
  createdAt: string;
}

interface CompactionEvent {
  sessionId: string;
  timestamp: string;
  trigger: string;
  preTokens: number;
  tools: string[];
  version: string;
  slug: string;
}

const ACTION_ICONS: Record<string, string> = {
  file_write: '📝',
  command: '▶',
  decision: '🧭',
  error: '❌',
  status: '📊',
  deploy: '🚀',
  test: '🧪',
  review: '👀',
};

function EntryNode({ entry, depth, collapsed, onToggle, formatTime }: {
  entry: ActivityEntry;
  depth: number;
  collapsed: Set<number>;
  onToggle: (id: number) => void;
  formatTime: (ts: string) => string;
}) {
  const hasChildren = entry.children && entry.children.length > 0;
  const isCollapsed = collapsed.has(entry.id);

  return (
    <>
      <div
        className={`activity-entry activity-${entry.actionType}${depth > 0 ? ' activity-child' : ''}`}
        style={{ paddingLeft: `${0.6 + depth * 1.2}rem` }}
      >
        {hasChildren ? (
          <button className="activity-tree-toggle" onClick={() => onToggle(entry.id)}>
            {isCollapsed ? '▶' : '▼'}
          </button>
        ) : (
          <span className="activity-icon">{ACTION_ICONS[entry.actionType] || '•'}</span>
        )}
        <div className="activity-body">
          <div className="activity-summary">
            {hasChildren && <span className="activity-icon" style={{ marginRight: '0.3rem' }}>{ACTION_ICONS[entry.actionType] || '•'}</span>}
            {entry.summary}
            {hasChildren && <span className="activity-child-count">({entry.children!.length})</span>}
          </div>
          <div className="activity-meta">
            {entry.paneTarget && <span className="activity-pane">{entry.paneTarget}</span>}
            {entry.project && <span className="activity-project">{entry.project}</span>}
            <span className="activity-type">{entry.actionType}</span>
            <span className="activity-time">{formatTime(entry.createdAt)}</span>
          </div>
        </div>
      </div>
      {hasChildren && !isCollapsed && entry.children!.map((child) => (
        <EntryNode
          key={child.id}
          entry={child}
          depth={depth + 1}
          collapsed={collapsed}
          onToggle={onToggle}
          formatTime={formatTime}
        />
      ))}
    </>
  );
}

export function ActivityView() {
  const [entries, setEntries] = useState<ActivityEntry[]>([]);
  const [pendingApprovals, setPendingApprovals] = useState<ApprovalRequest[]>([]);
  const [compactions, setCompactions] = useState<CompactionEvent[]>([]);
  const [compactionsOpen, setCompactionsOpen] = useState(false);
  const [filterPane, setFilterPane] = useState('');
  const [filterProject, setFilterProject] = useState('');
  const [filterAction, setFilterAction] = useState('');
  const [treeMode, setTreeMode] = useState(true);
  const [collapsed, setCollapsed] = useState<Set<number>>(new Set());
  const [loading, setLoading] = useState(false);
  const bottomRef = useRef<HTMLDivElement>(null);
  const listRef = useRef<HTMLDivElement>(null);

  const toggleCollapse = useCallback((id: number) => {
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const params = new URLSearchParams();
      if (filterPane) params.set('pane', filterPane);
      if (filterProject) params.set('project', filterProject);
      if (filterAction) params.set('action_type', filterAction);
      if (treeMode) params.set('tree', 'true');
      params.set('limit', '200');
      const data = await get<ActivityEntry[]>(`/activity?${params}`);
      setEntries(data ?? []);
    } catch (err) {
      console.error('Failed to load activity:', err);
    } finally {
      setLoading(false);
    }
  }, [filterPane, filterProject, filterAction, treeMode]);

  const refreshApprovals = useCallback(async () => {
    try {
      const data = await get<ApprovalRequest[]>('/approvals?status=pending');
      setPendingApprovals(data ?? []);
    } catch (err) {
      console.error('Failed to load approvals:', err);
    }
  }, []);

  const refreshCompactions = useCallback(async () => {
    try {
      const data = await get<{ compactions: CompactionEvent[] }>('/compactions');
      setCompactions(data?.compactions ?? []);
    } catch (err) {
      console.error('Failed to load compactions:', err);
    }
  }, []);

  useEffect(() => { refresh(); refreshApprovals(); refreshCompactions(); }, [refresh, refreshApprovals, refreshCompactions]);

  // Listen for WebSocket broadcasts of new activity entries
  useEffect(() => {
    const handleWS = (e: Event) => {
      const detail = (e as CustomEvent).detail;
      if (detail?.type === 'activity') {
        // For live updates, just prepend and let next refresh sort tree structure
        setEntries((prev) => [detail.data, ...prev].slice(0, 200));
      }
    };
    window.addEventListener('workshop-ws', handleWS);
    return () => window.removeEventListener('workshop-ws', handleWS);
  }, []);

  // Listen for new approval requests via WS broadcast
  useEffect(() => {
    const handleApproval = (e: Event) => {
      const detail = (e as CustomEvent).detail;
      if (detail?.type === 'approval_request' && detail?.data) {
        const d = detail.data;
        setPendingApprovals((prev) => {
          if (prev.some((a) => a.id === d.approvalId)) return prev;
          return [{
            id: d.approvalId,
            paneTarget: d.paneTarget ?? '',
            agentName: d.agentName ?? '',
            action: d.action ?? '',
            details: d.details ?? '',
            diff: d.diff ?? '',
            project: d.project ?? '',
            status: 'pending',
            createdAt: new Date().toISOString(),
          }, ...prev];
        });
      }
    };
    window.addEventListener('workshop-ws', handleApproval);
    return () => window.removeEventListener('workshop-ws', handleApproval);
  }, []);

  const resolveApproval = useCallback(async (approvalId: number, decision: 'approved' | 'denied') => {
    try {
      await post(`/approvals/${approvalId}/resolve`, { decision });
    } catch (err) {
      console.error('Failed to resolve approval:', err);
    }
    setPendingApprovals((prev) => prev.filter((a) => a.id !== approvalId));
  }, []);

  const formatTime = (ts: string) => {
    const d = new Date(ts);
    const now = new Date();
    const diffMs = now.getTime() - d.getTime();
    if (diffMs < 60_000) return 'just now';
    if (diffMs < 3600_000) return `${Math.floor(diffMs / 60_000)}m ago`;
    if (d.toDateString() === now.toDateString()) {
      return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
    }
    return d.toLocaleDateString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
  };

  // Collect unique values for filter dropdowns (from flat list)
  const allEntries: ActivityEntry[] = [];
  const flatten = (list: ActivityEntry[]) => {
    for (const e of list) {
      allEntries.push(e);
      if (e.children) flatten(e.children);
    }
  };
  flatten(entries);
  const panes = [...new Set(allEntries.map((e) => e.paneTarget).filter(Boolean))];
  const projects = [...new Set(allEntries.map((e) => e.project).filter(Boolean))];
  const actionTypes = [...new Set(allEntries.map((e) => e.actionType).filter(Boolean))];

  return (
    <div className="activity-view">
      {/* Approval queue */}
      {pendingApprovals.length > 0 && (
        <div className="approval-queue">
          <h3>Pending Approvals ({pendingApprovals.length})</h3>
          {pendingApprovals.map((a) => (
            <div key={a.id} className="approval-card">
              <div className="approval-card-header">
                <span className="approval-action">{a.action}</span>
                {a.paneTarget && <span className="activity-pane">{a.paneTarget}</span>}
                {a.project && <span className="activity-project">{a.project}</span>}
              </div>
              <div className="approval-details"><Markdown remarkPlugins={[remarkGfm]}>{a.details}</Markdown></div>
              {a.diff && (
                <div className="approval-diff"><Markdown remarkPlugins={[remarkGfm]}>{a.diff}</Markdown></div>
              )}
              <div className="approval-actions">
                <button className="btn-approve" onClick={() => resolveApproval(a.id, 'approved')}>
                  Approve
                </button>
                <button className="btn-deny" onClick={() => resolveApproval(a.id, 'denied')}>
                  Deny
                </button>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Compaction timeline */}
      {compactions.length > 0 && (
        <div className="compaction-section">
          <h3 onClick={() => setCompactionsOpen((p) => !p)} style={{ cursor: 'pointer' }}>
            {compactionsOpen ? '▼' : '▶'} Compactions ({compactions.length})
          </h3>
          {compactionsOpen && (
            <div className="compaction-list">
              {compactions.map((c, i) => (
                <div key={i} className="compaction-entry">
                  <span className="compaction-tokens">{(c.preTokens / 1000).toFixed(0)}k tokens</span>
                  <span className="compaction-trigger">{c.trigger}</span>
                  <span className="compaction-session" title={c.sessionId}>{c.slug || c.sessionId.slice(0, 8)}</span>
                  <span className="activity-time">{formatTime(c.timestamp)}</span>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      <div className="activity-header">
        <h2>Activity Feed</h2>
        <div className="activity-filters">
          <button
            className={`btn-small${treeMode ? ' active' : ''}`}
            onClick={() => setTreeMode((p) => !p)}
            title={treeMode ? 'Tree view' : 'Flat view'}
          >
            {treeMode ? '🌳' : '≡'}
          </button>
          <select value={filterPane} onChange={(e) => setFilterPane(e.target.value)}>
            <option value="">All panes</option>
            {panes.map((p) => <option key={p} value={p}>{p}</option>)}
          </select>
          <select value={filterProject} onChange={(e) => setFilterProject(e.target.value)}>
            <option value="">All projects</option>
            {projects.map((p) => <option key={p} value={p}>{p}</option>)}
          </select>
          <select value={filterAction} onChange={(e) => setFilterAction(e.target.value)}>
            <option value="">All actions</option>
            {actionTypes.map((a) => <option key={a} value={a}>{a}</option>)}
          </select>
          <button className="btn-small" onClick={refresh} disabled={loading}>
            {loading ? '...' : '↻'}
          </button>
        </div>
      </div>
      <div className="activity-list" ref={listRef}>
        {entries.length === 0 && !loading && (
          <div className="activity-empty">
            <p>No activity yet. Agents report actions here via the <code>report_activity</code> MCP tool.</p>
          </div>
        )}
        {entries.map((entry) => (
          <EntryNode
            key={entry.id}
            entry={entry}
            depth={0}
            collapsed={collapsed}
            onToggle={toggleCollapse}
            formatTime={formatTime}
          />
        ))}
        <div ref={bottomRef} />
      </div>
    </div>
  );
}
