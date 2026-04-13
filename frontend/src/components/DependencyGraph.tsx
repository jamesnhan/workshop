import { useCallback, useEffect, useMemo, useState } from 'react';
import ReactFlow, {
  Background,
  Controls,
  MiniMap,
  addEdge,
  useEdgesState,
  useNodesState,
  type Connection,
  type Edge,
  type Node,
  type NodeTypes,
  type Viewport,
} from 'reactflow';
import 'reactflow/dist/style.css';
import { get, post, del } from '../api/client';
import './DependencyGraph.css';

interface Card {
  id: number;
  title: string;
  column: string;
  project: string;
  priority: string;
  cardType: string;
}

interface Dependency {
  id: number;
  blockerId: number;
  blockedId: number;
}

interface DependencyGraphProps {
  defaultProject?: string;
  onOpenCard?: (id: number) => void;
  onConfirm?: (opts: { title: string; message?: string; danger?: boolean }) => Promise<boolean>;
}

// Columns arrayed left to right in the MVP layout.
const COLUMN_ORDER = ['backlog', 'in_progress', 'review', 'done'];

const VIEWPORT_KEY_PREFIX = 'workshop:depGraph:viewport:';

function loadViewport(project: string): Viewport | null {
  try {
    const raw = localStorage.getItem(VIEWPORT_KEY_PREFIX + (project || '__all__'));
    return raw ? (JSON.parse(raw) as Viewport) : null;
  } catch {
    return null;
  }
}

function saveViewport(project: string, vp: Viewport) {
  try {
    localStorage.setItem(VIEWPORT_KEY_PREFIX + (project || '__all__'), JSON.stringify(vp));
  } catch {
    // ignore quota errors
  }
}
const COL_X_GAP = 300;
const ROW_Y_GAP = 90;

function columnColor(col: string): string {
  switch (col) {
    case 'backlog': return '#475569';
    case 'in_progress': return '#2563eb';
    case 'review': return '#d97706';
    case 'done': return '#16a34a';
    default: return '#374151';
  }
}

function priorityBorder(p: string): string {
  switch (p) {
    case 'P0': return '#ef4444';
    case 'P1': return '#f97316';
    case 'P2': return '#eab308';
    default: return '#64748b';
  }
}

function layoutNodes(cards: Card[]): Node[] {
  // Group by column, lay out in vertical stacks.
  const byCol: Record<string, Card[]> = {};
  for (const c of cards) {
    (byCol[c.column] ??= []).push(c);
  }
  const nodes: Node[] = [];
  COLUMN_ORDER.forEach((col, colIdx) => {
    const list = byCol[col] ?? [];
    list.forEach((card, rowIdx) => {
      nodes.push({
        id: String(card.id),
        position: { x: colIdx * COL_X_GAP, y: rowIdx * ROW_Y_GAP },
        data: {
          label: (
            <div className="dep-node-body" style={{ borderColor: priorityBorder(card.priority) }}>
              <div className="dep-node-title">#{card.id} {card.title}</div>
              <div className="dep-node-meta">
                <span className="dep-node-pill" style={{ background: columnColor(card.column) }}>{card.column}</span>
                {card.priority && <span className="dep-node-priority">{card.priority}</span>}
              </div>
            </div>
          ),
        },
        style: {
          background: '#0f172a',
          color: '#e2e8f0',
          border: `1px solid ${columnColor(card.column)}`,
          borderRadius: 8,
          padding: 0,
          width: 240,
        },
      });
    });
    // Nodes without a recognized column fall through unlayouted; collect once.
  });
  // Any card whose column wasn't in COLUMN_ORDER: append after.
  let extraRow = 0;
  const seen = new Set(nodes.map((n) => n.id));
  for (const c of cards) {
    if (seen.has(String(c.id))) continue;
    nodes.push({
      id: String(c.id),
      position: { x: COLUMN_ORDER.length * COL_X_GAP, y: extraRow++ * ROW_Y_GAP },
      data: { label: `#${c.id} ${c.title}` },
    });
  }
  return nodes;
}

export function DependencyGraph({ defaultProject, onOpenCard, onConfirm }: DependencyGraphProps) {
  const [cards, setCards] = useState<Card[]>([]);
  const [deps, setDeps] = useState<Dependency[]>([]);
  const [projects, setProjects] = useState<string[]>([]);
  const [project, setProject] = useState<string>(defaultProject ?? '');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [showDone, setShowDone] = useState(false);

  const refresh = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [cs, ds, ps] = await Promise.all([
        get<Card[]>(`/cards${project ? `?project=${encodeURIComponent(project)}` : ''}`),
        get<Dependency[]>(`/card-dependencies${project ? `?project=${encodeURIComponent(project)}` : ''}`),
        get<string[]>('/projects'),
      ]);
      // Hide "done" cards by default to keep graph readable? Show everything for MVP.
      setCards(cs ?? []);
      setDeps(ds ?? []);
      setProjects(ps ?? []);
    } catch (e: any) {
      setError(e?.message ?? 'Failed to load graph');
    } finally {
      setLoading(false);
    }
  }, [project]);

  useEffect(() => { void refresh(); }, [refresh]);

  const visibleCards = useMemo(
    () => (showDone ? cards : cards.filter((c) => c.column !== 'done')),
    [cards, showDone],
  );
  const visibleIds = useMemo(() => new Set(visibleCards.map((c) => c.id)), [visibleCards]);

  const initialNodes = useMemo(() => layoutNodes(visibleCards), [visibleCards]);
  const initialEdges = useMemo<Edge[]>(
    () => deps
      .filter((d) => visibleIds.has(d.blockerId) && visibleIds.has(d.blockedId))
      .map((d) => ({
        id: `e${d.id}`,
        source: String(d.blockerId),
        target: String(d.blockedId),
        animated: true,
        style: { stroke: '#94a3b8' },
        label: 'blocks',
        labelStyle: { fill: '#94a3b8', fontSize: 10 },
      })),
    [deps, visibleIds],
  );

  const [nodes, setNodes, onNodesChange] = useNodesState(initialNodes);
  const [edges, setEdges, onEdgesChange] = useEdgesState(initialEdges);

  // Keep local react-flow state in sync when data refetches.
  useEffect(() => { setNodes(initialNodes); }, [initialNodes, setNodes]);
  useEffect(() => { setEdges(initialEdges); }, [initialEdges, setEdges]);

  // Creating an edge: drag from source handle to target handle.
  const onConnect = useCallback(async (conn: Connection) => {
    if (!conn.source || !conn.target || conn.source === conn.target) return;
    const blockerId = Number(conn.source);
    const blockedId = Number(conn.target);
    // Optimistic add
    setEdges((eds) => addEdge({ ...conn, animated: true, label: 'blocks' }, eds));
    try {
      await post(`/cards/${blockedId}/blocks`, { blockerId });
      void refresh();
    } catch (e: any) {
      setError(e?.message ?? 'Failed to add dependency');
      void refresh();
    }
  }, [refresh, setEdges]);

  // Delete edge: select then Backspace / Delete.
  const onEdgesDelete = useCallback(async (removed: Edge[]) => {
    for (const e of removed) {
      if (!e.source || !e.target) continue;
      try {
        await del(`/cards/${e.target}/blocks/${e.source}`);
      } catch {
        // ignore; refresh will restore
      }
    }
    void refresh();
  }, [refresh]);

  // Double-click an edge to delete it (selection-based delete is unreliable
  // without a focused wrapper; double-click is the discoverable fallback).
  const onEdgeDoubleClick = useCallback(async (_: React.MouseEvent, edge: Edge) => {
    if (!edge.source || !edge.target) return;
    const ok = onConfirm
      ? await onConfirm({
          title: 'Remove dependency?',
          message: `#${edge.source} will no longer block #${edge.target}.`,
          danger: true,
        })
      : window.confirm(`Remove dependency? #${edge.source} no longer blocks #${edge.target}.`);
    if (!ok) return;
    setEdges((eds) => eds.filter((e) => e.id !== edge.id));
    try {
      await del(`/cards/${edge.target}/blocks/${edge.source}`);
    } catch (e: any) {
      setError(e?.message ?? 'Failed to remove dependency');
    } finally {
      void refresh();
    }
  }, [onConfirm, refresh, setEdges]);

  const onNodeDoubleClick = useCallback((_: React.MouseEvent, node: Node) => {
    onOpenCard?.(Number(node.id));
  }, [onOpenCard]);

  const nodeTypes = useMemo<NodeTypes>(() => ({}), []);

  // Persist viewport per project so leaving + returning restores the same spot.
  const savedViewport = useMemo(() => loadViewport(project), [project]);
  const handleMoveEnd = useCallback((_: unknown, viewport: Viewport) => {
    saveViewport(project, viewport);
  }, [project]);

  return (
    <div className="dep-graph">
      <div className="dep-graph-toolbar">
        <label>
          Project:&nbsp;
          <select value={project} onChange={(e) => setProject(e.target.value)}>
            <option value="">(all)</option>
            {projects.map((p) => <option key={p} value={p}>{p}</option>)}
          </select>
        </label>
        <button className="btn-small" onClick={() => void refresh()} disabled={loading}>
          {loading ? 'Loading…' : 'Refresh'}
        </button>
        <label className="dep-graph-toggle">
          <input type="checkbox" checked={showDone} onChange={(e) => setShowDone(e.target.checked)} />
          &nbsp;Show Done
        </label>
        <span className="dep-graph-hint">
          Drag between nodes to add a "blocks" edge. Double-click an edge to remove it. Double-click a node to open the card.
        </span>
        {error && <span className="dep-graph-error">{error}</span>}
      </div>
      <div className="dep-graph-canvas">
        <ReactFlow
          key={project || '__all__'}
          nodes={nodes}
          edges={edges}
          onNodesChange={onNodesChange}
          onEdgesChange={onEdgesChange}
          onConnect={onConnect}
          onEdgesDelete={onEdgesDelete}
          onEdgeDoubleClick={onEdgeDoubleClick}
          onNodeDoubleClick={onNodeDoubleClick}
          onMoveEnd={handleMoveEnd}
          nodeTypes={nodeTypes}
          defaultViewport={savedViewport ?? undefined}
          fitView={!savedViewport}
          fitViewOptions={{ padding: 0.2 }}
          proOptions={{ hideAttribution: true }}
        >
          <Background color="#334155" gap={20} />
          <Controls />
          <MiniMap
            nodeColor={(n) => {
              const card = cards.find((c) => String(c.id) === n.id);
              return card ? columnColor(card.column) : '#475569';
            }}
            maskColor="rgba(15,23,42,0.6)"
            style={{ background: '#0f172a' }}
            pannable
            zoomable
          />
        </ReactFlow>
      </div>
    </div>
  );
}
