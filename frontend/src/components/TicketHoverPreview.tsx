import { useEffect, useState } from 'react';
import { get } from '../api/client';

interface Card {
  id: number;
  title: string;
  description: string;
  column: string;
  project: string;
  cardType: string;
  priority: string;
  labels: string;
}

interface Props {
  cardId: number;
  x: number;
  y: number;
}

const typeColors: Record<string, string> = {
  bug: 'var(--error)',
  feature: 'var(--accent)',
  task: 'var(--success)',
  chore: 'var(--text-muted)',
};

const priorityColors: Record<string, string> = {
  P0: 'var(--error)',
  P1: '#f59e0b',
  P2: 'var(--text-muted)',
  P3: 'var(--text-dim)',
};

const columnLabels: Record<string, string> = {
  backlog: 'Backlog',
  in_progress: 'In Progress',
  review: 'Review',
  done: 'Done',
};

// Cache so we don't refetch on every hover
const cardCache = new Map<number, Card | null>();

export function TicketHoverPreview({ cardId, x, y }: Props) {
  const [card, setCard] = useState<Card | null | undefined>(cardCache.get(cardId));

  useEffect(() => {
    if (cardCache.has(cardId)) {
      setCard(cardCache.get(cardId));
      return;
    }
    let cancelled = false;
    get<Card>(`/cards/${cardId}`)
      .then((c) => {
        cardCache.set(cardId, c);
        if (!cancelled) setCard(c);
      })
      .catch(() => {
        cardCache.set(cardId, null);
        if (!cancelled) setCard(null);
      });
    return () => { cancelled = true; };
  }, [cardId]);

  if (card === undefined) {
    return (
      <div className="ticket-hover-preview" style={{ top: y + 20, left: x }}>
        <div className="ticket-hover-loading">Loading #{cardId}…</div>
      </div>
    );
  }

  if (card === null) {
    return (
      <div className="ticket-hover-preview" style={{ top: y + 20, left: x }}>
        <div className="ticket-hover-empty">#{cardId} not found</div>
      </div>
    );
  }

  // Constrain so it doesn't go off-screen
  const top = Math.min(y + 20, window.innerHeight - 280);
  const left = Math.min(x, window.innerWidth - 360);

  return (
    <div className="ticket-hover-preview" style={{ top, left }}>
      <div className="ticket-hover-header">
        <span className="ticket-hover-id">#{card.id}</span>
        {card.cardType && (
          <span className="ticket-hover-type" style={{ borderColor: typeColors[card.cardType] || 'var(--border)' }}>
            {card.cardType}
          </span>
        )}
        {card.priority && (
          <span className="ticket-hover-priority" style={{ color: priorityColors[card.priority] || 'var(--text-muted)' }}>
            {card.priority}
          </span>
        )}
        <span className="ticket-hover-status">{columnLabels[card.column] || card.column}</span>
      </div>
      <div className="ticket-hover-title">{card.title}</div>
      {card.description && (
        <div className="ticket-hover-desc">
          {card.description.slice(0, 240)}{card.description.length > 240 ? '…' : ''}
        </div>
      )}
      {card.project && (
        <div className="ticket-hover-meta">
          <span className="ticket-hover-project">{card.project}</span>
        </div>
      )}
    </div>
  );
}
