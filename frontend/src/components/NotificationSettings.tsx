import { useState } from 'react';
import { type CustomPattern, loadCustomPatterns, saveCustomPatterns, type Notification } from '../hooks/useNotifications';

interface Props {
  onClose: () => void;
}

let nextPatternId = Date.now();

const TYPES: Notification['type'][] = ['error', 'complete', 'input-needed', 'info'];

export function NotificationSettings({ onClose }: Props) {
  const [patterns, setPatterns] = useState<CustomPattern[]>(loadCustomPatterns);
  const [newRegex, setNewRegex] = useState('');
  const [newMessage, setNewMessage] = useState('');
  const [newType, setNewType] = useState<Notification['type']>('error');
  const [regexError, setRegexError] = useState('');

  const save = (updated: CustomPattern[]) => {
    setPatterns(updated);
    saveCustomPatterns(updated);
  };

  const handleAdd = () => {
    if (!newRegex.trim()) return;
    // Validate regex
    try {
      new RegExp(newRegex);
      setRegexError('');
    } catch (e) {
      setRegexError('Invalid regex: ' + (e as Error).message);
      return;
    }

    const pattern: CustomPattern = {
      id: `cp-${nextPatternId++}`,
      regex: newRegex.trim(),
      type: newType,
      message: newMessage.trim() || `Matched: ${newRegex.trim()}`,
      enabled: true,
    };
    save([...patterns, pattern]);
    setNewRegex('');
    setNewMessage('');
  };

  const handleToggle = (id: string) => {
    save(patterns.map((p) => p.id === id ? { ...p, enabled: !p.enabled } : p));
  };

  const handleDelete = (id: string) => {
    save(patterns.filter((p) => p.id !== id));
  };

  return (
    <div className="switcher-overlay" onClick={onClose}>
      <div className="notif-settings" onClick={(e) => e.stopPropagation()}>
        <div className="notif-settings-header">
          <span>Notification Patterns</span>
          <button className="search-close" onClick={onClose}>x</button>
        </div>

        <div className="notif-settings-body">
          <div className="notif-settings-section">
            <h4>Add Custom Pattern</h4>
            <div className="notif-add-form">
              <input
                type="text"
                placeholder="Regex pattern (e.g. error|FAIL|panic)"
                value={newRegex}
                onChange={(e) => setNewRegex(e.target.value)}
                className="switcher-input"
              />
              <input
                type="text"
                placeholder="Notification message (optional)"
                value={newMessage}
                onChange={(e) => setNewMessage(e.target.value)}
                className="switcher-input"
              />
              <div className="notif-add-row">
                <select
                  value={newType}
                  onChange={(e) => setNewType(e.target.value as Notification['type'])}
                  className="theme-select"
                >
                  {TYPES.map((t) => <option key={t} value={t}>{t}</option>)}
                </select>
                <button className="btn-create" onClick={handleAdd}>Add Pattern</button>
              </div>
              {regexError && <div className="notif-regex-error">{regexError}</div>}
            </div>
          </div>

          <div className="notif-settings-section">
            <h4>Custom Patterns ({patterns.length})</h4>
            {patterns.length === 0 && (
              <p className="muted">No custom patterns. Add one above.</p>
            )}
            {patterns.map((p) => (
              <div key={p.id} className={`notif-pattern-row${p.enabled ? '' : ' disabled'}`}>
                <input
                  type="checkbox"
                  checked={p.enabled}
                  onChange={() => handleToggle(p.id)}
                />
                <code className="notif-pattern-regex">{p.regex}</code>
                <span className="notif-pattern-type">{p.type}</span>
                <span className="notif-pattern-msg">{p.message}</span>
                <button className="notif-dismiss" onClick={() => handleDelete(p.id)}>x</button>
              </div>
            ))}
          </div>

          <div className="notif-settings-section">
            <h4>Built-in Patterns</h4>
            <p className="muted" style={{ fontSize: '0.75rem' }}>These are always active and cannot be modified.</p>
            <div className="notif-builtin-list">
              <div className="notif-pattern-row">
                <span className="notif-pattern-type">complete</span>
                <code className="notif-pattern-regex">Worked/Baked/Sautéed/... for Ns</code>
              </div>
              <div className="notif-pattern-row">
                <span className="notif-pattern-type">input</span>
                <code className="notif-pattern-regex">Do you want to proceed?</code>
              </div>
              <div className="notif-pattern-row">
                <span className="notif-pattern-type">input</span>
                <code className="notif-pattern-regex">Esc to cancel · Tab to amend</code>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
