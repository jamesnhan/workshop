import { useEffect, useRef } from 'react';
import type { LayoutState } from '../types';
import { counters, resetCounters, getSessionId, post, readStaleBreadcrumbs, recordBreadcrumb } from '../lib/telemetry';

// Watchdog writes a timestamp to localStorage every TICK_MS. If the main
// thread freezes, the interval stops firing. On next page load we detect
// a stale prior tick and POST a `watchdog.freeze_detected` event so the
// freeze shows up in Loki. Also emits periodic heartbeats with rich
// context (memory, long tasks, WS volume, event-loop lag) for freeze
// correlation analysis.
const TICK_MS = 1000;
const HEARTBEAT_EVERY_N_TICKS = 30; // emit heartbeat every 30s
const STALE_THRESHOLD_MS = 5000; // treat > 5s old as evidence of a freeze
const WAKE_GAP_MS = 5000; // tick-to-tick gap > this means the tab was paused
const LONG_TASK_EVENT_MS = 500; // individual long tasks >= this get their own event
const STORAGE_KEY = 'workshop:watchdog';

interface WatchdogState {
  ts: number;
  sessionId: string;
  tickCount: number;
  cellCount: number;
  focusedCellId: string;
  focusedTarget: string | null;
  connected: boolean;
}

function memoryUsageMb(): number | undefined {
  const mem = (performance as unknown as { memory?: { usedJSHeapSize: number } }).memory;
  if (!mem) return undefined;
  return Math.round(mem.usedJSHeapSize / 1024 / 1024);
}

export function useLockupWatchdog(layout: LayoutState, connected: boolean): void {
  const layoutRef = useRef(layout);
  const connectedRef = useRef(connected);

  useEffect(() => { layoutRef.current = layout; }, [layout]);
  useEffect(() => { connectedRef.current = connected; }, [connected]);

  useEffect(() => {
    const sessionId = getSessionId();

    // --- Freeze detection on load ---
    // Read breadcrumbs BEFORE any new breadcrumb is recorded (the first
    // recordBreadcrumb call overwrites localStorage). These are the "last
    // things that ran" in the prior session, straight from its localStorage.
    const staleBreadcrumbs = readStaleBreadcrumbs();
    try {
      const raw = localStorage.getItem(STORAGE_KEY);
      if (raw) {
        const prev = JSON.parse(raw) as WatchdogState;
        const age = Date.now() - prev.ts;
        if (age > STALE_THRESHOLD_MS && prev.sessionId !== sessionId) {
          post({
            msg: 'watchdog.freeze_detected',
            staleness_ms: age,
            prior_session_id: prev.sessionId,
            prior_tick_count: prev.tickCount,
            prior_cell_count: prev.cellCount,
            prior_focused_cell_id: prev.focusedCellId,
            prior_focused_target: prev.focusedTarget,
            prior_connected: prev.connected,
            prior_ts: prev.ts,
            detected_at: Date.now(),
            breadcrumbs: staleBreadcrumbs,
          });
        }
      }
    } catch {}
    recordBreadcrumb('session.start');

    // --- Long-task observer ---
    // PerformanceObserver fires for any task > 50ms that blocked the main
    // thread. Count them all (rolled up in heartbeat); emit an individual
    // event for any task over LONG_TASK_EVENT_MS so we can correlate with
    // other signals.
    let longTaskObserver: PerformanceObserver | null = null;
    try {
      longTaskObserver = new PerformanceObserver((list) => {
        for (const entry of list.getEntries()) {
          counters.longTasks++;
          if (entry.duration > counters.maxLongTaskMs) {
            counters.maxLongTaskMs = entry.duration;
          }
          if (entry.duration >= LONG_TASK_EVENT_MS) {
            post({
              msg: 'frontend.long_task',
              duration_ms: Math.round(entry.duration),
              start_time_ms: Math.round(entry.startTime),
            });
            recordBreadcrumb('long_task', undefined, Math.round(entry.duration));
          }
        }
      });
      longTaskObserver.observe({ type: 'longtask', buffered: true });
    } catch {}

    // --- Visibility + page lifecycle ---
    let lastVisibility = typeof document !== 'undefined' ? document.visibilityState : 'visible';
    const onVisibilityChange = () => {
      const next = document.visibilityState;
      if (next !== lastVisibility) {
        post({
          msg: 'frontend.visibility_change',
          from: lastVisibility,
          to: next,
          connected: connectedRef.current,
        });
        recordBreadcrumb('visibility', { from: lastVisibility, to: next });
        lastVisibility = next;
      }
    };
    document.addEventListener('visibilitychange', onVisibilityChange);

    const onPageFreeze = () => { post({ msg: 'frontend.page_freeze' }); recordBreadcrumb('page_freeze'); };
    const onPageResume = () => { post({ msg: 'frontend.page_resume' }); recordBreadcrumb('page_resume'); };
    const onPageShow = (e: PageTransitionEvent) => { post({ msg: 'frontend.pageshow', persisted: e.persisted }); recordBreadcrumb('pageshow', { persisted: e.persisted }); };
    const onPageHide = (e: PageTransitionEvent) => { post({ msg: 'frontend.pagehide', persisted: e.persisted }); recordBreadcrumb('pagehide', { persisted: e.persisted }); };
    document.addEventListener('freeze', onPageFreeze);
    document.addEventListener('resume', onPageResume);
    window.addEventListener('pageshow', onPageShow);
    window.addEventListener('pagehide', onPageHide);

    // --- Event loop lag sentinel ---
    // Periodically schedule a zero-delay timer and measure actual delay.
    // Inactive background tabs throttle setTimeout, so this reveals both
    // real main-thread saturation AND browser throttling.
    let lagTimer: ReturnType<typeof setInterval> | null = null;
    lagTimer = setInterval(() => {
      const t0 = performance.now();
      setTimeout(() => {
        const lag = performance.now() - t0;
        if (lag > counters.maxEventLoopLagMs) counters.maxEventLoopLagMs = lag;
      }, 0);
    }, 2000);

    // --- rAF health check ---
    // Distinguishes a true main-thread stall (no timers, no rAF) from a
    // render-only stall (timers fire, rAF is stuck — browser compositor
    // or paint path hung). If rAF hasn't fired in over 5s while timers
    // still tick, we emit a `frontend.raf_stall` beacon and attempt a
    // recovery kick: force reflow + cancel the pending rAF + reschedule.
    // Observed on Brave/Chromium: the tab can stay visible but have rAF
    // paused for minutes. Manual tab-switch unfreezes it; the recovery
    // kick here is an attempt to automate that.
    let lastRafTs = performance.now();
    let rafPending = false;
    let pendingRafHandle: number | null = null;
    const scheduleRaf = () => {
      rafPending = true;
      pendingRafHandle = requestAnimationFrame(() => {
        pendingRafHandle = null;
        rafPending = false;
        lastRafTs = performance.now();
        scheduleRaf();
      });
    };
    scheduleRaf();

    // Force an immediate rAF recovery. Used both by the setInterval stall
    // detector and by the focus/pointerdown handlers so waking the tab
    // doesn't have to wait for the next 2s poll.
    const kickRaf = (source: string) => {
      if (pendingRafHandle !== null) {
        cancelAnimationFrame(pendingRafHandle);
        pendingRafHandle = null;
        rafPending = false;
      }
      // Force synchronous layout — reading offsetHeight flushes any
      // pending style/layout work and can trip the compositor back on.
      void document.body.offsetHeight;
      scheduleRaf();
      recordBreadcrumb('raf_kick', { source });
    };

    // Wake-from-idle: when the system was idle and the user interacts,
    // kick the compositor immediately instead of waiting for the stall
    // detector. Idempotent in the healthy case (just queues one extra
    // rAF that gets cancelled on the next normal tick).
    const onWindowFocus = () => {
      if (performance.now() - lastRafTs > 2000) kickRaf('focus');
    };
    const onPointerDown = () => {
      if (performance.now() - lastRafTs > 2000) kickRaf('pointerdown');
    };
    window.addEventListener('focus', onWindowFocus);
    window.addEventListener('pointerdown', onPointerDown, { capture: true, passive: true });
    const rafWatchdog = setInterval(() => {
      const gap = performance.now() - lastRafTs;
      // Ignore when tab is hidden (rAF is legitimately paused).
      if (gap > 5000 && document.visibilityState === 'visible') {
        post({
          msg: 'frontend.raf_stall',
          gap_ms: Math.round(gap),
          raf_pending: rafPending,
          visibility_state: document.visibilityState,
        });
        recordBreadcrumb('raf_stall', { gap_ms: Math.round(gap), pending: rafPending });
        kickRaf('watchdog');
        lastRafTs = performance.now(); // reset so we don't spam
      }
    }, 2000);

    // --- Tick loop ---
    let tickCount = 0;
    let lastTickTs = Date.now();
    const tick = () => {
      tickCount++;
      const now = Date.now();
      const tickGap = now - lastTickTs;
      lastTickTs = now;

      // Detect wake-from-long-pause (sleep, heavy throttling, or a main-thread
      // stall that barely missed the freeze threshold).
      if (tickGap > WAKE_GAP_MS) {
        post({
          msg: 'frontend.wake_detected',
          gap_ms: tickGap,
          visibility_state: document.visibilityState,
          connected: connectedRef.current,
          tick_count: tickCount,
        });
        recordBreadcrumb('wake', { gap_ms: tickGap, visibility: document.visibilityState });
      }

      const l = layoutRef.current;
      const focusedTarget = l.cells.find((c) => c.id === l.focusedId)?.target ?? null;
      const state: WatchdogState = {
        ts: now,
        sessionId,
        tickCount,
        cellCount: l.cells.length,
        focusedCellId: l.focusedId,
        focusedTarget,
        connected: connectedRef.current,
      };
      try { localStorage.setItem(STORAGE_KEY, JSON.stringify(state)); } catch {}

      // Record a lightweight tick breadcrumb every 10s so the ring keeps
      // producing entries during idle periods. Without this, a quiet tab
      // stops generating breadcrumbs and the post-freeze trail is stale
      // by the time the main thread actually hangs.
      if (tickCount % 10 === 0) {
        recordBreadcrumb('watchdog.tick', {
          tick: tickCount,
          visibility: document.visibilityState,
          connected: connectedRef.current,
        });
      }

      if (tickCount % HEARTBEAT_EVERY_N_TICKS === 0) {
        recordBreadcrumb('watchdog.heartbeat.pre', { tick: tickCount });
        post({
          msg: 'watchdog.heartbeat',
          tick_count: state.tickCount,
          cell_count: state.cellCount,
          focused_cell_id: state.focusedCellId,
          focused_target: state.focusedTarget,
          connected: state.connected,
          visibility_state: document.visibilityState,
          memory_mb: memoryUsageMb(),
          tick_gap_ms: tickGap,
          long_tasks_since_beat: counters.longTasks,
          max_long_task_ms: Math.round(counters.maxLongTaskMs),
          max_event_loop_lag_ms: Math.round(counters.maxEventLoopLagMs),
          ws_messages_since_beat: counters.wsMessages,
          ws_bytes_since_beat: counters.wsBytes,
          ws_reconnects_since_beat: counters.wsReconnects,
          output_flushes_since_beat: counters.outputFlushes,
          output_flush_bytes_since_beat: counters.outputFlushBytes,
          max_output_flush_ms: Math.round(counters.maxOutputFlushMs),
        });
        recordBreadcrumb('watchdog.heartbeat.post', { tick: tickCount });
        resetCounters();
      }
    };

    post({
      msg: 'watchdog.session_start',
      user_agent: typeof navigator !== 'undefined' ? navigator.userAgent : '',
      initial_visibility: document.visibilityState,
    });

    const interval = setInterval(tick, TICK_MS);
    return () => {
      clearInterval(interval);
      if (lagTimer) clearInterval(lagTimer);
      clearInterval(rafWatchdog);
      longTaskObserver?.disconnect();
      document.removeEventListener('visibilitychange', onVisibilityChange);
      document.removeEventListener('freeze', onPageFreeze);
      document.removeEventListener('resume', onPageResume);
      window.removeEventListener('pageshow', onPageShow);
      window.removeEventListener('pagehide', onPageHide);
      window.removeEventListener('focus', onWindowFocus);
      window.removeEventListener('pointerdown', onPointerDown, { capture: true } as EventListenerOptions);
    };
  }, []);
}
