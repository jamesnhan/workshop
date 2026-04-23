interface Props {
  onClose: () => void;
}

const isMac = typeof navigator !== 'undefined' && navigator.platform.toUpperCase().indexOf('MAC') >= 0;
const mod = isMac ? 'Cmd' : 'Ctrl';
const nav = isMac ? 'Ctrl' : 'Alt';

const sections = [
  {
    title: 'Navigation',
    keys: [
      [`${nav} + h/j/k/l`, 'Navigate between cells'],
      [`${nav} + 1-9`, 'Focus cell by number'],
      [`${nav} + Left/Right`, 'Pane history back/forward'],
    ],
  },
  {
    title: 'Pane Tabs',
    keys: [
      [`${nav} + [`, 'Previous tab'],
      [`${nav} + ]`, 'Next tab'],
      [`${nav} + W`, 'Close current tab'],
    ],
  },
  {
    title: 'Layout',
    keys: [
      [`${nav} + F`, 'Maximize / restore cell'],
      [`${nav} + Shift + H/J/K/L`, 'Merge cell in direction'],
      [`${nav} + Shift + S`, 'Split merged cell'],
      [`${nav} + Shift + Arrows`, 'Swap focused cell with neighbor'],
      [`${nav} + B`, 'Toggle sidebar'],
    ],
  },
  {
    title: 'Views & Panels',
    keys: [
      [`${mod} + P`, 'Pane switcher'],
      [`${mod} + Shift + P`, 'Command palette'],
      [`${mod} + Shift + F`, 'Search pane output'],
      [`${mod} + Shift + K`, 'Kanban board'],
      ['\`', 'Split view (Kanban + Terminal)'],
      [`${mod} + Shift + L`, 'Split view (Kanban + Terminal)'],
      [`${mod} + Shift + D`, 'Agent dashboard'],
      ['z', 'Pin / unpin hover preview (for screenshots)'],
      ['?', 'This hotkey menu'],
      ['Escape', 'Close any panel or unpin hover'],
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
      [isMac ? 'Cmd + C' : 'Ctrl + Shift + C', 'Copy selection'],
      [isMac ? 'Cmd + V' : 'Ctrl + V', 'Paste'],
      ['Tab / Shift+Tab', 'Tab / reverse tab'],
      ['Ctrl + Backspace', 'Delete word'],
      [`${nav} + Backspace`, 'Delete word'],
      ['Ctrl + Arrow', 'Word navigation'],
      [`${nav} + Arrow`, 'Word navigation'],
      [`${nav} + T`, 'Toggle thinking mode'],
      ['#', 'Ticket lookup — insert #id into terminal'],
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
