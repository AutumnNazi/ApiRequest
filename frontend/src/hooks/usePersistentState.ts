import { useCallback, useRef, useState } from 'react';

// localStorage 持久化的 useState：读失败/损坏回落默认值，写入失败静默。
export function usePersistentState<T>(key: string, initial: T): [T, (v: T) => void] {
  const [state, setState] = useState<T>(() => {
    try {
      const raw = localStorage.getItem(key);
      return raw == null ? initial : (JSON.parse(raw) as T);
    } catch {
      return initial;
    }
  });

  const set = useCallback(
    (next: T) => {
      setState(next);
      try {
        localStorage.setItem(key, JSON.stringify(next));
      } catch {
        // 持久化不可用时仅保留会话内状态
      }
    },
    [key],
  );

  // key 变化时重置（理论上固定 key，防御性处理）
  const keyRef = useRef(key);
  if (keyRef.current !== key) {
    keyRef.current = key;
    setState(initial);
  }
  return [state, set];
}