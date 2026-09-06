// 存储体检：行数统计 + 数据目录体积 + VACUUM（docs/ops.md 运维可见性）。
// 保留策略（history/runner_run）删除行后库文件不会自动收缩，
// VACUUM 由用户在设置页显式触发。
package storage

import (
	"io/fs"
	"os"

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
