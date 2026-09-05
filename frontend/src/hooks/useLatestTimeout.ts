import { useCallback, useEffect, useRef } from 'react';

// 组件作用域的延迟回调定时器：重入时清掉上一个，卸载时必然清理。
// 事件处理器里裸调 setTimeout 会让定时器逃逸出组件生命周期——
// 测试环境销毁后回调再触发 setState 会直接崩溃（CI 上 window 已被移除）。
export function useLatestTimeout() {
  const ref = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => {
    if (ref.current) clearTimeout(ref.current);
  }, []);
  return useCallback((fn: () => void, ms: number) => {
    if (ref.current) clearTimeout(ref.current);
    ref.current = setTimeout(fn, ms);
  }, []);
}
