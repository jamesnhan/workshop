// Vitest setup: runs before every test file.
//
// Adds jest-dom matchers, stubs browser APIs that jsdom doesn't implement
// out of the box, and resets localStorage between tests.

import '@testing-library/jest-dom/vitest';
import { afterEach, beforeEach } from 'vitest';
import { cleanup } from '@testing-library/react';

// jsdom doesn't implement these — react-flow and other libs touch them.
if (!window.ResizeObserver) {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  (window as any).ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
}

if (!window.DOMRect) {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  (window as any).DOMRect = class {
    x = 0; y = 0; width = 0; height = 0;
    top = 0; right = 0; bottom = 0; left = 0;
    toJSON() { return this; }
  };
}

// Silence the "Not implemented: window.scrollTo" jsdom warning.
if (typeof window.scrollTo !== 'function') {
  window.scrollTo = () => {};
}

beforeEach(() => {
  localStorage.clear();
  sessionStorage.clear();
});

afterEach(() => {
  cleanup();
});
