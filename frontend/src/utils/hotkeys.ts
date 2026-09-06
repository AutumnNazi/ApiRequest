// 全局快捷键的抑制判定。
//
// 早期实现用 event.target.closest('[role="dialog"]')，但弹窗打开时焦点通常仍留在
// 背后的触发按钮或 body 上，事件 target 落在弹窗外，判断随即失效、快捷键穿透。
// 因此改为查询文档内是否存在活动弹窗，与事件 target 的位置解耦。
const MODAL_SELECTOR =
  '[role="dialog"]:not([aria-hidden="true"]), [role="alertdialog"]:not([aria-hidden="true"])';

// target 参数保留用于将来按落点细化策略（如放行输入框内的编辑类快捷键）；
// 当前只要页面存在活动弹窗就一律抑制。
export function isHotkeySuppressed(_target: EventTarget | null): boolean {
  return document.querySelector(MODAL_SELECTOR) !== null;
}

// ── 自定义快捷键（docs/frontend.md §快捷键：默认，可改）──

export interface HotkeyMap {
  send: string; // 发送请求
  save: string; // 保存
  newTab: string; // 新建标签
  closeTab: string; // 关闭标签
  env: string; // 切换环境
  palette: string; // 命令面板
}

export const DEFAULT_HOTKEYS: HotkeyMap = {
  send: 'mod+enter',
  save: 'mod+s',
  newTab: 'mod+t',
  closeTab: 'mod+w',
  env: 'mod+e',
  palette: 'mod+k',
};

// 事件 → 归一化组合串（"mod+shift+s"）。mod = Ctrl（Win/Linux）或 Cmd（macOS）。
// 纯修饰键按下不产生组合（返回空串）。
export function eventCombo(e: {
  ctrlKey: boolean; metaKey: boolean; shiftKey: boolean; altKey: boolean; key: string;
}): string {
  const key = e.key;
  if (['Control', 'Meta', 'Shift', 'Alt'].includes(key)) return '';
  const parts: string[] = [];
  if (e.ctrlKey || e.metaKey) parts.push('mod');
  if (e.altKey) parts.push('alt');
  if (e.shiftKey) parts.push('shift');
  parts.push(key === 'Enter' ? 'enter' : key.length === 1 ? key.toLowerCase() : key.toLowerCase());
  return parts.join('+');
}

// 组合串 → 展示文案（按平台渲染 mod 的实际键名）
export function formatCombo(combo: string, platform: 'mac' | 'other'): string {
  const modLabel = platform === 'mac' ? 'Cmd' : 'Ctrl';
  return combo
    .split('+')
    .map((part) => {
      if (part === 'mod') return modLabel;
      if (part === 'enter') return 'Enter';
      if (part.length === 1) return part.toUpperCase();
      return part.charAt(0).toUpperCase() + part.slice(1);
    })
    .join('+');
}

// settings 里存的 JSON → 完整热键表（缺失/非法字段回退默认）
export function parseHotkeySettings(raw: string | null | undefined): HotkeyMap {
  const merged: HotkeyMap = { ...DEFAULT_HOTKEYS };
  if (!raw) return merged;
  try {
    const parsed = JSON.parse(raw) as Record<string, unknown>;
    for (const key of Object.keys(DEFAULT_HOTKEYS) as Array<keyof HotkeyMap>) {
      const v = parsed[key];
      if (typeof v === 'string' && v.trim() !== '') {
        merged[key] = v.trim().toLowerCase();
      }
    }
  } catch {
    // 坏 JSON 全量回退默认
  }
  return merged;
}

// 平台判定：macOS 用 Cmd，其余用 Ctrl（展示与捕获共用）
export function detectPlatform(): 'mac' | 'other' {
  return /mac/i.test(navigator.platform) ? 'mac' : 'other';
}

// 非模态自定义组合的安全性门槛：必须带 Ctrl/Cmd，或直接是 F 键——
// 避免在输入框打字时触发单字母快捷键
export function isSafeCombo(combo: string): boolean {
  return combo.startsWith('mod+') || /^f\d+$/.test(combo);
}
