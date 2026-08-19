package storage

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"apirequest/backend/model"
	"apirequest/backend/secrets"
)

// SyncNodeRow 同步用节点行（含墓碑）
type SyncNodeRow struct {
	Node      model.Node
	DeletedAt int64
}

// SyncSnapshotWrite is a complete workspace snapshot ordered parent-before-child.
// ApplySyncSnapshot commits its SQLite rows and Secret Vault changes as one batch.
type SyncSnapshotWrite struct {
	WorkspaceId     string
	WorkspaceName   string
	Nodes           []SyncNodeRow
	Environments    []model.Environment
	Globals         []model.Variable
	GlobalsRevision int64
}

type syncQueryer interface {
	rowQueryer
	Query(query string, args ...any) (*sql.Rows, error)
}

type syncSQL interface {
	rowQueryer
	Exec(query string, args ...any) (sql.Result, error)
}

const syncOwnershipQueryBatchSize = 500

// ValidateSyncOwnership rejects entity ID collisions before a merged snapshot
// writes anything. Parent topology is validated by the sync package itself.
func (s *Store) ValidateSyncOwnership(workspaceId string, nodes []SyncNodeRow, environments []model.Environment) error {
	return validateSyncOwnership(s.db, workspaceId, nodes, environments)
}

func validateSyncOwnership(queryer syncQueryer, workspaceId string, nodes []SyncNodeRow, environments []model.Environment) error {
	nodeKinds := make(map[string]string, len(nodes))
	nodeIds := make([]string, 0, len(nodes))
	for _, row := range nodes {
		if row.Node.Id == "" {
			return errors.New("sync node id is required")
		}
		if _, exists := nodeKinds[row.Node.Id]; exists {
			return fmt.Errorf("duplicate sync node id %q", row.Node.Id)
		}
		nodeKinds[row.Node.Id] = row.Node.Kind
		nodeIds = append(nodeIds, row.Node.Id)
	}
	for start := 0; start < len(nodeIds); start += syncOwnershipQueryBatchSize {
		end := min(start+syncOwnershipQueryBatchSize, len(nodeIds))
		args := make([]any, end-start)
		for i, id := range nodeIds[start:end] {
			args[i] = id
		}
		rows, err := queryer.Query(
			"SELECT id, workspace_id, kind FROM node WHERE id IN ("+sqlPlaceholders(len(args))+")",
			args...,
		)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id, existingWorkspace, existingKind string
			if err := rows.Scan(&id, &existingWorkspace, &existingKind); err != nil {
				rows.Close()
				return err
			}
			if existingWorkspace != workspaceId {
				rows.Close()
				return fmt.Errorf("sync node %q belongs to a different workspace", id)
			}
			if existingKind != nodeKinds[id] {
				rows.Close()
				return fmt.Errorf("sync node %q kind cannot be changed", id)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
	}

	environmentIds := make([]string, 0, len(environments))
	seenEnvironments := make(map[string]struct{}, len(environments))
	for _, environment := range environments {
		if environment.Id == "" {
			return errors.New("sync environment id is required")
		}
		if _, exists := seenEnvironments[environment.Id]; exists {
			return fmt.Errorf("duplicate sync environment id %q", environment.Id)
		}
		seenEnvironments[environment.Id] = struct{}{}
		environmentIds = append(environmentIds, environment.Id)
	}
	for start := 0; start < len(environmentIds); start += syncOwnershipQueryBatchSize {
		end := min(start+syncOwnershipQueryBatchSize, len(environmentIds))
		args := make([]any, end-start)
		for i, id := range environmentIds[start:end] {
			args[i] = id
		}
		rows, err := queryer.Query(
			"SELECT id, workspace_id FROM environment WHERE id IN ("+sqlPlaceholders(len(args))+")",
			args...,
		)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id, existingWorkspace string
			if err := rows.Scan(&id, &existingWorkspace); err != nil {
				rows.Close()
				return err
			}
			if existingWorkspace != workspaceId {
				rows.Close()
				return fmt.Errorf("sync environment %q belongs to a different workspace", id)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
	}
	return nil
}

func sqlPlaceholders(count int) string {
	return strings.TrimSuffix(strings.Repeat("?,", count), ",")
}

// ApplySyncSnapshot atomically applies a complete workspace snapshot. Nodes
// must be ordered parent-before-child so foreign keys never observe a gap.
func (s *Store) ApplySyncSnapshot(snapshot SyncSnapshotWrite) error {
	if snapshot.WorkspaceId == "" {
		return errors.New("sync workspace id is required")
	}
	nodes := append([]SyncNodeRow(nil), snapshot.Nodes...)
	for i := range nodes {
		nodes[i].Node.WorkspaceId = snapshot.WorkspaceId
	}
	environments := append([]model.Environment(nil), snapshot.Environments...)
	for i := range environments {
		environments[i].WorkspaceId = snapshot.WorkspaceId
	}
	if err := validateOrderedSyncNodes(nodes); err != nil {
		return err
	}

	return s.withSecretWrite(func(writer secrets.SecretWriter) error {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		name := snapshot.WorkspaceName
		if name == "" {
			name = "Synced Workspace"
		}
		now := nowMs()
		if _, err := tx.Exec(`
			INSERT INTO workspace (id, name, type, created_at, updated_at)
			VALUES (?,?, 'local', ?, ?)
			ON CONFLICT(id) DO NOTHING`, snapshot.WorkspaceId, name, now, now); err != nil {
			return err
		}
		if err := validateSyncOwnership(tx, snapshot.WorkspaceId, nodes, environments); err != nil {
			return err
		}
		nodeReferences, err := loadSyncNodeReferences(tx, snapshot.WorkspaceId)
		if err != nil {
			return err
		}
		environmentReferences, err := loadSyncEnvironmentReferences(tx, snapshot.WorkspaceId)
		if err != nil {
			return err
		}
		globalReferences, err := storedGlobalSecretReferencesFrom(tx, snapshot.WorkspaceId)
		if err != nil {
			return err
		}
		for _, row := range nodes {
			if err := applySyncNodeWithReferences(tx, writer, row, nodeReferences[row.Node.Id]); err != nil {
				return err
			}
		}
		for _, environment := range environments {
			if err := applySyncEnvironmentWithReferences(
				tx, writer, environment, environmentReferences[environment.Id],
			); err != nil {
				return err
			}
		}
		if err := applySyncGlobals(
			tx, writer, snapshot.WorkspaceId, snapshot.Globals, snapshot.GlobalsRevision, globalReferences,
		); err != nil {
			return err
		}
		return tx.Commit()
	})
}

func validateOrderedSyncNodes(nodes []SyncNodeRow) error {
	type nodePosition struct {
		kind  string
		index int
	}
	positions := make(map[string]nodePosition, len(nodes))
	for index, row := range nodes {
		node := row.Node
		if node.Id == "" {
			return errors.New("sync node id is required")
		}
		if _, exists := positions[node.Id]; exists {
			return fmt.Errorf("duplicate sync node id %q", node.Id)
		}
		switch node.Kind {
		case "collection":
			if node.ParentId != "" {
				return fmt.Errorf("sync collection %q must stay at root", node.Id)
			}
		case "folder":
			if node.ParentId == "" {
				return fmt.Errorf("sync folder %q requires a parent", node.Id)
			}
		case "request":
		default:
			return fmt.Errorf("sync node %q kind is invalid", node.Id)
		}
		if node.ParentId != "" {
			parent, exists := positions[node.ParentId]
			if !exists || parent.index >= index {
				return fmt.Errorf("sync node %q parent must precede it in the snapshot", node.Id)
			}
			if parent.kind != "collection" && parent.kind != "folder" {
				return fmt.Errorf("sync node %q parent must be a collection or folder", node.Id)
			}
		}
		positions[node.Id] = nodePosition{kind: node.Kind, index: index}
	}
	return nil
}

func loadSyncNodeReferences(queryer syncQueryer, workspaceId string) (map[string][]string, error) {
	rows, err := queryer.Query(
		"SELECT id, request_data, auth, variables FROM node WHERE workspace_id = ?",
		workspaceId,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	references := make(map[string][]string)
	for rows.Next() {
		var id string
		var requestData, authData, variablesData sql.NullString
		if err := rows.Scan(&id, &requestData, &authData, &variablesData); err != nil {
			return nil, err
		}
		counts := make(map[string]int)
		if err := appendStoredNodeReferences(counts, requestData, authData, variablesData); err != nil {
			return nil, err
		}
		references[id] = sortedSecretReferences(counts)
	}
	return references, rows.Err()
}

func loadSyncEnvironmentReferences(queryer syncQueryer, workspaceId string) (map[string][]string, error) {
	rows, err := queryer.Query(
		"SELECT id, variables FROM environment WHERE workspace_id = ?",
		workspaceId,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	references := make(map[string][]string)
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		var variables []model.Variable
		if err := json.Unmarshal([]byte(raw), &variables); err != nil {
			return nil, fmt.Errorf("decode stored environment credentials: %w", err)
		}
		references[id] = secrets.VariableReferences(variables)
	}
	return references, rows.Err()
}

func applySyncNode(database syncSQL, writer secrets.SecretWriter, row SyncNodeRow) error {
	node := row.Node
	oldRefs, err := storedNodeSecretReferencesFrom(database, node.Id)
	if err != nil {
		return err
	}
	return applySyncNodeWithReferences(database, writer, row, oldRefs)
}

func applySyncNodeWithReferences(database syncSQL, writer secrets.SecretWriter, row SyncNodeRow, oldRefs []string) error {
	node := row.Node
	stored, err := protectNode(writer, node)
	if err != nil {
		return err
	}
	var requestData, authData, variablesData sql.NullString
	if stored.Request != nil {
		data, err := json.Marshal(stored.Request)
		if err != nil {
			return err
		}
		requestData = sql.NullString{String: string(data), Valid: true}
	}
	if stored.Auth != nil {
		data, err := json.Marshal(stored.Auth)
		if err != nil {
			return err
		}
		authData = sql.NullString{String: string(data), Valid: true}
	}
	if len(stored.Variables) > 0 {
		data, err := json.Marshal(stored.Variables)
		if err != nil {
			return err
		}
		variablesData = sql.NullString{String: string(data), Valid: true}
	}
	if err := deleteRemovedSecretReferences(writer, oldRefs, secrets.NodeReferences(stored)); err != nil {
		return err
	}
	_, err = database.Exec(`
		INSERT INTO node (id, workspace_id, parent_id, kind, name, sort_order,
		                  request_data, auth, variables, pre_script, test_script,
		                  created_at, updated_at, deleted_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
		  workspace_id = excluded.workspace_id,
		  parent_id = excluded.parent_id,
		  name = excluded.name,
		  sort_order = excluded.sort_order,
		  request_data = excluded.request_data,
		  auth = excluded.auth,
		  variables = excluded.variables,
		  pre_script = excluded.pre_script,
		  test_script = excluded.test_script,
		  updated_at = excluded.updated_at,
		  deleted_at = excluded.deleted_at`,
		stored.Id, stored.WorkspaceId,
		sql.NullString{String: stored.ParentId, Valid: stored.ParentId != ""},
		stored.Kind, stored.Name, stored.SortOrder,
		requestData, authData, variablesData,
		sql.NullString{String: stored.PreScript, Valid: stored.PreScript != ""},
		sql.NullString{String: stored.TestScript, Valid: stored.TestScript != ""},
		stored.CreatedAt, stored.UpdatedAt,
		sql.NullInt64{Int64: row.DeletedAt, Valid: row.DeletedAt > 0})
	return err
}

func applySyncEnvironment(database syncSQL, writer secrets.SecretWriter, environment model.Environment) error {
	oldRefs, err := storedEnvironmentSecretReferencesFrom(database, environment.Id)
	if err != nil {
		return err
	}
	return applySyncEnvironmentWithReferences(database, writer, environment, oldRefs)
}

func applySyncEnvironmentWithReferences(database syncSQL, writer secrets.SecretWriter, environment model.Environment, oldRefs []string) error {
	protected, err := protectVariables(writer, environment.Variables, "environment/"+environment.Id)
	if err != nil {
		return err
	}
	variables, err := json.Marshal(protected)
	if err != nil {
		return err
	}
	if err := deleteRemovedSecretReferences(writer, oldRefs, secrets.VariableReferences(protected)); err != nil {
		return err
	}
	_, err = database.Exec(`
		INSERT INTO environment (id, workspace_id, name, variables, is_active, created_at, updated_at)
		VALUES (?,?,?,?,0,?,?)
		ON CONFLICT(id) DO UPDATE SET
		  name = excluded.name,
		  variables = excluded.variables,
		  updated_at = excluded.updated_at`,
		environment.Id, environment.WorkspaceId, environment.Name, string(variables),
		environment.CreatedAt, environment.UpdatedAt)
	return err
}

func applySyncGlobals(
	database syncSQL,
	writer secrets.SecretWriter,
	workspaceId string,
	variables []model.Variable,
	updatedAt int64,
	oldRefs []string,
) error {
	if variables == nil {
		variables = []model.Variable{}
	}
	protected, err := protectVariables(writer, variables, "workspace/"+workspaceId+"/globals")
	if err != nil {
		return err
	}
	data, err := json.Marshal(protected)
	if err != nil {
		return err
	}
	if err := deleteRemovedSecretReferences(writer, oldRefs, secrets.VariableReferences(protected)); err != nil {
		return err
	}
	_, err = database.Exec(`
		INSERT INTO global_var (workspace_id, variables, updated_at) VALUES (?,?,?)
		ON CONFLICT(workspace_id) DO UPDATE SET
		  variables = excluded.variables,
		  updated_at = excluded.updated_at`, workspaceId, string(data), updatedAt)
	return err
}

// ListNodesForSync 返回工作区全部节点（含软删除墓碑，同步传播删除用）
func (s *Store) ListNodesForSync(workspaceId string) ([]SyncNodeRow, error) {
	rows, err := s.db.Query(`
		SELECT id, workspace_id, parent_id, kind, name, sort_order,
		       request_data, auth, variables, pre_script, test_script,
		       created_at, updated_at, deleted_at
		FROM node WHERE workspace_id = ?`, workspaceId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []SyncNodeRow{}
	for rows.Next() {
		var n model.Node
		var r nodeRow
		var deletedAt sql.NullInt64
		if err := rows.Scan(&n.Id, &n.WorkspaceId, &r.parentId, &n.Kind, &n.Name, &n.SortOrder,
			&r.requestData, &r.auth, &r.variables, &r.preScript, &r.testScript,
			&n.CreatedAt, &n.UpdatedAt, &deletedAt); err != nil {
			return nil, err
		}
		if err := s.hydrateNode(&n, &r); err != nil {
			return nil, err
		}
		out = append(out, SyncNodeRow{Node: n, DeletedAt: deletedAt.Int64})
	}
	return out, rows.Err()
}

// EnsureWorkspace 确保指定 id 的工作区存在（同步拉取远端工作区时用）
func (s *Store) EnsureWorkspace(id, name string) error {
	if name == "" {
		name = "Synced Workspace"
	}
	now := nowMs()
	_, err := s.db.Exec(`
		INSERT INTO workspace (id, name, type, created_at, updated_at)
		VALUES (?,?, 'local', ?, ?)
		ON CONFLICT(id) DO NOTHING`, id, name, now, now)
	return err
}

// ApplySyncNode 原样写入节点（保留 id/时间戳/墓碑；不走 UpsertNode 的时间戳刷新）
func (s *Store) ApplySyncNode(row SyncNodeRow) error {
	n := row.Node
	if err := s.validateSyncNodeOwnership(n); err != nil {
		return err
	}
	return s.withSecretWrite(func(writer secrets.SecretWriter) error {
		return applySyncNode(s.db, writer, row)
	})
}

func (s *Store) validateSyncNodeOwnership(n model.Node) error {
	var workspaceExists bool
	if err := s.db.QueryRow("SELECT EXISTS(SELECT 1 FROM workspace WHERE id = ?)", n.WorkspaceId).Scan(&workspaceExists); err != nil {
		return err
	}
	if !workspaceExists {
		return errors.New("sync workspace not found")
	}
	var existingWorkspace, existingKind string
	err := s.db.QueryRow("SELECT workspace_id, kind FROM node WHERE id = ?", n.Id).Scan(&existingWorkspace, &existingKind)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		if existingWorkspace != n.WorkspaceId {
			return errors.New("sync node belongs to a different workspace")
		}
		if existingKind != n.Kind {
			return errors.New("sync node kind cannot be changed")
		}
	}
	if n.Kind == "collection" {
		if n.ParentId != "" {
			return errors.New("sync collection must stay at root")
		}
		return nil
	}
	if n.Kind != "folder" && n.Kind != "request" {
		return errors.New("sync node kind is invalid")
	}
	if n.Kind == "folder" && n.ParentId == "" {
		return errors.New("sync folder nodes require a parent")
	}
	if n.ParentId == "" {
		return nil // request 可放在根级
	}
	if n.ParentId == n.Id {
		return errors.New("sync node cannot be its own parent")
	}
	var parentWorkspace, parentKind string
	if err := s.db.QueryRow("SELECT workspace_id, kind FROM node WHERE id = ?", n.ParentId).Scan(&parentWorkspace, &parentKind); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("sync parent node not found")
		}
		return err
	}
	if parentWorkspace != n.WorkspaceId {
		return errors.New("sync parent must belong to the same workspace")
	}
	if parentKind != "collection" && parentKind != "folder" {
		return errors.New("sync parent must be a collection or folder")
	}
	return nil
}

// ApplySyncEnvironment 原样写入环境（保留 id 与时间戳）
func (s *Store) ApplySyncEnvironment(e model.Environment) error {
	var existingWorkspace string
	if err := s.db.QueryRow("SELECT workspace_id FROM environment WHERE id = ?", e.Id).Scan(&existingWorkspace); err == nil {
		if existingWorkspace != e.WorkspaceId {
			return errors.New("sync environment belongs to a different workspace")
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return s.withSecretWrite(func(writer secrets.SecretWriter) error {
		return applySyncEnvironment(s.db, writer, e)
	})
}
