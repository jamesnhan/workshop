import { useEffect, useRef, useImperativeHandle, forwardRef } from 'react';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { SearchAddon } from '@xterm/addon-search';
import '@xterm/xterm/css/xterm.css';

export interface PaneViewerHandle {
  write: (data: string) => void;
  focus: () => void;
  searchInTerminal: (text: string) => boolean;
  clearSearch: () => void;
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
}

export const PaneViewer = forwardRef<PaneViewerHandle, Props>(
  ({ target, terminalTheme, onData, onResize }, ref) => {
    const containerRef = useRef<HTMLDivElement>(null);
    const termRef = useRef<Terminal | null>(null);
    const searchRef = useRef<SearchAddon | null>(null);
    const onDataRef = useRef(onData);
    const onResizeRef = useRef(onResize);
    onDataRef.current = onData;
    onResizeRef.current = onResize;

    useImperativeHandle(ref, () => ({
      write: (data: string) => {
        termRef.current?.write(data);
      },
      focus: () => {
        termRef.current?.focus();
      },
      searchInTerminal: (text: string) => {
        return searchRef.current?.findNext(text, { caseSensitive: false }) ?? false;
      },
      clearSearch: () => {
        searchRef.current?.clearDecorations();
      },
    }));

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

      // Fit to cell size, then tell backend the dimensions
      requestAnimationFrame(() => {
        fit.fit();
        onResizeRef.current(term.cols, term.rows);
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

        if (e.key === 'Tab' && !e.ctrlKey && !e.altKey) {
          if (e.shiftKey) return intercept(e, '\x1b[Z');
          return intercept(e, '\x09');
        }
        if (e.ctrlKey && !e.shiftKey && !e.altKey && e.key === 'c') {
          return intercept(e, '\x03');
        }
        if (e.ctrlKey && e.shiftKey && e.key === 'C') {
          if (term.hasSelection()) {
            navigator.clipboard.writeText(term.getSelection()).catch(() => {});
          }
          e.preventDefault();
          e.stopPropagation();
          return;
        }
        if (e.ctrlKey && (e.key === 'v' || e.key === 'V')) {
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
          fit.fit();
          onResizeRef.current(term.cols, term.rows);
        }, 150);
      });
      observer.observe(container);

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
        observer.disconnect();
        container.removeEventListener('touchstart', handleTouchStart);
        container.removeEventListener('touchmove', handleTouchMove);
        document.removeEventListener('keydown', handleKeyDown, true);
        container.removeEventListener('paste', handlePaste);
        term.dispose();
        termRef.current = null;
        searchRef.current = null;
      };
    }, [target]);

    return (
      <div className="pane-viewer">
        <div className="pane-header">
          <span className="pane-target">{target}</span>
        </div>
        <div ref={containerRef} className="pane-terminal" />
      </div>
    );
  }
);
