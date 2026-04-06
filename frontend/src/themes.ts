export interface Theme {
  name: string;
  // UI colors
  bg: string;
  bgSecondary: string;
  bgTertiary: string;
  border: string;
  text: string;
  textMuted: string;
  textDim: string;
  accent: string;
  accentDim: string;
  success: string;
  error: string;
  warning: string;
  // Terminal colors (xterm.js)
  terminal: {
    background: string;
    foreground: string;
    cursor: string;
    selectionBackground: string;
    black: string;
    red: string;
    green: string;
    yellow: string;
    blue: string;
    magenta: string;
    cyan: string;
    white: string;
    brightBlack: string;
    brightRed: string;
    brightGreen: string;
    brightYellow: string;
    brightBlue: string;
    brightMagenta: string;
    brightCyan: string;
    brightWhite: string;
  };
}

export const themes: Record<string, Theme> = {
  'catppuccin-mocha': {
    name: 'Catppuccin Mocha',
    bg: '#1e1e2e',
    bgSecondary: '#181825',
    bgTertiary: '#11111b',
    border: '#313244',
    text: '#cdd6f4',
    textMuted: '#6c7086',
    textDim: '#45475a',
    accent: '#cba6f7',     // mauve
    accentDim: '#cba6f733',
    success: '#a6e3a1',
    error: '#f38ba8',
    warning: '#f9e2af',
    terminal: {
      background: '#1e1e2e',
      foreground: '#cdd6f4',
      cursor: '#f5e0dc',    // rosewater
      selectionBackground: '#45475a',
      black: '#45475a',     // surface1
      red: '#f38ba8',
      green: '#a6e3a1',
      yellow: '#f9e2af',
      blue: '#89b4fa',
      magenta: '#cba6f7',
      cyan: '#94e2d5',      // teal
      white: '#bac2de',     // subtext1
      brightBlack: '#585b70', // surface2
      brightRed: '#f38ba8',
      brightGreen: '#a6e3a1',
      brightYellow: '#f9e2af',
      brightBlue: '#89b4fa',
      brightMagenta: '#cba6f7',
      brightCyan: '#94e2d5',
      brightWhite: '#a6adc8', // subtext0
    },
  },
  'catppuccin-latte': {
    name: 'Catppuccin Latte',
    bg: '#eff1f5',
    bgSecondary: '#e6e9ef',
    bgTertiary: '#dce0e8',
    border: '#ccd0da',
    text: '#4c4f69',
    textMuted: '#8c8fa1',
    textDim: '#bcc0cc',
    accent: '#8839ef',
    accentDim: '#8839ef33',
    success: '#40a02b',
    error: '#d20f39',
    warning: '#df8e1d',
    terminal: {
      background: '#eff1f5',
      foreground: '#4c4f69',
      cursor: '#dc8a78',
      selectionBackground: '#ccd0da',
      black: '#5c5f77',
      red: '#d20f39',
      green: '#40a02b',
      yellow: '#df8e1d',
      blue: '#1e66f5',
      magenta: '#8839ef',
      cyan: '#179299',
      white: '#acb0be',
      brightBlack: '#6c6f85',
      brightRed: '#d20f39',
      brightGreen: '#40a02b',
      brightYellow: '#df8e1d',
      brightBlue: '#1e66f5',
      brightMagenta: '#8839ef',
      brightCyan: '#179299',
      brightWhite: '#bcc0cc',
    },
  },
  'tokyo-night': {
    name: 'Tokyo Night',
    bg: '#1a1b26',
    bgSecondary: '#16161e',
    bgTertiary: '#13131a',
    border: '#292e42',
    text: '#c0caf5',
    textMuted: '#565f89',
    textDim: '#3b4261',
    accent: '#7aa2f7',
    accentDim: '#7aa2f733',
    success: '#9ece6a',
    error: '#f7768e',
    warning: '#e0af68',
    terminal: {
      background: '#1a1b26',
      foreground: '#c0caf5',
      cursor: '#c0caf5',
      selectionBackground: '#292e42',
      black: '#414868',
      red: '#f7768e',
      green: '#9ece6a',
      yellow: '#e0af68',
      blue: '#7aa2f7',
      magenta: '#bb9af7',
      cyan: '#7dcfff',
      white: '#a9b1d6',
      brightBlack: '#414868',
      brightRed: '#f7768e',
      brightGreen: '#9ece6a',
      brightYellow: '#e0af68',
      brightBlue: '#7aa2f7',
      brightMagenta: '#bb9af7',
      brightCyan: '#7dcfff',
      brightWhite: '#c0caf5',
    },
  },
  // ── Workshop Theme Family ──────────────────────────────────────
  // Signature: electric purple accent (#6c63ff), deep blue-purple bases
  // Each variant shifts the mood while keeping the core identity

  'workshop-dark': {
    name: 'Workshop Dark',
    // The original — deep midnight navy with electric purple
    bg: '#0f0f1a',
    bgSecondary: '#1a1a2e',
    bgTertiary: '#0a0a14',
    border: '#2a2a4a',
    text: '#e0e0e0',
    textMuted: '#888888',
    textDim: '#555555',
    accent: '#6c63ff',
    accentDim: '#6c63ff33',
    success: '#4caf50',
    error: '#f44336',
    warning: '#ffc107',
    terminal: {
      background: '#0a0a14',
      foreground: '#c8c8d8',
      cursor: '#6c63ff',
      selectionBackground: '#2a2a4a',
      black: '#1a1a2e',
      red: '#f44336',
      green: '#4caf50',
      yellow: '#ffc107',
      blue: '#6c63ff',
      magenta: '#ce93d8',
      cyan: '#4dd0e1',
      white: '#e0e0e0',
      brightBlack: '#555555',
      brightRed: '#ef5350',
      brightGreen: '#66bb6a',
      brightYellow: '#ffca28',
      brightBlue: '#8a80ff',
      brightMagenta: '#e1bee7',
      brightCyan: '#4dd0e1',
      brightWhite: '#ffffff',
    },
  },
  'workshop-midnight': {
    name: 'Workshop Midnight',
    // Even deeper — near-black with subtle purple undertones, high contrast
    bg: '#08080f',
    bgSecondary: '#101020',
    bgTertiary: '#05050a',
    border: '#1e1e38',
    text: '#d4d4e8',
    textMuted: '#6a6a8a',
    textDim: '#3a3a55',
    accent: '#7c6cff',
    accentDim: '#7c6cff28',
    success: '#58d68d',
    error: '#ff6b6b',
    warning: '#ffd93d',
    terminal: {
      background: '#05050a',
      foreground: '#d4d4e8',
      cursor: '#7c6cff',
      selectionBackground: '#1e1e38',
      black: '#101020',
      red: '#ff6b6b',
      green: '#58d68d',
      yellow: '#ffd93d',
      blue: '#7c6cff',
      magenta: '#d4a5ff',
      cyan: '#56d4e0',
      white: '#d4d4e8',
      brightBlack: '#3a3a55',
      brightRed: '#ff8787',
      brightGreen: '#7ee8a5',
      brightYellow: '#ffe066',
      brightBlue: '#9a8fff',
      brightMagenta: '#e2c0ff',
      brightCyan: '#7ae0ea',
      brightWhite: '#f0f0ff',
    },
  },
  'workshop-sakura': {
    name: 'Workshop Sakura',
    // Cherry blossom — soft pink accent on warm dark base
    bg: '#14101a',
    bgSecondary: '#1e1824',
    bgTertiary: '#0e0a12',
    border: '#332840',
    text: '#e8dce8',
    textMuted: '#9a8a9e',
    textDim: '#584860',
    accent: '#f472b6',
    accentDim: '#f472b633',
    success: '#86efac',
    error: '#fb7185',
    warning: '#fbbf24',
    terminal: {
      background: '#0e0a12',
      foreground: '#e8dce8',
      cursor: '#f472b6',
      selectionBackground: '#332840',
      black: '#1e1824',
      red: '#fb7185',
      green: '#86efac',
      yellow: '#fbbf24',
      blue: '#93c5fd',
      magenta: '#f472b6',
      cyan: '#a5f3fc',
      white: '#e8dce8',
      brightBlack: '#584860',
      brightRed: '#fda4af',
      brightGreen: '#a7f3d0',
      brightYellow: '#fde68a',
      brightBlue: '#bfdbfe',
      brightMagenta: '#f9a8d4',
      brightCyan: '#cffafe',
      brightWhite: '#fdf2f8',
    },
  },
  'workshop-ocean': {
    name: 'Workshop Ocean',
    // Deep sea — teal/cyan accent on blue-green dark base
    bg: '#0a1218',
    bgSecondary: '#12202a',
    bgTertiary: '#060e14',
    border: '#1a3040',
    text: '#d0e8f0',
    textMuted: '#6a8ea0',
    textDim: '#3a5868',
    accent: '#22d3ee',
    accentDim: '#22d3ee28',
    success: '#34d399',
    error: '#f87171',
    warning: '#fbbf24',
    terminal: {
      background: '#060e14',
      foreground: '#d0e8f0',
      cursor: '#22d3ee',
      selectionBackground: '#1a3040',
      black: '#12202a',
      red: '#f87171',
      green: '#34d399',
      yellow: '#fbbf24',
      blue: '#60a5fa',
      magenta: '#a78bfa',
      cyan: '#22d3ee',
      white: '#d0e8f0',
      brightBlack: '#3a5868',
      brightRed: '#fca5a5',
      brightGreen: '#6ee7b7',
      brightYellow: '#fde68a',
      brightBlue: '#93c5fd',
      brightMagenta: '#c4b5fd',
      brightCyan: '#67e8f9',
      brightWhite: '#ecfeff',
    },
  },
  'workshop-ember': {
    name: 'Workshop Ember',
    // Warm fire — amber/orange accent on warm dark base
    bg: '#151010',
    bgSecondary: '#201818',
    bgTertiary: '#0e0a0a',
    border: '#3a2828',
    text: '#e8dcd0',
    textMuted: '#9a8878',
    textDim: '#604840',
    accent: '#f59e0b',
    accentDim: '#f59e0b28',
    success: '#84cc16',
    error: '#ef4444',
    warning: '#f59e0b',
    terminal: {
      background: '#0e0a0a',
      foreground: '#e8dcd0',
      cursor: '#f59e0b',
      selectionBackground: '#3a2828',
      black: '#201818',
      red: '#ef4444',
      green: '#84cc16',
      yellow: '#f59e0b',
      blue: '#60a5fa',
      magenta: '#e879f9',
      cyan: '#2dd4bf',
      white: '#e8dcd0',
      brightBlack: '#604840',
      brightRed: '#f87171',
      brightGreen: '#a3e635',
      brightYellow: '#fbbf24',
      brightBlue: '#93c5fd',
      brightMagenta: '#f0abfc',
      brightCyan: '#5eead4',
      brightWhite: '#fef3c7',
    },
  },
  'workshop-light': {
    name: 'Workshop Light',
    // Light variant — clean white with purple accent, easy on the eyes
    bg: '#f8f7ff',
    bgSecondary: '#eeedf5',
    bgTertiary: '#e4e2f0',
    border: '#d4d0e8',
    text: '#2a2640',
    textMuted: '#7a7690',
    textDim: '#b0acc8',
    accent: '#6c63ff',
    accentDim: '#6c63ff20',
    success: '#16a34a',
    error: '#dc2626',
    warning: '#ca8a04',
    terminal: {
      background: '#f8f7ff',
      foreground: '#2a2640',
      cursor: '#6c63ff',
      selectionBackground: '#d4d0e8',
      black: '#2a2640',
      red: '#dc2626',
      green: '#16a34a',
      yellow: '#ca8a04',
      blue: '#6c63ff',
      magenta: '#a855f7',
      cyan: '#0891b2',
      white: '#e4e2f0',
      brightBlack: '#7a7690',
      brightRed: '#ef4444',
      brightGreen: '#22c55e',
      brightYellow: '#eab308',
      brightBlue: '#8a80ff',
      brightMagenta: '#c084fc',
      brightCyan: '#06b6d4',
      brightWhite: '#f8f7ff',
    },
  },
};

const STORAGE_KEY = 'workshop:theme';

export function getActiveThemeName(): string {
  try {
    return localStorage.getItem(STORAGE_KEY) || 'catppuccin-mocha';
  } catch {
    return 'catppuccin-mocha';
  }
}

export function setActiveThemeName(name: string) {
  try {
    localStorage.setItem(STORAGE_KEY, name);
  } catch {
    // ignore
  }
}

export function getActiveTheme(): Theme {
  return themes[getActiveThemeName()] || themes['catppuccin-mocha'];
}

// Apply theme as CSS custom properties on :root
export function applyTheme(theme: Theme) {
  const root = document.documentElement;
  root.style.setProperty('--bg', theme.bg);
  root.style.setProperty('--bg-secondary', theme.bgSecondary);
  root.style.setProperty('--bg-tertiary', theme.bgTertiary);
  root.style.setProperty('--border', theme.border);
  root.style.setProperty('--text', theme.text);
  root.style.setProperty('--text-muted', theme.textMuted);
  root.style.setProperty('--text-dim', theme.textDim);
  root.style.setProperty('--accent', theme.accent);
  root.style.setProperty('--accent-dim', theme.accentDim);
  root.style.setProperty('--success', theme.success);
  root.style.setProperty('--error', theme.error);
  root.style.setProperty('--warning', theme.warning);
}
