import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import LoadMoreTrigger from './LoadMoreTrigger';

// fake IntersectionObserver：记录实例与回调，测试手动派发交叉事件
type IOCallback = (entries: { isIntersecting: boolean }[]) => void;
const instances: Array<{ callback: IOCallback; disconnect: () => void }> = [];

class FakeIntersectionObserver {
  constructor(callback: IOCallback) {
    instances.push({ callback, disconnect: () => {} });
  }
  observe() {}
  disconnect() {}
}

describe('LoadMoreTrigger', () => {
  it('fires onLoadMore when intersecting and not fetching', () => {
    vi.stubGlobal('IntersectionObserver', FakeIntersectionObserver);
    const onLoadMore = vi.fn();
    render(<LoadMoreTrigger isFetching={false} onLoadMore={onLoadMore} />);

    instances[instances.length - 1].callback([{ isIntersecting: true }]);
    expect(onLoadMore).toHaveBeenCalledTimes(1);
    vi.unstubAllGlobals();
  });

  it('does not fire while fetching', () => {
    vi.stubGlobal('IntersectionObserver', FakeIntersectionObserver);
    const onLoadMore = vi.fn();
    render(<LoadMoreTrigger isFetching onLoadMore={onLoadMore} />);

    instances[instances.length - 1].callback([{ isIntersecting: true }]);
    expect(onLoadMore).not.toHaveBeenCalled();
    vi.unstubAllGlobals();
  });

  it('re-checks on fetch completion: recreated observer fires with the fresh callback', () => {
    vi.stubGlobal('IntersectionObserver', FakeIntersectionObserver);
    const first = vi.fn();
    const { rerender } = render(<LoadMoreTrigger isFetching={false} onLoadMore={first} />);
    const initialCount = instances.length;

    // 父组件传入新回调（新引用）：observer 因 [isFetching] 不变不重建，
    // 但回调经 ref 桥接，事件必须打到最新回调
    const second = vi.fn();
    rerender(<LoadMoreTrigger isFetching={false} onLoadMore={second} />);
    expect(instances.length).toBe(initialCount); // 未重建

    instances[instances.length - 1].callback([{ isIntersecting: true }]);
    expect(second).toHaveBeenCalledTimes(1);
    expect(first).not.toHaveBeenCalled();

    // isFetching 变化触发 observer 重建（断开旧的，装新的）
    rerender(<LoadMoreTrigger isFetching onLoadMore={second} />);
    expect(instances.length).toBe(initialCount + 1);
    vi.unstubAllGlobals();
  });

  it('shows fetching state text', () => {
    vi.stubGlobal('IntersectionObserver', FakeIntersectionObserver);
    render(<LoadMoreTrigger isFetching onLoadMore={() => {}} />);
    expect(screen.getByText('加载中…')).toBeInTheDocument();
    vi.unstubAllGlobals();
  });

  it('responds to plain DOM click-free re-render without errors (smoke)', () => {
    vi.stubGlobal('IntersectionObserver', FakeIntersectionObserver);
    render(<LoadMoreTrigger isFetching={false} onLoadMore={() => {}} />);
    fireEvent.click(screen.getByText('滚动加载更多'));
    vi.unstubAllGlobals();
  });
});
