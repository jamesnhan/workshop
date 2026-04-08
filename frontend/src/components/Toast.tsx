import { useEffect } from 'react';

export type ToastKind = 'info' | 'success' | 'warning' | 'error';

export interface ToastItem {
  id: number;
  message: string;
  kind: ToastKind;
}

interface Props {
  toasts: ToastItem[];
  onDismiss: (id: number) => void;
}

const KIND_ICONS: Record<ToastKind, string> = {
  info: 'ℹ',
  success: '✓',
  warning: '⚠',
  error: '✕',
};

const AUTO_DISMISS_MS = 4000;

export function ToastContainer({ toasts, onDismiss }: Props) {
  return (
    <div className="toast-container">
      {toasts.map((t) => (
        <Toast key={t.id} toast={t} onDismiss={onDismiss} />
      ))}
    </div>
  );
}

function Toast({ toast, onDismiss }: { toast: ToastItem; onDismiss: (id: number) => void }) {
  useEffect(() => {
    const timer = setTimeout(() => onDismiss(toast.id), AUTO_DISMISS_MS);
    return () => clearTimeout(timer);
  }, [toast.id, onDismiss]);

  return (
    <div className={`toast toast-${toast.kind}`} onClick={() => onDismiss(toast.id)}>
      <span className="toast-icon">{KIND_ICONS[toast.kind]}</span>
      <span className="toast-message">{toast.message}</span>
    </div>
  );
}
