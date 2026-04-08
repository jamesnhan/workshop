import { useState, useCallback, useEffect, useRef } from 'react';
import Markdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { get } from '../api/client';
import { ResizeHandle } from './ResizeHandle';

interface DocFile {
  path: string;
  name: string;
}

interface PinnedDoc {
  path: string;
  name: string;
}

const PINS_KEY = 'workshop:pinnedDocs';

function loadPins(): PinnedDoc[] {
  try {
    return JSON.parse(localStorage.getItem(PINS_KEY) || '[]');
  } catch { return []; }
}

function savePins(pins: PinnedDoc[]) {
  localStorage.setItem(PINS_KEY, JSON.stringify(pins));
}

interface DocsViewProps {
  openPath?: string | null;
  onOpenPathConsumed?: () => void;
}

export function DocsView({ openPath, onOpenPathConsumed }: DocsViewProps = {}) {
  const [pins, setPins] = useState<PinnedDoc[]>(loadPins);
  const [files, setFiles] = useState<DocFile[]>([]);
  const [searchDir, setSearchDir] = useState('~/repos');
  const [activeDoc, setActiveDoc] = useState<{ path: string; name: string; content: string } | null>(null);
  const [loading, setLoading] = useState(false);
  const [browseOpen, setBrowseOpen] = useState(false);

  // Load file list
  const loadFiles = useCallback(async () => {
    try {
      const res = await get<DocFile[]>(`/docs/list?dir=${encodeURIComponent(searchDir)}`);
      setFiles(res ?? []);
    } catch { setFiles([]); }
  }, [searchDir]);

  // Open a doc
  const openDoc = useCallback(async (path: string, name: string) => {
    setLoading(true);
    try {
      const res = await get<{ path: string; name: string; content: string }>(`/docs/read?path=${encodeURIComponent(path)}`);
      if (res) setActiveDoc(res);
    } catch {
      setActiveDoc({ path, name, content: '# Error\nFailed to load file.' });
    } finally {
      setLoading(false);
      setBrowseOpen(false);
    }
  }, []);

  // Open a doc pushed from the MCP tool / backend
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

  const [sidebarWidth, setSidebarWidth] = useState(280);
  const [copied, setCopied] = useState(false);
  const copyTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const copyToClipboard = useCallback(() => {
    if (!activeDoc) return;
    navigator.clipboard.writeText(activeDoc.content).then(() => {
      setCopied(true);
      if (copyTimerRef.current) clearTimeout(copyTimerRef.current);
      copyTimerRef.current = setTimeout(() => setCopied(false), 1500);
    });
  }, [activeDoc]);

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
            className={`docs-item${activeDoc?.path === pin.path ? ' active' : ''}`}
            onClick={() => openDoc(pin.path, pin.name)}
          >
            <span className="docs-item-name">{pin.name}</span>
            <button className="docs-unpin" onClick={(e) => { e.stopPropagation(); togglePin(pin.path, pin.name); }}>✕</button>
          </div>
        ))}

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
                  className={`docs-item${activeDoc?.path === f.path ? ' active' : ''}`}
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
      </div>
      <ResizeHandle onResize={(d) => setSidebarWidth((w) => Math.min(600, Math.max(180, w + d)))} />
      <div className="docs-content">
        {loading && <div className="docs-loading">Loading...</div>}
        {!loading && !activeDoc && (
          <div className="docs-empty">
            <h2>Docs</h2>
            <p>Pin markdown files for quick access, or browse to find them.</p>
          </div>
        )}
        {!loading && activeDoc && (
          <>
            <div className="docs-content-header">
              <span className="docs-content-title">{activeDoc.name}</span>
              <span className="docs-content-path">{activeDoc.path}</span>
              <button className="docs-copy-btn" onClick={copyToClipboard}>
                {copied ? '✓ Copied' : '⎘ Copy'}
              </button>
              <button
                className={`docs-pin-btn${isPinned(activeDoc.path) ? ' pinned' : ''}`}
                onClick={() => togglePin(activeDoc.path, activeDoc.name)}
              >
                {isPinned(activeDoc.path) ? '📌 Pinned' : '📌 Pin'}
              </button>
            </div>
            <div className="docs-markdown">
              <Markdown remarkPlugins={[remarkGfm]}>{activeDoc.content}</Markdown>
            </div>
          </>
        )}
      </div>
    </div>
  );
}
