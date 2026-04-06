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
      ['Ctrl + Shift + B', 'Kanban board'],
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
  // On Mac: Cmd for panels/views, Ctrl for navigation/layout (avoids conflicts)
  const isMac = navigator.platform.toUpperCase().indexOf('MAC') >= 0;

  // Map shortcuts dynamically based on platform
  const displaySections = sections.map((section) => {
    if (isMac) {
      // On Mac: Ctrl stays Ctrl (navigation), Alt becomes Ctrl (for consistency)
      // Views & Panels section: Ctrl → Cmd
      if (section.title === 'Views & Panels' || section.title === 'Terminal') {
        return {
          ...section,
          keys: section.keys.map(([key, desc]) => [key.replace(/Ctrl/g, 'Cmd'), desc] as [string, string])
        };
      }
      // Navigation, Layout, Tabs: Alt → Ctrl (for conflict-free navigation)
      return {
        ...section,
        keys: section.keys.map(([key, desc]) => [key.replace(/Alt/g, 'Ctrl'), desc] as [string, string])
      };
    }
    return section;
  });

  return (
    <div className="switcher-overlay" onClick={onClose}>
      <div className="hotkey-menu" onClick={(e) => e.stopPropagation()}>
        <div className="hotkey-header">
          <span>Keyboard Shortcuts</span>
          <button className="search-close" onClick={onClose}>x</button>
        </div>
        <div className="hotkey-body">
          {displaySections.map((section) => (
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
