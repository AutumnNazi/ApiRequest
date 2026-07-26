package sync

import (
	"encoding/json"
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
func buildLocalSnapshot(store *storage.Store, workspaceId string) (*Snapshot, []model.Environment, []model.Variable, error) {
	workspaces, err := store.ListWorkspaces()
	if err != nil {
		return nil, nil, nil, err
	}
	wsName := ""
	for _, w := range workspaces {
		if w.Id == workspaceId {
			wsName = w.Name
			break
		}
	}
	rows, err := store.ListNodesForSync(workspaceId)
	if err != nil {
		return nil, nil, nil, err
	}
	envs, err := store.ListEnvironments(workspaceId)
	if err != nil {
		return nil, nil, nil, err
	}
	globals, err := store.GetGlobalVariables(workspaceId)
	if err != nil {
		return nil, nil, nil, err
	}

	snap := &Snapshot{
		SchemaVersion: snapshotSchemaVersion,
		WorkspaceName: wsName,
		SyncedAt:      time.Now().UnixMilli(),
		Environments:  envs,
		Globals:       globals,
	}
	var globalsRev int64
	for _, row := range rows {
		snap.Nodes = append(snap.Nodes, SyncNode{Node: row.Node, DeletedAt: row.DeletedAt})
	}
	// globals 无独立时间戳：用工作区内最近一次节点/环境修改近似
	for _, n := range snap.Nodes {
		if n.rev() > globalsRev {
			globalsRev = n.rev()
		}
	}
	for _, e := range envs {
		if e.UpdatedAt > globalsRev {
			globalsRev = e.UpdatedAt
		}
	}
	snap.GlobalsRev = globalsRev
	return snap, envs, globals, nil
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

// applyToLocal 把合并结果写回本地库（工作区不存在则先建，支持拉取远端新工作区）
func applyToLocal(store *storage.Store, workspaceId string, merged *Snapshot) error {
	if err := store.EnsureWorkspace(workspaceId, merged.WorkspaceName); err != nil {
		return err
	}
	for _, n := range merged.Nodes {
		n.WorkspaceId = workspaceId
		if err := store.ApplySyncNode(storage.SyncNodeRow{Node: n.Node, DeletedAt: n.DeletedAt}); err != nil {
			return err
		}
	}
	for _, e := range merged.Environments {
		e.WorkspaceId = workspaceId
		if err := store.ApplySyncEnvironment(e); err != nil {
			return err
		}
	}
	return store.SetGlobalVariables(workspaceId, merged.Globals)
}

// Sync 执行一次双向同步：拉远端 → 合并 → 写回本地 → 推合并结果。
// 远端不存在时直接初始化上传。
func Sync(store *storage.Store, workspaceId string, cfg DavConfig) (*Report, error) {
	client, err := newDavClient(cfg)
	if err != nil {
		return nil, err
	}
	local, localEnvs, localGlobals, err := buildLocalSnapshot(store, workspaceId)
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
		merged, report = merge(local, &remote)
		// 密钥补回本地值（远端可能是剥离过的）
		restoreLocalSecrets(merged, localEnvs, localGlobals)
		if err := applyToLocal(store, workspaceId, merged); err != nil {
			return nil, model.WrapError(model.KindStorage, err)
		}
	}

	// 推送合并结果（可选剥密钥）
	upload := *merged
	upload.SyncedAt = time.Now().UnixMilli()
	if cfg.OmitSecrets {
		// 深拷贝受影响切片再剥离，避免污染已写回本地的数据
		b, _ := json.Marshal(upload)
		var clone Snapshot
		json.Unmarshal(b, &clone)
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
