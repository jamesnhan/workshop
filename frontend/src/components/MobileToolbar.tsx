import { useState } from 'react';

interface MobileToolbarProps {
  onSend: (data: string) => void;
  visible: boolean;
}

const BUTTONS: { label: string; data: string }[] = [
  { label: 'Esc',    data: '\x1b' },
  { label: 'Tab',    data: '\x09' },
  { label: '\u2191',  data: '\x1b[A' },  // ↑
  { label: '\u2193',  data: '\x1b[B' },  // ↓
  { label: 'C-c',    data: '\x03' },
  { label: 'C-o',    data: '\x0f' },
  { label: 'C-l',    data: '\x0c' },
  { label: 'Enter',  data: '\r' },
];

export function MobileToolbar({ onSend, visible }: MobileToolbarProps) {
  const [hidden, setHidden] = useState(false);

  if (!visible || hidden) {
    // Show a small tab to bring the toolbar back
    return visible ? (
      <button
        className="mobile-toolbar-show"
        onPointerDown={(e) => { e.preventDefault(); setHidden(false); }}
      >
        ⌨
      </button>
    ) : null;
  }

  return (
    <div className="mobile-toolbar visible">
      {BUTTONS.map((btn) => (
        <button
          key={btn.label}
          onPointerDown={(e) => {
            e.preventDefault();
            onSend(btn.data);
          }}
        >
          {btn.label}
        </button>
      ))}
      <button
        className="mobile-toolbar-hide"
        onPointerDown={(e) => { e.preventDefault(); setHidden(true); }}
        title="Hide toolbar"
      >
        ✕
      </button>
    </div>
  );
}
