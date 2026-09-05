import { describe, expect, it } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { useEffect, useRef } from 'react';
import { computeVisibleRange } from './VirtualRowList';
import VirtualRowList from './VirtualRowList';

describe('computeVisibleRange', () => {
  it('returns the window covering the viewport plus overscan on both ends', () => {
    expect(computeVisibleRange(0, 400, 1000, 40, 5)).toEqual({ start: 0, end: 16 });
    expect(computeVisibleRange(400, 400, 1000, 40, 5)).toEqual({ start: 5, end: 26 });
  });

  it('clamps to the item bounds', () => {
    expect(computeVisibleRange(39600, 400, 1000, 40, 5)).toEqual({ start: 985, end: 1000 });
    expect(computeVisibleRange(0, 400, 6, 40, 5)).toEqual({ start: 0, end: 6 });
  });

  it('survives zero viewport and out-of-range scroll positions', () => {
    expect(computeVisibleRange(0, 0, 1000, 40, 5)).toEqual({ start: 0, end: 6 });
    expect(computeVisibleRange(-80, 400, 1000, 40, 5)).toEqual({ start: 0, end: 16 });
    expect(computeVisibleRange(0, 400, 0, 40, 5)).toEqual({ start: 0, end: 0 });
    // 滚动位置超出内容高度（条目数骤减后的旧 scrollTop）：收敛到尾部窗口
    expect(computeVisibleRange(5000, 400, 100, 40, 5)).toEqual({ start: 85, end: 100 });
  });
});

const RowHeight = 40;

function Harness({ count, footer }: { count: number; footer?: boolean }) {
  const items = Array.from({ length: count }, (_, i) => i);
  return (
    <div style={{ height: 600 }}>
      <VirtualRowList
        items={items}
        rowHeight={RowHeight}
        overscan={5}
        renderItem={(i) => (
          <div data-testid={`row-${i}`} style={{ height: RowHeight }}>row {i}</div>
        )}
        footer={footer ? <div data-testid="footer">footer</div> : undefined}
      />
    </div>
  );
}

// jsdom 没有布局：给滚动容器一个可写的 clientHeight
function withViewportHeight(view: ReturnType<typeof render>, height: number) {
  const scroller = view.container.querySelector('[data-virtual-scroller]') as HTMLElement;
  Object.defineProperty(scroller, 'clientHeight', { value: height, configurable: true });
  Object.defineProperty(scroller, 'scrollTop', {
    value: 0,
    writable: true,
    configurable: true,
  });
  return scroller;
}

function setScrollTop(scroller: HTMLElement, top: number) {
  (scroller as HTMLElement & { scrollTop: number }).scrollTop = top;
  fireEvent.scroll(scroller);
}

describe('VirtualRowList', () => {
  it('renders only the visible window instead of every item', () => {
    const view = render(<Harness count={1000} />);
    const scroller = withViewportHeight(view, 400);
    // 初次挂载时用 scrollTop=0 计算：0..15
    const rows = view.container.querySelectorAll('[data-testid^="row-"]');
    expect(rows.length).toBeLessThanOrEqual(25);
    expect(view.container.querySelectorAll('[data-testid^="row-"]').length).toBeLessThan(1000);
  });

  it('moves the window when scrolling and keeps a spacer for the full height', () => {
    const view = render(<Harness count={1000} />);
    const scroller = withViewportHeight(view, 400);
    setScrollTop(scroller, 4000);
    const rows = Array.from(view.container.querySelectorAll<HTMLElement>('[data-testid^="row-"]'));
    expect(rows.length).toBeLessThanOrEqual(25);
    const indexes = rows.map((r) => Number(r.dataset.testid!.slice(4)));
    expect(indexes[0]).toBe(95);
    expect(indexes.at(-1)).toBe(115);
    // spacer 高度 = 总条数 × 行高，保证滚动条与 LoadMore 触底语义
    const spacer = view.container.querySelector('[data-virtual-spacer]') as HTMLElement;
    expect(spacer.style.height).toBe(`${1000 * RowHeight}px`);
  });

  it('keeps anchor positions stable when the item count shrinks (search filtering)', () => {
    const view = render(<Harness count={1000} />);
    const scroller = withViewportHeight(view, 400);
    setScrollTop(scroller, 8000);
    view.rerender(<Harness count={20} />);
    const indexes = Array.from(view.container.querySelectorAll<HTMLElement>('[data-testid^="row-"]')).map((r) =>
      Number(r.dataset.testid!.slice(4)),
    );
    expect(indexes.at(-1)).toBe(19);
    expect(indexes.length).toBeLessThanOrEqual(20);
  });

  it('places the footer right after the last virtual row', () => {
    const view = render(<Harness count={10} footer />);
    const scroller = withViewportHeight(view, 400);
    const spacer = view.container.querySelector('[data-virtual-spacer]') as HTMLElement;
    expect(spacer.querySelector('[data-testid="footer"]')).not.toBeNull();
    expect(spacer.style.height).toBe(`${10 * RowHeight}px`);
  });

  it('renders nothing but the spacer for an empty list', () => {
    const view = render(<Harness count={0} />);
    const spacer = view.container.querySelector('[data-virtual-spacer]') as HTMLElement;
    expect(spacer.style.height).toBe('0px');
    expect(view.container.querySelectorAll('[data-testid^="row-"]')).toHaveLength(0);
  });
});

describe('VirtualRowList measurement fallback', () => {
  it('re-reads scrollTop via onScroll events only (no polling)', () => {
    // 滚动由事件驱动：组件不轮询 scrollTop，事件里读到什么算什么
    const view = render(<Harness count={100} />);
    const scroller = withViewportHeight(view, 200);
    setScrollTop(scroller, 1200);
    const indexes = Array.from(view.container.querySelectorAll<HTMLElement>('[data-testid^="row-"]')).map((r) =>
      Number(r.dataset.testid!.slice(4)),
    );
    expect(indexes[0]).toBe(25);
  });
});
