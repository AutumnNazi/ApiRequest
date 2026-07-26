// 主题 store：激活/自定义主题管理，写入 CSS 变量并持久化
import { create } from 'zustand';
import {
  BUILTIN_THEMES,
  CUSTOM_THEME_MAX,
  allThemes,
  applyTheme,
  createCustomId,
  findTheme,
  loadSnapshot,
  saveSnapshot,
  type ThemeDefinition,
  type ThemePalette,
} from './themes';

interface ThemeState {
  activeId: string;
  custom: ThemeDefinition[];
  themes(): ThemeDefinition[];
  active(): ThemeDefinition;
  select(id: string): void;
  /** 以某主题为底创建自定义副本，返回新 id；超上限返回 null */
  duplicate(baseId: string, name?: string): string | null;
  updateCustom(id: string, patch: { name?: string; palette?: Partial<ThemePalette> }): void;
  removeCustom(id: string): void;
}

const initial = loadSnapshot();

export const useTheme = create<ThemeState>((set, get) => ({
  activeId: initial.activeId,
  custom: initial.custom,

  themes: () => allThemes(get().custom),
  active: () => findTheme(get().custom, get().activeId),

  select(id) {
    const theme = findTheme(get().custom, id);
    applyTheme(theme);
    set({ activeId: theme.id });
    saveSnapshot({ version: 1, activeId: theme.id, custom: get().custom });
  },

  duplicate(baseId, name) {
    const state = get();
    if (state.custom.length >= CUSTOM_THEME_MAX) return null;
    const base = findTheme(state.custom, baseId);
    const theme: ThemeDefinition = {
      id: createCustomId(),
      name: name ?? `${base.name.replace(/^[^\s]+\s/, '')} 副本`,
      builtin: false,
      palette: { ...base.palette },
    };
    const custom = [...state.custom, theme];
    set({ custom });
    saveSnapshot({ version: 1, activeId: state.activeId, custom });
    return theme.id;
  },

  updateCustom(id, patch) {
    const custom = get().custom.map((t) =>
      t.id === id
        ? {
            ...t,
            ...(patch.name !== undefined ? { name: patch.name } : {}),
            palette: { ...t.palette, ...(patch.palette ?? {}) },
          }
        : t,
    );
    set({ custom });
    saveSnapshot({ version: 1, activeId: get().activeId, custom });
    if (get().activeId === id) applyTheme(findTheme(custom, id));
  },

  removeCustom(id) {
    const state = get();
    const custom = state.custom.filter((t) => t.id !== id);
    let activeId = state.activeId;
    if (activeId === id) {
      activeId = BUILTIN_THEMES[0].id;
      applyTheme(BUILTIN_THEMES[0]);
    }
    set({ custom, activeId });
    saveSnapshot({ version: 1, activeId, custom });
  },
}));

// 应用启动即恢复主题（模块加载时执行一次）
applyTheme(findTheme(initial.custom, initial.activeId));
