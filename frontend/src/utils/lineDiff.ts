// 行级 diff 与 JSON 归一：响应对比视图（ResponseDiffDialog）的数据层。
// 零依赖 LCS 实现——行数有上限，超限返回 null，由 UI 回退为并排原文。

export type DiffRowType = 'same' | 'add' | 'del';

export interface DiffRow {
  type: DiffRowType;
  text: string;
}

// 单侧最大参与 diff 的行数（O(n*m) DP，3000² ≈ 900 万格，秒级内可完成）
export const MAX_DIFF_LINES = 3000;

function splitLines(text: string): string[] {
  if (text === '') return [];
  const lines = text.split('\n');
  // 尾部换行产生的空行不是内容
  if (lines.length > 0 && lines[lines.length - 1] === '') lines.pop();
  return lines;
}

// 统一 diff：公共行 same，仅新侧有 add，仅旧侧有 del（相邻的 del 先于 add）
export function diffLines(a: string, b: string): DiffRow[] | null {
  const oldLines = splitLines(a);
  const newLines = splitLines(b);
  if (oldLines.length > MAX_DIFF_LINES || newLines.length > MAX_DIFF_LINES) {
    // 完全相同的超限文本无需 DP，直接短路
    if (a === b) return oldLines.map((text) => ({ type: 'same' as const, text }));
    return null;
  }
  const n = oldLines.length;
  const m = newLines.length;
  // dp[i][j] = oldLines[i..] 与 newLines[j..] 的 LCS 长度
  const dp: Uint32Array[] = Array.from({ length: n + 1 }, () => new Uint32Array(m + 1));
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      dp[i][j] = oldLines[i] === newLines[j]
        ? dp[i + 1][j + 1] + 1
        : Math.max(dp[i + 1][j], dp[i][j + 1]);
    }
  }
  const rows: DiffRow[] = [];
  const pushRun = (type: DiffRowType, start: number, end: number, lines: string[]) => {
    for (let k = start; k < end; k++) rows.push({ type, text: lines[k] });
  };
  let i = 0;
  let j = 0;
  let delStart = 0;
  let addStart = 0;
  while (i < n && j < m) {
    if (oldLines[i] === newLines[j]) {
      pushRun('del', delStart, i, oldLines);
      pushRun('add', addStart, j, newLines);
      rows.push({ type: 'same', text: oldLines[i] });
      i++;
      j++;
      delStart = i;
      addStart = j;
    } else if (dp[i + 1][j] >= dp[i][j + 1]) {
      i++;
    } else {
      j++;
    }
  }
  pushRun('del', delStart, n, oldLines);
  pushRun('add', addStart, m, newLines);
  return rows;
}

// JSON 归一：解析后按（排序键、两空格缩进）重新序列化，消除格式与键序差异。
// 非 JSON 返回 null，由调用方决定回退原文。
export function normalizeJsonText(text: string): string | null {
  const trimmed = text.trim();
  if (trimmed === '' || !(trimmed.startsWith('{') || trimmed.startsWith('['))) return null;
  try {
    return JSON.stringify(sortValue(JSON.parse(trimmed)), null, 2);
  } catch {
    return null;
  }
}

function sortValue(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(sortValue);
  if (value !== null && typeof value === 'object') {
    const out: Record<string, unknown> = {};
    for (const key of Object.keys(value as Record<string, unknown>).sort()) {
      out[key] = sortValue((value as Record<string, unknown>)[key]);
    }
    return out;
  }
  return value;
}
