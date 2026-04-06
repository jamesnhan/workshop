import type { WorkshopSettings, PreviewSize } from '../hooks/useSettings';
import { themes } from '../themes';

interface Props {
  settings: WorkshopSettings;
  onUpdate: (patch: Partial<WorkshopSettings>) => void;
  themeName: string;
  onThemeChange: (name: string) => void;
  notificationPermission: string;
  onRequestNotifications: () => void;
}

export function SettingsView({ settings, onUpdate, themeName, onThemeChange, notificationPermission, onRequestNotifications }: Props) {
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
