import { useEffect, useRef, useState, useCallback } from 'react';
import { counters, post, recordBreadcrumb } from '../lib/telemetry';

interface WsMessage {
  type: string;
  data?: {
    target: string;
    data: string;
    status?: string;
    message?: string;
    path?: string;
    cardId?: number;
    id?: number | string;
    session?: string;
    background?: boolean;
    action?: string;
    payload?: Record<string, unknown>;
  };
}

export interface UICommand {
  id?: string;
  action: string;
  payload: Record<string, unknown>;
}

type OutputHandler = (target: string, data: string) => void;
type StatusHandler = (target: string, status: string, message: string) => void;
type StatusClearHandler = (target: string) => void;
type ReconnectHandler = () => void;
type OpenDocHandler = (path: string) => void;
type SessionCreatedHandler = (target: string, background: boolean) => void;
type UICommandHandler = (cmd: UICommand) => void;

export function useWorkshopSocket() {
  const [connected, setConnected] = useState(false);
  const wsRef = useRef<WebSocket | null>(null);
  const subsRef = useRef<Set<string>>(new Set());
  const closedRef = useRef(false);
  const onOutputRef = useRef<OutputHandler | null>(null);
  const onStatusRef = useRef<StatusHandler | null>(null);
  const onStatusClearRef = useRef<StatusClearHandler | null>(null);
  const onReconnectRef = useRef<ReconnectHandler | null>(null);
  const onOpenDocRef = useRef<OpenDocHandler | null>(null);
  const onSessionCreatedRef = useRef<SessionCreatedHandler | null>(null);
  const onUICommandRef = useRef<UICommandHandler | null>(null);
  const wasConnected = useRef(false);

  useEffect(() => {
    closedRef.current = false;
    let retryCount = 0;

    function connect() {
      if (closedRef.current) return;

      const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
      const token = localStorage.getItem('workshop:api-key') || '';
      const wsUrl = `${protocol}//${window.location.host}/ws${token ? `?token=${encodeURIComponent(token)}` : ''}`;
      console.log('[workshop] connecting to', wsUrl.replace(/token=.*/, 'token=***'));
      const ws = new WebSocket(wsUrl);

      ws.onopen = () => {
        console.log('[workshop] ws connected');
        retryCount = 0; // Reset backoff on successful connection
        const isReconnect = wasConnected.current;
        wasConnected.current = true;
        setConnected(true);
        post({ msg: 'frontend.ws.connect', reconnect: isReconnect });
        recordBreadcrumb('ws.connect', { reconnect: isReconnect });
        if (isReconnect) counters.wsReconnects++;
        // Notify App to clear terminals on reconnect (stale PTY state)
        if (isReconnect) {
          console.log('[workshop] reconnect — clearing terminal state');
          onReconnectRef.current?.();
        }
        for (const target of subsRef.current) {
          ws.send(JSON.stringify({ type: 'subscribe', data: { target } }));
        }
      };

      ws.onerror = (e) => {
        console.error('[workshop] ws error:', e);
        post({ msg: 'frontend.ws.error' });
        recordBreadcrumb('ws.error');
      };

      ws.onclose = (e) => {
        console.log('[workshop] ws closed:', e.code, e.reason);
        setConnected(false);
        post({ msg: 'frontend.ws.disconnect', code: e.code, reason: e.reason, was_clean: e.wasClean });
        recordBreadcrumb('ws.disconnect', { code: e.code, clean: e.wasClean });
        if (!closedRef.current) {
          // Exponential backoff with jitter: 1s, 2s, 4s, 8s... capped at 30s
          const base = Math.min(1000 * Math.pow(2, retryCount), 30000);
          const jitter = Math.random() * 1000;
          retryCount++;
          console.log(`[workshop] reconnecting in ${Math.round(base + jitter)}ms (attempt ${retryCount})`);
          setTimeout(connect, base + jitter);
        }
      };

      ws.onmessage = (e) => {
        counters.wsMessages++;
        if (typeof e.data === 'string') counters.wsBytes += e.data.length;
        const t0 = performance.now();
        let msgType = 'unknown';
        try {
          const msg: WsMessage = JSON.parse(e.data);
          msgType = msg.type ?? 'unknown';
          if (msg.type === 'output' && msg.data) {
            onOutputRef.current?.(msg.data.target, msg.data.data);
          } else if (msg.type === 'pane_status' && msg.data) {
            onStatusRef.current?.(msg.data.target, msg.data.status || '', msg.data.message || '');
          } else if (msg.type === 'pane_status_clear' && msg.data) {
            onStatusClearRef.current?.(msg.data.target);
          } else if (msg.type === 'open_doc' && msg.data?.path) {
            onOpenDocRef.current?.(msg.data.path);
          } else if (msg.type === 'session_created' && msg.data?.target) {
            onSessionCreatedRef.current?.(msg.data.target, msg.data.background ?? true);
          } else if (msg.type === 'activity' && msg.data) {
            window.dispatchEvent(new CustomEvent('workshop-ws', { detail: msg }));
          } else if (msg.type === 'approval_request' && msg.data) {
            window.dispatchEvent(new CustomEvent('workshop-ws', { detail: msg }));
          } else if (msg.type === 'ui_command' && msg.data?.action) {
            onUICommandRef.current?.({
              id: typeof msg.data.id === 'string' ? msg.data.id : undefined,
              action: msg.data.action,
              payload: (msg.data.payload as Record<string, unknown>) ?? {},
            });
          }
        } catch (err) {
          console.error('[workshop] bad ws message:', err);
        }
        // Breadcrumb non-output messages individually (they're rare and
        // informative — session_created, ui_command, etc.). Output messages
        // only breadcrumb if the handler took >10ms so we don't flood the
        // ring buffer during streaming bursts.
        const ms = Math.round(performance.now() - t0);
        if (msgType !== 'output' || ms >= 10) {
          recordBreadcrumb('ws.msg', { type: msgType }, ms);
        }
      };

      wsRef.current = ws;
    }

    connect();

    return () => {
      closedRef.current = true;
      wsRef.current?.close();
    };
  }, []);

  const subscribe = useCallback((target: string) => {
    console.log('[workshop] subscribing to', target);
    subsRef.current.add(target);
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify({ type: 'subscribe', data: { target } }));
    }
  }, []);

  const unsubscribe = useCallback((target: string) => {
    console.log('[workshop] unsubscribing from', target);
    subsRef.current.delete(target);
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify({ type: 'unsubscribe', data: { target } }));
    }
  }, []);

  const sendInput = useCallback((target: string, data: string) => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify({ type: 'input', data: { target, data } }));
    }
  }, []);

  const sendResize = useCallback((target: string, cols: number, rows: number) => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify({ type: 'resize', data: { target, cols, rows } }));
    }
  }, []);

  const onOutput = useCallback((handler: OutputHandler) => {
    onOutputRef.current = handler;
  }, []);

  const startRecording = useCallback((target: string, name: string, cols: number, rows: number) => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify({ type: 'record_start', data: { target, name, cols, rows } }));
    }
  }, []);

  const stopRecording = useCallback((target: string) => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify({ type: 'record_stop', data: { target } }));
    }
  }, []);

  const onStatus = useCallback((handler: StatusHandler) => {
    onStatusRef.current = handler;
  }, []);

  const onStatusClear = useCallback((handler: StatusClearHandler) => {
    onStatusClearRef.current = handler;
  }, []);

  const onReconnect = useCallback((handler: ReconnectHandler) => {
    onReconnectRef.current = handler;
  }, []);

  const onOpenDoc = useCallback((handler: OpenDocHandler) => {
    onOpenDocRef.current = handler;
  }, []);

  const onSessionCreated = useCallback((handler: SessionCreatedHandler) => {
    onSessionCreatedRef.current = handler;
  }, []);

  const onUICommand = useCallback((handler: UICommandHandler) => {
    onUICommandRef.current = handler;
  }, []);

  return { connected, subscribe, unsubscribe, sendInput, sendResize, startRecording, stopRecording, onOutput, onStatus, onStatusClear, onReconnect, onOpenDoc, onSessionCreated, onUICommand };
}
