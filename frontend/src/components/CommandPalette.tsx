import { useState, useEffect, useRef, useCallback } from 'react';
import { Fzf, type FzfResultItem } from 'fzf';

export interface Command {
  id: string;
  label: string;
  category: string;
  shortcut?: string;
  action: () => void;
}

interface Props {
  commands: Command[];
  onClose: () => void;
}

function highlightPositions(text: string, positions: Set<number>) {
  if (positions.size === 0) return <>{text}</>;
  const parts: (string | React.ReactElement)[] = [];
  let current = '';
  let inHighlight = false;
  for (let i = 0; i < text.length; i++) {
    const isMatch = positions.has(i);
    if (isMatch && !inHighlight) {
      if (current) parts.push(current);
      current = text[i];
      inHighlight = true;
    } else if (isMatch && inHighlight) {
      current += text[i];
    } else if (!isMatch && inHighlight) {
      parts.push(<mark key={`h${i}`} className="search-highlight">{current}</mark>);
      current = text[i];
      inHighlight = false;
    } else {
      current += text[i];
    }
  }
  if (current) {
    if (inHighlight) {
      parts.push(<mark key="last" className="search-highlight">{current}</mark>);
    } else {
      parts.push(current);
    }
  }
  return <>{parts}</>;
}

export function CommandPalette({ commands, onClose }: Props) {
  const [query, setQuery] = useState('');
  const [selectedIdx, setSelectedIdx] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);

  const fzfResults: FzfResultItem<Command>[] = query.trim().length === 0
    ? commands.map((c) => ({ item: c, score: 0, positions: new Set<number>(), start: 0, end: 0 }))
    : new Fzf(commands, { selector: (c) => `${c.category}: ${c.label}`, limit: 30 }).find(query.trim());

  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  useEffect(() => {
    setSelectedIdx(0);
  }, [query]);

  useEffect(() => {
    const el = listRef.current?.children[selectedIdx] as HTMLElement | undefined;
    el?.scrollIntoView({ block: 'nearest' });
  }, [selectedIdx]);

  const runCommand = useCallback((cmd: Command) => {
    onClose();
    // Run after close so any state changes from the command don't conflict
    requestAnimationFrame(() => cmd.action());
  }, [onClose]);

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown' || (e.key === 'j' && e.ctrlKey)) {
      e.preventDefault();
      setSelectedIdx((i) => Math.min(i + 1, fzfResults.length - 1));
    } else if (e.key === 'ArrowUp' || (e.key === 'k' && e.ctrlKey)) {
      e.preventDefault();
      setSelectedIdx((i) => Math.max(i - 1, 0));
    } else if (e.key === 'Enter' && fzfResults.length > 0) {
      e.preventDefault();
      runCommand(fzfResults[selectedIdx].item);
    } else if (e.key === 'Escape') {
      onClose();
    }
  };

  // Group by category for display when no query
  const showCategories = query.trim().length === 0;

  return (
    <div className="switcher-overlay" onClick={onClose}>
      <div className="command-palette" onClick={(e) => e.stopPropagation()}>
        <input
          ref={inputRef}
          type="text"
          className="switcher-input"
          placeholder="Type a command..."
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={handleKeyDown}
        />
        <div className="command-list" ref={listRef}>
          {fzfResults.map((r, i) => (
            <div
              key={r.item.id}
              className={`command-item${i === selectedIdx ? ' selected' : ''}`}
              onClick={() => runCommand(r.item)}
              onMouseEnter={() => setSelectedIdx(i)}
            >
              {showCategories && <span className="command-category">{r.item.category}</span>}
              <span className="command-label">
                {query.trim() ? highlightPositions(`${r.item.category}: ${r.item.label}`, r.positions) : r.item.label}
              </span>
              {r.item.shortcut && <kbd className="command-shortcut">{r.item.shortcut}</kbd>}
            </div>
          ))}
          {fzfResults.length === 0 && (
            <div className="command-item muted">No matching commands</div>
          )}
        </div>
      </div>
    </div>
  );
}
