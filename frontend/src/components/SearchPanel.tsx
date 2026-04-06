import { useEffect, useRef, useState, useCallback } from 'react';
import { Fzf, type FzfResultItem } from 'fzf';
import AnsiToHtml from 'ansi-to-html';
import DOMPurify from 'dompurify';
import { get } from '../api/client';

interface SearchResult {
  target: string;
  line: number;
  content: string;
  raw?: string;
}

interface ContextResponse {
  target: string;
  line: number;
  context: number;
  lines: string[];
  startLine: number;
}

interface Props {
  onSelectResult: (target: string, searchText: string) => void;
  onClose: () => void;
}

type Mode = 'typing' | 'navigating' | 'preview';

const ansiConverter = new AnsiToHtml({
  fg: '#c8c8d8',
  bg: 'transparent',
});

export function SearchPanel({ onSelectResult, onClose }: Props) {
  const [query, setQuery] = useState('');
  const [allLines, setAllLines] = useState<SearchResult[]>([]);
  const [fzfResults, setFzfResults] = useState<FzfResultItem<SearchResult>[]>([]);
  const [loading, setLoading] = useState(false);
  const [mode, setMode] = useState<Mode>('typing');
  const [selectedIdx, setSelectedIdx] = useState(0);
  const [previewCtx, setPreviewCtx] = useState<ContextResponse | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const previewRef = useRef<HTMLPreElement>(null);

  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  // Fetch buffered lines on open and refresh every 3s while panel is open
  useEffect(() => {
    const fetchLines = () => {
      get<SearchResult[]>('/search/lines')
        .then((r) => setAllLines(r ?? []))
        .catch(() => {});
    };
    setLoading(true);
    get<SearchResult[]>('/search/lines')
      .then((r) => setAllLines(r ?? []))
      .catch(() => setAllLines([]))
      .finally(() => setLoading(false));
    const interval = setInterval(fetchLines, 3000);
    return () => clearInterval(interval);
  }, []);

  // Run fzf when query or data changes
  const lastQueryRef = useRef(query);
  useEffect(() => {
    if (query.trim().length < 2) {
      setFzfResults([]);
      return;
    }
    if (debounceRef.current) clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(() => {
      const fzf = new Fzf(allLines, {
        selector: (item) => item.content,
        limit: 100,
      });
      setFzfResults(fzf.find(query.trim()));
      // Only reset mode/selection/preview when the query itself changed, not on data refresh
      if (lastQueryRef.current !== query) {
        setSelectedIdx(0);
        setMode('typing');
        setPreviewCtx(null);
        lastQueryRef.current = query;
      }
    }, 100);
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
    };
  }, [query, allLines]);

  // Scroll selected item into view
  useEffect(() => {
    if (mode === 'typing') return;
    const el = listRef.current?.children[selectedIdx] as HTMLElement | undefined;
    el?.scrollIntoView({ block: 'nearest' });
  }, [selectedIdx, mode]);

  // Auto-scroll preview to matched line
  useEffect(() => {
    if (!previewCtx || !previewRef.current) return;
    const matchEl = previewRef.current.querySelector('.match');
    if (matchEl) {
      matchEl.scrollIntoView({ block: 'center', behavior: 'instant' });
    }
  }, [previewCtx]);

  // Load preview context
  const loadPreview = useCallback(async (idx: number) => {
    const r = fzfResults[idx];
    if (!r) return;
    try {
      const ctx = await get<ContextResponse>(
        `/search/context?target=${encodeURIComponent(r.item.target)}&line=${r.item.line}&context=8`
      );
      setPreviewCtx(ctx);
    } catch {
      setPreviewCtx(null);
    }
  }, [fzfResults]);

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') {
      if (mode === 'preview') {
        setMode('navigating');
        setPreviewCtx(null);
        e.preventDefault();
        return;
      }
      if (mode === 'navigating') {
        setMode('typing');
        setPreviewCtx(null);
        inputRef.current?.focus();
        e.preventDefault();
        return;
      }
      onClose();
      return;
    }

    if (mode === 'typing') {
      if (e.key === 'Enter' && fzfResults.length > 0) {
        e.preventDefault();
        setMode('navigating');
        setSelectedIdx(0);
        return;
      }
      if (e.key === 'ArrowDown') {
        e.preventDefault();
        setSelectedIdx((i) => Math.min(i + 1, fzfResults.length - 1));
      } else if (e.key === 'ArrowUp') {
        e.preventDefault();
        setSelectedIdx((i) => Math.max(i - 1, 0));
      }
      return;
    }

    if (mode === 'navigating') {
      e.preventDefault();
      if (e.key === 'j' || e.key === 'ArrowDown') {
        setSelectedIdx((i) => Math.min(i + 1, fzfResults.length - 1));
      } else if (e.key === 'k' || e.key === 'ArrowUp') {
        setSelectedIdx((i) => Math.max(i - 1, 0));
      } else if (e.key === 'Enter') {
        // Show preview
        setMode('preview');
        loadPreview(selectedIdx);
      } else if (e.key === 'i' || e.key === '/') {
        setMode('typing');
        inputRef.current?.focus();
      }
      return;
    }

    if (mode === 'preview') {
      e.preventDefault();
      if (e.key === 'Enter') {
        // Confirm: focus the pane and scroll to the text
        const r = fzfResults[selectedIdx];
        if (r) {
          const snippet = r.item.content.trim().slice(0, 60);
          onSelectResult(r.item.target, snippet);
        }
        setPreviewCtx(null);
        onClose();
      } else if (e.key === 'j' || e.key === 'ArrowDown') {
        // Move to next result and preview it
        const next = Math.min(selectedIdx + 1, fzfResults.length - 1);
        setSelectedIdx(next);
        loadPreview(next);
      } else if (e.key === 'k' || e.key === 'ArrowUp') {
        const prev = Math.max(selectedIdx - 1, 0);
        setSelectedIdx(prev);
        loadPreview(prev);
      }
      return;
    }
  };

  const modeLabel = mode === 'typing' ? 'FIND' : mode === 'navigating' ? 'NAV' : 'PREVIEW';
  const placeholder = mode === 'typing'
    ? 'Fuzzy search... (Enter to navigate)'
    : mode === 'navigating'
    ? 'j/k move, Enter preview, Esc back, / search'
    : 'Enter to go to pane, j/k next, Esc back';

  return (
    <div className="search-panel" onKeyDown={handleKeyDown} tabIndex={-1} ref={panelRef}>
      <div className="search-header">
        <input
          ref={inputRef}
          type="text"
          className="search-input"
          placeholder={placeholder}
          value={query}
          onChange={(e) => { setQuery(e.target.value); setMode('typing'); setPreviewCtx(null); }}
          readOnly={mode !== 'typing'}
        />
        <div className={`search-mode-badge mode-${mode}`}>{modeLabel}</div>
        <button className="search-close" onClick={onClose}>x</button>
      </div>

      {/* Preview popup */}
      {mode === 'preview' && previewCtx && (
        <div className="search-preview">
          <div className="search-preview-header">
            <span className="search-result-target">{previewCtx.target}</span>
            <span className="search-result-line"> line {previewCtx.line}</span>
          </div>
          <pre className="search-preview-content" ref={previewRef}>
            {previewCtx.lines.map((line, i) => {
              const lineNum = previewCtx.startLine + i;
              const isMatch = lineNum === previewCtx.line;
              const html = ansiConverter.toHtml(line);
              return (
                <div key={i} className={`search-preview-line${isMatch ? ' match' : ''}`}>
                  <span className="search-preview-linenum">{lineNum}</span>
                  <span dangerouslySetInnerHTML={{ __html: DOMPurify.sanitize(html) }} />
                </div>
              );
            })}
          </pre>
        </div>
      )}

      <div className="search-results" ref={listRef}>
        {loading && <div className="search-status">Loading output history...</div>}
        {!loading && query.trim().length >= 2 && fzfResults.length === 0 && (
          <div className="search-status muted">No results</div>
        )}
        {fzfResults.map((r, i) => (
          <div
            key={`${r.item.target}-${r.item.line}-${i}`}
            className={`search-result${i === selectedIdx ? ' selected' : ''}`}
            onClick={() => {
              setSelectedIdx(i);
              setMode('preview');
              loadPreview(i);
            }}
            onMouseEnter={() => { if (mode !== 'preview') setSelectedIdx(i); }}
          >
            <div className="search-result-header">
              <span className="search-result-target">{r.item.target}</span>
              <span className="search-result-line">:{r.item.line}</span>
            </div>
            <pre
              className="search-result-content"
              dangerouslySetInnerHTML={{ __html: DOMPurify.sanitize(ansiConverter.toHtml(r.item.raw || r.item.content)) }}
            />
          </div>
        ))}
      </div>
    </div>
  );
}
