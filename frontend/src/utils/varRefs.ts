// 变量引用收集：扫描请求文本字段中的 {{var}}，跳过 {{$dynamic}} 动态变量
import type { KV, Body } from '../ipc';

const VAR_REF_RE = /\{\{\s*([^}]+?)\s*\}\}/g;

interface DraftLike {
  url?: string;
  params?: KV[];
  headers?: KV[];
  body?: Body;
  auth?: { params?: Record<string, string> };
}

/** 收集请求中引用的全部变量名（去重，不含 {{$...}} 动态变量） */
export function collectVarRefs(draft: DraftLike): string[] {
  const set = new Set<string>();
  const scan = (s?: string) => {
    if (!s) return;
    for (const m of s.matchAll(VAR_REF_RE)) {
      const name = m[1].trim();
      if (name && !name.startsWith('$')) set.add(name);
    }
  };
  scan(draft.url);
  for (const kv of draft.params ?? []) {
    if (kv.enabled === false) continue;
    scan(kv.key);
    scan(kv.value);
  }
  for (const kv of draft.headers ?? []) {
    if (kv.enabled === false) continue;
    scan(kv.key);
    scan(kv.value);
  }
  const b = draft.body;
  if (b) {
    if (b.kind === 'raw') scan(b.text);
    else if (b.kind === 'graphql') {
      scan(b.query);
      scan(b.variables);
    } else if (b.kind === 'binary') scan(b.path);
    else if (b.kind === 'formdata' || b.kind === 'urlencoded') {
      for (const it of b.items ?? []) {
        if (it.enabled === false) continue;
        scan(it.key);
        scan(it.value);
        scan(it.path);
      }
    }
  }
  for (const v of Object.values(draft.auth?.params ?? {})) scan(v);
  return [...set];
}
