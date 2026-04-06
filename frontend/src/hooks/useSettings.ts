import { useState, useCallback } from 'react';

const SETTINGS_KEY = 'workshop:settings';

export interface WorkshopSettings {
  capsLockNormalization: boolean;
  // Future settings go here
}

const defaults: WorkshopSettings = {
  capsLockNormalization: true,
};

function load(): WorkshopSettings {
  try {
    const raw = localStorage.getItem(SETTINGS_KEY);
    if (!raw) return { ...defaults };
    return { ...defaults, ...JSON.parse(raw) };
  } catch {
    return { ...defaults };
  }
}

function save(settings: WorkshopSettings) {
  localStorage.setItem(SETTINGS_KEY, JSON.stringify(settings));
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
