// Package storage 提供 SQLite 访问层（modernc.org/sqlite，纯 Go 免 CGO，ADR-009）。
// Schema 见 docs/data-model.md §2；迁移用 PRAGMA user_version 顺序执行。
package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"apirequest/backend/secrets"

	"github.com/google/uuid"
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
	// 0003: Phase 3 Cookie Jar（跨工作区全局共享，与浏览器行为一致）
	`
	CREATE TABLE cookie (
	  id          TEXT PRIMARY KEY,
	  domain      TEXT NOT NULL,
	  path        TEXT NOT NULL DEFAULT '/',
	  name        TEXT NOT NULL,
	  value       TEXT NOT NULL,
	  expires_at  INTEGER,
	  http_only   INTEGER NOT NULL DEFAULT 0,
	  secure      INTEGER NOT NULL DEFAULT 0,
	  same_site   TEXT,
	  UNIQUE(domain, path, name)
	);
	`,
	// 0004: Phase 4 示例（"保存为示例"落点，Mock Server 数据源）
	`
	CREATE TABLE example (
	  id            TEXT PRIMARY KEY,
	  node_id       TEXT NOT NULL REFERENCES node(id),
	  name          TEXT NOT NULL,
	  request_snap  TEXT,
	  status        INTEGER NOT NULL,
	  headers       TEXT NOT NULL DEFAULT '[]',
	  body          TEXT,
	  created_at    INTEGER NOT NULL,
	  updated_at    INTEGER NOT NULL,
	  deleted_at    INTEGER
	);
	CREATE INDEX idx_example_node ON example(node_id);
	`,
	// 0005: History 列表投影，避免列表扫描完整 request_snap JSON
	`
	ALTER TABLE history ADD COLUMN method TEXT NOT NULL DEFAULT '';
	ALTER TABLE history ADD COLUMN url TEXT NOT NULL DEFAULT '';
	UPDATE history
	SET method = CASE WHEN json_valid(request_snap)
	                  THEN COALESCE(json_extract(request_snap, '$.method'), '') ELSE '' END,
	    url = CASE WHEN json_valid(request_snap)
	               THEN COALESCE(json_extract(request_snap, '$.url'), '') ELSE '' END;
	CREATE INDEX idx_history_ws_cursor ON history(workspace_id, created_at DESC, id DESC);
	`,
	// 0006: Global variables need an independent LWW revision for sync.
	`
	ALTER TABLE global_var ADD COLUMN updated_at INTEGER NOT NULL DEFAULT 0;
	UPDATE global_var
	SET updated_at = COALESCE(
	  (SELECT updated_at FROM workspace WHERE workspace.id = global_var.workspace_id), 0
	);
	`,
	// 0007: Preserve RFC 6265 host-only semantics. Existing rows default to the safer exact-host scope.
	`
	ALTER TABLE cookie ADD COLUMN host_only INTEGER NOT NULL DEFAULT 1;
	`,
	// 0008: History ownership is a database invariant, not a caller-order convention.
	`
	INSERT INTO setting(key, value)
	SELECT 'history.response-blobs.legacy-refs.v1', json_group_array(body_ref)
	FROM (SELECT DISTINCT body_ref FROM history WHERE body_ref IS NOT NULL AND body_ref != '');
	CREATE TABLE history_next (
	  id            TEXT PRIMARY KEY,
	  workspace_id  TEXT NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
	  request_snap  TEXT NOT NULL,
	  status        INTEGER,
	  duration_ms   INTEGER,
	  size_bytes    INTEGER,
	  response_meta TEXT,
	  body_ref      TEXT,
	  body_inline   TEXT,
	  test_results  TEXT,
	  created_at    INTEGER NOT NULL,
	  method        TEXT NOT NULL DEFAULT '',
	  url           TEXT NOT NULL DEFAULT ''
	);
	INSERT INTO history_next (
	  id, workspace_id, request_snap, status, duration_ms, size_bytes, response_meta,
	  body_ref, body_inline, test_results, created_at, method, url
	)
	SELECT h.id, h.workspace_id, h.request_snap, h.status, h.duration_ms, h.size_bytes,
	       h.response_meta, h.body_ref, h.body_inline, h.test_results, h.created_at,
	       h.method, h.url
	FROM history h
	INNER JOIN workspace w ON w.id = h.workspace_id;
	DROP TABLE history;
	ALTER TABLE history_next RENAME TO history;
	CREATE INDEX idx_history_ws_time ON history(workspace_id, created_at DESC);
	CREATE INDEX idx_history_ws_cursor ON history(workspace_id, created_at DESC, id DESC);
	`,
}

// Store 持有 DB 连接与 blobs 根目录
type Store struct {
	db            *sql.DB
	blobsDir      string
	vault         *secrets.Vault
	secretWriteMu sync.Mutex
}

// Open 打开（不存在则创建）数据库并执行迁移。dataDir 为应用数据目录。
func Open(dataDir string) (*Store, error) {
	return OpenWithVault(dataDir, secrets.New(dataDir))
}

// OpenWithVault 打开数据库并使用指定 Secret Vault。该入口用于测试和平台 Adapter 注入。
func OpenWithVault(dataDir string, vault *secrets.Vault) (*Store, error) {
	if vault == nil {
		return nil, errors.New("secret vault is required")
	}
	dbPath := filepath.Join(dataDir, "apirequest.db")
	// busy_timeout 避免并发写时 SQLITE_BUSY；WAL 提升读写并发
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	// modernc.org/sqlite 对单连接最稳（写串行化交给 database/sql 排队）
	db.SetMaxOpenConns(1)

	s := &Store{db: db, blobsDir: filepath.Join(dataDir, "blobs"), vault: vault}
	if err := os.MkdirAll(s.blobsDir, 0o700); err != nil {
		db.Close()
		return nil, fmt.Errorf("create blobs directory: %w", err)
	}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	if vault.Status().CanStore {
		if err := s.MigrateSecrets(); err != nil {
			db.Close()
			return nil, fmt.Errorf("migrate legacy secrets: %w", err)
		}
	}
	if err := s.MigrateAuditSecrets(); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.migrateHistoryResponseBlobs(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate history response blobs: %w", err)
	}
	if err := s.cleanupOrphanedBlobs(); err != nil {
		db.Close()
		return nil, fmt.Errorf("clean orphaned response blobs: %w", err)
	}
	return s, nil
}

// MigrateAuditSecrets redacts legacy history and example payloads. It is
// exported so unlocking the fallback Vault can finish rows whose references
// were intentionally left untouched while the Vault was locked.
func (s *Store) MigrateAuditSecrets() error {
	if err := s.migrateHistoryRequestSecrets(); err != nil {
		return fmt.Errorf("migrate history request secrets: %w", err)
	}
	if err := s.migrateHistoryResponseSecrets(); err != nil {
		return fmt.Errorf("migrate history response secrets: %w", err)
	}
	if err := s.migrateExampleSecrets(); err != nil {
		return fmt.Errorf("migrate example secrets: %w", err)
	}
	return nil
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

// Vault 返回存储层唯一使用的 Secret Vault。
func (s *Store) Vault() *secrets.Vault { return s.vault }

func (s *Store) blobPath(ref string) (string, error) {
	if ref == "" || strings.ContainsAny(ref, `/\`) || strings.Contains(ref, "..") {
		return "", fmt.Errorf("invalid blob ref: %q", ref)
	}
	return filepath.Join(s.blobsDir, ref), nil
}

// BlobInfo returns metadata without loading body bytes.
func (s *Store) BlobInfo(ref string) (int64, error) {
	path, err := s.blobPath(ref)
	if err != nil {
		return 0, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() {
		return 0, errors.New("blob is not a regular file")
	}
	return info.Size(), nil
}

// ReadBlobRange reads a bounded range. Individual calls are capped at 1 MiB.
func (s *Store) ReadBlobRange(ref string, offset, limit int64) ([]byte, bool, error) {
	if offset < 0 {
		return nil, false, errors.New("blob offset must be non-negative")
	}
	if limit <= 0 {
		limit = 64 << 10
	}
	const maxRange = int64(1 << 20)
	if limit > maxRange {
		return nil, false, fmt.Errorf("blob range exceeds %d byte limit", maxRange)
	}
	path, err := s.blobPath(ref)
	if err != nil {
		return nil, false, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, false, err
	}
	if offset > info.Size() {
		return nil, false, errors.New("blob offset exceeds file size")
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return nil, false, err
	}
	data := make([]byte, min(limit, info.Size()-offset))
	read, err := io.ReadFull(file, data)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, false, err
	}
	data = data[:read]
	return data, offset+int64(read) >= info.Size(), nil
}

// CopyBlob streams a response blob to destination without materialising it in memory.
func (s *Store) CopyBlob(ref, destination string) (int64, error) {
	sourcePath, err := s.blobPath(ref)
	if err != nil {
		return 0, err
	}
	if strings.TrimSpace(destination) == "" {
		return 0, errors.New("destination path is required")
	}
	sourceAbs, err := filepath.Abs(sourcePath)
	if err != nil {
		return 0, err
	}
	destinationAbs, err := filepath.Abs(destination)
	if err != nil {
		return 0, err
	}
	if strings.EqualFold(sourceAbs, destinationAbs) {
		return 0, errors.New("destination cannot be the source blob")
	}
	source, err := os.Open(sourceAbs)
	if err != nil {
		return 0, err
	}
	defer source.Close()
	temp, err := os.CreateTemp(filepath.Dir(destinationAbs), ".apirequest-save-*.tmp")
	if err != nil {
		return 0, err
	}
	tempPath := temp.Name()
	committed := false
	defer func() {
		_ = temp.Close()
		if !committed {
			_ = os.Remove(tempPath)
		}
	}()
	written, err := io.Copy(temp, source)
	if err != nil {
		return written, err
	}
	if err := temp.Sync(); err != nil {
		return written, err
	}
	if err := temp.Close(); err != nil {
		return written, err
	}
	if err := replaceFile(tempPath, destinationAbs, os.Rename); err != nil {
		return written, err
	}
	committed = true
	return written, nil
}

// RemoveBlob removes one validated response artifact. Missing files are already released.
func (s *Store) RemoveBlob(ref string) error {
	path, err := s.blobPath(ref)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func replaceFile(tempPath, destination string, rename func(string, string) error) error {
	info, err := os.Lstat(destination)
	if errors.Is(err, os.ErrNotExist) {
		return rename(tempPath, destination)
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("destination is not a regular file")
	}

	backup, err := os.CreateTemp(filepath.Dir(destination), ".apirequest-backup-*.tmp")
	if err != nil {
		return err
	}
	backupPath := backup.Name()
	if err := backup.Close(); err != nil {
		_ = os.Remove(backupPath)
		return err
	}
	if err := os.Remove(backupPath); err != nil {
		return err
	}
	if err := rename(destination, backupPath); err != nil {
		return fmt.Errorf("backup destination: %w", err)
	}
	if err := rename(tempPath, destination); err != nil {
		if restoreErr := rename(backupPath, destination); restoreErr != nil {
			return fmt.Errorf("replace destination: %w; restore original: %v (backup retained at %s)", err, restoreErr, backupPath)
		}
		return fmt.Errorf("replace destination: %w", err)
	}
	if err := os.Remove(backupPath); err != nil {
		return fmt.Errorf("destination saved but backup cleanup failed: %w", err)
	}
	return nil
}

// ReadBlob 按相对引用读 blob 文件；拒绝路径逃逸与超大文件
func (s *Store) ReadBlob(ref string) ([]byte, error) {
	path, err := s.blobPath(ref)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("blob is not a regular file")
	}
	const maxBlobLoad = 32 << 20 // 32 MiB
	if info.Size() > maxBlobLoad {
		return nil, fmt.Errorf("blob too large to load inline (%d bytes)", info.Size())
	}
	data, err := io.ReadAll(io.LimitReader(file, maxBlobLoad+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxBlobLoad {
		return nil, fmt.Errorf("blob too large to load inline (more than %d bytes)", maxBlobLoad)
	}
	return data, nil
}

// removeBlobFile 删除 blob 文件；ref 校验与 ReadBlob 一致，避免路径逃逸。
// 文件不存在或删除失败均不报错（调用方已删除 DB 行，孤儿文件不影响正确性）。
func (s *Store) removeBlobFile(ref string) {
	path, err := s.blobPath(ref)
	if err != nil {
		return
	}
	os.Remove(path)
}

func (s *Store) removeUnreferencedBlobFiles(refs []string) {
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		if ref == "" {
			continue
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		var marker int
		err := s.db.QueryRow("SELECT 1 FROM history WHERE body_ref = ? LIMIT 1", ref).Scan(&marker)
		if errors.Is(err, sql.ErrNoRows) {
			s.removeBlobFile(ref)
		}
	}
}

// cleanupOrphanedBlobs removes completed or temporary response files that have
// no History Detail owner. The grace period avoids racing another process that
// has renamed a response file but has not inserted its History Detail yet.
func (s *Store) cleanupOrphanedBlobs() error {
	const orphanGracePeriod = 24 * time.Hour
	rows, err := s.db.Query("SELECT DISTINCT body_ref FROM history WHERE body_ref IS NOT NULL AND body_ref != ''")
	if err != nil {
		return err
	}
	referenced := map[string]struct{}{}
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			rows.Close()
			return err
		}
		referenced[ref] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	entries, err := os.ReadDir(s.blobsDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !isManagedResponseBlobName(entry.Name()) {
			continue
		}
		if _, ok := referenced[entry.Name()]; ok {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || time.Since(info.ModTime()) < orphanGracePeriod {
			continue
		}
		_ = os.Remove(filepath.Join(s.blobsDir, entry.Name()))
	}
	return nil
}

func isManagedResponseBlobName(name string) bool {
	if strings.HasPrefix(name, ".response-") && strings.HasSuffix(name, ".tmp") {
		return len(name) > len(".response-.tmp")
	}
	if !strings.HasSuffix(name, ".bin") {
		return false
	}
	_, err := uuid.Parse(strings.TrimSuffix(name, ".bin"))
	return err == nil
}

// Close 关闭底层连接
func (s *Store) Close() error { return s.db.Close() }
