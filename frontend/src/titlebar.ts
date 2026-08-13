// 无边框窗口标题栏的拖拽区域标记。
// - macOS: WebkitAppRegion: drag
// - Windows (Wails WebView2): --wails-draggable: drag
// React 类型未收录这些属性，故断言。
import type { CSSProperties } from 'react';

export const dragRegion = {
  WebkitAppRegion: 'drag',
  '--wails-draggable': 'drag',
} as CSSProperties;

export const noDragRegion = {
  WebkitAppRegion: 'no-drag',
  '--wails-draggable': 'no-drag',
} as CSSProperties;
