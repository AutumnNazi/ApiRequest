package runner

import (
	"math"
	"testing"
)

func TestComputeStatsPercentilesAndRates(t *testing.T) {
	// 10 个请求，耗时 10..100ms：最近邻秩法 P50=50、P90=90、P95=95、P99=100
	results := make([]RequestResult, 0, 10)
	for i := 1; i <= 10; i++ {
		results = append(results, RequestResult{
			RequestName: "r", DurationMs: int64(i * 10),
			Failed: i == 3, // 1 个失败
		})
	}
	report := &Report{
		RunId: "r1", Total: 10, Passed: 9, Failed: 1,
		DurationMs: 2000, // 汇总墙钟 2s → 10 个执行 / 2s = 5 rps
		Results:    results,
	}
	stats := ComputeStats(report)

	// 最近邻秩法：rank = ceil(p/100×n)。10 样本 → P50=第5个(50) P90=第9个(90) P95=P99=第10个(100)
	if stats.P50Ms != 50 || stats.P90Ms != 90 || stats.P95Ms != 100 || stats.P99Ms != 100 {
		t.Fatalf("percentiles = P50:%d P90:%d P95:%d P99:%d", stats.P50Ms, stats.P90Ms, stats.P95Ms, stats.P99Ms)
	}
	if stats.MinMs != 10 || stats.MaxMs != 100 {
		t.Fatalf("min/max = %d/%d", stats.MinMs, stats.MaxMs)
	}
	if stats.ErrorRatePct != 10.0 {
		t.Fatalf("errorRate = %v", stats.ErrorRatePct)
	}
	// RPS = 执行数 / 总墙钟秒：用报告自带的 DurationMs 语义（汇总墙钟）
	// 2000ms 墙钟 + 10 个请求 → 5 rps（允许 ±0.1 浮点误差）
	if math.Abs(stats.Rps-5.0) > 0.1 {
		t.Fatalf("rps = %v, want ~5", stats.Rps)
	}
}

func TestComputeStatsIgnoresZeroDurations(t *testing.T) {
	// 未执行/取消的条目 DurationMs 为 0：不参与分位数，但计入总执行数
	report := &Report{
		Total: 3, Passed: 1, Failed: 1, Skipped: 1,
		DurationMs: 1000,
		Results: []RequestResult{
			{RequestName: "a", DurationMs: 100, Failed: true},
			{RequestName: "b", DurationMs: 0}, // 未执行
			{RequestName: "c", DurationMs: 300},
		},
	}
	stats := ComputeStats(report)
	if stats.P50Ms != 100 || stats.P99Ms != 300 {
		t.Fatalf("percentiles over executed only: P50:%d P99:%d", stats.P50Ms, stats.P99Ms)
	}
	if stats.ErrorRatePct != 50.0 {
		// 错误率按实际执行的请求算：1 失败 / 2 执行 = 50%（未执行不是错误）
		t.Fatalf("errorRate = %v", stats.ErrorRatePct)
	}
}

func TestComputeStatsEmptyReport(t *testing.T) {
	stats := ComputeStats(&Report{RunId: "empty"})
	if stats.P50Ms != 0 || stats.Rps != 0 || stats.ErrorRatePct != 0 {
		t.Fatalf("empty stats = %+v", stats)
	}
}
