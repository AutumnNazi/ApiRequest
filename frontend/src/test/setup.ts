import '@testing-library/jest-dom/vitest';
import { cleanup } from '@testing-library/react';
import { afterEach } from 'vitest';

// jsdom 未实现 scrollIntoView（消息时间线等自动滚底功能依赖）
if (typeof Element !== 'undefined' && !Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = () => {};
}

// jsdom 未实现 IntersectionObserver（LoadMoreTrigger 无限滚动触发依赖）。
// 需要派发交叉事件的测试在本地用 vi.stubGlobal 覆盖此无操作实现。
if (typeof window !== 'undefined' && !('IntersectionObserver' in window)) {
  class NoopIntersectionObserver {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
  // @ts-expect-error 测试桩与规范接口不完整等价
  window.IntersectionObserver = NoopIntersectionObserver;
}

afterEach(() => cleanup());
