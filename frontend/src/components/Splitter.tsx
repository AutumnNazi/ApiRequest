// 可拖拽分割条：分隔上下（horizontal）或左右（vertical）两个面板。
// 通过 onRatio 回调向上反馈比例（0~1），由父层持久化。
import { useCallback, useEffect, useRef } from 'react';

interface Props {
  orientation: 'horizontal' | 'vertical';
  onRatio(ratio: number): void;
  ratio: number; // 当前比例，用于限制拖拽方向相对初始点
}

export default function Splitter({ orientation, onRatio, ratio }: Props) {
  const dragging = useRef(false);
  const startPayload = useRef({ client: 0, ratio });
  const onRatioRef = useRef(onRatio);
  onRatioRef.current = onRatio;

  const isRow = orientation === 'vertical'; // 侧栏 → 左右分隔，容器为 row

  useEffect(() => {
    const move = (e: MouseEvent) => {
      if (!dragging.current) return;
      const { client, ratio: startRatio } = startPayload.current;
      const delta = isRow ? e.clientX - client : e.clientY - client;
      const size = isRow ? window.innerWidth : window.innerHeight;
      if (size <= 0) return;
      const next = Math.min(0.85, Math.max(0.15, startRatio + delta / size));
      onRatioRef.current(next);
    };
    const up = () => {
      if (!dragging.current) return;
      dragging.current = false;
      document.body.style.cursor = '';
      document.body.style.userSelect = '';
      window.removeEventListener('mousemove', move);
      window.removeEventListener('mouseup', up);
    };
    if (dragging.current) {
      window.addEventListener('mousemove', move);
      window.addEventListener('mouseup', up);
    }
    return () => {
      window.removeEventListener('mousemove', move);
      window.removeEventListener('mouseup', up);
    };
  }, [isRow]);

  const onMouseDown = (e: React.MouseEvent) => {
    e.preventDefault();
    dragging.current = true;
    startPayload.current = { client: isRow ? e.clientX : e.clientY, ratio };
    document.body.style.cursor = isRow ? 'col-resize' : 'row-resize';
    document.body.style.userSelect = 'none';
  };

  return isRow ? (
    <div
      onMouseDown={onMouseDown}
      className="w-1.5 shrink-0 cursor-col-resize hover:bg-blue-400/60 active:bg-blue-500 transition-colors"
      title="拖动调整侧栏宽度"
    />
  ) : (
    <div
      onMouseDown={onMouseDown}
      className="h-1.5 shrink-0 cursor-row-resize hover:bg-blue-400/60 active:bg-blue-500 transition-colors"
      title="拖动调整编辑区与响应区高度"
    />
  );
}