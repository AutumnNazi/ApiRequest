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
