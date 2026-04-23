import { useCallback, useRef } from 'react';

interface Props {
  /** 'horizontal' = drag left/right (between side-by-side panels), 'vertical' = drag up/down */
  direction?: 'horizontal' | 'vertical';
  /** Called with the delta in pixels while dragging */
  onResize: (delta: number) => void;
  /** Called when drag ends */
  onResizeEnd?: () => void;
}

export function ResizeHandle({ direction = 'horizontal', onResize, onResizeEnd }: Props) {
  const startPos = useRef(0);
  const dragging = useRef(false);

  const handleMouseDown = useCallback((e: React.MouseEvent) => {
    e.preventDefault();
    dragging.current = true;
    startPos.current = direction === 'horizontal' ? e.clientX : e.clientY;

    // Coalesce rapid mousemove events into one onResize call per frame.
    // Without throttling, mousemove fires 60-120Hz and each call triggers a
    // full App re-render + ResizeObserver cascade on every PaneViewer cell.
    let pendingDelta = 0;
    let rafId: number | null = null;
    const flush = () => {
      rafId = null;
      if (pendingDelta === 0) return;
      const delta = pendingDelta;
      pendingDelta = 0;
      onResize(delta);
    };

    const handleMouseMove = (e: MouseEvent) => {
      if (!dragging.current) return;
      const current = direction === 'horizontal' ? e.clientX : e.clientY;
      pendingDelta += current - startPos.current;
      startPos.current = current;
      if (rafId === null) {
        rafId = requestAnimationFrame(flush);
      }
    };

    const handleMouseUp = () => {
      dragging.current = false;
      if (rafId !== null) {
        cancelAnimationFrame(rafId);
        rafId = null;
      }
      if (pendingDelta !== 0) {
        const delta = pendingDelta;
        pendingDelta = 0;
        onResize(delta);
      }
      document.removeEventListener('mousemove', handleMouseMove);
      document.removeEventListener('mouseup', handleMouseUp);
      document.body.style.cursor = '';
      document.body.style.userSelect = '';
      onResizeEnd?.();
    };

    document.addEventListener('mousemove', handleMouseMove);
    document.addEventListener('mouseup', handleMouseUp);
    document.body.style.cursor = direction === 'horizontal' ? 'col-resize' : 'row-resize';
    document.body.style.userSelect = 'none';
  }, [direction, onResize, onResizeEnd]);

  return (
    <div
      className={`resize-handle resize-${direction}`}
      onMouseDown={handleMouseDown}
    />
  );
}
