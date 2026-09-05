// 零依赖虚拟滚动列表：绝对定位可视窗口 + 撑高 spacer，配合 LoadMoreTrigger 触底加载。
// jsdom 无布局：clientHeight/scrollTop 由测试注入，滚动完全由事件驱动（不轮询）。
import { useEffect, useRef, useState, type ReactNode } from 'react';

export interface VisibleRange {
  start: number;
  end: number;
}

/** 计算可视窗口（含两端 overscan），越界输入安全收敛。 */
export function computeVisibleRange(
  scrollTop: number,
  viewportHeight: number,
  itemCount: number,
  rowHeight: number,
  overscan: number,
): VisibleRange {
  if (itemCount <= 0 || rowHeight <= 0) return { start: 0, end: 0 };
  const height = Math.max(0, viewportHeight);
  // 内容变短时浏览器会把 scrollTop 收敛到 maxScroll，窗口计算内置同样语义，
  // 避免条目数骤减（如搜索过滤）后拿旧 scrollTop 算出空窗口
  const maxScroll = Math.max(0, itemCount * rowHeight - height);
  const top = Math.min(Math.max(0, scrollTop), maxScroll);
  const first = Math.floor(top / rowHeight);
  const visibleCount = Math.ceil(height / rowHeight) + 1;
  const start = Math.max(0, first - overscan);
  const end = Math.min(itemCount, first + visibleCount + overscan);
  return { start, end: Math.max(start, end) };
}

interface Props<T> {
  items: T[];
  rowHeight: number;
  overscan?: number;
  renderItem(item: T, index: number): ReactNode;
  /** 滚动区末尾的固定内容（如 LoadMoreTrigger）；跟在虚拟行之后。 */
  footer?: ReactNode;
  /** 列表内容变化时回调（首次布局 + 每次滚动）。 */
  onRangeChange?(range: VisibleRange): void;
  className?: string;
}

export default function VirtualRowList<T>({
  items,
  rowHeight,
  overscan = 5,
  renderItem,
  footer,
  onRangeChange,
  className,
}: Props<T>) {
  const scrollerRef = useRef<HTMLDivElement>(null);
  const [range, setRange] = useState<VisibleRange>({ start: 0, end: 0 });

  const recompute = () => {
    const el = scrollerRef.current;
    if (!el) return;
    const next = computeVisibleRange(el.scrollTop, el.clientHeight, items.length, rowHeight, overscan);
    setRange((prev) => (prev.start === next.start && prev.end === next.end ? prev : next));
  };

  // 条数变化（翻页追加/搜索过滤）时重算窗口，用当前 scrollTop 作锚点
  useEffect(recompute, [items.length, rowHeight, overscan]);

  useEffect(() => {
    if (onRangeChange) onRangeChange(range);
  }, [range, onRangeChange]);

  return (
    <div
      data-virtual-scroller
      className={className ?? 'h-full overflow-auto'}
      ref={scrollerRef}
      onScroll={recompute}
    >
      <div data-virtual-spacer style={{ height: `${items.length * rowHeight}px`, position: 'relative' }}>
        {items.slice(range.start, range.end).map((item, offset) => {
          const index = range.start + offset;
          return (
            <div
              key={index}
              style={{
                position: 'absolute',
                top: `${index * rowHeight}px`,
                left: 0,
                right: 0,
                height: `${rowHeight}px`,
              }}
            >
              {renderItem(item, index)}
            </div>
          );
        })}
        {/* footer 紧跟最后一行之后；spacer 高度不含 footer，由内容自身撑开 */}
        <div style={{ position: 'absolute', top: `${items.length * rowHeight}px`, left: 0, right: 0 }}>
          {footer}
        </div>
      </div>
    </div>
  );
}
