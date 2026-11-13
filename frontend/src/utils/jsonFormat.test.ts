import { describe, expect, it } from 'vitest';
import { compressJsonText, formatJsonText, tryFormatJsonText } from './jsonFormat';

describe('formatJsonText', () => {
  it('matches native pretty-printing for safe values', () => {
    const source = '{"b":[1,2],"a":{"c":true,"d":null}}';
    expect(formatJsonText(source)).toBe(JSON.stringify(JSON.parse(source), null, 2));
  });

  it('keeps integers beyond 2^53-1 exact', () => {
    const source = '{"id":1547040075470688258}';
    const formatted = formatJsonText(source);
    expect(formatted).toBe('{\n  "id": 1547040075470688258\n}');
    // 反例守卫：原生解析会把雪花 ID 变成 1547040075470688300
    expect(formatted).not.toContain('1547040075470688300');
  });

  it('indents nested containers and inlines empty ones', () => {
    expect(formatJsonText('{"a":{"b":[1]}}')).toBe('{\n  "a": {\n    "b": [\n      1\n    ]\n  }\n}');
    expect(formatJsonText('{"a":{},"b":[]}')).toBe('{\n  "a": {},\n  "b": []\n}');
  });

  it('preserves string escapes and their inner whitespace verbatim', () => {
    expect(formatJsonText('{"s":"a \\"b\\" c\\n\\u0041"}')).toBe('{\n  "s": "a \\"b\\" c\\n\\u0041"\n}');
  });

  it('throws on invalid JSON like the native parser', () => {
    expect(() => formatJsonText('{"a":')).toThrow();
    expect(() => formatJsonText('')).toThrow();
  });
});

describe('compressJsonText', () => {
  it('strips insignificant whitespace but keeps big integers', () => {
    expect(compressJsonText('{\n  "id": 1547040075470688258,\n  "items": [ 1, 2 ]\n}')).toBe(
      '{"id":1547040075470688258,"items":[1,2]}',
    );
  });

  it('throws on invalid JSON', () => {
    expect(() => compressJsonText('[1,2')).toThrow();
  });
});

describe('tryFormatJsonText', () => {
  it('returns null for non-JSON input instead of throwing', () => {
    expect(tryFormatJsonText('plain text')).toBeNull();
    expect(tryFormatJsonText('{"a":')).toBeNull();
  });

  it('formats valid JSON input', () => {
    expect(tryFormatJsonText('[1]')).toBe('[\n  1\n]');
  });
});
