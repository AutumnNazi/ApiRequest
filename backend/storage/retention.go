package storage

import "strconv"

// 保留策略设置化（docs/ops.md 存储体检联动）：
// setting 键 → 生效上限；未设置/非法值回落内置默认。
// 读取走 s.settingCache 之外的即时查询：写路径（每次 insert/save）低频，
// 不值得为它维护缓存一致性与失效逻辑。
const (
	historyRetentionKey   = "retention.history"
	runnerRunRetentionKey = "retention.runnerRuns"
)

// historyRetention 当前生效的历史保留上限（每 workspace）
func (s *Store) historyRetention() int {
	if v, ok := retentionSetting(s, historyRetentionKey, historyRetentionLimit); ok {
		return v
	}
	return historyRetentionLimit
}

// runnerRunRetention 当前生效的 Runner 运行保留上限（每 workspace+collection）
func (s *Store) runnerRunRetention() int {
	if v, ok := retentionSetting(s, runnerRunRetentionKey, runnerRunRetentionLimit); ok {
		return v
	}
	return runnerRunRetentionLimit
}

func retentionSetting(s *Store, key string, fallback int) (int, bool) {
	raw, err := s.GetSetting(key)
	if err != nil || raw == "" {
		return fallback, false
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return fallback, false
	}
	return v, true
}
