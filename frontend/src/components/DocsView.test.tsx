import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { DocsView } from './DocsView';

// Mock the api client module so DocsView uses a fake backend. The component
// calls `get` with various paths; we intercept by URL.
vi.mock('../api/client', () => {
  return {
    get: vi.fn((path: string) => {
      if (path.startsWith('/docs/list')) {
        return Promise.resolve([{ path: '/home/me/a.md', name: 'a.md' }]);
      }
      if (path.startsWith('/docs/read')) {
        const url = new URL(path, 'http://x');
        const p = url.searchParams.get('path') ?? '';
        return Promise.resolve({
          path: p,
          name: p.split('/').pop() ?? p,
          content: '# ' + (p.split('/').pop() ?? 'doc') + '\nbody',
        });
      }
      return Promise.resolve(null);
    }),
  };
});

beforeEach(() => {
  localStorage.clear();
});

describe('DocsView', () => {
  it('restores the last-open doc from localStorage on mount (#439)', async () => {
    localStorage.setItem(
      'workshop:activeDoc',
      JSON.stringify({ path: '/home/me/pinned.md', name: 'pinned.md' }),
    );

    render(<DocsView />);

    // The restore effect reads workshop:activeDoc on mount and calls openDoc,
    // which fetches /docs/read and renders the content heading.
    await waitFor(() => {
      expect(screen.getByRole('heading', { name: 'pinned.md' })).toBeInTheDocument();
    });
  });

  it('shows the empty state when no active doc is saved', async () => {
    render(<DocsView />);
    // Empty state heading is a literal <h2>Docs</h2>
    expect(screen.getByRole('heading', { name: 'Docs' })).toBeInTheDocument();
  });

  it('persists the opened doc to localStorage via the openPath prop', async () => {
    const { rerender } = render(<DocsView />);
    rerender(<DocsView openPath="/home/me/from-mcp.md" />);

    await waitFor(() => {
      const raw = localStorage.getItem('workshop:activeDoc');
      expect(raw).not.toBeNull();
    });
    const saved = JSON.parse(localStorage.getItem('workshop:activeDoc')!);
    expect(saved.path).toBe('/home/me/from-mcp.md');
  });
});
