import { useEffect, useRef, useState } from 'react';

export type DialogKind = 'confirm' | 'prompt';

interface Props {
  open: boolean;
  kind: DialogKind;
  title: string;
  message?: string;
  initialValue?: string;
  confirmLabel?: string;
  cancelLabel?: string;
  danger?: boolean;
  onConfirm: (value?: string) => void;
  onCancel: () => void;
}

export function ConfirmDialog({
  open,
  kind,
  title,
  message,
  initialValue = '',
  confirmLabel = 'Confirm',
  cancelLabel = 'Cancel',
  danger = false,
  onConfirm,
  onCancel,
}: Props) {
  const [value, setValue] = useState(initialValue);
  const inputRef = useRef<HTMLInputElement>(null);
  const dialogRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (open) {
      setValue(initialValue);
      // Focus input on next frame so the keypress that opened the dialog
      // doesn't leak into it.
      requestAnimationFrame(() => {
        inputRef.current?.focus();
        inputRef.current?.select();
      });
    }
  }, [open, initialValue]);

  if (!open) return null;

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      onConfirm(kind === 'prompt' ? value : undefined);
    } else if (e.key === 'Escape') {
      e.preventDefault();
      onCancel();
    } else if (e.key === 'Tab') {
      // Focus trap: cycle focus only among focusable elements inside the dialog.
      const root = dialogRef.current;
      if (!root) return;
      const focusable = Array.from(
        root.querySelectorAll<HTMLElement>('input, button, [tabindex]:not([tabindex="-1"])')
      ).filter((el) => !el.hasAttribute('disabled'));
      if (focusable.length === 0) return;
      const active = document.activeElement as HTMLElement | null;
      const idx = active ? focusable.indexOf(active) : -1;
      e.preventDefault();
      const nextIdx = e.shiftKey
        ? (idx <= 0 ? focusable.length - 1 : idx - 1)
        : (idx === focusable.length - 1 ? 0 : idx + 1);
      focusable[nextIdx]?.focus();
    }
  };

  return (
    <div className="confirm-dialog-overlay" onClick={onCancel}>
      <div ref={dialogRef} className="confirm-dialog" onClick={(e) => e.stopPropagation()} onKeyDown={handleKeyDown}>
        <div className="confirm-dialog-title">{title}</div>
        {message && <div className="confirm-dialog-message">{message}</div>}
        {kind === 'prompt' && (
          <input
            ref={inputRef}
            type="text"
            className="confirm-dialog-input"
            value={value}
            onChange={(e) => setValue(e.target.value)}
          />
        )}
        <div className="confirm-dialog-actions">
          <button className="confirm-dialog-btn" onClick={onCancel}>
            {cancelLabel}
          </button>
          <button
            className={`confirm-dialog-btn primary${danger ? ' danger' : ''}`}
            onClick={() => onConfirm(kind === 'prompt' ? value : undefined)}
            autoFocus={kind === 'confirm'}
          >
            {confirmLabel}
          </button>
        </div>
      </div>
    </div>
  );
}
