import { useState, useCallback, useEffect, useRef } from 'react';
import Markdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import rehypeHighlight from 'rehype-highlight';
import 'highlight.js/styles/github-dark.css';
import { findAndReplace } from 'mdast-util-find-and-replace';
import { get } from '../api/client';
import { ResizeHandle } from './ResizeHandle';

// Remark plugin: transforms #123 in text into links to #card/123.
function remarkTicketLinks() {
  return (tree: Parameters<typeof findAndReplace>[0]) => {
    findAndReplace(tree, [
      [/(?:^|(?<=[\s(]))#(\d+)\b/g, (_: string, id: string) => ({
        type: 'link' as const,
        url: `#card/${id}`,
        children: [{ type: 'text' as const, value: `#${id}` }],
      })],
    ]);
  };
}

interface DocFile { path: string; name: string; }
interface PinnedDoc { path: string; name: string; }
interface DocContent { path: string; name: string; content: string; }

const PINS_KEY = 'workshop:pinnedDocs';
const ACTIVE_KEY = 'workshop:activeDoc';
const RIGHT_KEY = 'workshop:rightDoc';

function loadPins(): PinnedDoc[] {
  try { return JSON.parse(localStorage.getItem(PINS_KEY) || '[]'); }
  catch { return []; }
}
function savePins(pins: PinnedDoc[]) { localStorage.setItem(PINS_KEY, JSON.stringify(pins)); }
function loadSavedDoc(key: string): { path: string; name: string } | null {
  try { const raw = localStorage.getItem(key); return raw ? JSON.parse(raw) : null; }
  catch { return null; }
}

// --- DocPane: renders a single document with header + markdown ---

interface DocPaneProps {
  doc: DocContent | null;
  loading: boolean;
  focused: boolean;
  isPinned: (path: string) => boolean;
  onCopy: () => void;
  copied: boolean;
  onPin: (path: string, name: string) => void;
  onTicketClick?: (id: number) => void;
  onTicketHover?: (id: number | null, x: number, y: number) => void;
  onFocus: () => void;
  onSplit?: () => void;
  onClose?: () => void;
  emptyMessage?: string;
}

function DocPane({ doc, loading, focused, isPinned, onCopy, copied, onPin, onTicketClick, onTicketHover, onFocus, onSplit, onClose, emptyMessage }: DocPaneProps) {
  return (
    <div className={`docs-content${focused ? ' docs-focused' : ''}`} onClick={onFocus}>
      {loading && <div className="docs-loading">Loading...</div>}
      {!loading && !doc && (
        <div className="docs-empty">
          <h2>Docs</h2>
          <p>{emptyMessage || 'Pin markdown files for quick access, or browse to find them.'}</p>
        </div>
      )}
      {!loading && doc && (
        <>
          <div className="docs-content-header">
            <span className="docs-content-title">{doc.name}</span>
            <span className="docs-content-path">{doc.path}</span>
            <button className="docs-copy-btn" onClick={onCopy}>
              {copied ? '✓ Copied' : '⎘ Copy'}
            </button>
            <button
              className={`docs-pin-btn${isPinned(doc.path) ? ' pinned' : ''}`}
              onClick={() => onPin(doc.path, doc.name)}
            >
              {isPinned(doc.path) ? '📌 Pinned' : '📌 Pin'}
            </button>
            {onSplit && <button className="btn-small docs-split-btn" onClick={onSplit} title="Split right">⫿</button>}
            {onClose && <button className="btn-small docs-close-btn" onClick={onClose} title="Close pane">✕</button>}
          </div>
          <div className="docs-markdown">
            <Markdown remarkPlugins={[remarkGfm, remarkTicketLinks]} rehypePlugins={[rehypeHighlight]} components={{
              a: ({ href, children }) => {
                const cardMatch = href?.match(/^#card\/(\d+)$/);
                if (cardMatch) {
                  const cardId = Number(cardMatch[1]);
                  return (
                    <a
                      className="ticket-ref"
                      onClick={(e) => { e.preventDefault(); onTicketHover?.(null, 0, 0); onTicketClick?.(cardId); }}
                      onMouseEnter={(e) => { const r = e.currentTarget.getBoundingClientRect(); onTicketHover?.(cardId, r.left, r.bottom + 4); }}
                      onMouseLeave={() => onTicketHover?.(null, 0, 0)}
                    >
                      {children}
                    </a>
                  );
                }
                return <a href={href} target="_blank" rel="noopener noreferrer">{children}</a>;
              },
            }}>{doc.content}</Markdown>
          </div>
        </>
      )}
    </div>
  );
}

// --- DocsView: main layout with sidebar + 1-2 doc panes ---

interface DocsViewProps {
  openPath?: string | null;
  onOpenPathConsumed?: () => void;
  onTicketClick?: (id: number) => void;
  onTicketHover?: (id: number | null, x: number, y: number) => void;
}

export function DocsView({ openPath, onOpenPathConsumed, onTicketClick, onTicketHover }: DocsViewProps = {}) {
  const [pins, setPins] = useState<PinnedDoc[]>(loadPins);
  const [files, setFiles] = useState<DocFile[]>([]);
  const [searchDir, setSearchDir] = useState('~/repos');
  const [browseOpen, setBrowseOpen] = useState(false);
  const [searchQuery, setSearchQuery] = useState('');
  const [searchResults, setSearchResults] = useState<{ path: string; name: string; line: number; context: string }[]>([]);
  const [searching, setSearching] = useState(false);

  // Two pane slots
  const [leftDoc, setLeftDoc] = useState<DocContent | null>(null);
  const [rightDoc, setRightDoc] = useState<DocContent | null>(null);
  const [leftLoading, setLeftLoading] = useState(false);
  const [rightLoading, setRightLoading] = useState(false);
  const [focusedPane, setFocusedPane] = useState<'left' | 'right'>('left');

  const [sidebarWidth, setSidebarWidth] = useState(280);
  const [leftCopied, setLeftCopied] = useState(false);
  const [rightCopied, setRightCopied] = useState(false);
  const leftCopyTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const rightCopyTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const isSplit = rightDoc !== null;

  // Load file list
  const loadFiles = useCallback(async () => {
    try {
      const res = await get<DocFile[]>(`/docs/list?dir=${encodeURIComponent(searchDir)}`);
      setFiles(res ?? []);
    } catch { setFiles([]); }
  }, [searchDir]);

  // Search docs
  const searchDocs = useCallback(async (q: string) => {
    if (!q.trim()) { setSearchResults([]); return; }
    setSearching(true);
    try {
      const res = await get<{ path: string; name: string; line: number; context: string }[]>(
        `/docs/search?dir=${encodeURIComponent(searchDir)}&q=${encodeURIComponent(q)}`
      );
      setSearchResults(res ?? []);
    } catch { setSearchResults([]); }
    finally { setSearching(false); }
  }, [searchDir]);

  // Open a doc in the focused pane
  const openDoc = useCallback(async (path: string, name: string) => {
    const isRight = focusedPane === 'right' && isSplit;
    const setDoc = isRight ? setRightDoc : setLeftDoc;
    const setLoad = isRight ? setRightLoading : setLeftLoading;
    const storageKey = isRight ? RIGHT_KEY : ACTIVE_KEY;

    setLoad(true);
    try {
      const res = await get<DocContent>(`/docs/read?path=${encodeURIComponent(path)}`);
      if (res) {
        setDoc(res);
        localStorage.setItem(storageKey, JSON.stringify({ path: res.path, name: res.name }));
      }
    } catch {
      setDoc({ path, name, content: '# Error\nFailed to load file.' });
    } finally {
      setLoad(false);
      setBrowseOpen(false);
    }
  }, [focusedPane, isSplit]);

  // Restore last-open docs on mount
  useEffect(() => {
    const last = loadSavedDoc(ACTIVE_KEY);
    if (last) openDoc(last.path, last.name);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Open a doc pushed from MCP tool
  useEffect(() => {
    if (!openPath) return;
    const name = openPath.split('/').pop() || openPath;
    openDoc(openPath, name);
    onOpenPathConsumed?.();
  }, [openPath]); // eslint-disable-line react-hooks/exhaustive-deps

  // Pin/unpin
  const togglePin = useCallback((path: string, name: string) => {
    setPins((prev) => {
      const exists = prev.some((p) => p.path === path);
      const next = exists ? prev.filter((p) => p.path !== path) : [...prev, { path, name }];
      savePins(next);
      return next;
    });
  }, []);

  const isPinned = (path: string) => pins.some((p) => p.path === path);

  const isOpenInAny = (path: string) => leftDoc?.path === path || rightDoc?.path === path;

  // Copy
  const copyLeft = useCallback(() => {
    if (!leftDoc) return;
    navigator.clipboard.writeText(leftDoc.content).then(() => {
      setLeftCopied(true);
      if (leftCopyTimer.current) clearTimeout(leftCopyTimer.current);
      leftCopyTimer.current = setTimeout(() => setLeftCopied(false), 1500);
    });
  }, [leftDoc]);

  const copyRight = useCallback(() => {
    if (!rightDoc) return;
    navigator.clipboard.writeText(rightDoc.content).then(() => {
      setRightCopied(true);
      if (rightCopyTimer.current) clearTimeout(rightCopyTimer.current);
      rightCopyTimer.current = setTimeout(() => setRightCopied(false), 1500);
    });
  }, [rightDoc]);

  // Split: open the same doc in the right pane (or empty)
  const handleSplit = useCallback(() => {
    setRightDoc(leftDoc ? { ...leftDoc } : null);
    setFocusedPane('right');
    if (leftDoc) localStorage.setItem(RIGHT_KEY, JSON.stringify({ path: leftDoc.path, name: leftDoc.name }));
  }, [leftDoc]);

  // Close right pane
  const handleCloseRight = useCallback(() => {
    setRightDoc(null);
    setFocusedPane('left');
    localStorage.removeItem(RIGHT_KEY);
  }, []);

  return (
    <div className="docs-view">
      <div className="docs-sidebar" style={{ width: sidebarWidth }}>
        <div className="docs-sidebar-header">
          <h3>Pinned</h3>
        </div>
        {pins.length === 0 && <p className="muted docs-hint">No pinned docs. Browse to pin one.</p>}
        {pins.map((pin) => (
          <div
            key={pin.path}
            className={`docs-item${isOpenInAny(pin.path) ? ' active' : ''}`}
            onClick={() => openDoc(pin.path, pin.name)}
          >
            <span className="docs-item-name">{pin.name}</span>
            <button className="docs-unpin" onClick={(e) => { e.stopPropagation(); togglePin(pin.path, pin.name); }}>✕</button>
          </div>
        ))}

        <div className="docs-sidebar-header" style={{ marginTop: '0.75rem' }}>
          <h3>Search</h3>
        </div>
        <div className="docs-search-row">
          <input
            type="text"
            className="docs-search-input"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && searchDocs(searchQuery)}
            placeholder="Search docs..."
          />
          <button className="btn-small" onClick={() => searchDocs(searchQuery)} disabled={searching}>
            {searching ? '...' : '⌕'}
          </button>
        </div>
        {searchResults.length > 0 && (
          <div className="docs-file-list docs-search-results">
            {searchResults.map((r, i) => (
              <div
                key={`${r.path}:${r.line}:${i}`}
                className={`docs-item${isOpenInAny(r.path) ? ' active' : ''}`}
                onClick={() => openDoc(r.path, r.name)}
              >
                <span className="docs-item-name">{r.name}</span>
                <span className="docs-search-context">{r.context}</span>
              </div>
            ))}
          </div>
        )}
        {searchQuery && searchResults.length === 0 && !searching && (
          <p className="muted docs-hint">No results</p>
        )}

        <div className="docs-sidebar-header" style={{ marginTop: '0.75rem' }}>
          <h3>Browse</h3>
          <button className="btn-small" onClick={() => { setBrowseOpen((p) => !p); if (!browseOpen) loadFiles(); }}>
            {browseOpen ? '▼' : '▶'}
          </button>
        </div>
        {browseOpen && (
          <>
            <div className="docs-search-row">
              <input
                type="text"
                className="docs-search-input"
                value={searchDir}
                onChange={(e) => setSearchDir(e.target.value)}
                onKeyDown={(e) => e.key === 'Enter' && loadFiles()}
                placeholder="Directory to search..."
              />
              <button className="btn-small" onClick={loadFiles}>Go</button>
            </div>
            <div className="docs-file-list">
              {files.map((f) => (
                <div
                  key={f.path}
                  className={`docs-item${isOpenInAny(f.path) ? ' active' : ''}`}
                  onClick={() => openDoc(f.path, f.name)}
                >
                  <span className="docs-item-name">{f.name}</span>
                  <button
                    className={`docs-pin-btn${isPinned(f.path) ? ' pinned' : ''}`}
                    onClick={(e) => { e.stopPropagation(); togglePin(f.path, f.name); }}
                    title={isPinned(f.path) ? 'Unpin' : 'Pin'}
                  >
                    📌
                  </button>
                </div>
              ))}
              {files.length === 0 && <p className="muted docs-hint">No .md files found</p>}
            </div>
          </>
        )}

        {isSplit && (
          <div className="docs-sidebar-header" style={{ marginTop: '0.75rem' }}>
            <p className="muted docs-hint">
              Opening in: <strong>{focusedPane}</strong> pane
            </p>
          </div>
        )}
      </div>
      <ResizeHandle onResize={(d) => setSidebarWidth((w) => Math.min(600, Math.max(180, w + d)))} />

      <DocPane
        doc={leftDoc}
        loading={leftLoading}
        focused={focusedPane === 'left'}
        isPinned={isPinned}
        onCopy={copyLeft}
        copied={leftCopied}
        onPin={togglePin}
        onTicketClick={onTicketClick}
        onTicketHover={onTicketHover}
        onFocus={() => setFocusedPane('left')}
        onSplit={!isSplit ? handleSplit : undefined}
      />

      {isSplit && (
        <>
          <ResizeHandle onResize={() => {}} />
          <DocPane
            doc={rightDoc}
            loading={rightLoading}
            focused={focusedPane === 'right'}
            isPinned={isPinned}
            onCopy={copyRight}
            copied={rightCopied}
            onPin={togglePin}
            onTicketClick={onTicketClick}
            onTicketHover={onTicketHover}
            onFocus={() => setFocusedPane('right')}
            onClose={handleCloseRight}
            emptyMessage="Click a doc in the sidebar to open it here."
          />
        </>
      )}
    </div>
  );
}
