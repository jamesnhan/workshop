// Global hover pin state — shared across all hover components.
// When pinned, onMouseLeave should not dismiss hover previews.

let pinned = false;
const listeners = new Set<() => void>();

export function isHoverPinned(): boolean {
  return pinned;
}

export function setHoverPinned(value: boolean): void {
  pinned = value;
  listeners.forEach((fn) => fn());
}

export function onHoverPinChange(fn: () => void): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}
