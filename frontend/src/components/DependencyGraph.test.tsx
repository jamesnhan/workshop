import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';

// --- Mocks ---

// Track the latest props passed to the fake ReactFlow so tests can assert
// on nodes/edges and invoke its handlers (onConnect, onEdgeDoubleClick,
// onMoveEnd, etc.) imperatively.
type RFProps = Record<string, unknown>;
const rfProps: { current: RFProps } = { current: {} };

vi.mock('reactflow', async () => {
  const React = await import('react');
  const ReactFlow = (props: RFProps) => {
    rfProps.current = props;
    const nodes = props.nodes as Array<{ id: string }> | undefined;
    const edges = props.edges as Array<{ id: string; source: string; target: string }> | undefined;
    return React.createElement(
      'div',
      { 'data-testid': 'reactflow' },
      React.createElement('span', { 'data-testid': 'node-count' }, String(nodes?.length ?? 0)),
      React.createElement('span', { 'data-testid': 'edge-count' }, String(edges?.length ?? 0)),
      ...(nodes ?? []).map((n) =>
        React.createElement('span', { key: n.id, 'data-testid': `node-${n.id}` }, n.id),
      ),
      ...(edges ?? []).map((e) =>
        React.createElement('span', { key: e.id, 'data-testid': `edge-${e.source}-${e.target}` }),
      ),
    );
  };
  return {
    default: ReactFlow,
    Background: () => null,
    Controls: () => null,
    MiniMap: () => null,
    addEdge: (conn: { source: string; target: string }, eds: unknown[]) => [
      ...eds,
      { id: `e-${conn.source}-${conn.target}`, ...conn },
    ],
    useNodesState: <T,>(initial: T[]): [T[], (v: T[]) => void, () => void] => {
      const [nodes, setNodes] = React.useState<T[]>(initial);
      return [nodes, setNodes, () => {}];
    },
    useEdgesState: <T,>(initial: T[]): [T[], (v: T[]) => void, () => void] => {
      const [edges, setEdges] = React.useState<T[]>(initial);
      return [edges, setEdges, () => {}];
    },
  };
});

// Mock the API client. vi.mock is hoisted before imports, so the shared
// object has to be created via vi.hoisted().
const apiMocks = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  del: vi.fn(),
}));
vi.mock('../api/client', () => apiMocks);

// Now that the mocks are in place, import the component under test.
import { DependencyGraph } from './DependencyGraph';

// --- Fixtures ---

function mockCards() {
  return [
    { id: 1, title: 'one', column: 'backlog', project: 'p', priority: 'P1', cardType: 'feature' },
    { id: 2, title: 'two', column: 'in_progress', project: 'p', priority: 'P2', cardType: 'bug' },
    { id: 3, title: 'three', column: 'done', project: 'p', priority: 'P3', cardType: 'chore' },
    { id: 4, title: 'four', column: 'review', project: 'p', priority: 'P0', cardType: 'task' },
  ];
}

function mockDeps() {
  return [
    { id: 10, blockerId: 1, blockedId: 2 },
    { id: 11, blockerId: 2, blockedId: 4 },
    { id: 12, blockerId: 2, blockedId: 3 }, // endpoint in done → hidden by default
  ];
}

function installFetchMocks() {
  apiMocks.get.mockImplementation((path: string) => {
    if (path.startsWith('/cards?') || path === '/cards') return Promise.resolve(mockCards());
    if (path.startsWith('/card-dependencies')) return Promise.resolve(mockDeps());
    if (path === '/projects') return Promise.resolve(['p']);
    return Promise.resolve(null);
  });
}

beforeEach(() => {
  localStorage.clear();
  rfProps.current = {};
  apiMocks.get.mockReset();
  apiMocks.post.mockReset();
  apiMocks.del.mockReset();
  apiMocks.post.mockResolvedValue({});
  apiMocks.del.mockResolvedValue(undefined);
  installFetchMocks();
});

// --- Hide Done default (#445) ---

describe('DependencyGraph — hide Done (#445)', () => {
  it('filters out done cards from the graph by default', async () => {
    render(<DependencyGraph defaultProject="p" />);

    await waitFor(() => {
      expect(screen.getByTestId('node-count').textContent).toBe('3');
    });

    // The 3 non-done cards should be present; the done one should not.
    expect(screen.getByTestId('node-1')).toBeInTheDocument();
    expect(screen.getByTestId('node-2')).toBeInTheDocument();
    expect(screen.getByTestId('node-4')).toBeInTheDocument();
    expect(screen.queryByTestId('node-3')).toBeNull();
  });

  it('also prunes edges whose endpoint is hidden', async () => {
    render(<DependencyGraph defaultProject="p" />);

    await waitFor(() => {
      // 2 of the 3 deps have both endpoints visible; the 2→3 edge drops.
      expect(screen.getByTestId('edge-count').textContent).toBe('2');
    });
    expect(screen.getByTestId('edge-1-2')).toBeInTheDocument();
    expect(screen.getByTestId('edge-2-4')).toBeInTheDocument();
    expect(screen.queryByTestId('edge-2-3')).toBeNull();
  });
});

// --- Viewport persistence (#447) ---

describe('DependencyGraph — viewport persistence (#447)', () => {
  it('passes saved viewport as defaultViewport on mount', async () => {
    localStorage.setItem(
      'workshop:depGraph:viewport:p',
      JSON.stringify({ x: 150, y: -50, zoom: 0.75 }),
    );

    render(<DependencyGraph defaultProject="p" />);

    await waitFor(() => {
      expect(rfProps.current.defaultViewport).toEqual({ x: 150, y: -50, zoom: 0.75 });
    });
    // When a saved viewport exists, fitView should be disabled so we don't
    // override the restored position.
    expect(rfProps.current.fitView).toBe(false);
  });

  it('falls back to fitView when no saved viewport exists', async () => {
    render(<DependencyGraph defaultProject="p" />);

    await waitFor(() => {
      expect(rfProps.current.fitView).toBe(true);
    });
    expect(rfProps.current.defaultViewport).toBeUndefined();
  });

  it('persists viewport via onMoveEnd', async () => {
    render(<DependencyGraph defaultProject="p" />);
    await waitFor(() => expect(rfProps.current.onMoveEnd).toBeDefined());

    // Drive the handler directly with a synthetic viewport.
    const onMoveEnd = rfProps.current.onMoveEnd as (e: unknown, v: { x: number; y: number; zoom: number }) => void;
    onMoveEnd(null, { x: 42, y: 99, zoom: 2 });

    const stored = localStorage.getItem('workshop:depGraph:viewport:p');
    expect(stored).not.toBeNull();
    expect(JSON.parse(stored!)).toEqual({ x: 42, y: 99, zoom: 2 });
  });

  it('keys viewport per project', async () => {
    render(<DependencyGraph defaultProject="alpha" />);
    await waitFor(() => expect(rfProps.current.onMoveEnd).toBeDefined());

    const onMoveEnd = rfProps.current.onMoveEnd as (e: unknown, v: { x: number; y: number; zoom: number }) => void;
    onMoveEnd(null, { x: 1, y: 2, zoom: 3 });

    expect(localStorage.getItem('workshop:depGraph:viewport:alpha')).not.toBeNull();
    expect(localStorage.getItem('workshop:depGraph:viewport:p')).toBeNull();
  });
});

// --- Edge handlers ---

describe('DependencyGraph — edge handlers', () => {
  it('onConnect posts a blocks dependency to the backend', async () => {
    render(<DependencyGraph defaultProject="p" />);
    await waitFor(() => expect(rfProps.current.onConnect).toBeDefined());

    const onConnect = rfProps.current.onConnect as (c: { source: string; target: string }) => Promise<void>;
    await onConnect({ source: '1', target: '4' });

    expect(apiMocks.post).toHaveBeenCalledWith('/cards/4/blocks', { blockerId: 1 });
  });

  it('onConnect ignores self-connects', async () => {
    render(<DependencyGraph defaultProject="p" />);
    await waitFor(() => expect(rfProps.current.onConnect).toBeDefined());

    const onConnect = rfProps.current.onConnect as (c: { source: string; target: string }) => Promise<void>;
    await onConnect({ source: '1', target: '1' });

    expect(apiMocks.post).not.toHaveBeenCalled();
  });

  it('onEdgeDoubleClick prompts then calls DELETE on confirm', async () => {
    const onConfirm = vi.fn().mockResolvedValue(true);
    render(<DependencyGraph defaultProject="p" onConfirm={onConfirm} />);
    await waitFor(() => expect(rfProps.current.onEdgeDoubleClick).toBeDefined());

    const onEdgeDoubleClick = rfProps.current.onEdgeDoubleClick as (
      e: unknown,
      edge: { id: string; source: string; target: string },
    ) => Promise<void>;
    await onEdgeDoubleClick(null, { id: 'e10', source: '1', target: '2' });

    expect(onConfirm).toHaveBeenCalledWith(
      expect.objectContaining({ title: expect.stringContaining('Remove') }),
    );
    expect(apiMocks.del).toHaveBeenCalledWith('/cards/2/blocks/1');
  });

  it('onEdgeDoubleClick bails without DELETE when confirm is rejected', async () => {
    const onConfirm = vi.fn().mockResolvedValue(false);
    render(<DependencyGraph defaultProject="p" onConfirm={onConfirm} />);
    await waitFor(() => expect(rfProps.current.onEdgeDoubleClick).toBeDefined());

    const onEdgeDoubleClick = rfProps.current.onEdgeDoubleClick as (
      e: unknown,
      edge: { id: string; source: string; target: string },
    ) => Promise<void>;
    await onEdgeDoubleClick(null, { id: 'e10', source: '1', target: '2' });

    expect(apiMocks.del).not.toHaveBeenCalled();
  });
});
