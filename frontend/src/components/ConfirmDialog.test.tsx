import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ConfirmDialog } from './ConfirmDialog';

function renderConfirm(overrides: Partial<Parameters<typeof ConfirmDialog>[0]> = {}) {
  const onConfirm = vi.fn();
  const onCancel = vi.fn();
  const props = {
    open: true,
    kind: 'confirm' as const,
    title: 'Delete thing?',
    onConfirm,
    onCancel,
    ...overrides,
  };
  render(<ConfirmDialog {...props} />);
  return { onConfirm, onCancel };
}

describe('ConfirmDialog', () => {
  it('renders nothing when open is false', () => {
    const onConfirm = vi.fn();
    const onCancel = vi.fn();
    render(<ConfirmDialog open={false} kind="confirm" title="x" onConfirm={onConfirm} onCancel={onCancel} />);
    expect(screen.queryByText('x')).toBeNull();
  });

  it('confirm click fires onConfirm with no value', async () => {
    const { onConfirm, onCancel } = renderConfirm();
    await userEvent.click(screen.getByRole('button', { name: 'Confirm' }));
    expect(onConfirm).toHaveBeenCalledOnce();
    expect(onConfirm).toHaveBeenCalledWith(undefined);
    expect(onCancel).not.toHaveBeenCalled();
  });

  it('cancel click fires onCancel', async () => {
    const { onConfirm, onCancel } = renderConfirm();
    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(onCancel).toHaveBeenCalledOnce();
    expect(onConfirm).not.toHaveBeenCalled();
  });

  it('Enter triggers confirm', async () => {
    const { onConfirm } = renderConfirm();
    // Focus the dialog root so keydown routes to the handler.
    const dialog = screen.getByText('Delete thing?').parentElement!;
    dialog.focus();
    await userEvent.keyboard('{Enter}');
    expect(onConfirm).toHaveBeenCalled();
  });

  it('Escape triggers cancel', async () => {
    const { onCancel } = renderConfirm();
    const dialog = screen.getByText('Delete thing?').parentElement!;
    dialog.focus();
    await userEvent.keyboard('{Escape}');
    expect(onCancel).toHaveBeenCalled();
  });

  it('overlay click triggers cancel', async () => {
    const { onCancel } = renderConfirm();
    // The overlay has the onClick={onCancel}; click the outermost div by
    // finding a non-dialog ancestor.
    const dialogBox = screen.getByText('Delete thing?').parentElement!;
    const overlay = dialogBox.parentElement!;
    await userEvent.click(overlay);
    expect(onCancel).toHaveBeenCalled();
  });

  it('danger styling applies a danger class to the confirm button', () => {
    renderConfirm({ danger: true, confirmLabel: 'Delete' });
    const btn = screen.getByRole('button', { name: 'Delete' });
    expect(btn.className).toContain('danger');
  });

  describe('prompt kind', () => {
    it('renders an input with the initial value', () => {
      renderConfirm({ kind: 'prompt', initialValue: 'hello' });
      const input = screen.getByRole('textbox') as HTMLInputElement;
      expect(input.value).toBe('hello');
    });

    it('confirm passes the current input value', async () => {
      const { onConfirm } = renderConfirm({ kind: 'prompt', initialValue: 'abc' });
      const input = screen.getByRole('textbox') as HTMLInputElement;
      await userEvent.clear(input);
      await userEvent.type(input, 'new value');
      await userEvent.click(screen.getByRole('button', { name: 'Confirm' }));
      expect(onConfirm).toHaveBeenCalledWith('new value');
    });

    it('Enter inside the input submits with the current value', async () => {
      const { onConfirm } = renderConfirm({ kind: 'prompt', initialValue: 'abc' });
      const input = screen.getByRole('textbox') as HTMLInputElement;
      input.focus();
      await userEvent.type(input, '{Enter}');
      expect(onConfirm).toHaveBeenCalledWith('abc');
    });
  });
});
