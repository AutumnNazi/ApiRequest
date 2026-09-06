import { describe, expect, it } from 'vitest';
import { buildAssertions, type ResponseLike } from './assertions';

const mkResponse = (over: Partial<ResponseLike> = {}): ResponseLike => ({
  status: 200,
  headers: [{ key: 'Content-Type', value: 'application/json; charset=utf-8', enabled: true }],
  bodyText: '{"id":42,"name":"alice","nested":{"a":1}}',
  durationMs: 123,
  ...over,
});

describe('buildAssertions', () => {
  it('always offers a status assertion with the actual code', () => {
    const snippets = buildAssertions(mkResponse());
    const status = snippets.find((s) => s.label.includes('状态码'))!;
    expect(status.code).toContain('pm.response.to.have.status(200)');
  });

  it('offers a content-type assertion when the header exists', () => {
    const snippets = buildAssertions(mkResponse());
    const ct = snippets.find((s) => s.label.includes('Content-Type'))!;
    expect(ct.code).toContain("to.include('application/json')");
  });

  it('offers property assertions for top-level JSON keys (capped)', () => {
    const snippets = buildAssertions(mkResponse());
    const propSnippets = snippets.filter((s) => s.label.includes('字段'));
    expect(propSnippets.map((s) => s.label)).toEqual(
      expect.arrayContaining([expect.stringContaining('id'), expect.stringContaining('name')]),
    );
    // 上限 5 个顶层键
    expect(propSnippets.length).toBeLessThanOrEqual(5);
    expect(propSnippets[0].code).toContain("to.have.property('id')");
  });

  it('skips property assertions for non-JSON bodies', () => {
    const snippets = buildAssertions(mkResponse({ bodyText: '<html></html>' }));
    expect(snippets.filter((s) => s.label.includes('字段'))).toHaveLength(0);
  });

  it('offers a duration assertion', () => {
    const snippets = buildAssertions(mkResponse());
    const dur = snippets.find((s) => s.label.includes('耗时'))!;
    expect(dur.code).toContain('pm.response.duration');
  });
});
