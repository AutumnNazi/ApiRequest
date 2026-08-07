// AppError 解析：Wails 把 Go error 序列化为字符串 rejection，
// 后端保证其为 AppError 的 JSON（docs/api-contract.md §2）。
export type ErrorKind =
  | 'network'
  | 'tls'
  | 'script'
  | 'storage'
  | 'import'
  | 'validation'
  | 'unknown';

export interface AppError {
  kind: ErrorKind;
  detail: string;
  phase?: string;
  line?: number;
  format?: string;
}

export function toAppError(e: unknown): AppError {
  // 已经是 AppError 对象（call() 包装器抛出的）
  if (e && typeof e === 'object' && 'kind' in e && 'detail' in e
    && typeof (e as AppError).kind === 'string' && typeof (e as AppError).detail === 'string') {
    return e as AppError;
  }
  const text = typeof e === 'string' ? e : e instanceof Error ? e.message : String(e);
  try {
    const parsed = JSON.parse(text);
    if (parsed && typeof parsed.kind === 'string' && typeof parsed.detail === 'string') {
      return parsed as AppError;
    }
  } catch {
    // 非 JSON：包装为 unknown
  }
  return { kind: 'unknown', detail: text };
}

/** 包一层调用：把 rejection 统一转为 AppError 再抛出 */
export async function call<T>(fn: () => Promise<T>): Promise<T> {
  try {
    return await fn();
  } catch (e) {
    throw toAppError(e);
  }
}
