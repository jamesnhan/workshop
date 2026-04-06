import type { WorkshopSettings } from '../hooks/useSettings';

interface Props {
  settings: WorkshopSettings;
  onUpdate: (patch: Partial<WorkshopSettings>) => void;
}

export function SettingsView({ settings, onUpdate }: Props) {
  return (
    <div className="settings-view">
      <div className="settings-header">Settings</div>

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
    </div>
  );
}
