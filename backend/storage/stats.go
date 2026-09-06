// 存储体检：行数统计 + 数据目录体积 + VACUUM（docs/ops.md 运维可见性）。
// 保留策略（history/runner_run）删除行后库文件不会自动收缩，
// VACUUM 由用户在设置页显式触发。
package storage

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"apirequest/backend/model"
)

func (s *Store) countRows(table string) (int64, error) {
	var n int64
	// 表名来自包内调用点常量，非用户输入
	err := s.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n)
	return n, err
}

// StorageStats 汇总各表行数与数据目录体积
func (s *Store) StorageStats() (model.StorageStats, error) {
	out := model.StorageStats{}
	var err error
	if out.Workspaces, err = s.countRows("workspace"); err != nil {
		return out, err
	}
	if out.Nodes, err = s.countRows("node"); err != nil {
		return out, err
	}
	if out.Examples, err = s.countRows("example"); err != nil {
		return out, err
	}
	if out.Environments, err = s.countRows("environment"); err != nil {
		return out, err
	}
	if out.History, err = s.countRows("history"); err != nil {
		return out, err
	}
	if out.RunnerRuns, err = s.countRows("runner_run"); err != nil {
		return out, err
	}
	// blob 目录遍历尽力而为：统计失败不阻断体检
	_ = fs.WalkDir(os.DirFS(s.blobsDir), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path == "." {
			return nil
		}
		out.BlobFiles++
		if info, infoErr := d.Info(); infoErr == nil {
			out.BlobBytes += info.Size()
		}
		return nil
	})
	if info, infoErr := os.Stat(s.dbPath); infoErr == nil {
		out.DbBytes = info.Size()
	}
	if info, infoErr := os.Stat(s.dbPath + "-wal"); infoErr == nil {
		out.WalBytes = info.Size()
	}
	return out, nil
}

// Vacuum 重建数据库文件（回收删除行占用的空间）。执行期间短暂阻塞写入，
// 由调用方（设置页）确认后触发。
func (s *Store) Vacuum() error {
	_, err := s.db.Exec("VACUUM")
	return err
}

// Backup 用 VACUUM INTO 生成一致性快照（紧凑、单文件、免锁文件拷贝的 WAL 一致性问题），
// 滚动保留 keep 份（超出按文件名时间戳清理最旧）。返回快照路径。
func (s *Store) Backup(dir string, keep int) (string, error) {
	if keep <= 0 {
		keep = 1
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", model.WrapError(model.KindStorage, err)
	}
	name := "apirequest-backup-" + time.Now().Format("20060102-150405") + ".db"
	dest := filepath.Join(dir, name)
	// 先写临时名再 rename：VACUUM INTO 目标已存在会报错，且半成品不该参与滚动清理
	tmp := dest + ".in-progress"
	if _, err := s.db.Exec("VACUUM INTO ?", tmp); err != nil {
		_ = os.Remove(tmp)
		return "", model.WrapError(model.KindStorage, err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return "", model.WrapError(model.KindStorage, err)
	}
	s.pruneBackups(dir, keep)
	return dest, nil
}

// pruneBackups 按名字（内嵌时间戳）排序，保留最新 keep 份
func (s *Store) pruneBackups(dir string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "apirequest-backup-") && strings.HasSuffix(e.Name(), ".db") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names) // 时间戳命名 = 字典序即时间序
	for i := 0; i < len(names)-keep; i++ {
		_ = os.Remove(filepath.Join(dir, names[i]))
	}
}

// AutoBackup 每日滚动备份：24 小时内已有快照则跳过。
// 返回是否实际执行了备份。
func (s *Store) AutoBackup(dir string, keep int) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		return false, model.WrapError(model.KindStorage, err)
	}
	newest := time.Time{}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "apirequest-backup-") || !strings.HasSuffix(e.Name(), ".db") {
			continue
		}
		if info, infoErr := e.Info(); infoErr == nil && info.ModTime().After(newest) {
			newest = info.ModTime()
		}
	}
	if time.Since(newest) < 24*time.Hour {
		return false, nil
	}
	_, err = s.Backup(dir, keep)
	return err == nil, err
}

// DbPath 返回数据库文件路径（备份目录定位用）
func (s *Store) DbPath() string { return s.dbPath }

// ListBackups 列出备份目录中的快照（按时间倒序）
func (s *Store) ListBackups(dir string) ([]model.BackupInfo, error) {
	out := []model.BackupInfo{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, model.WrapError(model.KindStorage, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "apirequest-backup-") || !strings.HasSuffix(e.Name(), ".db") {
			continue
		}
		info, infoErr := e.Info()
		if infoErr != nil {
			continue
		}
		out = append(out, model.BackupInfo{
			Name:      e.Name(),
			SizeBytes: info.Size(),
			CreatedAt: info.ModTime().UnixMilli(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out, nil
}
