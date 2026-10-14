// Runner 报告的断言聚合视图：把逐请求的断言结果按断言名归组，
// 呈现"哪些断言规则反复失败、在哪些请求上失败"——比逐行浏览更快定位共性回归。
import type { RunnerReport } from '../ipc';

export interface AggregateRow {
  name: string;
  total: number;
  passed: number;
  failed: number;
  failingRequests: string[]; // 去重后的失败请求名
}

export function aggregateAssertions(report: RunnerReport): AggregateRow[] {
  const byName = new Map<string, { total: number; passed: number; failed: number; failing: Set<string> }>();
  for (const result of report.results ?? []) {
    for (const test of result.testResults ?? []) {
      if (!test?.name) continue;
      let agg = byName.get(test.name);
      if (!agg) {
        agg = { total: 0, passed: 0, failed: 0, failing: new Set() };
        byName.set(test.name, agg);
      }
      agg.total++;
      if (test.pass) {
        agg.passed++;
      } else {
        agg.failed++;
        agg.failing.add(result.requestName);
      }
    }
  }
  return [...byName.entries()]
    .map(([name, agg]) => ({
      name,
      total: agg.total,
      passed: agg.passed,
      failed: agg.failed,
      failingRequests: [...agg.failing],
    }))
    .sort((a, b) =>
      b.failed - a.failed ||          // 失败多者优先
      b.total - a.total ||            // 同失败数：覆盖面广（总次数多）优先
      a.name.localeCompare(b.name),   // 再按名称稳定排序
    );
}

// ── 性能聚合：与 backend/runner/aggregate.go 同口径（最近邻秩分位数）──

export interface RunnerStats {
  minMs: number;
  maxMs: number;
  p50Ms: number;
  p90Ms: number;
  p95Ms: number;
  p99Ms: number;
  executed: number;
  errorRatePct: number;
  rps: number;
}

// 只统计实际执行的请求（durationMs > 0）——未执行/取消的条目没有耗时特征；
// 错误率按执行数算（未执行不算错误），RPS = 执行数 / 报告总墙钟秒。
export function computeRunnerStats(report: RunnerReport): RunnerStats {
  const durations: number[] = [];
  let failed = 0;
  for (const result of report.results ?? []) {
    const ms = result.durationMs ?? 0;
    if (ms <= 0) continue;
    durations.push(ms);
    if (result.failed) failed++;
  }
  const executed = durations.length;
  if (executed === 0) {
    return { minMs: 0, maxMs: 0, p50Ms: 0, p90Ms: 0, p95Ms: 0, p99Ms: 0, executed: 0, errorRatePct: 0, rps: 0 };
  }
  durations.sort((a, b) => a - b);
  const nearestRank = (p: number): number => {
    const rank = Math.max(1, Math.ceil((p / 100) * executed));
    return durations[rank - 1];
  };
  const stats: RunnerStats = {
    minMs: durations[0],
    maxMs: durations[executed - 1],
    p50Ms: nearestRank(50),
    p90Ms: nearestRank(90),
    p95Ms: nearestRank(95),
    p99Ms: nearestRank(99),
    executed,
    errorRatePct: (failed / executed) * 100,
    rps: 0,
  };
  if ((report.durationMs ?? 0) > 0) {
    stats.rps = executed / ((report.durationMs ?? 0) / 1000);
  }
  return stats;
}
