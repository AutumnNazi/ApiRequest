// 无边框窗口标题栏的拖拽区域标记（WebKit/WebView2）。React 类型未收录该属性，故断言。
import type { CSSProperties } from 'react';

export const dragRegion = { WebkitAppRegion: 'drag' } as CSSProperties;
export const noDragRegion = { WebkitAppRegion: 'no-drag' } as CSSProperties;