// Package storage 提供 SQLite 访问层（modernc.org/sqlite，纯 Go 免 CGO，ADR-009）。
// Schema 见 docs/data-model.md §2；迁移用 PRAGMA user_version 顺序执行。
package storage

import (
	"database/sql"
	"fmt"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// migrations 按下标顺序执行；user_version 记录已执行到的版本号（= len 时最新）。
// 只允许追加，禁止修改已发布的条目。
var migrations = []string{
	// 0001: Phase 1 核心三表 + 设置
	`
	CREATE TABLE workspace (
	  id          TEXT PRIMARY KEY,
	  name        TEXT NOT NULL,
	  type        TEXT NOT NULL DEFAULT 'local',
	  created_at  INTEGER NOT NULL,
	  updated_at  INTEGER NOT NULL
	);

	CREATE TABLE node (
	  id            TEXT PRIMARY KEY,
	  workspace_id  TEXT NOT NULL REFERENCES workspace(id),
	  parent_id     TEXT REFERENCES node(id),
	  kind          TEXT NOT NULL,
	  name          TEXT NOT NULL,
	  sort_order    REAL NOT NULL DEFAULT 0,
	  request_data  TEXT,
	  auth          TEXT,
	  variables     TEXT,
	  pre_script    TEXT,
	  test_script   TEXT,
	  created_at    INTEGER NOT NULL,
	  updated_at    INTEGER NOT NULL,
	  deleted_at    INTEGER
	);
	CREATE INDEX idx_node_ws_parent ON node(workspace_id, parent_id);

	CREATE TABLE history (
	  id            TEXT PRIMARY KEY,
	  workspace_id  TEXT NOT NULL,
	  request_snap  TEXT NOT NULL,
	  status        INTEGER,
	  duration_ms   INTEGER,
	  size_bytes    INTEGER,
	  response_meta TEXT,
	  body_ref      TEXT,
	  body_inline   TEXT,
	  test_results  TEXT,
	  created_at    INTEGER NOT NULL
	);
	CREATE INDEX idx_history_ws_time ON history(workspace_id, created_at DESC);

	CREATE TABLE setting (key TEXT PRIMARY KEY, value TEXT NOT NULL);
	`,
	// 0002: Phase 2 环境与全局变量
	`
	CREATE TABLE environment (
	  id            TEXT PRIMARY KEY,
	  workspace_id  TEXT NOT NULL REFERENCES workspace(id),
	  name          TEXT NOT NULL,
	  variables     TEXT NOT NULL DEFAULT '[]',
	  is_active     INTEGER NOT NULL DEFAULT 0,
	  created_at    INTEGER NOT NULL,
	  updated_at    INTEGER NOT NULL
	);

	CREATE TABLE global_var (
	  workspace_id  TEXT PRIMARY KEY REFERENCES workspace(id),
	  variables     TEXT NOT NULL DEFAULT '[]'
	);
	`,
}

// Store 持有 DB 连接与 blobs 根目录
type Store struct {
	db       *sql.DB
	blobsDir string
}

// Open 打开（不存在则创建）数据库并执行迁移。dataDir 为应用数据目录。
func Open(dataDir string) (*Store, error) {
	dbPath := filepath.Join(dataDir, "apirequest.db")
	// busy_timeout 避免并发写时 SQLITE_BUSY；WAL 提升读写并发
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	// modernc.org/sqlite 对单连接最稳（写串行化交给 database/sql 排队）
	db.SetMaxOpenConns(1)

	s := &Store{db: db, blobsDir: filepath.Join(dataDir, "blobs")}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	for i := version; i < len(migrations); i++ {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d failed: %w", i+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// BlobsDir 返回大响应体存放目录
func (s *Store) BlobsDir() string { return s.blobsDir }

// Close 关闭底层连接
func (s *Store) Close() error { return s.db.Close() }
