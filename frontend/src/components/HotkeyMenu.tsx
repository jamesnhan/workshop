interface Props {
  onClose: () => void;
}

const sections = [
  {
    title: 'Navigation',
    keys: [
      ['Alt + h/j/k/l', 'Navigate between cells'],
      ['Alt + 1-9', 'Focus cell by number'],
      ['Alt + Left/Right', 'Pane history back/forward'],
    ],
  },
  {
    title: 'Pane Tabs',
    keys: [
      ['Alt + [', 'Previous tab'],
      ['Alt + ]', 'Next tab'],
      ['Alt + W', 'Close current tab'],
    ],
  },
  {
    title: 'Layout',
    keys: [
      ['Alt + F', 'Maximize / restore cell'],
      ['Alt + Shift + H/J/K/L', 'Merge cell in direction'],
      ['Alt + Shift + S', 'Split merged cell'],
      ['Alt + B', 'Toggle sidebar'],
    ],
  },
  {
    title: 'Views & Panels',
    keys: [
      ['Ctrl + P', 'Pane switcher'],
      ['Ctrl + Shift + P', 'Command palette'],
      ['Ctrl + Shift + F', 'Search pane output'],
      ['Ctrl + Shift + K', 'Kanban board'],
      ['Ctrl + Shift + D', 'Agent dashboard'],
      ['?', 'This hotkey menu'],
      ['Escape', 'Close any panel'],
    ],
  },
  {
    title: 'Ctrl+P Switcher',
    keys: [
      ['Tab / Shift+Tab', 'Cycle switcher tabs'],
      ['Enter', 'Enter NAV mode'],
      ['j / k', 'Navigate results (NAV)'],
      ['/ or i', 'Back to search (NAV)'],
    ],
  },
  {
    title: 'Terminal',
    keys: [
      ['Ctrl + C', 'SIGINT'],
      ['Ctrl + Shift + C', 'Copy selection'],
      ['Ctrl + V', 'Paste'],
      ['Tab / Shift+Tab', 'Tab / reverse tab'],
      ['Ctrl + Backspace', 'Delete word'],
      ['Alt + Backspace', 'Delete word'],
      ['Ctrl + Arrow', 'Word navigation'],
      ['Alt + Arrow', 'Word navigation'],
      ['Alt + T', 'Toggle thinking mode'],
    ],
  },
];

export function HotkeyMenu({ onClose }: Props) {
  return (
    <div className="switcher-overlay" onClick={onClose}>
      <div className="hotkey-menu" onClick={(e) => e.stopPropagation()}>
        <div className="hotkey-header">
          <span>Keyboard Shortcuts</span>
          <button className="search-close" onClick={onClose}>x</button>
        </div>
        <div className="hotkey-body">
          {sections.map((section) => (
            <div key={section.title} className="hotkey-section">
              <h3 className="hotkey-section-title">{section.title}</h3>
              {section.keys.map(([key, desc]) => (
                <div key={key} className="hotkey-row">
                  <kbd className="hotkey-key">{key}</kbd>
                  <span className="hotkey-desc">{desc}</span>
                </div>
              ))}
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
