package sync

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"apirequest/backend/model"
	"apirequest/backend/storage"
)

// Report 一次同步的结果摘要
type Report struct {
	Pushed      int    `json:"pushed"`      // 本地 → 远端的实体数
	Pulled      int    `json:"pulled"`      // 远端 → 本地的实体数
	Deleted     int    `json:"deleted"`     // 应用到本地的删除（墓碑）
	RemoteFresh bool   `json:"remoteFresh"` // 远端首次初始化
	SyncedAt    int64  `json:"syncedAt"`
	Remote      string `json:"remote"` // 远端快照路径
}

// remotePath 每工作区一个快照文件
func remotePath(workspaceId string) string {
	return "ApiRequest/workspace-" + workspaceId + ".json"
}

// buildLocalSnapshot 从本地库构建快照
func buildLocalSnapshot(store *storage.Store, workspaceId string) (*Snapshot, error) {
	workspaces, err := store.ListWorkspaces()
	if err != nil {
		return nil, err
	}
	wsName := ""
	workspaceFound := false
	for _, w := range workspaces {
		if w.Id == workspaceId {
			wsName = w.Name
			workspaceFound = true
			break
		}
	}
	if !workspaceFound {
		return nil, errors.New("workspace not found")
	}
	rows, err := store.ListNodesForSync(workspaceId)
	if err != nil {
		return nil, err
	}
	envs, err := store.ListEnvironments(workspaceId)
	if err != nil {
		return nil, err
	}
	for i := range envs {
		envs[i].IsActive = false
	}
	globals, err := store.GetGlobalVariables(workspaceId)
	if err != nil {
		return nil, err
	}
	globalsRev, err := store.GlobalVariablesRevision(workspaceId)
	if err != nil {
		return nil, err
	}

	snap := &Snapshot{
		SchemaVersion: snapshotSchemaVersion,
		WorkspaceName: wsName,
		SyncedAt:      time.Now().UnixMilli(),
		Environments:  envs,
		Globals:       globals,
		GlobalsRev:    globalsRev,
	}
	for _, row := range rows {
		snap.Nodes = append(snap.Nodes, SyncNode{Node: row.Node, DeletedAt: row.DeletedAt})
	}
	return snap, nil
}

// merge 实体级 LWW：同 id 取 rev 大者；对方独有的保留。
// 返回合并结果与统计（相对本地视角：pulled = 远端胜出/独有数）。
func merge(local, remote *Snapshot) (*Snapshot, *Report) {
	report := &Report{}
	out := &Snapshot{
		SchemaVersion: snapshotSchemaVersion,
		WorkspaceName: local.WorkspaceName,
	}

	// ── 节点 ──
	localNodes := map[string]SyncNode{}
	for _, n := range local.Nodes {
		localNodes[n.Id] = n
	}
	seen := map[string]bool{}
	for _, rn := range remote.Nodes {
		seen[rn.Id] = true
		ln, exists := localNodes[rn.Id]
		switch {
		case !exists:
			// 远端独有 → 拉取（含墓碑：本地本就没有，仅记录墓碑以防回流）
			out.Nodes = append(out.Nodes, rn)
			if rn.DeletedAt == 0 {
				report.Pulled++
			}
		case rn.rev() > ln.rev():
			out.Nodes = append(out.Nodes, rn)
			if rn.DeletedAt > 0 && ln.DeletedAt == 0 {
				report.Deleted++
			} else {
				report.Pulled++
			}
		default:
			out.Nodes = append(out.Nodes, ln)
			if ln.rev() > rn.rev() {
				report.Pushed++
			}
		}
	}
	for _, ln := range local.Nodes {
		if !seen[ln.Id] {
			out.Nodes = append(out.Nodes, ln)
			if ln.DeletedAt == 0 {
				report.Pushed++
			}
		}
	}

	// ── 环境（同 id LWW；环境无墓碑——删除环境属低频，暂不传播）──
	localEnvs := map[string]model.Environment{}
	for _, e := range local.Environments {
		localEnvs[e.Id] = e
	}
	seenEnv := map[string]bool{}
	for _, re := range remote.Environments {
		seenEnv[re.Id] = true
		le, exists := localEnvs[re.Id]
		if !exists || re.UpdatedAt > le.UpdatedAt {
			out.Environments = append(out.Environments, re)
			report.Pulled++
		} else {
			out.Environments = append(out.Environments, le)
			if le.UpdatedAt > re.UpdatedAt {
				report.Pushed++
			}
		}
	}
	for _, le := range local.Environments {
		if !seenEnv[le.Id] {
			out.Environments = append(out.Environments, le)
			report.Pushed++
		}
	}

	// ── 全局变量（整体 LWW）──
	if remote.GlobalsRev > local.GlobalsRev {
		out.Globals = remote.Globals
		out.GlobalsRev = remote.GlobalsRev
	} else {
		out.Globals = local.Globals
		out.GlobalsRev = local.GlobalsRev
	}
	return out, report
}

// applyToLocal 把合并结果写回本地库，并确保目标工作区存在。
func applyToLocal(store *storage.Store, workspaceId string, merged *Snapshot) error {
	orderedNodes, err := validateAndOrderSyncNodes(merged.Nodes)
	if err != nil {
		return err
	}
	rows := make([]storage.SyncNodeRow, len(orderedNodes))
	for i, node := range orderedNodes {
		node.WorkspaceId = workspaceId
		rows[i] = storage.SyncNodeRow{Node: node.Node, DeletedAt: node.DeletedAt}
	}
	return store.ApplySyncSnapshot(storage.SyncSnapshotWrite{
		WorkspaceId:     workspaceId,
		WorkspaceName:   merged.WorkspaceName,
		Nodes:           rows,
		Environments:    merged.Environments,
		Globals:         merged.Globals,
		GlobalsRevision: merged.GlobalsRev,
	})
}

const maxSnapshotEntities = 100_000

func validateSnapshot(snapshot *Snapshot) error {
	if snapshot.SchemaVersion < 1 {
		return errors.New("sync snapshot schemaVersion is required")
	}
	if len(snapshot.Nodes) > maxSnapshotEntities || len(snapshot.Environments) > maxSnapshotEntities-len(snapshot.Nodes) {
		return fmt.Errorf("sync snapshot exceeds %d entity limit", maxSnapshotEntities)
	}
	if _, err := validateAndOrderSyncNodes(snapshot.Nodes); err != nil {
		return err
	}
	environmentIds := make(map[string]struct{}, len(snapshot.Environments))
	for _, environment := range snapshot.Environments {
		if strings.TrimSpace(environment.Id) == "" {
			return errors.New("sync environment id is required")
		}
		if _, duplicate := environmentIds[environment.Id]; duplicate {
			return fmt.Errorf("sync snapshot contains duplicate environment id %q", environment.Id)
		}
		environmentIds[environment.Id] = struct{}{}
	}
	return nil
}

func validateAndOrderSyncNodes(nodes []SyncNode) ([]SyncNode, error) {
	if len(nodes) > maxSnapshotEntities {
		return nil, fmt.Errorf("sync snapshot exceeds %d node limit", maxSnapshotEntities)
	}
	byId := make(map[string]SyncNode, len(nodes))
	indegree := make(map[string]int, len(nodes))
	children := make(map[string][]string, len(nodes))
	for _, node := range nodes {
		if strings.TrimSpace(node.Id) == "" {
			return nil, errors.New("sync node id is required")
		}
		if _, duplicate := byId[node.Id]; duplicate {
			return nil, fmt.Errorf("sync snapshot contains duplicate node id %q", node.Id)
		}
		if node.DeletedAt < 0 || node.CreatedAt < 0 || node.UpdatedAt < 0 {
			return nil, fmt.Errorf("sync node %q contains a negative timestamp", node.Id)
		}
		switch node.Kind {
		case "collection":
			if node.ParentId != "" {
				return nil, fmt.Errorf("sync collection %q must stay at root", node.Id)
			}
		case "folder", "request":
			if node.ParentId == "" {
				return nil, fmt.Errorf("sync node %q requires a parent", node.Id)
			}
		default:
			return nil, fmt.Errorf("sync node %q has invalid kind %q", node.Id, node.Kind)
		}
		byId[node.Id] = node
		indegree[node.Id] = 0
	}
	for _, node := range nodes {
		if node.ParentId == "" {
			continue
		}
		parent, exists := byId[node.ParentId]
		if !exists {
			return nil, fmt.Errorf("sync node %q references missing parent %q", node.Id, node.ParentId)
		}
		if parent.Kind != "collection" && parent.Kind != "folder" {
			return nil, fmt.Errorf("sync node %q has invalid parent kind %q", node.Id, parent.Kind)
		}
		if parent.DeletedAt > 0 && node.DeletedAt == 0 {
			return nil, fmt.Errorf("live sync node %q has deleted parent %q", node.Id, parent.Id)
		}
		indegree[node.Id] = 1
		children[node.ParentId] = append(children[node.ParentId], node.Id)
	}
	queue := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if indegree[node.Id] == 0 {
			queue = append(queue, node.Id)
		}
	}
	ordered := make([]SyncNode, 0, len(nodes))
	for cursor := 0; cursor < len(queue); cursor++ {
		id := queue[cursor]
		ordered = append(ordered, byId[id])
		for _, childId := range children[id] {
			indegree[childId]--
			if indegree[childId] == 0 {
				queue = append(queue, childId)
			}
		}
	}
	if len(ordered) != len(nodes) {
		return nil, errors.New("sync snapshot contains a parent cycle")
	}
	return ordered, nil
}

// Sync 执行一次双向同步：拉远端 → 合并 → 写回本地 → 推合并结果。
// 远端不存在时直接初始化上传。
func Sync(store *storage.Store, workspaceId string, cfg DavConfig) (*Report, error) {
	return SyncWithClient(store, workspaceId, cfg, nil)
}

// SyncWithClient executes WebDAV requests through the supplied network policy.
func SyncWithClient(store *storage.Store, workspaceId string, cfg DavConfig, httpClient *http.Client) (*Report, error) {
	client, err := newDavClientWithHTTP(cfg, httpClient)
	if err != nil {
		return nil, err
	}
	local, err := buildLocalSnapshot(store, workspaceId)
	if err != nil {
		return nil, model.WrapError(model.KindStorage, err)
	}
	path := remotePath(workspaceId)

	remoteData, exists, err := client.Get(path)
	if err != nil {
		return nil, err
	}

	var merged *Snapshot
	report := &Report{}
	if !exists {
		merged = local
		report.RemoteFresh = true
		report.Pushed = len(local.Nodes) + len(local.Environments)
	} else {
		var remote Snapshot
		if err := json.Unmarshal(remoteData, &remote); err != nil {
			return nil, model.NewError(model.KindImport, "remote snapshot corrupt: "+err.Error())
		}
		if remote.SchemaVersion > snapshotSchemaVersion {
			return nil, model.NewError(model.KindValidation,
				"remote snapshot from newer app version; please upgrade")
		}
		if err := validateSnapshot(&remote); err != nil {
			return nil, model.NewError(model.KindImport, "remote snapshot invalid: "+err.Error())
		}
		merged, report = merge(local, &remote)
		// Only snapshots explicitly stripped by their writer may borrow local values.
		restoreRemoteOmittedSecrets(merged, local, &remote)
		if err := applyToLocal(store, workspaceId, merged); err != nil {
			return nil, model.WrapError(model.KindStorage, err)
		}
	}

	// 推送合并结果（可选剥密钥）
	upload := *merged
	upload.SyncedAt = time.Now().UnixMilli()
	if cfg.OmitSecrets {
		// 深拷贝受影响切片再剥离，避免污染已写回本地的数据
		b, err := json.Marshal(upload)
		if err != nil {
			return nil, model.WrapError(model.KindValidation, err)
		}
		var clone Snapshot
		if err := json.Unmarshal(b, &clone); err != nil {
			return nil, model.WrapError(model.KindValidation, err)
		}
		stripSecrets(&clone)
		upload = clone
	}
	data, err := json.MarshalIndent(upload, "", "  ")
	if err != nil {
		return nil, model.WrapError(model.KindValidation, err)
	}
	if err := client.Put(path, data); err != nil {
		return nil, err
	}
	report.SyncedAt = upload.SyncedAt
	report.Remote = path
	return report, nil
}
