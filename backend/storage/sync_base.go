package storage

import "database/sql"

// sync_base：字段级同步合并的本地基线（ADR-017，docs/decisions.md）。
// 基线 = 最近一次合并结果的完整快照（未剥密钥），作为三路合并的公共祖先；
// 无基线（首次同步/升级后第一轮）该轮回退实体级 LWW。

// GetSyncBase 返回基线（schemaVersion, snapshot JSON）。ok=false 表示无基线。
func (s *Store) GetSyncBase(workspaceId string) (schemaVersion int, snapshot string, ok bool, err error) {
	row := s.db.QueryRow(`SELECT schema_version, snapshot FROM sync_base WHERE workspace_id = ?`, workspaceId)
	if err := row.Scan(&schemaVersion, &snapshot); err != nil {
		if err == sql.ErrNoRows {
			return 0, "", false, nil
		}
		return 0, "", false, err
	}
	return schemaVersion, snapshot, true, nil
}

// PutSyncBase 覆盖写基线（同步成功完成本地写入后调用）
func (s *Store) PutSyncBase(workspaceId string, schemaVersion int, snapshot string) error {
	_, err := s.db.Exec(`
		INSERT INTO sync_base (workspace_id, schema_version, snapshot, merged_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(workspace_id) DO UPDATE SET
		  schema_version = excluded.schema_version,
		  snapshot = excluded.snapshot,
		  merged_at = excluded.merged_at`,
		workspaceId, schemaVersion, snapshot, nowMs())
	return err
}

// DeleteSyncBase 清除基线（快照协议版本变化等场景，下轮回退实体级 LWW）
func (s *Store) DeleteSyncBase(workspaceId string) error {
	_, err := s.db.Exec(`DELETE FROM sync_base WHERE workspace_id = ?`, workspaceId)
	return err
}
