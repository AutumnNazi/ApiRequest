import { describe, expect, it } from 'vitest';
import {
  encodeQueryComponent,
  parseBatchLines,
  formatBatchLines,
  parseQueryPairs,
  splitUrlParts,
  syncParamsFromUrl,
  withParamsQuery,
} from './urlParams';
import type { KV } from '../ipc';

const row = (key: string, value: string, enabled = true): KV => ({ key, value, enabled });

describe('splitUrlParts', () => {
  it('splits base, query and fragment', () => {
    expect(splitUrlParts('https://api.test/p?a=1&b=2#frag')).toEqual({
      base: 'https://api.test/p',
      query: 'a=1&b=2',
      fragment: '#frag',
    });
  });

  it('keeps URLs without query or fragment intact', () => {
    expect(splitUrlParts('https://api.test/p')).toEqual({
      base: 'https://api.test/p',
      query: '',
      fragment: '',
    });
    expect(splitUrlParts('')).toEqual({ base: '', query: '', fragment: '' });
  });

  it('treats ? as query start even without a scheme (template hosts)', () => {
    expect(splitUrlParts('{{host}}/p?x=1')).toEqual({
      base: '{{host}}/p',
      query: 'x=1',
      fragment: '',
    });
  });

  it('keeps a lone ? as empty query', () => {
    expect(splitUrlParts('https://api.test/p?')).toEqual({
      base: 'https://api.test/p',
      query: '',
      fragment: '',
    });
  });
});

describe('parseQueryPairs', () => {
  it('parses order-preserving pairs including repeats', () => {
    expect(parseQueryPairs('b=2&a=1&a=3')).toEqual([
      { key: 'b', value: '2' },
      { key: 'a', value: '1' },
      { key: 'a', value: '3' },
    ]);
  });

  it('decodes percent escapes and + as space', () => {
    expect(parseQueryPairs('q=hello%20world&n=a%2Bb&t=x+y')).toEqual([
      { key: 'q', value: 'hello world' },
      { key: 'n', value: 'a+b' },
      { key: 't', value: 'x y' },
    ]);
  });

  it('keeps template variables raw', () => {
    expect(parseQueryPairs('ts={{$timestamp}}&u={{user_id}}')).toEqual([
      { key: 'ts', value: '{{$timestamp}}' },
      { key: 'u', value: '{{user_id}}' },
    ]);
  });

  it('accepts keys without = and drops empty segments', () => {
    expect(parseQueryPairs('flag&a=1&&b=')).toEqual([
      { key: 'flag', value: '' },
      { key: 'a', value: '1' },
      { key: 'b', value: '' },
    ]);
  });

  it('keeps malformed escapes raw instead of throwing', () => {
    expect(parseQueryPairs('p=50%')).toEqual([{ key: 'p', value: '50%' }]);
  });
});

describe('encodeQueryComponent', () => {
  it('encodes structurally dangerous characters', () => {
    expect(encodeQueryComponent('a b+c&d#e%f')).toBe('a%20b%2Bc%26d%23e%25f');
  });

  it('keeps template variables untouched', () => {
    expect(encodeQueryComponent('{{$guid}}')).toBe('{{$guid}}');
    expect(encodeQueryComponent('pre-{{var}}-post')).toBe('pre-{{var}}-post');
  });

  it('keeps unreserved characters readable', () => {
    expect(encodeQueryComponent('k-/?:=,._~')).toBe('k-/?:=,._~');
  });
});

describe('withParamsQuery', () => {
  it('serializes enabled rows in order and preserves base and fragment', () => {
    const url = withParamsQuery('https://api.test/p#f', [
      row('b', '2'),
      row('a', '1 x'),
      row('c', 'off', false),
    ]);
    expect(url).toBe('https://api.test/p?b=2&a=1%20x#f');
  });

  it('strips the query entirely when no enabled rows exist', () => {
    expect(withParamsQuery('https://api.test/p?a=1#f', [row('a', '1', false)])).toBe(
      'https://api.test/p#f',
    );
    expect(withParamsQuery('https://api.test/p?a=1', [])).toBe('https://api.test/p');
  });

  it('keeps template values raw', () => {
    expect(withParamsQuery('{{host}}', [row('q', '{{search}}')])).toBe('{{host}}?q={{search}}');
  });

  it('encodes special characters in values', () => {
    expect(withParamsQuery('https://x.test', [row('k', 'a&b+c')])).toBe('https://x.test?k=a%26b%2Bc');
  });

  it('round-trips parsed pairs back to the same URL', () => {
    const raw = 'https://x.test/p?a=1&b=hello%20world&k=#z';
    const { base, query, fragment } = splitUrlParts(raw);
    const pairs = parseQueryPairs(query);
    const rebuilt = withParamsQuery(
      base + (query ? '?' + query : '') + fragment,
      pairs.map((p) => row(p.key, p.value)),
    );
    expect(rebuilt).toBe(raw);
  });

  it('normalizes a bare key without = into key= on table edits (semantically identical)', () => {
    const { base, query, fragment } = splitUrlParts('https://x.test/p?flag#z');
    const pairs = parseQueryPairs(query);
    expect(withParamsQuery(base + '?' + query + fragment, pairs.map((p) => row(p.key, p.value)))).toBe(
      'https://x.test/p?flag=#z',
    );
  });
});

describe('syncParamsFromUrl', () => {
  it('returns null when only the base or fragment changed', () => {
    expect(syncParamsFromUrl('https://x.test/p?a=1', 'https://y.test/p?a=1', [row('a', '1')])).toBeNull();
    expect(syncParamsFromUrl('https://x.test/p?a=1#f', 'https://x.test/p?a=1#g', [row('a', '1')])).toBeNull();
  });

  it('parses the new query into rows when the query changed', () => {
    const next = syncParamsFromUrl('https://x.test/p?a=1', 'https://x.test/p?a=1&b=2', [row('a', '1')]);
    expect(next).toEqual([row('a', '1'), row('b', '2')]);
  });

  it('clears the table when the query is deleted from the URL', () => {
    const next = syncParamsFromUrl('https://x.test/p?a=1&b=2', 'https://x.test/p', [
      row('a', '1'),
      row('b', '2'),
    ]);
    expect(next).toEqual([]);
  });

  it('replaces rows the user deleted from the query text', () => {
    const next = syncParamsFromUrl('https://x.test/p?a=1&b=2', 'https://x.test/p?a=2', [
      row('a', '1'),
      row('b', '2'),
    ]);
    expect(next).toEqual([row('a', '2')]);
  });

  it('preserves disabled rows whose key is absent from the new query', () => {
    const next = syncParamsFromUrl('https://x.test/p?a=1', 'https://x.test/p?a=1&c=3', [
      row('a', '1'),
      row('b', '2', false),
    ]);
    expect(next).toEqual([row('a', '1'), row('c', '3'), row('b', '2', false)]);
  });

  it('drops a disabled duplicate when the query re-adds its key', () => {
    const next = syncParamsFromUrl('https://x.test/p?a=1', 'https://x.test/p?a=2', [
      row('a', '1'),
      row('a', '5', false),
    ]);
    expect(next).toEqual([row('a', '2')]);
  });

  it('merges into existing rows instead of replacing when the URL previously had no query', () => {
    const next = syncParamsFromUrl('https://x.test/p', 'https://x.test/p?b=2', [
      row('a', '1'),
      row('b', '3', false),
    ]);
    // 旧 URL 无查询串：视作表格为主的存量状态，追加而非清空
    expect(next).toEqual([row('a', '1'), row('b', '3', false), row('b', '2')]);
  });

  it('keeps disabled rows when the query is deleted from the URL', () => {
    const next = syncParamsFromUrl('https://x.test/p?a=1', 'https://x.test/p', [
      row('a', '1'),
      row('b', '2', false),
    ]);
    expect(next).toEqual([row('b', '2', false)]);
  });
});

describe('batch edit text', () => {
  it('formats rows as key:value lines with // prefix for disabled rows', () => {
    expect(formatBatchLines([row('Accept', 'application/json'), row('Cache-Control', 'no-cache', false)])).toBe(
      'Accept:application/json\n//Cache-Control:no-cache',
    );
  });

  it('parses colon or equals separated lines and preserves order', () => {
    expect(parseBatchLines('Accept:application/json\nlimit=10\n\n//X-Debug:1')).toEqual([
      { key: 'Accept', value: 'application/json', enabled: true },
      { key: 'limit', value: '10', enabled: true },
      { key: 'X-Debug', value: '1', enabled: false },
    ]);
  });

  it('splits on the first separator only so values may contain : or =', () => {
    expect(parseBatchLines('Referer:https://a.test/x?b=1')).toEqual([
      { key: 'Referer', value: 'https://a.test/x?b=1', enabled: true },
    ]);
  });

  it('drops lines without a separator', () => {
    expect(parseBatchLines('Accept:application/json\nbroken')).toEqual([
      { key: 'Accept', value: 'application/json', enabled: true },
    ]);
  });

  it('round-trips format through parse', () => {
    const rows = [row('a', '1'), row('b', 'two words', false)];
    expect(parseBatchLines(formatBatchLines(rows))).toEqual(rows);
  });
});
