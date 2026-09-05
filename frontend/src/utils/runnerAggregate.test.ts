import { describe, expect, it } from 'vitest';
import { aggregateAssertions, type AggregateRow } from './runnerAggregate';
import { type RunnerReport } from '../ipc';

// Wails 生成的 Report 类带类方法，纯数据对象经 as 断言即可
const report = (results: Array<Partial<RunnerReport['results'][number]>>): RunnerReport =>
  ({
    runId: 'r',
    total: results.length,
    passed: 0,
    failed: 0,
    skipped: 0,
    durationMs: 0,
    canceled: false,
    results,
  }) as unknown as RunnerReport;

describe('aggregateAssertions', () => {
  it('groups assertion results by name with pass/fail counts and failing request names', () => {
    const r = report([
      {
        iteration: 1, requestName: 'login', nodeId: 'a', status: 200, durationMs: 1, failed: false,
        testResults: [
          { name: 'status is 2xx', pass: true },
          { name: 'body has token', pass: true },
        ],
      },
      {
        iteration: 2, requestName: 'login', nodeId: 'a', status: 500, durationMs: 1, failed: true, error: 'assert failed',
        testResults: [
          { name: 'status is 2xx', pass: false, error: '500 != 2xx' },
          { name: 'body has token', pass: false, error: 'token missing' },
        ],
      },
      {
        iteration: 1, requestName: 'list', nodeId: 'b', status: 200, durationMs: 1, failed: false,
        testResults: [{ name: 'status is 2xx', pass: true }],
      },
    ]);
    const rows = aggregateAssertions(r);
    expect(rows).toEqual<AggregateRow[]>([
      { name: 'status is 2xx', total: 3, passed: 2, failed: 1, failingRequests: ['login'] },
      { name: 'body has token', total: 2, passed: 1, failed: 1, failingRequests: ['login'] },
    ]);
  });

  it('sorts failed-first, then by total runs, then by name', () => {
    const r = report([
      {
        iteration: 1, requestName: 'x', nodeId: 'a', status: 200, durationMs: 1, failed: false,
        testResults: [
          { name: 'aaa', pass: false },
          { name: 'bbb', pass: false },
          { name: 'ccc', pass: true },
          { name: 'ddd', pass: false },
        ],
      },
      {
        iteration: 1, requestName: 'y', nodeId: 'b', status: 200, durationMs: 1, failed: false,
        testResults: [
          { name: 'bbb', pass: true },
          { name: 'eee', pass: false },
        ],
      },
    ]);
    const rows = aggregateAssertions(r);
    expect(rows.map((row) => `${row.name}:${row.failed}/${row.total}`)).toEqual([
      'bbb:1/2', // 有失败 & 总次数最高 → 第一
      'aaa:1/1',
      'ddd:1/1',
      'eee:1/1',
      'ccc:0/1', // 全过的排最后
    ]);
  });

  it('dedupes failing request names across iterations', () => {
    const r = report([
      {
        iteration: 1, requestName: 'same', nodeId: 'a', status: 500, durationMs: 1, failed: true,
        testResults: [{ name: 't', pass: false }],
      },
      {
        iteration: 2, requestName: 'same', nodeId: 'a', status: 500, durationMs: 1, failed: true,
        testResults: [{ name: 't', pass: false }],
      },
    ]);
    expect(aggregateAssertions(r)[0].failingRequests).toEqual(['same']);
  });

  it('returns empty for reports without test results', () => {
    expect(aggregateAssertions(report([]))).toEqual([]);
  });
});
