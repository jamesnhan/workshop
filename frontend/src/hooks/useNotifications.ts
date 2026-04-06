import { useRef, useState, useCallback } from 'react';

export interface Notification {
  id: string;
  target: string;
  type: 'input-needed' | 'error' | 'complete' | 'info';
  message: string;
  timestamp: number;
  read: boolean;
}

export interface CustomPattern {
  id: string;
  regex: string;
  type: Notification['type'];
  message: string;
  enabled: boolean;
}

const CUSTOM_PATTERNS_KEY = 'workshop:notifPatterns';

export function loadCustomPatterns(): CustomPattern[] {
  try {
    return JSON.parse(localStorage.getItem(CUSTOM_PATTERNS_KEY) || '[]');
  } catch { return []; }
}

export function saveCustomPatterns(patterns: CustomPattern[]) {
  localStorage.setItem(CUSTOM_PATTERNS_KEY, JSON.stringify(patterns));
}

// Built-in patterns
const builtinPatterns: { type: Notification['type']; regex: RegExp; message: string }[] = [
  { type: 'complete', regex: /Worked for \d+[ms]/, message: 'Task completed' },
  { type: 'complete', regex: /Baked for \d+[ms]/, message: 'Task completed' },
  { type: 'complete', regex: /Saut.ed for \d+[ms]/, message: 'Task completed' },
  { type: 'complete', regex: /Cogitated for \d+[ms]/, message: 'Task completed' },
  { type: 'complete', regex: /Pollinated for \d+[ms]/, message: 'Task completed' },
  { type: 'complete', regex: /Cooled for \d+[ms]/, message: 'Task completed' },
  { type: 'complete', regex: /Charred for \d+[ms]/, message: 'Task completed' },
  { type: 'input-needed', regex: /Do you want to proceed\?/, message: 'Needs permission approval' },
  { type: 'input-needed', regex: /Esc to cancel.*Tab to amend/, message: 'Needs permission approval' },
];

// Simple ANSI stripping for pattern matching
function stripAnsi(s: string): string {
  return s.replace(/\x1b\[[0-9;]*[a-zA-Z]/g, '').replace(/\x1b\][^\x07]*\x07/g, '');
}

let nextId = 1;

type CompiledPattern = { type: Notification['type']; regex: RegExp; message: string };

function compileCustomPatterns(): CompiledPattern[] {
  return loadCustomPatterns()
    .filter((p) => p.enabled)
    .map((p) => {
      try { return { type: p.type, regex: new RegExp(p.regex), message: p.message }; }
      catch { return null; }
    })
    .filter((p): p is CompiledPattern => p !== null);
}

export function useNotifications() {
  const [notifications, setNotifications] = useState<Notification[]>([]);
  const lastNotifyTime = useRef<Record<string, number>>({});
  const permissionGranted = useRef(false);
  // Cache compiled custom patterns — refreshed when settings change, not on every chunk
  const customPatternsRef = useRef<CompiledPattern[]>(compileCustomPatterns());
  const customPatternsVersion = useRef(localStorage.getItem(CUSTOM_PATTERNS_KEY) || '[]');

  const [permissionState, setPermissionState] = useState<'default' | 'granted' | 'denied' | 'unsupported'>(
    'Notification' in window ? Notification.permission as any : 'unsupported'
  );

  // Request browser notification permission (must be called from user gesture on mobile)
  const requestPermission = useCallback(async () => {
    if (!('Notification' in window)) {
      setPermissionState('unsupported');
      return;
    }
    if (Notification.permission === 'granted') {
      permissionGranted.current = true;
      setPermissionState('granted');
      return;
    }
    if (Notification.permission === 'denied') {
      setPermissionState('denied');
      return;
    }
    try {
      const result = await Notification.requestPermission();
      permissionGranted.current = result === 'granted';
      setPermissionState(result as any);
    } catch {
      setPermissionState('denied');
    }
  }, []);

  // Track when each pane was first subscribed — ignore output for 5s after
  const subscribeTime = useRef<Record<string, number>>({});

  const markSubscribed = useCallback((target: string) => {
    subscribeTime.current[target] = Date.now();
  }, []);

  // Scan output for notification patterns
  const scanOutput = useCallback((target: string, data: string, focusedTarget: string | null) => {
    // Don't notify for the currently focused pane
    if (target === focusedTarget) return;

    const now = Date.now();

    // Skip notifications for 5s after subscribing (initial screen content)
    const subTime = subscribeTime.current[target];
    if (subTime && now - subTime < 5000) return;

    const clean = stripAnsi(data);

    // Refresh cached custom patterns only if localStorage changed
    const currentRaw = localStorage.getItem(CUSTOM_PATTERNS_KEY) || '[]';
    if (currentRaw !== customPatternsVersion.current) {
      customPatternsVersion.current = currentRaw;
      customPatternsRef.current = compileCustomPatterns();
    }
    const allPatterns = [...builtinPatterns, ...customPatternsRef.current];

    for (const pattern of allPatterns) {
      if (pattern.regex.test(clean)) {
        // Debounce: don't spam the same type for the same target within 30s
        const key = `${target.replace(/[:.]/g, '-')}-${pattern.type}`;
        if (lastNotifyTime.current[key] && now - lastNotifyTime.current[key] < 30000) {
          continue;
        }
        lastNotifyTime.current[key] = now;

        const notif: Notification = {
          id: `n-${nextId++}`,
          target,
          type: pattern.type,
          message: pattern.message,
          timestamp: now,
          read: false,
        };

        setNotifications((prev) => [notif, ...prev].slice(0, 30));

        // Browser notification
        if ('Notification' in window && Notification.permission === 'granted') {
          try {
            new window.Notification(`Workshop: ${pattern.message}`, {
              body: `Pane ${target}`,
              tag: `workshop-${pattern.type}`,
              silent: true,
            });
          } catch {
            // notification API can throw
          }
        }

        break; // Only one notification per output chunk
      }
    }
  }, []);

  const markRead = useCallback((id: string) => {
    setNotifications((prev) =>
      prev.map((n) => n.id === id ? { ...n, read: true } : n)
    );
  }, []);

  const markAllRead = useCallback(() => {
    setNotifications((prev) => prev.map((n) => ({ ...n, read: true })));
  }, []);

  const dismiss = useCallback((id: string) => {
    setNotifications((prev) => prev.filter((n) => n.id !== id));
  }, []);

  const clearAll = useCallback(() => {
    setNotifications([]);
  }, []);

  const unreadCount = notifications.filter((n) => !n.read).length;

  return {
    notifications,
    unreadCount,
    scanOutput,
    markSubscribed,
    markRead,
    markAllRead,
    dismiss,
    clearAll,
    requestPermission,
    permissionState,
  };
}
