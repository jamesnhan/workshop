// Frontend telemetry — shared session id, aggregate counters, and a beacon
// helper for posting events to the backend's /api/v1/debug/log endpoint
// (which logs via slog → OTel → Loki).
//
// All hooks that emit telemetry use `post(...)` to ship events. Counters
// are shared mutable state rolled up by useLockupWatchdog into heartbeat
// events at a fixed cadence, so we get both an individual-event log and an
// aggregate time series.

import { authHeaders } from '../api/client';

let sessionIdCache: string | null = null;

export function getSessionId(): string {
  if (!sessionIdCache) {
    sessionIdCache = (typeof crypto !== 'undefined' && 'randomUUID' in crypto)
      ? crypto.randomUUID()
      : `${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
  }
  return sessionIdCache;
}

// Mutable counters rolled up by the watchdog heartbeat.
export const counters = {
  wsMessages: 0,
  wsBytes: 0,
  wsReconnects: 0,
  longTasks: 0,
  maxLongTaskMs: 0,
  maxEventLoopLagMs: 0,
  outputFlushes: 0,
  outputFlushBytes: 0,
  maxOutputFlushMs: 0,
};

export function resetCounters(): void {
  counters.wsMessages = 0;
  counters.wsBytes = 0;
  counters.wsReconnects = 0;
  counters.longTasks = 0;
  counters.maxLongTaskMs = 0;
  counters.maxEventLoopLagMs = 0;
  counters.outputFlushes = 0;
  counters.outputFlushBytes = 0;
  counters.maxOutputFlushMs = 0;
}

export function post(body: Record<string, unknown>): void {
  try {
    const url = '/api/v1/debug/log';
    const payload = JSON.stringify({ session_id: getSessionId(), ...body });
    if (typeof navigator !== 'undefined' && navigator.sendBeacon) {
      const blob = new Blob([payload], { type: 'application/json' });
      if (navigator.sendBeacon(url, blob)) return;
    }
    fetch(url, {
      method: 'POST',
      headers: authHeaders(),
      body: payload,
      keepalive: true,
    }).catch(() => {});
  } catch {}
}

// --- Breadcrumbs ---
//
// A bounded ring buffer of the most recent "things that ran". Each time any
// instrumented event fires (WS message, xterm flush, etc.) we push a
// breadcrumb and persist the buffer to localStorage synchronously. If the
// main thread freezes after some specific handler, that handler is the
// last entry in the buffer — the next page load reads it from localStorage
// and ships it in the `watchdog.freeze_detected` event.
//
// Synchronous persistence is important: a frozen thread can't run any
// async callbacks (beacons, fetch retries, microtasks), so the evidence
// has to already be on disk when the freeze hits.
//
// Size is kept small (~50 entries × ~100 bytes = ~5KB) to avoid bloating
// localStorage writes. The ring rolls over on overflow.

export interface Breadcrumb {
  ts: number;
  name: string;
  meta?: Record<string, string | number | boolean>;
  ms?: number;
}

const MAX_BREADCRUMBS = 50;
const BREADCRUMBS_KEY = 'workshop:watchdog-breadcrumbs';
const breadcrumbs: Breadcrumb[] = [];

export function recordBreadcrumb(
  name: string,
  meta?: Breadcrumb['meta'],
  ms?: number,
): void {
  const crumb: Breadcrumb = { ts: Date.now(), name };
  if (meta !== undefined) crumb.meta = meta;
  if (ms !== undefined) crumb.ms = ms;
  breadcrumbs.push(crumb);
  if (breadcrumbs.length > MAX_BREADCRUMBS) breadcrumbs.shift();
  try { localStorage.setItem(BREADCRUMBS_KEY, JSON.stringify(breadcrumbs)); } catch {}
}

// Wrap a synchronous function with timing + breadcrumb. Always records,
// with meta and ms filled in. Errors are re-thrown but still breadcrumbed.
export function timeBreadcrumb<T>(
  name: string,
  fn: () => T,
  meta?: Breadcrumb['meta'],
): T {
  const t0 = performance.now();
  let threw = false;
  try {
    return fn();
  } catch (e) {
    threw = true;
    throw e;
  } finally {
    const ms = Math.round(performance.now() - t0);
    recordBreadcrumb(name, threw ? { ...meta, threw: true } : meta, ms);
  }
}

// Read the prior session's breadcrumbs. Call this on mount BEFORE any new
// breadcrumbs are recorded (which would overwrite the stored array).
export function readStaleBreadcrumbs(): Breadcrumb[] {
  try {
    const raw = localStorage.getItem(BREADCRUMBS_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    return Array.isArray(parsed) ? parsed : [];
  } catch {
    return [];
  }
}
