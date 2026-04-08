import { useEffect, useRef, useState, useCallback } from 'react';
import { get } from '../api/client';
import type { AutocompleteCard } from '../hooks/useTicketAutocomplete';

const PRIORITY_COLORS: Record<string, string> = {
  P0: 'var(--error)', P1: 'var(--warning, #f9e2af)', P2: 'var(--accent)', P3: 'var(--text-muted)',
};
const COLUMN_LABELS: Record<string, string> = {
  backlog: 'backlog', in_progress: 'in progress', review: 'review', done: 'done',
};

interface Props {
  onClose: () => void;
  onInsert?: (text: string) => void; // when opened from terminal: insert into PTY instead of clipboard
}

export function TicketLookupDialog({ onClose, onInsert }: Props) {
  const [query, setQuery] = useState('');
  const [cards, setCards] = useState<AutocompleteCard[]>([]);
  const [selectedIdx, setSelectedIdx] = useState(0);
  const [copied, setCopied] = useState<string | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const copyTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    get<AutocompleteCard[]>('/cards').then((c) => { if (c) setCards(c); }).catch(() => {});
    inputRef.current?.focus();
  }, []);

  const filtered = cards.filter((c) => {
    if (!query) return true;
    const q = query.toLowerCase();
    return String(c.id).startsWith(q) || c.title.toLowerCase().includes(q);
  }).slice(0, 10);

  useEffect(() => { setSelectedIdx(0); }, [query]);

  const copyCard = useCallback((card: AutocompleteCard) => {
    const ref = `#${card.id} `;
    if (onInsert) {
      // Terminal mode: type the reference directly into the PTY
      onInsert(ref);
      onClose();
    } else {
      // Clipboard mode: copy and show confirmation
      navigator.clipboard.writeText(ref.trim()).then(() => {
        setCopied(ref.trim());
        if (copyTimer.current) clearTimeout(copyTimer.current);
        copyTimer.current = setTimeout(() => {
          setCopied(null);
          onClose();
        }, 800);
      });
    }
  }, [onInsert, onClose]);

  const handleKeyDown = useCallback((e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown') { e.preventDefault(); setSelectedIdx((i) => Math.min(i + 1, filtered.length - 1)); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); setSelectedIdx((i) => Math.max(i - 1, 0)); }
    else if (e.key === 'Enter') { e.preventDefault(); if (filtered[selectedIdx]) copyCard(filtered[selectedIdx]); }
    else if (e.key === 'Escape') { e.preventDefault(); if (onInsert) onInsert('#'); onClose(); }
    else if (e.key === 'Tab') {
      // Focus trap: this dialog only has one focusable control (the input),
      // so Tab has nowhere meaningful to go. Swallow it and keep the input focused.
      e.preventDefault();
      inputRef.current?.focus();
    }
  }, [filtered, selectedIdx, copyCard, onClose, onInsert]);

  return (
    <div className="ticket-lookup-overlay" onClick={onClose}>
      <div className="ticket-lookup-dialog" onClick={(e) => e.stopPropagation()}>
        <div className="ticket-lookup-header">
          <input
            ref={inputRef}
            className="ticket-lookup-input"
            placeholder="Search tickets..."
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={handleKeyDown}
          />
          {copied && <span className="ticket-lookup-copied">✓ {copied} copied</span>}
        </div>
        <div className="ticket-lookup-list">
          {filtered.length === 0 && <div className="ticket-lookup-empty">No tickets found</div>}
          {filtered.map((card, i) => (
            <div
              key={card.id}
              className={`ticket-lookup-item${i === selectedIdx ? ' selected' : ''}`}
              onMouseEnter={() => setSelectedIdx(i)}
              onClick={() => copyCard(card)}
            >
              <span className="ticket-suggestion-id" style={{ color: PRIORITY_COLORS[card.priority] || 'var(--text-muted)' }}>
                #{card.id}
              </span>
              <span className="ticket-suggestion-title">{card.title}</span>
              <span className="ticket-suggestion-meta">{COLUMN_LABELS[card.column] ?? card.column}</span>
            </div>
          ))}
        </div>
        <div className="ticket-lookup-footer">Enter to copy · Esc to close</div>
      </div>
    </div>
  );
}
