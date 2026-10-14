package runner

import (
	"math"
	"sort"
)

// Stats 运行报告的轻量级性能聚合（docs/advanced.md §2.1 报告）：
// 分位数采用最近邻秩法（nearest-rank，与前端 runnerAggregate.ts 同口径），
// 只统计实际执行的请求（DurationMs > 0）——未执行/取消的条目没有耗时特征。
type Stats struct {
	MinMs        int64   `json:"minMs"`
	MaxMs        int64   `json:"maxMs"`
	P50Ms        int64   `json:"p50Ms"`
	P90Ms        int64   `json:"p90Ms"`
	P95Ms        int64   `json:"p95Ms"`
	P99Ms        int64   `json:"p99Ms"`
	Executed     int     `json:"executed"`     // DurationMs > 0 的请求数
	ErrorRatePct float64 `json:"errorRatePct"` // 失败数 / 执行数 × 100（未执行不算错误）
	Rps          float64 `json:"rps"`          // 执行数 / 报告总墙钟秒
}

// ComputeStats 从运行报告计算性能聚合。空报告或全为未执行的条目返回零值 Stats。
func ComputeStats(report *Report) Stats {
	durations := make([]int64, 0, len(report.Results))
	failed := 0
	for _, r := range report.Results {
		if r.DurationMs <= 0 {
			continue
		}
		durations = append(durations, r.DurationMs)
		if r.Failed {
			failed++
		}
	}
	executed := len(durations)
	if executed == 0 {
		return Stats{}
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })

	nearestRank := func(p float64) int64 {
		rank := int(math.Ceil(p / 100 * float64(executed)))
		if rank < 1 {
			rank = 1
		}
		return durations[rank-1]
	}

	stats := Stats{
		MinMs:    durations[0],
		MaxMs:    durations[executed-1],
		P50Ms:    nearestRank(50),
		P90Ms:    nearestRank(90),
		P95Ms:    nearestRank(95),
		P99Ms:    nearestRank(99),
		Executed: executed,
	}
	stats.ErrorRatePct = float64(failed) / float64(executed) * 100
	if report.DurationMs > 0 {
		stats.Rps = float64(executed) / (float64(report.DurationMs) / 1000)
	}
	return stats
}
