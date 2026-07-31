package sync

import (
	"encoding/base64"

	"apirequest/backend/model"
	"apirequest/backend/secrets"
)

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// Snapshot 工作区快照：远端存储的完整状态（快照文件模式，非 oplog——
// oplog 留给未来的实时协作；快照对 WebDAV 这种"哑存储"更稳）
type Snapshot struct {
	SchemaVersion  int    `json:"schemaVersion"`
	WorkspaceName  string `json:"workspaceName"`
	SyncedAt       int64  `json:"syncedAt"` // Unix ms
	SecretsOmitted bool   `json:"secretsOmitted,omitempty"`

	// Nodes 含墓碑（DeletedAt>0）：删除也要传播
	Nodes        []SyncNode          `json:"nodes"`
	Environments []model.Environment `json:"environments"`
	Globals      []model.Variable    `json:"globals"`
	GlobalsRev   int64               `json:"globalsRev"` // 全局变量整体一个 rev
}

// SyncNode 同步用节点：完整字段 + 删除墓碑
type SyncNode struct {
	model.Node
	DeletedAt int64 `json:"deletedAt,omitempty"`
}

// rev 实体版本：LWW 比较键。删除时间也算修改。
func (n SyncNode) rev() int64 {
	if n.DeletedAt > n.UpdatedAt {
		return n.DeletedAt
	}
	return n.UpdatedAt
}

const snapshotSchemaVersion = 2

// stripSecrets 剥离密钥变量值（保留键名占位，拉取端本地补回）
func stripSecrets(snap *Snapshot) {
	snap.SecretsOmitted = true
	for i := range snap.Nodes {
		snap.Nodes[i].Node = secrets.OmitNodeSecrets(snap.Nodes[i].Node)
	}
	for i := range snap.Environments {
		snap.Environments[i].Variables = secrets.OmitVariables(snap.Environments[i].Variables)
	}
	snap.Globals = secrets.OmitVariables(snap.Globals)
}

func restoreRemoteOmittedSecrets(merged, local, remote *Snapshot) {
	// Version 1 did not record whether values were stripped. Preserve its
	// conservative behavior so existing secret-omitting snapshots stay usable.
	if remote.SecretsOmitted || remote.SchemaVersion < 2 {
		restoreLocalSecrets(merged, local)
	}
}

// restoreLocalSecrets 用本地值补回被剥离的密钥（合并后调用）
func restoreLocalSecrets(merged, local *Snapshot) {
	localNodes := map[string]model.Node{}
	for _, node := range local.Nodes {
		localNodes[node.Id] = node.Node
	}
	for i := range merged.Nodes {
		if localNode, ok := localNodes[merged.Nodes[i].Id]; ok {
			secrets.RestoreOmittedNodeSecrets(&merged.Nodes[i].Node, localNode)
		}
	}
	localEnvironments := map[string][]model.Variable{}
	for _, e := range local.Environments {
		localEnvironments[e.Id] = e.Variables
	}
	for i := range merged.Environments {
		if localVariables, ok := localEnvironments[merged.Environments[i].Id]; ok {
			merged.Environments[i].Variables = secrets.RestoreOmittedVariables(
				merged.Environments[i].Variables, localVariables,
			)
		}
	}
	merged.Globals = secrets.RestoreOmittedVariables(merged.Globals, local.Globals)
}
