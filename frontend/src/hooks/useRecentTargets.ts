// 最近使用的协议目标地址：localStorage 记录，供 WS/gRPC/GraphQL 面板复用。
// 仅存字符串列表（上限 recentLimit），无敏感信息。
import { useCallback, useEffect, useState } from 'react';
import { sanitizeRecentTarget } from '../utils/request';

const RECENT_LIMIT = 8;

function readList(key: string): string[] {
  try {
    const raw = localStorage.getItem(key);
    const parsed = raw == null ? [] : (JSON.parse(raw) as unknown);
    if (!Array.isArray(parsed)) return [];
    const sanitized = parsed
      .filter((x): x is string => typeof x === 'string')
      .map(sanitizeRecentTarget)
      .filter((x): x is string => Boolean(x));
    const next = [...new Set(sanitized)].slice(0, RECENT_LIMIT);
    const serialized = JSON.stringify(next);
    if (serialized !== raw) localStorage.setItem(key, serialized);
    return next;
  } catch {
    return [];
  }
}

/** 读取最近地址列表（localStorage），写入失败静默 */
export function useRecentTargets(key: string): { recents: string[]; recall(label: string): void } {
  const [recents, setRecents] = useState<string[]>(() => readList(key));

  useEffect(() => {
    const onStorage = () => setRecents(readList(key));
    window.addEventListener('storage', onStorage);
    return () => window.removeEventListener('storage', onStorage);
  }, [key]);

  /** 记入最近列表：去重 + 头部插入 + 截断 */
  const recall = useCallback(
    (label: string) => {
      const sanitized = sanitizeRecentTarget(label);
      if (!sanitized) return;
      setRecents((prev) => {
        const next = [sanitized, ...prev.filter((x) => x !== sanitized)].slice(0, RECENT_LIMIT);
        try {
          localStorage.setItem(key, JSON.stringify(next));
        } catch {
          // 持久化不可用时仅保留会话内状态
        }
        return next;
      });
    },
    [key],
  );

  return { recents, recall };
}
