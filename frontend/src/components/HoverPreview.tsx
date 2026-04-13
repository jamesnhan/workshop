import type { CSSProperties, ReactNode } from 'react';

interface Props {
  x: number;
  y: number;
  width?: number;
  maxHeight?: number;
  offset?: number;
  className?: string;
  style?: CSSProperties;
  children: ReactNode;
}

/**
 * Generic positioned hover card. Clamps to the viewport so it never goes
 * off-screen, and is non-interactive (pointer-events: none) — consumers
 * that need clickable content should override className + pointer-events.
 *
 * Shared by TicketHoverPreview, GitInfoHoverPreview, and future preview
 * surfaces (URL preview, git commit hash preview, etc).
 */
export function HoverPreview({
  x,
  y,
  width = 340,
  maxHeight = 280,
  offset = 20,
  className,
  style,
  children,
}: Props) {
  const top = Math.min(y + offset, Math.max(0, window.innerHeight - maxHeight));
  const left = Math.min(x, Math.max(0, window.innerWidth - width));
  return (
    <div
      className={`hover-preview${className ? ' ' + className : ''}`}
      style={{ top, left, width, ...style }}
    >
      {children}
    </div>
  );
}
