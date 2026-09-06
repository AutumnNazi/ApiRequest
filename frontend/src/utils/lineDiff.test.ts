import { describe, expect, it } from 'vitest';
import { diffLines, normalizeJsonText, MAX_DIFF_LINES } from './lineDiff';

describe('diffLines', () => {
  it('identical texts produce only same rows', () => {
    const rows = diffLines('a\nb\nc', 'a\nb\nc') ?? [];
    expect(rows.map((r) => r.type)).toEqual(['same', 'same', 'same']);
  });

  it('detects a changed line as del followed by add', () => {
    const rows = diffLines('a\nb\nc', 'a\nX\nc') ?? [];
    expect(rows.map((r) => r.type)).toEqual(['same', 'del', 'add', 'same']);
    expect(rows[1].text).toBe('b');
    expect(rows[2].text).toBe('X');
  });

  it('detects an inserted line in the middle', () => {
    const rows = diffLines('a\nc', 'a\nb\nc') ?? [];
    expect(rows.map((r) => r.type)).toEqual(['same', 'add', 'same']);
  });

  it('detects a removed line', () => {
    const rows = diffLines('a\nb\nc', 'a\nc') ?? [];
    expect(rows.map((r) => r.type)).toEqual(['same', 'del', 'same']);
  });

  it('handles empty inputs', () => {
    expect(diffLines('', 'a\nb')!.map((r) => r.type)).toEqual(['add', 'add']);
    expect(diffLines('a\nb', '')!.map((r) => r.type)).toEqual(['del', 'del']);
    expect(diffLines('', '')).toEqual([]);
  });

  it('prefers stable output when content moves', () => {
    // LCS：公共行尽量对齐，避免整体逐行标记为全换
    const rows = diffLines('x\ncommon\ny', 'a\ncommon\nb') ?? [];
    expect(rows.some((r) => r.type === 'same' && r.text === 'common')).toBe(true);
  });

  it('returns null when either side exceeds the line cap', () => {
    const big = Array.from({ length: MAX_DIFF_LINES + 1 }, (_, i) => `l${i}`).join('\n');
    expect(diffLines('a', big)).toBeNull();
    expect(diffLines(big, 'a')).toBeNull();
    expect(diffLines(big, big)).not.toBeNull(); // 超限但完全相同仍可安全短路
  });
});

describe('normalizeJsonText', () => {
  it('pretty-prints JSON so formatting differences disappear', () => {
    const a = normalizeJsonText('{"a":1,"b":[2,3]}');
    const b = normalizeJsonText('{\n  "b": [2, 3],\n "a": 1\n}');
    expect(a).toBe(b);
    expect(a).toContain('"a": 1');
  });

  it('keeps key order differences via sorted serialization', () => {
    // 排序键归一：仅键序不同视为相同
    const a = normalizeJsonText('{"x":1,"y":2}');
    const b = normalizeJsonText('{"y":2,"x":1}');
    expect(a).toBe(b);
  });

  it('returns null for non-JSON text', () => {
    expect(normalizeJsonText('plain text')).toBeNull();
    expect(normalizeJsonText('{"a":')).toBeNull();
  });
});
