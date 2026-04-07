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

    const handleMouseMove = (e: MouseEvent) => {
      if (!dragging.current) return;
      const current = direction === 'horizontal' ? e.clientX : e.clientY;
      const delta = current - startPos.current;
      startPos.current = current;
      onResize(delta);
    };

    const handleMouseUp = () => {
      dragging.current = false;
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
