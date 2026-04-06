import { useEffect, useRef, useState, useCallback } from 'react';

interface WsMessage {
  type: string;
  data?: { target: string; data: string; status?: string; message?: string };
}

type OutputHandler = (target: string, data: string) => void;
type StatusHandler = (target: string, status: string, message: string) => void;
type StatusClearHandler = (target: string) => void;
type ReconnectHandler = () => void;

export function useWorkshopSocket() {
  const [connected, setConnected] = useState(false);
  const wsRef = useRef<WebSocket | null>(null);
  const subsRef = useRef<Set<string>>(new Set());
  const closedRef = useRef(false);
  const onOutputRef = useRef<OutputHandler | null>(null);
  const onStatusRef = useRef<StatusHandler | null>(null);
  const onStatusClearRef = useRef<StatusClearHandler | null>(null);
  const onReconnectRef = useRef<ReconnectHandler | null>(null);
  const wasConnected = useRef(false);

  useEffect(() => {
    closedRef.current = false;

    function connect() {
      if (closedRef.current) return;

      const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
      const wsUrl = `${protocol}//${window.location.host}/ws`;
      console.log('[workshop] connecting to', wsUrl);
      const ws = new WebSocket(wsUrl);

      ws.onopen = () => {
        console.log('[workshop] ws connected');
        const isReconnect = wasConnected.current;
        wasConnected.current = true;
        setConnected(true);
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
      };

      ws.onclose = (e) => {
        console.log('[workshop] ws closed:', e.code, e.reason);
        setConnected(false);
        if (!closedRef.current) {
          setTimeout(connect, 2000);
        }
      };

      ws.onmessage = (e) => {
        try {
          const msg: WsMessage = JSON.parse(e.data);
          if (msg.type === 'output' && msg.data) {
            onOutputRef.current?.(msg.data.target, msg.data.data);
          } else if (msg.type === 'pane_status' && msg.data) {
            onStatusRef.current?.(msg.data.target, msg.data.status || '', msg.data.message || '');
          } else if (msg.type === 'pane_status_clear' && msg.data) {
            onStatusClearRef.current?.(msg.data.target);
          }
        } catch (err) {
          console.error('[workshop] bad ws message:', err);
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

  return { connected, subscribe, unsubscribe, sendInput, sendResize, startRecording, stopRecording, onOutput, onStatus, onStatusClear, onReconnect };
}
