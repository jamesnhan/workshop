import { useEffect, useState } from 'react';
import { get } from '../api/client';
import { HoverPreview } from './HoverPreview';

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
  pinned?: boolean;
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

const columnColors: Record<string, string> = {
  backlog: 'var(--text-muted)',
  in_progress: 'var(--accent)',
  review: '#f59e0b',
  done: 'var(--success)',
};

// Cache so we don't refetch on every hover
const cardCache = new Map<number, Card | null>();

export function TicketHoverPreview({ cardId, x, y, pinned }: Props) {
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
      <HoverPreview x={x} y={y} className={`ticket-hover-preview${pinned ? ' hover-pinned-inline' : ''}`}>
        <div className="ticket-hover-loading">Loading #{cardId}…</div>
      </HoverPreview>
    );
  }

  if (card === null) {
    return (
      <HoverPreview x={x} y={y} className={`ticket-hover-preview${pinned ? ' hover-pinned-inline' : ''}`}>
        <div className="ticket-hover-empty">#{cardId} not found</div>
      </HoverPreview>
    );
  }

  return (
    <HoverPreview x={x} y={y} className={`ticket-hover-preview${pinned ? ' hover-pinned-inline' : ''}`}>
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
        <span className="ticket-hover-status" style={{
          background: `color-mix(in srgb, ${columnColors[card.column] || 'var(--text-muted)'} 20%, transparent)`,
          color: columnColors[card.column] || 'var(--text-muted)',
        }}>{columnLabels[card.column] || card.column}</span>
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
    </HoverPreview>
  );
}
