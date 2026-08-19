// 可拖拽分割条：分隔上下（horizontal）或左右（vertical）两个面板。
// 通过 onRatio 回调向上反馈比例（0~1），由父层持久化。
import { useEffect, useRef } from 'react';
import { formatMessage } from '../i18n/locale';

interface Props {
  orientation: 'horizontal' | 'vertical';
  onRatio(ratio: number): void;
  ratio: number; // 当前比例，用于限制拖拽方向相对初始点
}

export default function Splitter({ orientation, onRatio, ratio }: Props) {
  const startPayload = useRef({ client: 0, ratio });
  const onRatioRef = useRef(onRatio);
  onRatioRef.current = onRatio;
  // 拖拽期间的清理函数：注册在 mousedown，卸载与 mouseup 都要能调用到
  const cleanupRef = useRef<(() => void) | null>(null);

  const isRow = orientation === 'vertical'; // 侧栏 → 左右分隔，容器为 row

  // 卸载时若仍在拖拽，撤掉监听器与 body 样式，避免残留全局副作用
  useEffect(() => () => cleanupRef.current?.(), []);

  const onMouseDown = (e: React.MouseEvent) => {
    e.preventDefault();
    // 上一次拖拽未收尾（如 mouseup 落在窗口外）时先清理，避免监听器叠加
    cleanupRef.current?.();
    startPayload.current = { client: isRow ? e.clientX : e.clientY, ratio };
    // 记下拖拽前的内联样式，收尾时原样写回（空串代表原本没有内联样式）
    const previousCursor = document.body.style.cursor;
    const previousUserSelect = document.body.style.userSelect;
    document.body.style.cursor = isRow ? 'col-resize' : 'row-resize';
    document.body.style.userSelect = 'none';

    const move = (event: MouseEvent) => {
      const { client, ratio: startRatio } = startPayload.current;
      const delta = isRow ? event.clientX - client : event.clientY - client;
      const size = isRow ? window.innerWidth : window.innerHeight;
      if (size <= 0) return;
      const next = Math.min(0.85, Math.max(0.15, startRatio + delta / size));
      onRatioRef.current(next);
    };
    const cleanup = () => {
      cleanupRef.current = null;
      document.body.style.cursor = previousCursor;
      document.body.style.userSelect = previousUserSelect;
      window.removeEventListener('mousemove', move);
      window.removeEventListener('mouseup', cleanup);
    };

    cleanupRef.current = cleanup;
    // mousedown 时才注册：ref 赋值不触发重渲染，无法靠 effect 依赖驱动注册
    window.addEventListener('mousemove', move);
    window.addEventListener('mouseup', cleanup);
  };

  return isRow ? (
    <div
      onMouseDown={onMouseDown}
      className="w-1.5 shrink-0 cursor-col-resize hover:bg-blue-400/60 active:bg-blue-500 transition-colors"
      title={formatMessage('拖动调整侧栏宽度')}
    />
  ) : (
    <div
      onMouseDown={onMouseDown}
      className="h-1.5 shrink-0 cursor-row-resize hover:bg-blue-400/60 active:bg-blue-500 transition-colors"
      title={formatMessage('拖动调整编辑区与响应区高度')}
    />
  );
}