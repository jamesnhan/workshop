import { useState, useCallback } from 'react';

const SETTINGS_KEY = 'workshop:settings';

export type PreviewSize = 'small' | 'medium' | 'large';

export interface WorkshopSettings {
  capsLockNormalization: boolean;
  previewSize: PreviewSize;
  ticketAutocomplete: boolean;
  terminalHashKey: boolean;
  sfwMode: boolean;
  nsfwProjects: string[];
}

const defaults: WorkshopSettings = {
  capsLockNormalization: true,
  previewSize: 'medium',
  ticketAutocomplete: true,
  terminalHashKey: true,
  sfwMode: false,
  nsfwProjects: ['goonwall', 'fansly', 'lewd-lens'],
};

function load(): WorkshopSettings {
  try {
    const raw = localStorage.getItem(SETTINGS_KEY);
    const base = raw ? { ...defaults, ...JSON.parse(raw) } : { ...defaults };
    // Migrate legacy preview size if settings don't have it yet
    if (!raw) {
      const legacy = localStorage.getItem('workshop-preview-size');
      if (legacy === 'small' || legacy === 'medium' || legacy === 'large') {
        base.previewSize = legacy;
      }
    }
    return base;
  } catch {
    return { ...defaults };
  }
}

function save(settings: WorkshopSettings) {
  localStorage.setItem(SETTINGS_KEY, JSON.stringify(settings));
  // Sync preview size to legacy key for Sidebar compatibility
  localStorage.setItem('workshop-preview-size', settings.previewSize);
}

export function useSettings() {
  const [settings, setSettingsState] = useState<WorkshopSettings>(load);

  const updateSettings = useCallback((patch: Partial<WorkshopSettings>) => {
    setSettingsState((prev) => {
      const next = { ...prev, ...patch };
      save(next);
      return next;
    });
  }, []);

  return { settings, updateSettings };
}
