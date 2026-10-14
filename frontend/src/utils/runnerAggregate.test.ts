import { describe, expect, it } from 'vitest';
import { aggregateAssertions, computeRunnerStats, type AggregateRow } from './runnerAggregate';
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

// ── computeRunnerStats：与后端 ComputeStats 同口径的守门测试 ──

describe('computeRunnerStats', () => {
  const reportWith = (results: Array<{ durationMs: number; failed?: boolean }>, durationMs: number): RunnerReport => ({
    runId: 'r1',
    total: results.length,
    passed: results.filter((r) => !r.failed).length,
    failed: results.filter((r) => r.failed).length,
    skipped: 0,
    durationMs,
    results: results.map((r, i) => ({
      iteration: 1, requestName: `r${i}`, nodeId: `n${i}`, status: 200,
      durationMs: r.durationMs, failed: !!r.failed, testResults: [],
    })),
    canceled: false,
  } as unknown as RunnerReport);

  it('nearest-rank percentiles, error rate and rps（与 Go 端同数值）', () => {
    const results = Array.from({ length: 10 }, (_, i) => ({ durationMs: (i + 1) * 10, failed: i === 2 }));
    const stats = computeRunnerStats(reportWith(results, 2000));
    // 最近邻秩：P50=第5个(50) P90=第9个(90) P95=P99=第10个(100)；1 失败/10 执行 = 10%；10/2s = 5 rps
    expect(stats).toMatchObject({
      minMs: 10, maxMs: 100, p50Ms: 50, p90Ms: 90, p95Ms: 100, p99Ms: 100,
      executed: 10, errorRatePct: 10, rps: 5,
    });
  });

  it('ignores zero-duration entries and yields zeros for empty reports', () => {
    const stats = computeRunnerStats(reportWith(
      [{ durationMs: 100, failed: true }, { durationMs: 0 }, { durationMs: 300 }],
      1000,
    ));
    expect(stats.p50Ms).toBe(100);
    expect(stats.p99Ms).toBe(300);
    expect(stats.executed).toBe(2);
    expect(stats.errorRatePct).toBe(50);

    expect(computeRunnerStats(reportWith([], 500)).p50Ms).toBe(0);
  });
});
