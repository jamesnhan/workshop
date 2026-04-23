import { useEffect, useState, useCallback, useRef } from 'react';
import { get, post } from '../api/client';
import { useTicketAutocomplete } from '../hooks/useTicketAutocomplete';
import { TicketSuggestions } from './TicketSuggestions';
import { CardDetailSidebar } from './CardDetailSidebar';

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

interface WorkflowColumn { id: string; label: string; }
interface WorkflowConfig { columns: WorkflowColumn[]; transitions: Record<string, string[]>; }

const DEFAULT_COLUMNS: WorkflowColumn[] = [
  { id: 'backlog', label: 'Backlog' },
  { id: 'in_progress', label: 'In Progress' },
  { id: 'review', label: 'Review' },
  { id: 'done', label: 'Done' },
];

const CARD_TYPES = ['', 'bug', 'feature', 'task', 'chore'];
const PRIORITIES = ['', 'P0', 'P1', 'P2', 'P3'];

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

// Checklist helpers — parse markdown checkboxes in descriptions
function parseChecklist(desc: string): { checked: number; total: number } | null {
  const matches = desc.match(/^- \[([ x])\]/gm);
  if (!matches || matches.length === 0) return null;
  const checked = matches.filter((m) => m === '- [x]').length;
  return { checked, total: matches.length };
}

function toggleCheckbox(desc: string, index: number): string {
  let i = 0;
  return desc.replace(/^(- \[)([ x])(\])/gm, (match, pre, state, post) => {
    if (i++ === index) {
      return `${pre}${state === 'x' ? ' ' : 'x'}${post}`;
    }
    return match;
  });
}

function formatLogAction(entry: { action: string; beforeValue: string; afterValue: string }): string {
  switch (entry.action) {
    case 'created': return `Created card`;
    case 'moved': return `Moved from ${entry.beforeValue || '?'} → ${entry.afterValue}`;
    case 'title_changed': return `Renamed: "${entry.beforeValue}" → "${entry.afterValue}"`;
    case 'priority_changed': return `Priority: ${entry.beforeValue || 'none'} → ${entry.afterValue || 'none'}`;
    case 'type_changed': return `Type: ${entry.beforeValue || 'none'} → ${entry.afterValue || 'none'}`;
    case 'description_changed': return `Description updated`;
    case 'note_added': return `Added note: "${entry.afterValue.slice(0, 60)}${entry.afterValue.length > 60 ? '…' : ''}"`;
    case 'deleted': return `Deleted`;
    default: return entry.action;
  }
}

interface Props {
  onNavigateToPane: (target: string) => void;
  defaultProject?: string;
  focusedPath?: string;
  ticketAutocomplete?: boolean;
  openCardId?: number | null; // when set, expands the matching card on next render
  onCardOpened?: () => void; // called after openCardId has been consumed
  sfwMode?: boolean;
  nsfwProjects?: string[];
}

export function KanbanBoard({ onNavigateToPane, defaultProject, focusedPath, ticketAutocomplete = true, openCardId, onCardOpened, sfwMode = false, nsfwProjects = [] }: Props) {
  const [cards, setCards] = useState<Card[]>([]);
  const [projects, setProjects] = useState<string[]>([]);
  const [filterProject, setFilterProject] = useState(defaultProject ?? '');
  const [hasAutoFiltered, setHasAutoFiltered] = useState(false);
  const [repoName, setRepoName] = useState<string | undefined>();

  // Fetch repo name from git info when we have a path
  useEffect(() => {
    if (focusedPath) {
      get<{ repoName: string }>(`/git/info?dir=${encodeURIComponent(focusedPath)}`)
        .then((info) => { if (info?.repoName) setRepoName(info.repoName); })
        .catch(() => {});
    }
  }, [focusedPath]);
  const [columns, setColumns] = useState<WorkflowColumn[]>(DEFAULT_COLUMNS);
  const [transitions, setTransitions] = useState<Record<string, string[]>>({});
  const [collapsedCols, setCollapsedCols] = useState<Set<string>>(new Set());
  const [collapsedParents, setCollapsedParents] = useState<Set<number>>(new Set());
  const [dragCard, setDragCard] = useState<Card | null>(null);
  const [dropTarget, setDropTarget] = useState<{ column: string; position: number } | null>(null);
  const [expandedCard, setExpandedCard] = useState<Card | null>(null);
  const [editCard, setEditCard] = useState<Card | null>(null);
  const [notes, setNotes] = useState<CardNote[]>([]);
  const [cardLog, setCardLog] = useState<CardLogEntry[]>([]);
  const [messages, setMessages] = useState<CardMessage[]>([]);
  const [showChangelog, setShowChangelog] = useState(false);
  const [projectLog, setProjectLog] = useState<CardLogEntry[]>([]);
  const [newCardCol, setNewCardCol] = useState<string | null>(null);
  const [newTitle, setNewTitle] = useState('');
  const [newDesc, setNewDesc] = useState('');
  const [newProject, setNewProject] = useState('');
  const [newPaneTarget, setNewPaneTarget] = useState('');
  const [newLabels, setNewLabels] = useState('');
  const [newType, setNewType] = useState('');
  const [newPriority, setNewPriority] = useState('');

  const editDescRef = useRef<HTMLTextAreaElement>(null);
  const newDescRef = useRef<HTMLTextAreaElement>(null);

  const editDescAC = useTicketAutocomplete({
    inputRef: editDescRef,
    value: editCard?.description ?? '',
    onChange: (v) => editCard && setEditCard({ ...editCard, description: v }),
    cards,
    enabled: ticketAutocomplete,
  });
  const newDescAC = useTicketAutocomplete({ inputRef: newDescRef, value: newDesc, onChange: setNewDesc, cards, enabled: ticketAutocomplete });
  // External open: when openCardId is provided, find and expand that card
  // once the card list is loaded.
  useEffect(() => {
    if (openCardId == null) return;
    const card = cards.find((c) => c.id === openCardId);
    if (card) {
      setExpandedCard(card);
      onCardOpened?.();
    }
  }, [openCardId, cards, onCardOpened]);

  // Fetch notes, activity log, and messages when expanding a card
  useEffect(() => {
    if (expandedCard) {
      get<CardNote[]>(`/cards/${expandedCard.id}/notes`).then((n) => setNotes(n ?? [])).catch(() => setNotes([]));
      get<CardLogEntry[]>(`/cards/${expandedCard.id}/log`).then((l) => setCardLog(l ?? [])).catch(() => setCardLog([]));
      get<CardMessage[]>(`/cards/${expandedCard.id}/messages`).then((m) => setMessages(m ?? [])).catch(() => setMessages([]));
    } else {
      setNotes([]);
      setCardLog([]);
    }
  }, [expandedCard]);


  // Fetch project changelog when toggled on
  useEffect(() => {
    if (showChangelog) {
      const q = filterProject ? `?project=${encodeURIComponent(filterProject)}&limit=200` : '?limit=200';
      get<CardLogEntry[]>(`/cards/log${q}`).then((l) => setProjectLog(l ?? [])).catch(() => setProjectLog([]));
    }
  }, [showChangelog, filterProject]);

  const wasDragging = useRef(false);

  const refresh = useCallback(() => {
    const params = new URLSearchParams();
    if (filterProject) params.set('project', filterProject);
    params.set('include_archived', 'true');
    get<Card[]>(`/cards?${params}`).then(setCards).catch((err) => console.error('Failed to load cards:', err));
    get<string[]>('/projects').then((p) => setProjects(p ?? [])).catch((err) => console.error('Failed to load projects:', err));
  }, [filterProject]);

  useEffect(() => { refresh(); }, [refresh]);

  // Auto-filter on first load: try session name, path basename, then substring match.
  // Separated from refresh to avoid a re-render loop (refresh depends on filterProject,
  // and auto-filter sets filterProject).
  useEffect(() => {
    if (hasAutoFiltered || projects.length === 0) return;
    let match = '';
    if (defaultProject && projects.includes(defaultProject)) {
      match = defaultProject;
    }
    if (!match && repoName && projects.includes(repoName)) {
      match = repoName;
    }
    if (!match && focusedPath) {
      const basename = focusedPath.split('/').filter(Boolean).pop() || '';
      if (projects.includes(basename)) match = basename;
    }
    if (!match && focusedPath) {
      const pathLower = focusedPath.toLowerCase();
      for (const proj of projects) {
        if (pathLower.includes(proj.toLowerCase())) { match = proj; break; }
      }
    }
    if (match) {
      setFilterProject(match);
      setHasAutoFiltered(true);
    }
  }, [projects, defaultProject, focusedPath, repoName, hasAutoFiltered]);

  // Fetch workflow columns + transitions when the active project changes.
  useEffect(() => {
    const params = filterProject ? `?project=${encodeURIComponent(filterProject)}` : '';
    get<WorkflowConfig>(`/workflows${params}`)
      .then((wf) => {
        if (wf?.columns?.length) setColumns(wf.columns);
        else setColumns(DEFAULT_COLUMNS);
        setTransitions(wf?.transitions ?? {});
      })
      .catch(() => { setColumns(DEFAULT_COLUMNS); setTransitions({}); });
  }, [filterProject]);

  const nsfwSet = new Set(nsfwProjects.map((p) => p.toLowerCase()));
  const isHidden = (project: string) => sfwMode && nsfwSet.has(project.toLowerCase());
  const visibleProjects = sfwMode ? projects.filter((p) => !isHidden(p)) : projects;
  const visibleCards = sfwMode ? cards.filter((c) => !isHidden(c.project)) : cards;

  const handleCreate = async () => {
    if (!newTitle.trim() || !newCardCol) return;
    try {
      await post('/cards', {
        title: newTitle.trim(),
        description: newDesc.trim(),
        column: newCardCol,
        project: newProject.trim(),
        paneTarget: newPaneTarget.trim(),
        labels: newLabels.trim(),
        cardType: newType,
        priority: newPriority,
      });
      setNewTitle(''); setNewDesc(''); setNewProject(''); setNewPaneTarget('');
      setNewLabels(''); setNewType(''); setNewPriority(''); setNewCardCol(null);
      refresh();
    } catch (err) {
      alert('Failed to create card: ' + err);
    }
  };

  const handleUpdate = async () => {
    if (!editCard) return;
    try {
      const resp = await fetch(`/api/v1/cards/${editCard.id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(editCard),
      });
      if (!resp.ok) throw new Error(`${resp.status}`);
      setEditCard(null); setExpandedCard(null);
      refresh();
    } catch (err) {
      alert('Failed to update card: ' + err);
    }
  };

  const handleDelete = async (id: number) => {
    if (!confirm('Delete this card?')) return;
    try {
      const resp = await fetch(`/api/v1/cards/${id}`, { method: 'DELETE' });
      if (!resp.ok) throw new Error(`${resp.status}`);
      setExpandedCard(null);
      refresh();
    } catch (err) {
      alert('Failed to delete card: ' + err);
    }
  };

  const handleDrop = async (column: string, position?: number) => {
    if (!dragCard) { setDragCard(null); setDropTarget(null); return; }
    const pos = position ?? 0;
    if (dragCard.column === column && dragCard.position === pos) { setDragCard(null); setDropTarget(null); return; }
    try {
      await post(`/cards/${dragCard.id}/move`, { column, position: pos });
    } catch (err: any) {
      alert(err?.message || 'Move failed');
    }
    setDragCard(null);
    setDropTarget(null);
    refresh();
  };

  const handleCardDragOver = (e: React.DragEvent, column: string, position: number) => {
    e.preventDefault();
    e.stopPropagation();
    setDropTarget({ column, position });
  };

  // Only show root cards (no parent) in column lists.
  // Children render nested under their parent.
  const columnCards = (col: string) => visibleCards.filter((c) => c.column === col && !c.parentId);
  const cardById = new Map(visibleCards.map((c) => [c.id, c]));
  // Build a children index: parentId → child cards
  const childrenByParent = new Map<number, Card[]>();
  for (const c of visibleCards) {
    if (c.parentId) {
      const arr = childrenByParent.get(c.parentId) ?? [];
      arr.push(c);
      childrenByParent.set(c.parentId, arr);
    }
  }
  // Track which parents are collapsed (component-local state)
  const childrenOf = (id: number) => childrenByParent.get(id) ?? [];
  const completionFor = (id: number) => {
    const kids = childrenOf(id);
    if (kids.length === 0) return null;
    const done = kids.filter((k) => k.column === 'done').length;
    return { done, total: kids.length };
  };

  return (
    <div className="kanban-overlay">
      <div className="kanban-header">
        <select className="theme-select" value={filterProject} onChange={(e) => setFilterProject(e.target.value)}>
          <option value="">All Projects</option>
          {visibleProjects.map((p) => <option key={p} value={p}>{p}</option>)}
        </select>
        <button
          className={`btn-toggle${showChangelog ? ' active' : ''}`}
          onClick={() => setShowChangelog((s) => !s)}
          title="Toggle changelog view"
        >
          {showChangelog ? 'Board' : 'Changelog'}
        </button>
      </div>

      {/* Edit modal */}
      {editCard && (
        <div className="kanban-modal-overlay" onClick={() => setEditCard(null)}>
          <div className="kanban-modal" onClick={(e) => e.stopPropagation()}>
            <h3>Edit Card</h3>
            <div className="kanban-edit-form">
              <input type="text" value={editCard.title} onChange={(e) => setEditCard({ ...editCard, title: e.target.value })} placeholder="Title" />
              <div className="kanban-autocomplete-wrapper">
                <textarea ref={editDescRef} value={editCard.description} onChange={editDescAC.handleChange} onKeyDown={editDescAC.handleKeyDown} placeholder="Description" rows={4} />
                {editDescAC.showDropdown && <TicketSuggestions suggestions={editDescAC.suggestions} selectedIdx={editDescAC.selectedIdx} onSelect={editDescAC.accept} />}
              </div>
              <div className="kanban-edit-row">
                <select value={editCard.cardType} onChange={(e) => setEditCard({ ...editCard, cardType: e.target.value })}>
                  {CARD_TYPES.map((t) => <option key={t} value={t}>{t || 'No type'}</option>)}
                </select>
                <select value={editCard.priority} onChange={(e) => setEditCard({ ...editCard, priority: e.target.value })}>
                  {PRIORITIES.map((p) => <option key={p} value={p}>{p || 'No priority'}</option>)}
                </select>
              </div>
              <input type="text" placeholder="Project" value={editCard.project} onChange={(e) => setEditCard({ ...editCard, project: e.target.value })} />
              <input type="text" placeholder="Pane target" value={editCard.paneTarget} onChange={(e) => setEditCard({ ...editCard, paneTarget: e.target.value })} />
              <input type="text" placeholder="Labels" value={editCard.labels} onChange={(e) => setEditCard({ ...editCard, labels: e.target.value })} />
              <input
                type="number"
                placeholder="Parent ticket # (0 = no parent)"
                value={editCard.parentId || ''}
                onChange={(e) => setEditCard({ ...editCard, parentId: parseInt(e.target.value, 10) || 0 })}
              />
              <div className="kanban-card-actions">
                <button className="btn-create" onClick={handleUpdate}>Save</button>
                <button className="btn-small" onClick={() => setEditCard(null)}>Cancel</button>
              </div>
            </div>
          </div>
        </div>
      )}

      {showChangelog && (
        <div className="kanban-changelog">
          <h3 className="kanban-changelog-title">Changelog {filterProject && <span className="kanban-changelog-project">— {filterProject}</span>}</h3>
          {projectLog.length === 0 && <p className="muted">No activity yet</p>}
          {projectLog.map((entry) => {
            const card = cardById.get(entry.cardId);
            return (
              <div key={entry.id} className="kanban-changelog-entry" onClick={() => card && setExpandedCard(card)}>
                <span className="kanban-log-date">{new Date(entry.createdAt).toLocaleString()}</span>
                {card ? (
                  <span className="kanban-changelog-card">#{card.id} {card.title}</span>
                ) : (
                  <span className="kanban-changelog-card muted">#{entry.cardId} (deleted)</span>
                )}
                <span className="kanban-log-action">{formatLogAction(entry)}</span>
                {entry.source !== 'user' && <span className="kanban-log-source">{entry.source}</span>}
              </div>
            );
          })}
        </div>
      )}

      {!showChangelog && <div className={`kanban-body${expandedCard && !editCard ? ' sidebar-open' : ''}`}>
      <div className="kanban-columns">
        {columns.map((col) => {
          const allowed = dragCard && dragCard.column !== col.id
            ? (transitions[dragCard.column] ?? []).includes(col.id)
            : true;
          return (
          <div
            key={col.id}
            className={`kanban-column${dropTarget?.column === col.id ? (allowed ? ' drop-target' : ' drop-target invalid-drop') : ''}${collapsedCols.has(col.id) ? ' collapsed' : ''}`}
            onDragOver={(e) => { e.preventDefault(); setDropTarget({ column: col.id, position: columnCards(col.id).length }); }}
            onDragLeave={() => setDropTarget(null)}
            onDrop={() => handleDrop(col.id, dropTarget?.column === col.id ? dropTarget.position : 0)}
          >
            <div className="kanban-column-header" onClick={() => setCollapsedCols((prev) => {
              const next = new Set(prev);
              next.has(col.id) ? next.delete(col.id) : next.add(col.id);
              return next;
            })}>
              <span className="kanban-col-toggle">{collapsedCols.has(col.id) ? '▶' : '▼'}</span>
              <span>{col.label}</span>
              <span className="kanban-count">{columnCards(col.id).length}</span>
              <button className="btn-small" onClick={(e) => { e.stopPropagation(); setNewCardCol(col.id); }}>+</button>
            </div>

            {!collapsedCols.has(col.id) && newCardCol === col.id && (
              <div className="kanban-card kanban-card-new">
                <input type="text" placeholder="Title" value={newTitle} onChange={(e) => setNewTitle(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && handleCreate()} autoFocus />
                <div className="kanban-autocomplete-wrapper">
                  <textarea ref={newDescRef} placeholder="Description" value={newDesc} onChange={newDescAC.handleChange} onKeyDown={newDescAC.handleKeyDown} rows={2} />
                  {newDescAC.showDropdown && <TicketSuggestions suggestions={newDescAC.suggestions} selectedIdx={newDescAC.selectedIdx} onSelect={newDescAC.accept} />}
                </div>
                <div className="kanban-edit-row">
                  <select value={newType} onChange={(e) => setNewType(e.target.value)}>
                    {CARD_TYPES.map((t) => <option key={t} value={t}>{t || 'Type'}</option>)}
                  </select>
                  <select value={newPriority} onChange={(e) => setNewPriority(e.target.value)}>
                    {PRIORITIES.map((p) => <option key={p} value={p}>{p || 'Priority'}</option>)}
                  </select>
                </div>
                <input type="text" placeholder="Project" value={newProject} onChange={(e) => setNewProject(e.target.value)} />
                <input type="text" placeholder="Pane target" value={newPaneTarget} onChange={(e) => setNewPaneTarget(e.target.value)} />
                <input type="text" placeholder="Labels" value={newLabels} onChange={(e) => setNewLabels(e.target.value)} />
                <div className="kanban-card-actions">
                  <button className="btn-create" onClick={handleCreate}>Add</button>
                  <button className="btn-small" onClick={() => setNewCardCol(null)}>Cancel</button>
                </div>
              </div>
            )}

            {!collapsedCols.has(col.id) && columnCards(col.id).map((card, idx) => {
              const kids = childrenOf(card.id);
              const completion = completionFor(card.id);
              const isCollapsed = collapsedParents.has(card.id);
              return (
              <div key={card.id}>
                {/* Drop indicator before this card */}
                {dropTarget?.column === col.id && dropTarget.position === idx && dragCard?.id !== card.id && (
                  <div className="kanban-drop-indicator" />
                )}
                <div
                  className={`kanban-card${dragCard?.id === card.id ? ' dragging' : ''}${kids.length ? ' has-children' : ''}`}
                  draggable
                  onDragStart={() => { setDragCard(card); wasDragging.current = true; }}
                  onDragEnd={() => { setDragCard(null); setDropTarget(null); setTimeout(() => { wasDragging.current = false; }, 100); }}
                  onDragOver={(e) => handleCardDragOver(e, col.id, idx)}
                  onMouseUp={() => {
                    if (!wasDragging.current) {
                      setExpandedCard(card);
                    }
                  }}
                  onMouseDown={() => { wasDragging.current = false; }}
                >
                <div className="kanban-card-top">
                  {kids.length > 0 && (
                    <button
                      className="kanban-tree-toggle"
                      onMouseDown={(e) => { e.stopPropagation(); }}
                      onMouseUp={(e) => { e.stopPropagation(); }}
                      onClick={(e) => {
                        e.stopPropagation();
                        setCollapsedParents((prev) => {
                          const next = new Set(prev);
                          if (next.has(card.id)) next.delete(card.id);
                          else next.add(card.id);
                          return next;
                        });
                      }}
                      title={isCollapsed ? 'Expand subtasks' : 'Collapse subtasks'}
                    >
                      {isCollapsed ? '▶' : '▼'}
                    </button>
                  )}
                  {card.cardType && (
                    <span className="kanban-type-dot" style={{ background: typeColors[card.cardType] || 'var(--text-dim)' }} title={card.cardType} />
                  )}
                  <span className="kanban-card-id">#{card.id}</span>
                  <span className="kanban-card-title">{card.title}</span>
                  {completion && (
                    <span className="kanban-card-completion" title={`${completion.done} of ${completion.total} subtasks done`}>
                      {completion.done}/{completion.total}
                    </span>
                  )}
                  {(() => { const cl = card.description ? parseChecklist(card.description) : null; return cl ? (
                    <span className="kanban-card-checklist" title={`${cl.checked} of ${cl.total} items checked`}>
                      {cl.checked === cl.total ? '\u2611' : '\u2610'} {cl.checked}/{cl.total}
                    </span>
                  ) : null; })()}
                  {card.priority && (
                    <span className="kanban-priority-sm" style={{ color: priorityColors[card.priority] }}>{card.priority}</span>
                  )}
                </div>
                {card.description && <div className="kanban-card-desc">{card.description.slice(0, 80)}{card.description.length > 80 ? '...' : ''}</div>}
                <div className="kanban-card-meta">
                  {card.project && <span className="kanban-label project">{card.project}</span>}
                  {card.labels && card.labels.split(',').slice(0, 3).map((l) => (
                    <span key={l.trim()} className="kanban-label">{l.trim()}</span>
                  ))}
                </div>
              </div>
              {/* Render children nested */}
              {!isCollapsed && kids.map((child) => (
                <div
                  key={child.id}
                  className="kanban-card kanban-card-child"
                  onMouseUp={() => { if (!wasDragging.current) setExpandedCard(child); }}
                >
                  <div className="kanban-card-top">
                    <span className="kanban-card-tree-line">└</span>
                    {child.cardType && (
                      <span className="kanban-type-dot" style={{ background: typeColors[child.cardType] || 'var(--text-dim)' }} title={child.cardType} />
                    )}
                    <span className="kanban-card-id">#{child.id}</span>
                    <span className="kanban-card-title">{child.title}</span>
                    {child.column === 'done' && <span className="kanban-card-check">✓</span>}
                    {child.priority && (
                      <span className="kanban-priority-sm" style={{ color: priorityColors[child.priority] }}>{child.priority}</span>
                    )}
                  </div>
                </div>
              ))}
              </div>
              );
            })}
            {/* Drop indicator at end of column */}
            {!collapsedCols.has(col.id) && dropTarget?.column === col.id && dropTarget.position === columnCards(col.id).length && dragCard && (
              <div className="kanban-drop-indicator" />
            )}
          </div>
          );
        })}
      </div>
      {expandedCard && !editCard && (
        <CardDetailSidebar
          card={expandedCard}
          notes={notes}
          messages={messages}
          cardLog={cardLog}
          allCards={cards}
          ticketAutocomplete={ticketAutocomplete}
          onClose={() => setExpandedCard(null)}
          onAddNote={async (text) => {
            try {
              await post(`/cards/${expandedCard.id}/notes`, { text });
              const updated = await get<CardNote[]>(`/cards/${expandedCard.id}/notes`);
              setNotes(updated ?? []);
            } catch {}
          }}
          onAddMessage={async (author, text) => {
            try {
              await post(`/cards/${expandedCard.id}/messages`, { author, text });
              const updated = await get<CardMessage[]>(`/cards/${expandedCard.id}/messages`);
              setMessages(updated ?? []);
            } catch {}
          }}
          onEdit={() => setEditCard(expandedCard)}
          onDelete={() => handleDelete(expandedCard.id)}
          onNavigateToPane={(target) => { onNavigateToPane(target); }}
          onCheckboxToggle={async (card, checkIndex) => {
            const newDesc = toggleCheckbox(card.description, checkIndex);
            const updated = { ...card, description: newDesc };
            setExpandedCard(updated);
            setCards((prev) => prev.map((c) => c.id === updated.id ? { ...c, description: newDesc } : c));
            await fetch(`/api/v1/cards/${card.id}`, {
              method: 'PUT', headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify(updated),
            });
          }}
        />
      )}
      </div>}
    </div>
  );
}
