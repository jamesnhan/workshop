import { useEffect, useState } from 'react';
import type { WorkshopSettings, PreviewSize } from '../hooks/useSettings';
import { themes } from '../themes';
import { get } from '../api/client';

interface Props {
  settings: WorkshopSettings;
  onUpdate: (patch: Partial<WorkshopSettings>) => void;
  themeName: string;
  onThemeChange: (name: string) => void;
  notificationPermission: string;
  onRequestNotifications: () => void;
}

export function SettingsView({ settings, onUpdate, themeName, onThemeChange, notificationPermission, onRequestNotifications }: Props) {
  // Channel delivery mode lives on the backend (per-server-instance state),
  // not in localStorage — fetch it once and provide a setter that PUTs.
  const [channelMode, setChannelMode] = useState<string>('auto');
  useEffect(() => {
    get<{ mode: string }>('/channel-mode').then((r) => { if (r?.mode) setChannelMode(r.mode); }).catch(() => {});
  }, []);
  const updateChannelMode = (mode: string) => {
    setChannelMode(mode);
    fetch('/api/v1/channel-mode', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ mode }),
    }).catch(() => {});
  };
  return (
    <div className="settings-view">
      <div className="settings-header">Settings</div>

      <div className="settings-section">
        <div className="settings-section-title">Appearance</div>

        <div className="settings-field">
          <span className="settings-field-label">Theme</span>
          <select
            className="settings-select"
            value={themeName}
            onChange={(e) => onThemeChange(e.target.value)}
          >
            {Object.entries(themes).map(([key, t]) => (
              <option key={key} value={key}>{t.name}</option>
            ))}
          </select>
        </div>

        <div className="settings-field">
          <span className="settings-field-label">Sidebar preview size</span>
          <select
            className="settings-select"
            value={settings.previewSize}
            onChange={(e) => onUpdate({ previewSize: e.target.value as PreviewSize })}
          >
            <option value="small">Small</option>
            <option value="medium">Medium</option>
            <option value="large">Large</option>
          </select>
        </div>
      </div>

      <div className="settings-section">
        <div className="settings-section-title">Keyboard</div>

        <label className="settings-toggle">
          <input
            type="checkbox"
            checked={settings.capsLockNormalization}
            onChange={(e) => onUpdate({ capsLockNormalization: e.target.checked })}
          />
          <div className="settings-toggle-info">
            <span className="settings-toggle-label">CapsLock normalization</span>
            <span className="settings-toggle-desc">
              Normalize hotkeys so they work regardless of CapsLock state. When enabled, Alt+h works even with CapsLock on.
            </span>
          </div>
        </label>

        <label className="settings-toggle">
          <input
            type="checkbox"
            checked={settings.terminalHashKey}
            onChange={(e) => onUpdate({ terminalHashKey: e.target.checked })}
          />
          <div className="settings-toggle-info">
            <span className="settings-toggle-label"># ticket lookup in terminal</span>
            <span className="settings-toggle-desc">
              Press # in a focused terminal to open the ticket lookup dialog. The selected ticket reference is typed directly into the PTY.
            </span>
          </div>
        </label>
      </div>

      <div className="settings-section">
        <div className="settings-section-title">Ticket Autocomplete</div>

        <label className="settings-toggle">
          <input
            type="checkbox"
            checked={settings.ticketAutocomplete}
            onChange={(e) => onUpdate({ ticketAutocomplete: e.target.checked })}
          />
          <div className="settings-toggle-info">
            <span className="settings-toggle-label"># autocomplete in text inputs</span>
            <span className="settings-toggle-desc">
              Show a ticket dropdown when typing # in kanban notes, card descriptions, and agent prompts.
            </span>
          </div>
        </label>
      </div>

      <div className="settings-section">
        <div className="settings-section-title">Channels (inter-agent messaging)</div>

        <div className="settings-field">
          <span className="settings-field-label">Delivery mode</span>
          <select
            className="settings-select"
            value={channelMode}
            onChange={(e) => updateChannelMode(e.target.value)}
          >
            <option value="auto">Auto (native if available, fall back to compat)</option>
            <option value="compat">Compat (always type into input via send_text)</option>
            <option value="native">Native (claude/channel notifications only)</option>
          </select>
        </div>
        <div className="settings-toggle-desc" style={{ marginTop: '0.4rem', color: 'var(--text-muted)', fontSize: '0.85rem' }}>
          Native mode requires Claude Code to be launched with <code>--dangerously-load-development-channels server:workshop</code> until the Workshop channel is approved by Anthropic. In Auto mode, panes that haven't registered a native listener fall back to the compat path automatically.
        </div>
      </div>

      <div className="settings-section">
        <div className="settings-section-title">SFW Mode</div>

        <label className="settings-toggle">
          <input
            type="checkbox"
            checked={settings.sfwMode}
            onChange={(e) => onUpdate({ sfwMode: e.target.checked })}
          />
          <div className="settings-toggle-info">
            <span className="settings-toggle-label">Hide NSFW projects</span>
            <span className="settings-toggle-desc">
              Hide marked projects from the kanban board, project dropdowns, and card lists. Great for screenshots and demos.
            </span>
          </div>
        </label>

        <div className="settings-field" style={{ marginTop: '0.5rem' }}>
          <span className="settings-field-label">Hidden projects</span>
          <span className="settings-toggle-desc" style={{ marginBottom: '0.4rem' }}>
            Comma-separated list of project names to hide when SFW mode is on.
          </span>
          <input
            type="text"
            className="settings-select"
            value={settings.nsfwProjects.join(', ')}
            onChange={(e) => onUpdate({ nsfwProjects: e.target.value.split(',').map((s) => s.trim()).filter(Boolean) })}
            placeholder="goonwall, fansly, lewd-lens"
          />
        </div>
      </div>

      <div className="settings-section">
        <div className="settings-section-title">Notifications</div>

        <div className="settings-field">
          <span className="settings-field-label">Browser notifications</span>
          <div className="settings-field-row">
            <span className={`settings-badge ${notificationPermission === 'granted' ? 'badge-ok' : notificationPermission === 'denied' ? 'badge-err' : ''}`}>
              {notificationPermission}
            </span>
            {notificationPermission !== 'granted' && notificationPermission !== 'denied' && (
              <button className="settings-btn" onClick={onRequestNotifications}>
                Enable
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
