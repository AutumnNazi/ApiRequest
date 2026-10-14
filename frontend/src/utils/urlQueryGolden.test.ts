// Golden 向量共享测试：与 backend/requesturl/query_golden_test.go 共读
// testdata/url_query_golden.json，锁定 UI 表格显示与引擎解析的一致性。
// 前端侧按表格行的有序键值对断言（表格顺序即用户所见顺序）。
import { describe, expect, it } from 'vitest';
import { parseQueryPairs } from './urlParams';
import golden from '../../../testdata/url_query_golden.json';

interface GoldenFile {
  cases: Array<{ name: string; query: string; pairs: string[][] }>;
}

const vectors = golden as GoldenFile;

describe('url query golden vectors（双端共享）', () => {
  it('向量文件非空（防止文件被清空后测试空转）', () => {
    expect(vectors.cases.length).toBeGreaterThan(0);
  });

  it.each(vectors.cases.map((c) => [c.name, c] as const))('%s', (_name, tc) => {
    expect(parseQueryPairs(tc.query)).toEqual(
      tc.pairs.map(([key, value]) => ({ key, value })),
    );
  });
});
