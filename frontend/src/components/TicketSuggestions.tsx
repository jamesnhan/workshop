import type { AutocompleteCard } from '../hooks/useTicketAutocomplete';

const COLUMN_LABELS: Record<string, string> = {
  backlog: 'backlog', in_progress: 'in progress', review: 'review', done: 'done',
};

const PRIORITY_COLORS: Record<string, string> = {
  P0: 'var(--error)', P1: 'var(--warning, #f9e2af)', P2: 'var(--accent)', P3: 'var(--text-muted)',
};

interface Props {
  suggestions: AutocompleteCard[];
  selectedIdx: number;
  onSelect: (card: AutocompleteCard) => void;
}

export function TicketSuggestions({ suggestions, selectedIdx, onSelect }: Props) {
  if (suggestions.length === 0) return null;

  return (
    <div className="ticket-suggestions">
      {suggestions.map((card, i) => (
        <div
          key={card.id}
          className={`ticket-suggestion-item${i === selectedIdx ? ' selected' : ''}`}
          onMouseDown={(e) => { e.preventDefault(); onSelect(card); }}
        >
          <span className="ticket-suggestion-id" style={{ color: PRIORITY_COLORS[card.priority] || 'var(--text-muted)' }}>
            #{card.id}
          </span>
          <span className="ticket-suggestion-title">{card.title}</span>
          <span className="ticket-suggestion-meta">{COLUMN_LABELS[card.column] ?? card.column}</span>
        </div>
      ))}
    </div>
  );
}
