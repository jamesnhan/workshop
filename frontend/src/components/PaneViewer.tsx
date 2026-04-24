import { useEffect, useRef, useImperativeHandle, forwardRef, useCallback } from 'react';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { SearchAddon } from '@xterm/addon-search';
import '@xterm/xterm/css/xterm.css';
import { counters, recordBreadcrumb } from '../lib/telemetry';

export interface PaneViewerHandle {
  write: (data: string) => void;
  focus: () => void;
  searchInTerminal: (text: string) => boolean;
  clearSearch: () => void;
  refit: () => void;
  forceResize: (force?: boolean) => void;
}

interface TerminalTheme {
  background: string;
  foreground: string;
  cursor: string;
  selectionBackground: string;
  black: string;
  red: string;
  green: string;
  yellow: string;
  blue: string;
  magenta: string;
  cyan: string;
  white: string;
  brightBlack: string;
  brightRed: string;
  brightGreen: string;
  brightYellow: string;
  brightBlue: string;
  brightMagenta: string;
  brightCyan: string;
  brightWhite: string;
}

interface Props {
  target: string;
  terminalTheme: TerminalTheme;
  onData: (data: string) => void;
  onResize: (cols: number, rows: number) => void;
  onTicketHover?: (cardId: number | null, x: number, y: number) => void;
  onTicketClick?: (cardId: number) => void;
  onUrlHover?: (url: string | null, x: number, y: number) => void;
  onCommitHover?: (sha: string | null, x: number, y: number) => void;
  onHashKey?: () => void;
}

export const PaneViewer = forwardRef<PaneViewerHandle, Props>(
  ({ target, terminalTheme, onData, onResize, onTicketHover, onTicketClick, onUrlHover, onCommitHover, onHashKey }, ref) => {
    const containerRef = useRef<HTMLDivElement>(null);
    const termRef = useRef<Terminal | null>(null);
    const searchRef = useRef<SearchAddon | null>(null);
    const fitRef = useRef<FitAddon | null>(null);
    const onDataRef = useRef(onData);
    const onResizeRef = useRef(onResize);
    const onTicketHoverRef = useRef(onTicketHover);
    const onTicketClickRef = useRef(onTicketClick);
    const onUrlHoverRef = useRef(onUrlHover);
    const onCommitHoverRef = useRef(onCommitHover);
    const onHashKeyRef = useRef(onHashKey);
    onHashKeyRef.current = onHashKey;
    const pendingWriteRef = useRef('');
    const writeFrameRef = useRef<number | null>(null);
    const lastSizeRef = useRef<{ cols: number; rows: number } | null>(null);
    onDataRef.current = onData;
    onResizeRef.current = onResize;
    onTicketHoverRef.current = onTicketHover;
    onTicketClickRef.current = onTicketClick;
    onUrlHoverRef.current = onUrlHover;
    onCommitHoverRef.current = onCommitHover;

    const notifyResizeIfChanged = () => {
      const term = termRef.current;
      if (!term) return;
      const next = { cols: term.cols, rows: term.rows };
      if (lastSizeRef.current?.cols === next.cols && lastSizeRef.current?.rows === next.rows) {
        return;
      }
      lastSizeRef.current = next;
      onResizeRef.current(next.cols, next.rows);
    };

    const scheduleWriteFlush = () => {
      if (writeFrameRef.current !== null) return;
      writeFrameRef.current = requestAnimationFrame(() => {
        writeFrameRef.current = null;
        const term = termRef.current;
        const pending = pendingWriteRef.current;
        pendingWriteRef.current = '';
        if (!term || !pending) return;

        const shouldStickToBottom = term.buffer.active.viewportY >= term.buffer.active.baseY;
        const writeStart = performance.now();
        const flushBytes = pending.length;
        counters.outputFlushes++;
        counters.outputFlushBytes += flushBytes;
        term.write(pending, () => {
          if (shouldStickToBottom) {
            term.scrollToBottom();
          }
          const dur = performance.now() - writeStart;
          if (dur > counters.maxOutputFlushMs) counters.maxOutputFlushMs = dur;
          // Breadcrumb only slow flushes (>=20ms) so the ring buffer isn't
          // filled with normal-rate output. Slow flushes are most likely
          // to correlate with a freeze.
          if (dur >= 20) {
            recordBreadcrumb('xterm.flush', { bytes: flushBytes }, Math.round(dur));
          }
          if (pendingWriteRef.current) {
            scheduleWriteFlush();
          }
        });
      });
    };

    const runFit = () => {
      const term = termRef.current;
      const fit = fitRef.current;
      const container = containerRef.current;
      if (!term || !fit || !container) return;
      if (container.offsetWidth === 0 || container.offsetHeight === 0) return;
      fit.fit();
      notifyResizeIfChanged();
    };

    useImperativeHandle(ref, () => ({
      write: (data: string) => {
        if (!data) return;
        pendingWriteRef.current += data;
        scheduleWriteFlush();
      },
      focus: () => {
        const term = termRef.current;
        if (!term) return;
        term.focus();
        term.scrollToBottom();
        // Notify server of this client's current size without re-fitting.
        // This ensures the smallest-wins logic stays up to date on focus.
        notifyResizeIfChanged();
      },
      searchInTerminal: (text: string) => {
        return searchRef.current?.findNext(text, { caseSensitive: false }) ?? false;
      },
      clearSearch: () => {
        searchRef.current?.clearDecorations();
      },
      refit: () => {
        requestAnimationFrame(() => {
          requestAnimationFrame(() => {
            runFit();
          });
        });
      },
      forceResize: (force = false) => {
        // Fit and notify the backend. By default, only emit a resize message
        // when the dims actually changed — repeat emissions of unchanged dims
        // caused the #934 amplification loop (tmux still repainted and the
        // repaint was broadcast to every client).
        // Pass force=true from reconnect/first-mount paths where we genuinely
        // want tmux to reflow even if the browser-side dims match cache.
        requestAnimationFrame(() => {
          requestAnimationFrame(() => {
            const term = termRef.current;
            const fit = fitRef.current;
            const container = containerRef.current;
            if (!term || !fit || !container) return;
            if (container.offsetWidth === 0 || container.offsetHeight === 0) return;
            fit.fit();
            if (force) {
              lastSizeRef.current = { cols: term.cols, rows: term.rows };
              onResizeRef.current(term.cols, term.rows);
            } else {
              notifyResizeIfChanged();
            }
          });
        });
      },
    }));

    // When the target prop changes (e.g. after a swap), clear the terminal
    // and refit. The new target's data will repopulate via the App's
    // onOutput → write() flow.
    useEffect(() => {
      const term = termRef.current;
      const fit = fitRef.current;
      if (!term || !fit) return;
      term.clear();
      term.reset();
      pendingWriteRef.current = '';
      lastSizeRef.current = null;
      requestAnimationFrame(() => {
        requestAnimationFrame(() => {
          runFit();
        });
      });
    }, [target]);

    useEffect(() => {
      if (!containerRef.current) return;

      const term = new Terminal({
        cursorBlink: true,
        fontSize: 14,
        scrollback: 10000,
        fontFamily: "'CaskaydiaCove Nerd Font Propo', 'JetBrains Mono', 'Fira Code', monospace",
        theme: {
          ...terminalTheme,
        },
      });

      const fit = new FitAddon();
      const search = new SearchAddon();
      term.loadAddon(fit);
      term.loadAddon(search);
      term.open(containerRef.current);
      searchRef.current = search;
      fitRef.current = fit;

      // Intercept bare # before xterm consumes it — opens ticket lookup dialog
      term.attachCustomKeyEventHandler((e) => {
        if (e.type === 'keydown' && e.key === '#' && !e.ctrlKey && !e.altKey && !e.metaKey && onHashKeyRef.current) {
          e.preventDefault();   // stop browser from typing # into any newly-focused input
          e.stopPropagation();
          onHashKeyRef.current();
          return false; // prevent xterm from sending # to PTY
        }
        return true;
      });

      // Register link provider for ticket references like #123
      const ticketLinkProvider = term.registerLinkProvider({
        provideLinks(bufferLineNumber, callback) {
          const line = term.buffer.active.getLine(bufferLineNumber - 1);
          if (!line) {
            callback(undefined);
            return;
          }
          const text = line.translateToString(true);
          type Link = {
            range: { start: { x: number; y: number }; end: { x: number; y: number } };
            text: string;
            activate: () => void;
            hover: (e: MouseEvent) => void;
            leave: () => void;
          };
          const matches: Link[] = [];
          const regex = /#(\d+)/g;
          let m: RegExpExecArray | null;
          while ((m = regex.exec(text)) !== null) {
            const id = parseInt(m[1], 10);
            if (id > 0) {
              const startCol = m.index + 1;
              const endCol = m.index + m[0].length;
              matches.push({
                range: {
                  start: { x: startCol, y: bufferLineNumber },
                  end: { x: endCol, y: bufferLineNumber },
                },
                text: m[0],
                activate: () => onTicketClickRef.current?.(id),
                hover: (e: MouseEvent) => onTicketHoverRef.current?.(id, e.clientX, e.clientY),
                leave: () => onTicketHoverRef.current?.(null, 0, 0),
              });
            }
          }
          callback(matches);
        },
      });

      // Register link provider for URLs
      const urlLinkProvider = term.registerLinkProvider({
        provideLinks(bufferLineNumber, callback) {
          const line = term.buffer.active.getLine(bufferLineNumber - 1);
          if (!line) { callback(undefined); return; }
          const text = line.translateToString(true);
          type Link = {
            range: { start: { x: number; y: number }; end: { x: number; y: number } };
            text: string;
            activate: () => void;
            hover: (e: MouseEvent) => void;
            leave: () => void;
          };
          const matches: Link[] = [];
          const urlRegex = /https?:\/\/[^\s<>"{}|\\^`[\]]+/g;
          let m: RegExpExecArray | null;
          while ((m = urlRegex.exec(text)) !== null) {
            const url = m[0];
            const startCol = m.index + 1;
            const endCol = m.index + url.length;
            matches.push({
              range: {
                start: { x: startCol, y: bufferLineNumber },
                end: { x: endCol, y: bufferLineNumber },
              },
              text: url,
              activate: () => window.open(url, '_blank', 'noopener,noreferrer'),
              hover: (e: MouseEvent) => onUrlHoverRef.current?.(url, e.clientX, e.clientY),
              leave: () => onUrlHoverRef.current?.(null, 0, 0),
            });
          }
          callback(matches);
        },
      });

      // Register link provider for git commit SHAs
      const commitLinkProvider = term.registerLinkProvider({
        provideLinks(bufferLineNumber, callback) {
          const line = term.buffer.active.getLine(bufferLineNumber - 1);
          if (!line) { callback(undefined); return; }
          const text = line.translateToString(true);
          type Link = {
            range: { start: { x: number; y: number }; end: { x: number; y: number } };
            text: string;
            activate: () => void;
            hover: (e: MouseEvent) => void;
            leave: () => void;
          };
          const matches: Link[] = [];
          const shaRegex = /\b[0-9a-f]{7,40}\b/g;
          let m: RegExpExecArray | null;
          while ((m = shaRegex.exec(text)) !== null) {
            const sha = m[0];
            // Skip if it looks like a URL fragment or ticket ref (already handled)
            const isTicketRef = text.charAt(m.index - 1) === '#';
            const isUrlFragment = /https?:/.test(text.slice(Math.max(0, m.index - 10), m.index));
            if (!isTicketRef && !isUrlFragment) {
              const startCol = m.index + 1;
              const endCol = m.index + sha.length;
              matches.push({
                range: {
                  start: { x: startCol, y: bufferLineNumber },
                  end: { x: endCol, y: bufferLineNumber },
                },
                text: sha,
                activate: () => {},
                hover: (e: MouseEvent) => onCommitHoverRef.current?.(sha, e.clientX, e.clientY),
                leave: () => onCommitHoverRef.current?.(null, 0, 0),
              });
            }
          }
          callback(matches);
        },
      });

      // Fit to cell size, then tell backend the dimensions
      // Double RAF for Mac rendering timing
      requestAnimationFrame(() => {
        requestAnimationFrame(() => {
          runFit();
        });
      });

      term.onData((data) => {
        onDataRef.current(data);
      });

      termRef.current = term;

      const intercept = (e: KeyboardEvent, data: string) => {
        e.preventDefault();
        e.stopPropagation();
        onDataRef.current(data);
      };

      const container = containerRef.current;

      const handleKeyDown = (e: KeyboardEvent) => {
        if (!container.contains(document.activeElement)) return;

        const isMac = navigator.platform.toUpperCase().indexOf('MAC') >= 0;

        if (e.key === 'Tab' && !e.ctrlKey && !e.altKey) {
          if (e.shiftKey) return intercept(e, '\x1b[Z');
          return intercept(e, '\x09');
        }
        // Ctrl+C for SIGINT (terminal convention, always Ctrl)
        if (e.ctrlKey && !e.shiftKey && !e.altKey && e.key === 'c') {
          return intercept(e, '\x03');
        }
        // Copy: Cmd+C on Mac, Ctrl+Shift+C on others
        if ((isMac && e.metaKey && !e.shiftKey && e.key === 'c') ||
            (!isMac && e.ctrlKey && e.shiftKey && e.key === 'C')) {
          if (term.hasSelection()) {
            navigator.clipboard.writeText(term.getSelection()).catch(() => {});
          }
          e.preventDefault();
          e.stopPropagation();
          return;
        }
        // Paste: Cmd+V on Mac, Ctrl+V on others
        if ((isMac && e.metaKey && (e.key === 'v' || e.key === 'V')) ||
            (!isMac && e.ctrlKey && (e.key === 'v' || e.key === 'V'))) {
          e.preventDefault();
          e.stopPropagation();
          navigator.clipboard.readText().then((text) => {
            if (text) onDataRef.current(text);
          }).catch(() => {});
          return;
        }
        if (e.altKey && !e.ctrlKey && e.key === 't') return intercept(e, '\x1bt');
        if (e.ctrlKey && e.key === 'Backspace') return intercept(e, '\x17');
        if (e.altKey && e.key === 'Backspace') return intercept(e, '\x1b\x7f');
        if (e.ctrlKey && e.key === 'ArrowLeft') return intercept(e, '\x1b[1;5D');
        if (e.ctrlKey && e.key === 'ArrowRight') return intercept(e, '\x1b[1;5C');
        if (e.altKey && e.key === 'ArrowLeft') return intercept(e, '\x1b[1;3D');
        if (e.altKey && e.key === 'ArrowRight') return intercept(e, '\x1b[1;3C');
      };

      const handlePaste = (e: Event) => {
        const ce = e as ClipboardEvent;
        ce.preventDefault();
        const text = ce.clipboardData?.getData('text');
        if (text) onDataRef.current(text);
      };

      // Re-fit on container resize and notify backend
      let resizeTimer: ReturnType<typeof setTimeout>;
      const observer = new ResizeObserver(() => {
        clearTimeout(resizeTimer);
        resizeTimer = setTimeout(() => {
          runFit();
        }, 150);
      });
      observer.observe(container);

      // Window resize listener (Mac Safari needs this)
      const handleWindowResize = () => {
        clearTimeout(resizeTimer);
        resizeTimer = setTimeout(() => {
          runFit();
        }, 150);
      };
      window.addEventListener('resize', handleWindowResize);

      // No visualViewport constraint needed — on mobile we skip fit.fit()
      // entirely and let the container scroll through the full terminal.

      // Touch scroll → mouse wheel escape sequences for tmux
      let touchStartY = 0;
      let touchAccum = 0;
      const TOUCH_SCROLL_THRESHOLD = 15; // pixels per scroll line

      const handleTouchStart = (e: TouchEvent) => {
        touchStartY = e.touches[0].clientY;
        touchAccum = 0;
      };

      const handleTouchMove = (e: TouchEvent) => {
        if (e.touches.length !== 1) return;
        const deltaY = touchStartY - e.touches[0].clientY;
        touchStartY = e.touches[0].clientY;
        touchAccum += deltaY;

        // Send scroll events in increments
        while (Math.abs(touchAccum) >= TOUCH_SCROLL_THRESHOLD) {
          // SGR mouse wheel: \x1b[<64;col;rowM (up) or \x1b[<65;col;rowM (down)
          const button = touchAccum > 0 ? 65 : 64;
          const seq = `\x1b[<${button};1;1M`;
          onDataRef.current(seq);
          touchAccum -= touchAccum > 0 ? TOUCH_SCROLL_THRESHOLD : -TOUCH_SCROLL_THRESHOLD;
        }

        e.preventDefault(); // prevent page scroll
      };

      container.addEventListener('touchstart', handleTouchStart, { passive: true });
      container.addEventListener('touchmove', handleTouchMove, { passive: false });

      document.addEventListener('keydown', handleKeyDown, true);
      container.addEventListener('paste', handlePaste);

      return () => {
        clearTimeout(resizeTimer);
        pendingWriteRef.current = '';
        if (writeFrameRef.current !== null) cancelAnimationFrame(writeFrameRef.current);
        observer.disconnect();
        ticketLinkProvider.dispose();
        urlLinkProvider.dispose();
        commitLinkProvider.dispose();
        window.removeEventListener('resize', handleWindowResize);
        container.removeEventListener('touchstart', handleTouchStart);
        container.removeEventListener('touchmove', handleTouchMove);
        document.removeEventListener('keydown', handleKeyDown, true);
        container.removeEventListener('paste', handlePaste);
        term.dispose();
        termRef.current = null;
        fitRef.current = null;
        searchRef.current = null;
        writeFrameRef.current = null;
        lastSizeRef.current = null;
      };
    }, [target]);

    const fileInputRef = useRef<HTMLInputElement>(null);

    const handleUpload = useCallback(async (file: File) => {
      const form = new FormData();
      form.append('file', file);
      try {
        // Only set auth header — do NOT set Content-Type, the browser
        // needs to set multipart/form-data with the boundary automatically.
        const headers: Record<string, string> = {};
        const token = localStorage.getItem('workshop:api-key');
        if (token) headers['Authorization'] = `Bearer ${token}`;
        const resp = await fetch('/api/v1/upload', { method: 'POST', headers, body: form });
        if (!resp.ok) {
          const err = await resp.json().catch(() => ({ error: resp.statusText }));
          alert(`Upload failed: ${err.error || resp.statusText}`);
          return;
        }
        const { path } = await resp.json();
        // Type the file path into the terminal so the agent can read it
        onDataRef.current(path + ' ');
      } catch (err) {
        alert(`Upload failed: ${err}`);
      }
    }, []);

    return (
      <div className="pane-viewer">
        <div className="pane-header">
          <span className="pane-target">{target}</span>
          <input
            ref={fileInputRef}
            type="file"
            accept="image/*"
            style={{ display: 'none' }}
            onChange={(e) => {
              const file = e.target.files?.[0];
              if (file) handleUpload(file);
              e.target.value = ''; // reset so same file can be picked again
            }}
          />
          <button
            className="btn-upload"
            title="Upload image to terminal"
            onClick={() => fileInputRef.current?.click()}
          >
            📷
          </button>
        </div>
        <div ref={containerRef} className="pane-terminal" />
      </div>
    );
  }
);
