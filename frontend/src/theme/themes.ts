// 主题系统：CSS 变量 token + 内置预设 + 自定义主题（localStorage 持久化）。
// 架构参考 GoNavi：token 分 bg/fg/border/accent 族，预设生成变量块，
// data-theme 挂 body，自定义主题在预设基础上改色。
export interface ThemePalette {
  mode: 'light' | 'dark';
  // 背景族
  bgApp: string; // 应用底
  bgChrome: string; // 顶栏/侧栏
  bgPanel: string; // 面板/卡片
  bgPanel2: string; // 次级面板
  bgInput: string;
  bgHover: string;
  bgSelected: string;
  // 前景族
  fg1: string; // 主文本
  fg2: string;
  fg3: string; // 次要
  fg4: string; // 弱化
  // 边框族
  br1: string;
  br2: string;
  // 强调族
  accent: string;
  accentHover: string;
  accentSoft: string;
  onAccent: string;
  // 语义色
  success: string;
  warn: string;
  warnSoft: string;
  danger: string;
  dangerSoft: string;
}

export interface ThemeDefinition {
  id: string;
  name: string;
  builtin: boolean;
  palette: ThemePalette;
}

// ── 内置预设（命名与氛围对齐 GoNavi 的六套 + 默认亮色）──

const daylight: ThemePalette = {
  mode: 'light',
  bgApp: '#f6f6f4', bgChrome: '#ececea', bgPanel: '#ffffff', bgPanel2: '#fafaf8',
  bgInput: '#ffffff', bgHover: 'rgba(15,23,42,0.045)', bgSelected: 'rgba(37,99,235,0.10)',
  fg1: '#0c1322', fg2: '#1f2937', fg3: '#4b5563', fg4: '#9ca3af',
  br1: 'rgba(15,23,42,0.08)', br2: 'rgba(15,23,42,0.14)',
  accent: '#2563eb', accentHover: '#1d4ed8', accentSoft: '#dbeafe', onAccent: '#ffffff',
  success: '#15803d', warn: '#d97706', warnSoft: 'rgba(245,158,11,0.14)',
  danger: '#dc2626', dangerSoft: 'rgba(220,38,38,0.10)',
};

const comfortDark: ThemePalette = {
  mode: 'dark',
  bgApp: '#1e2126', bgChrome: '#25292f', bgPanel: '#2a2f36', bgPanel2: '#242930',
  bgInput: '#1d2127', bgHover: 'rgba(255,255,255,0.05)', bgSelected: 'rgba(96,165,250,0.16)',
  fg1: '#e7eaee', fg2: '#c8cdd4', fg3: '#9aa2ad', fg4: '#6b7280',
  br1: 'rgba(255,255,255,0.07)', br2: 'rgba(255,255,255,0.13)',
  accent: '#60a5fa', accentHover: '#3b82f6', accentSoft: 'rgba(96,165,250,0.15)', onAccent: '#0c1322',
  success: '#4ade80', warn: '#fbbf24', warnSoft: 'rgba(251,191,36,0.14)',
  danger: '#f87171', dangerSoft: 'rgba(248,113,113,0.12)',
};

const midnightNavy: ThemePalette = {
  ...comfortDark,
  bgApp: '#0f1626', bgChrome: '#131b2e', bgPanel: '#182136', bgPanel2: '#141c30',
  bgInput: '#0e1524', bgSelected: 'rgba(125,145,255,0.18)',
  fg1: '#e4e9f5', fg2: '#c3cbe0', fg3: '#8e9ab5', fg4: '#5f6b85',
  accent: '#7d91ff', accentHover: '#5f78ff', accentSoft: 'rgba(125,145,255,0.16)',
};

const nordSlate: ThemePalette = {
  ...comfortDark,
  bgApp: '#2e3440', bgChrome: '#343b49', bgPanel: '#3b4252', bgPanel2: '#353c4a',
  bgInput: '#2b313d', bgSelected: 'rgba(136,192,208,0.18)',
  fg1: '#eceff4', fg2: '#d8dee9', fg3: '#a6adbb', fg4: '#767e8c',
  accent: '#88c0d0', accentHover: '#6fb3c6', accentSoft: 'rgba(136,192,208,0.16)', onAccent: '#1a2129',
  success: '#a3be8c', warn: '#ebcb8b', danger: '#bf616a',
};

const deepOcean: ThemePalette = {
  ...comfortDark,
  bgApp: '#0b1e25', bgChrome: '#0f242c', bgPanel: '#132b34', bgPanel2: '#0f252d',
  bgInput: '#0a1c23', bgSelected: 'rgba(45,212,191,0.16)',
  fg1: '#dcf2f1', fg2: '#b5d9d8', fg3: '#7fa8a9', fg4: '#547779',
  accent: '#2dd4bf', accentHover: '#14b8a6', accentSoft: 'rgba(45,212,191,0.14)', onAccent: '#062a26',
  success: '#4ade80', warn: '#facc15', danger: '#fb7185',
};

const warmPaper: ThemePalette = {
  ...daylight,
  bgApp: '#f5efe4', bgChrome: '#ede5d6', bgPanel: '#fdf9f0', bgPanel2: '#f7f1e5',
  bgInput: '#fffdf7', bgSelected: 'rgba(180,83,9,0.12)',
  fg1: '#292018', fg2: '#453729', fg3: '#6d5d4b', fg4: '#a3937f',
  br1: 'rgba(69,55,41,0.10)', br2: 'rgba(69,55,41,0.18)',
  accent: '#b45309', accentHover: '#92400e', accentSoft: 'rgba(180,83,9,0.12)',
};

const mistJade: ThemePalette = {
  ...daylight,
  bgApp: '#eef4f0', bgChrome: '#e3ece6', bgPanel: '#fbfdfc', bgPanel2: '#f2f7f4',
  bgInput: '#ffffff', bgSelected: 'rgba(13,148,136,0.10)',
  fg1: '#122420', fg2: '#28423b', fg3: '#51695f', fg4: '#8ba198',
  br1: 'rgba(18,36,32,0.08)', br2: 'rgba(18,36,32,0.15)',
  accent: '#0d9488', accentHover: '#0f766e', accentSoft: 'rgba(13,148,136,0.12)',
};

export const BUILTIN_THEMES: ThemeDefinition[] = [
  { id: 'daylight', name: '☀️ 明亮（默认）', builtin: true, palette: daylight },
  { id: 'comfort-dark', name: '🌙 舒适暗色', builtin: true, palette: comfortDark },
  { id: 'midnight-navy', name: '🌌 午夜藏蓝', builtin: true, palette: midnightNavy },
  { id: 'nord-slate', name: '🧊 北欧石板', builtin: true, palette: nordSlate },
  { id: 'deep-ocean', name: '🌊 深海青', builtin: true, palette: deepOcean },
  { id: 'warm-paper', name: '📜 暖纸', builtin: true, palette: warmPaper },
  { id: 'mist-jade', name: '🍃 雾松青玉', builtin: true, palette: mistJade },
];

// ── 应用与持久化 ──

const STORAGE_KEY = 'apirequest-themes-v1';
export const CUSTOM_THEME_MAX = 12;

interface ThemeSnapshot {
  version: 1;
  activeId: string;
  custom: ThemeDefinition[];
}

export function loadSnapshot(): ThemeSnapshot {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (raw) {
      const parsed = JSON.parse(raw) as ThemeSnapshot;
      if (parsed.version === 1) {
        // builtin id 是保留命名空间，持久化数据不得遮蔽内置主题
        parsed.custom = (parsed.custom ?? []).filter(
          (t) => t && !BUILTIN_THEMES.some((b) => b.id === t.id) && t.palette,
        );
        return parsed;
      }
    }
  } catch {
    // 损坏数据回落默认
  }
  return { version: 1, activeId: 'daylight', custom: [] };
}

export function saveSnapshot(snap: ThemeSnapshot) {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(snap));
  } catch {
    // 存储不可用时静默（主题仍在会话内生效）
  }
}

export function allThemes(custom: ThemeDefinition[]): ThemeDefinition[] {
  return [...BUILTIN_THEMES, ...custom];
}

export function findTheme(custom: ThemeDefinition[], id: string): ThemeDefinition {
  return allThemes(custom).find((t) => t.id === id) ?? BUILTIN_THEMES[0];
}

/** 把主题写入 :root CSS 变量并标记 data-theme（供少数无法变量化的样式用） */
export function applyTheme(theme: ThemeDefinition) {
  const p = theme.palette;
  const root = document.documentElement;
  const vars: Record<string, string> = {
    '--t-bg-app': p.bgApp,
    '--t-bg-chrome': p.bgChrome,
    '--t-bg-panel': p.bgPanel,
    '--t-bg-panel2': p.bgPanel2,
    '--t-bg-input': p.bgInput,
    '--t-bg-hover': p.bgHover,
    '--t-bg-selected': p.bgSelected,
    '--t-fg-1': p.fg1,
    '--t-fg-2': p.fg2,
    '--t-fg-3': p.fg3,
    '--t-fg-4': p.fg4,
    '--t-br-1': p.br1,
    '--t-br-2': p.br2,
    '--t-accent': p.accent,
    '--t-accent-hover': p.accentHover,
    '--t-accent-soft': p.accentSoft,
    '--t-on-accent': p.onAccent,
    '--t-success': p.success,
    '--t-warn': p.warn,
    '--t-warn-soft': p.warnSoft,
    '--t-danger': p.danger,
    '--t-danger-soft': p.dangerSoft,
  };
  for (const [k, v] of Object.entries(vars)) root.style.setProperty(k, v);
  root.dataset.theme = theme.palette.mode;
  root.dataset.themeId = theme.id;
}

export function createCustomId(): string {
  return 'custom-' + Math.random().toString(36).slice(2, 10);
}
