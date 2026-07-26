package sync

import (
	"encoding/base64"

	"apirequest/backend/model"
)

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// Snapshot 工作区快照：远端存储的完整状态（快照文件模式，非 oplog——
// oplog 留给未来的实时协作；快照对 WebDAV 这种"哑存储"更稳）
type Snapshot struct {
	SchemaVersion int    `json:"schemaVersion"`
	WorkspaceName string `json:"workspaceName"`
	SyncedAt      int64  `json:"syncedAt"` // Unix ms

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

const snapshotSchemaVersion = 1

// stripSecrets 剥离密钥变量值（保留键名占位，拉取端本地补回）
func stripSecrets(snap *Snapshot) {
	for i := range snap.Environments {
		for j := range snap.Environments[i].Variables {
			if snap.Environments[i].Variables[j].Type == "secret" {
				snap.Environments[i].Variables[j].Value = ""
			}
		}
	}
	for j := range snap.Globals {
		if snap.Globals[j].Type == "secret" {
			snap.Globals[j].Value = ""
		}
	}
}

// restoreLocalSecrets 用本地值补回被剥离的密钥（合并后调用）
func restoreLocalSecrets(merged *Snapshot, localEnvs []model.Environment, localGlobals []model.Variable) {
	envSecret := map[string]string{} // envName/key → value
	for _, e := range localEnvs {
		for _, v := range e.Variables {
			if v.Type == "secret" && v.Value != "" {
				envSecret[e.Name+"/"+v.Key] = v.Value
			}
		}
	}
	for i := range merged.Environments {
		for j := range merged.Environments[i].Variables {
			v := &merged.Environments[i].Variables[j]
			if v.Type == "secret" && v.Value == "" {
				if lv, ok := envSecret[merged.Environments[i].Name+"/"+v.Key]; ok {
					v.Value = lv
				}
			}
		}
	}
	globalSecret := map[string]string{}
	for _, v := range localGlobals {
		if v.Type == "secret" && v.Value != "" {
			globalSecret[v.Key] = v.Value
		}
	}
	for j := range merged.Globals {
		v := &merged.Globals[j]
		if v.Type == "secret" && v.Value == "" {
			if lv, ok := globalSecret[v.Key]; ok {
				v.Value = lv
			}
		}
	}
}
