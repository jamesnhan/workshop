import { useState, useEffect, useRef } from 'react';
import { useTicketAutocomplete } from '../hooks/useTicketAutocomplete';
import { TicketSuggestions } from './TicketSuggestions';
import { Linkify } from './Linkify';

interface Card {
  id: number;
  title: string;
  description: string;
  column: string;
  project: string;
  position: number;
  paneTarget: string;
  labels: string;
  cardType: string;
  priority: string;
  parentId: number;
  archived: boolean;
  createdAt: string;
  updatedAt: string;
}

interface CardNote {
  id: number;
  cardId: number;
  text: string;
  createdAt: string;
}

interface CardMessage {
  id: number;
  cardId: number;
  author: string;
  text: string;
  createdAt: string;
}

interface CardLogEntry {
  id: number;
  cardId: number;
  action: string;
  beforeValue: string;
  afterValue: string;
  source: string;
  createdAt: string;
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



function formatLogAction(entry: { action: string; beforeValue: string; afterValue: string }): string {
  switch (entry.action) {
    case 'created': return `Created card`;
    case 'moved': return `Moved from ${entry.beforeValue || '?'} \u2192 ${entry.afterValue}`;
    case 'title_changed': return `Renamed: "${entry.beforeValue}" \u2192 "${entry.afterValue}"`;
    case 'priority_changed': return `Priority: ${entry.beforeValue || 'none'} \u2192 ${entry.afterValue || 'none'}`;
    case 'type_changed': return `Type: ${entry.beforeValue || 'none'} \u2192 ${entry.afterValue || 'none'}`;
    case 'description_changed': return `Description updated`;
    case 'note_added': return `Added note: "${entry.afterValue.slice(0, 60)}${entry.afterValue.length > 60 ? '\u2026' : ''}"`;
    case 'deleted': return `Deleted`;
    default: return entry.action;
  }
}

type Tab = 'details' | 'notes' | 'messages' | 'activity';

export interface CardDetailSidebarProps {
  card: Card;
  notes: CardNote[];
  messages: CardMessage[];
  cardLog: CardLogEntry[];
  allCards: Card[];
  ticketAutocomplete: boolean;
  onClose: () => void;
  onAddNote: (text: string) => void;
  onAddMessage: (author: string, text: string) => void;
  onEdit: () => void;
  onDelete: () => void;
  onNavigateToPane: (target: string) => void;
  onCheckboxToggle: (card: Card, checkIndex: number) => void;
}

export function CardDetailSidebar({
  card,
  notes,
  messages,
  cardLog,
  allCards,
  ticketAutocomplete,
  onClose,
  onAddNote,
  onAddMessage,
  onEdit,
  onDelete,
  onNavigateToPane,
  onCheckboxToggle,
}: CardDetailSidebarProps) {
  const [activeTab, setActiveTab] = useState<Tab>('details');
  const [newNote, setNewNote] = useState('');
  const [newMsgText, setNewMsgText] = useState('');
  const [msgAuthor, setMsgAuthor] = useState(() => localStorage.getItem('workshop:msg-author') || 'James');
  const noteInputRef = useRef<HTMLInputElement>(null);

  const noteAC = useTicketAutocomplete({
    inputRef: noteInputRef,
    value: newNote,
    onChange: setNewNote,
    cards: allCards,
    enabled: ticketAutocomplete,
  });

  // Reset state when card changes
  useEffect(() => {
    setNewNote('');
    setNewMsgText('');
  }, [card.id]);

  // Escape key closes sidebar
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [onClose]);

  const handleAddNote = () => {
    if (!newNote.trim()) return;
    onAddNote(newNote.trim());
    setNewNote('');
    noteInputRef.current?.focus();
  };

  const handleAddMessage = () => {
    if (!newMsgText.trim()) return;
    localStorage.setItem('workshop:msg-author', msgAuthor);
    onAddMessage(msgAuthor, newMsgText.trim());
    setNewMsgText('');
  };

  const tabs: { id: Tab; label: string; count?: number }[] = [
    { id: 'details', label: 'Details' },
    { id: 'notes', label: 'Notes', count: notes.length },
    { id: 'messages', label: 'Messages', count: messages.length },
    { id: 'activity', label: 'Activity', count: cardLog.length },
  ];

  return (
    <div className="card-sidebar" onClick={(e) => e.stopPropagation()}>
      <div className="card-sidebar-header">
        <div className="kanban-modal-badges">
          <span className="kanban-card-id-badge">#{card.id}</span>
          {card.cardType && (
            <span className="kanban-type-badge" style={{ borderColor: typeColors[card.cardType] || 'var(--border)' }}>
              {card.cardType}
            </span>
          )}
          {card.priority && (
            <span className="kanban-priority-badge" style={{ color: priorityColors[card.priority] || 'var(--text-muted)' }}>
              {card.priority}
            </span>
          )}
        </div>
        <button className="search-close" onClick={onClose}>x</button>
      </div>

      <h3 className="kanban-modal-title">{card.title}</h3>

      {/* Tabs */}
      <div className="card-sidebar-tabs">
        {tabs.map((tab) => (
          <button
            key={tab.id}
            className={`card-sidebar-tab${activeTab === tab.id ? ' active' : ''}`}
            onClick={() => setActiveTab(tab.id)}
          >
            {tab.label}
            {tab.count != null && tab.count > 0 && (
              <span className="card-sidebar-tab-count">{tab.count}</span>
            )}
          </button>
        ))}
      </div>

      {/* Tab content */}
      <div className="card-sidebar-content">
        {activeTab === 'details' && (
          <div className="card-sidebar-details">
            {card.description && (
              <div className="kanban-modal-desc">
                {card.description.split('\n').map((line, li) => {
                  const checkMatch = line.match(/^- \[([ x])\] (.*)$/);
                  if (checkMatch) {
                    const isChecked = checkMatch[1] === 'x';
                    const label = checkMatch[2];
                    const checkIndex = card.description.split('\n').slice(0, li + 1)
                      .filter((l) => /^- \[[ x]\]/.test(l)).length - 1;
                    return (
                      <label key={li} className="kanban-checklist-item">
                        <input type="checkbox" checked={isChecked} onChange={() => onCheckboxToggle(card, checkIndex)} />
                        <span className={isChecked ? 'checked' : ''}>{label}</span>
                      </label>
                    );
                  }
                  return <p key={li}>{line ? <Linkify>{line}</Linkify> : '\u00A0'}</p>;
                })}
              </div>
            )}
            <div className="kanban-modal-meta">
              {card.project && <span className="kanban-label project">{card.project}</span>}
              {card.labels && card.labels.split(',').map((l) => (
                <span key={l.trim()} className="kanban-label">{l.trim()}</span>
              ))}
            </div>
            {card.paneTarget && (
              <div className="kanban-card-pane" onClick={() => onNavigateToPane(card.paneTarget)}>
                &rarr; {card.paneTarget}
              </div>
            )}
          </div>
        )}

        {activeTab === 'notes' && (
          <div className="kanban-notes" style={{ border: 'none', margin: 0, padding: 0 }}>
            {notes.length === 0 && <p className="muted" style={{ fontSize: '0.75rem' }}>No notes yet</p>}
            {notes.map((n) => (
              <div key={n.id} className="kanban-note">
                <span className="kanban-note-date">{new Date(n.createdAt).toLocaleString()}</span>
                <span className="kanban-note-text"><Linkify>{n.text}</Linkify></span>
              </div>
            ))}
            <div className="kanban-note-input kanban-autocomplete-wrapper">
              <input
                ref={noteInputRef}
                type="text"
                placeholder="Add a note..."
                value={newNote}
                onChange={noteAC.handleChange}
                onKeyDown={(e) => { noteAC.handleKeyDown(e); if (!noteAC.showDropdown && e.key === 'Enter') handleAddNote(); }}
              />
              <button className="btn-create" onClick={handleAddNote} disabled={!newNote.trim()}>Add</button>
              {noteAC.showDropdown && <TicketSuggestions suggestions={noteAC.suggestions} selectedIdx={noteAC.selectedIdx} onSelect={noteAC.accept} />}
            </div>
          </div>
        )}

        {activeTab === 'messages' && (
          <div className="kanban-messages" style={{ border: 'none', margin: 0, padding: 0 }}>
            {messages.length === 0 && <p className="muted" style={{ fontSize: '0.75rem' }}>No messages yet</p>}
            {messages.map((m) => (
              <div key={m.id} className="kanban-msg-item">
                <div className="kanban-msg-meta">
                  <strong className="kanban-msg-author">{m.author || 'Anonymous'}</strong>
                  <span className="kanban-msg-date">{new Date(m.createdAt).toLocaleString()}</span>
                </div>
                <div className="kanban-msg-text"><Linkify>{m.text}</Linkify></div>
              </div>
            ))}
            <div className="kanban-msg-input">
              <input
                type="text"
                className="kanban-msg-author-input"
                placeholder="Name"
                value={msgAuthor}
                onChange={(e) => setMsgAuthor(e.target.value)}
              />
              <input
                type="text"
                placeholder="Write a message..."
                value={newMsgText}
                onChange={(e) => setNewMsgText(e.target.value)}
                onKeyDown={(e) => { if (e.key === 'Enter') handleAddMessage(); }}
              />
              <button className="btn-create" onClick={handleAddMessage} disabled={!newMsgText.trim()}>Send</button>
            </div>
          </div>
        )}

        {activeTab === 'activity' && (
          <div className="kanban-log" style={{ border: 'none', margin: 0, padding: 0 }}>
            {cardLog.length === 0 && <p className="muted" style={{ fontSize: '0.75rem' }}>No activity yet</p>}
            {cardLog.map((entry) => (
              <div key={entry.id} className="kanban-log-entry">
                <span className="kanban-log-date">{new Date(entry.createdAt).toLocaleString()}</span>
                <span className="kanban-log-action">{formatLogAction(entry)}</span>
                {entry.source !== 'user' && <span className="kanban-log-source">{entry.source}</span>}
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Footer — always visible */}
      <div className="kanban-modal-footer">
        <span className="kanban-modal-date">Created: {new Date(card.createdAt).toLocaleDateString()}</span>
        <div className="kanban-card-actions">
          <button className="btn-create" onClick={onEdit}>Edit</button>
          <button className="btn-danger-small" onClick={onDelete}>Delete</button>
        </div>
      </div>
    </div>
  );
}
