import { useState, useRef, useEffect, useCallback } from 'react';
import {
  listWorkspaces,
  saveWorkspace,
  loadWorkspace,
} from '../hooks/useLayoutPersistence';

interface Props {
  activeWorkspace: string | null;
  dirty?: boolean;
  onLoad: (name: string) => void;
  onSave: (name: string) => void;
  onDelete: (name: string) => void;
  onRename: (oldName: string, newName: string) => void;
}

interface WorkspaceInfo {
  name: string;
  gridRows: number;
  gridCols: number;
  paneCount: number;
}

function getWorkspaceInfo(name: string): WorkspaceInfo | null {
  try {
    const raw = localStorage.getItem(`workshop:workspace:${name}`);
    if (!raw) return null;
    const saved = JSON.parse(raw);
    const paneCount = (saved.cells || []).filter((c: { target: string | null }) => c.target).length;
    return { name, gridRows: saved.gridRows, gridCols: saved.gridCols, paneCount };
  } catch {
    return null;
  }
}

export function WorkspaceManager({ activeWorkspace, dirty, onLoad, onSave, onDelete, onRename }: Props) {
  const [open, setOpen] = useState(false);
  const [editingName, setEditingName] = useState<string | null>(null);
  const [renameValue, setRenameValue] = useState('');
  const [newName, setNewName] = useState('');
  const popoverRef = useRef<HTMLDivElement>(null);
  const renameRef = useRef<HTMLInputElement>(null);
  const newRef = useRef<HTMLInputElement>(null);

  const workspaces = open ? listWorkspaces().map((n) => getWorkspaceInfo(n)).filter(Boolean) as WorkspaceInfo[] : [];

  // Close on outside click
  useEffect(() => {
    if (!open) return;
    const handler = (e: MouseEvent) => {
      if (popoverRef.current && !popoverRef.current.contains(e.target as Node)) {
        setOpen(false);
        setEditingName(null);
      }
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, [open]);

  // Focus rename input
  useEffect(() => {
    if (editingName && renameRef.current) renameRef.current.focus();
  }, [editingName]);

  const handleRename = useCallback((oldName: string) => {
    const trimmed = renameValue.trim();
    if (!trimmed || trimmed === oldName) {
      setEditingName(null);
      return;
    }
    onRename(oldName, trimmed);
    setEditingName(null);
  }, [renameValue, onRename]);

  const handleSaveNew = useCallback(() => {
    const trimmed = newName.trim();
    if (!trimmed) return;
    onSave(trimmed);
    setNewName('');
  }, [newName, onSave]);

  const handleDuplicate = useCallback((name: string) => {
    const ws = loadWorkspace(name);
    if (!ws) return;
    const dupName = `${name} (copy)`;
    saveWorkspace(dupName, ws);
    // Force re-render by toggling
    setOpen(false);
    setTimeout(() => setOpen(true), 0);
  }, []);

  return (
    <div className="workspace-manager" ref={popoverRef}>
      <button
        className={`workspace-trigger ${activeWorkspace ? 'has-workspace' : ''}${dirty ? ' dirty' : ''}`}
        onClick={() => setOpen(!open)}
        title={dirty ? `${activeWorkspace} — unsaved changes` : 'Manage workspaces'}
      >
        {activeWorkspace || 'No workspace'}{dirty && <span className="workspace-dirty-dot" title="Unsaved changes">●</span>}
        <span className="workspace-trigger-arrow">{open ? '▴' : '▾'}</span>
      </button>

      {open && (
        <div className="workspace-popover">
          <div className="workspace-popover-header">
            Workspaces
            {dirty && activeWorkspace && (
              <button
                className="workspace-save-current-btn"
                onClick={() => { onSave(activeWorkspace); }}
                title={`Save changes to ${activeWorkspace}`}
              >
                Save "{activeWorkspace}"
              </button>
            )}
          </div>

          {workspaces.length === 0 && (
            <div className="workspace-empty">No saved workspaces yet</div>
          )}

          <div className="workspace-list">
            {workspaces.map((ws) => (
              <div
                key={ws.name}
                className={`workspace-item ${ws.name === activeWorkspace ? 'active' : ''}`}
              >
                {editingName === ws.name ? (
                  <input
                    ref={renameRef}
                    className="workspace-rename-input"
                    value={renameValue}
                    onChange={(e) => setRenameValue(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') handleRename(ws.name);
                      if (e.key === 'Escape') setEditingName(null);
                    }}
                    onBlur={() => handleRename(ws.name)}
                  />
                ) : (
                  <span
                    className="workspace-item-name"
                    onClick={() => { onLoad(ws.name); setOpen(false); }}
                  >
                    {ws.name}
                    {ws.name === activeWorkspace && <span className="workspace-active-dot" />}
                  </span>
                )}
                <span className="workspace-item-meta">
                  {ws.gridRows}×{ws.gridCols} · {ws.paneCount}p
                </span>
                <div className="workspace-item-actions">
                  <button
                    className="workspace-action-btn"
                    title="Rename"
                    onClick={(e) => {
                      e.stopPropagation();
                      setEditingName(ws.name);
                      setRenameValue(ws.name);
                    }}
                  >
                    ✎
                  </button>
                  <button
                    className="workspace-action-btn"
                    title="Duplicate"
                    onClick={(e) => {
                      e.stopPropagation();
                      handleDuplicate(ws.name);
                    }}
                  >
                    ⧉
                  </button>
                  <button
                    className="workspace-action-btn workspace-action-delete"
                    title="Delete"
                    onClick={(e) => {
                      e.stopPropagation();
                      onDelete(ws.name);
                    }}
                  >
                    ✕
                  </button>
                </div>
              </div>
            ))}
          </div>

          <div className="workspace-save-row">
            <input
              ref={newRef}
              className="workspace-save-input"
              placeholder="Save current as..."
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
              onKeyDown={(e) => { if (e.key === 'Enter') handleSaveNew(); }}
            />
            <button
              className="workspace-save-btn"
              onClick={handleSaveNew}
              disabled={!newName.trim()}
            >
              Save
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
