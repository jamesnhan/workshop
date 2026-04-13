import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, act, fireEvent } from '@testing-library/react';
import { ToastContainer, type ToastItem } from './Toast';

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

describe('ToastContainer', () => {
  it('renders every toast in the list', () => {
    const toasts: ToastItem[] = [
      { id: 1, message: 'first', kind: 'info' },
      { id: 2, message: 'second', kind: 'success' },
    ];
    render(<ToastContainer toasts={toasts} onDismiss={() => {}} />);

    expect(screen.getByText('first')).toBeInTheDocument();
    expect(screen.getByText('second')).toBeInTheDocument();
  });

  it('auto-dismisses after 4 seconds', () => {
    const onDismiss = vi.fn();
    const toasts: ToastItem[] = [{ id: 42, message: 'bye', kind: 'info' }];
    render(<ToastContainer toasts={toasts} onDismiss={onDismiss} />);

    expect(onDismiss).not.toHaveBeenCalled();
    act(() => {
      vi.advanceTimersByTime(4000);
    });
    expect(onDismiss).toHaveBeenCalledWith(42);
  });

  it('click dismisses immediately', () => {
    const onDismiss = vi.fn();
    const toasts: ToastItem[] = [{ id: 7, message: 'click me', kind: 'warning' }];
    render(<ToastContainer toasts={toasts} onDismiss={onDismiss} />);

    // Plain fireEvent — userEvent.click doesn't compose well with fake timers.
    fireEvent.click(screen.getByText('click me'));
    expect(onDismiss).toHaveBeenCalledWith(7);
  });

  it('applies kind-specific class', () => {
    const toasts: ToastItem[] = [{ id: 1, message: 'oops', kind: 'error' }];
    render(<ToastContainer toasts={toasts} onDismiss={() => {}} />);
    const el = screen.getByText('oops').parentElement!;
    expect(el.className).toContain('toast-error');
  });
});
