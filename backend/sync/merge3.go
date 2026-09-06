// 字段级三路合并（ADR-017，docs/decisions.md）：本地"上次合并基线"作公共祖先。
// 对象逐键递归（数组视作原子值），结构性操作（移动/墓碑）保持实体级 LWW；
// 双端改同一字段 → 确定性"本地胜出"并记录冲突清单，不弹交互式合并。
package sync

import (
	"encoding/json"
	"reflect"
	"sort"

	"apirequest/backend/model"
	"apirequest/backend/storage"
)

const (
	conflictListCap    = 100 // 冲突清单最多返回的条数（超出只丢明细，不丢事实）
	conflictValueLimit = 160 // 冲突值预览的最大字符数
)

// SyncConflict 一次字段级冲突（结果已按"本地胜出"落库，仅用于告知用户）
type SyncConflict struct {
	EntityType  string `json:"entityType"` // node | environment | workspace
	EntityId    string `json:"entityId"`
	EntityName  string `json:"entityName"`
	Field       string `json:"field"`
	LocalValue  string `json:"localValue"`
	RemoteValue string `json:"remoteValue"`
}

type conflictRecorder struct {
	list []SyncConflict
}

func (c *conflictRecorder) add(entityType, entityId, entityName, field string, localVal, remoteVal any) {
	if len(c.list) >= conflictListCap {
		return
	}
	c.list = append(c.list, SyncConflict{
		EntityType:  entityType,
		EntityId:    entityId,
		EntityName:  entityName,
		Field:       field,
		LocalValue:  valuePreview(localVal),
		RemoteValue: valuePreview(remoteVal),
	})
}

func valuePreview(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "?"
	}
	s := string(b)
	if len(s) > conflictValueLimit {
		s = s[:conflictValueLimit] + "…"
	}
	return s
}

// scalar3 标量三路：返回采纳值与"双端都改了"（冲突）标记
func scalar3[T comparable](l, r, b T) (T, bool) {
	switch {
	case l == r:
		return l, false
	case l == b:
		return r, false
	case r == b:
		return l, false
	default:
		return l, true // 本地胜出
	}
}

// mergeThreeWay 基于基线的三路合并。基线中不存在的实体按实体级 LWW 处理。
func mergeThreeWay(local, remote, base *Snapshot) (*Snapshot, *Report, []SyncConflict) {
	report := &Report{}
	rec := &conflictRecorder{}
	out := &Snapshot{SchemaVersion: snapshotSchemaVersion}

	// 工作区名：单字段三路
	if name, conflict := scalar3(local.WorkspaceName, remote.WorkspaceName, base.WorkspaceName); conflict {
		out.WorkspaceName = name
		rec.add("workspace", "", name, "workspaceName", local.WorkspaceName, remote.WorkspaceName)
	} else {
		out.WorkspaceName = name
	}

	// ── 节点 ──
	localNodes := map[string]SyncNode{}
	for _, n := range local.Nodes {
		localNodes[n.Id] = n
	}
	baseNodes := map[string]SyncNode{}
	for _, n := range base.Nodes {
		baseNodes[n.Id] = n
	}
	seen := map[string]bool{}
	for _, rn := range remote.Nodes {
		seen[rn.Id] = true
		ln, exists := localNodes[rn.Id]
		if !exists {
			out.Nodes = append(out.Nodes, rn)
			if rn.DeletedAt == 0 {
				report.Pulled++
			}
			continue
		}
		merged := mergeNodeThreeWay(ln, rn, baseNodes[rn.Id], rec)
		out.Nodes = append(out.Nodes, merged)
		// 计数：结果相对每侧的变化（字段合并可能双侧同时变更）
		if !reflect.DeepEqual(merged, ln) {
			if merged.DeletedAt > 0 && ln.DeletedAt == 0 {
				report.Deleted++
			} else {
				report.Pulled++
			}
		}
		if !reflect.DeepEqual(merged, rn) && merged.DeletedAt == 0 {
			report.Pushed++
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

	// ── 环境（无墓碑；name / variables 两个内容字段三路）──
	localEnvs := map[string]model.Environment{}
	for _, e := range local.Environments {
		localEnvs[e.Id] = e
	}
	baseEnvs := map[string]model.Environment{}
	for _, e := range base.Environments {
		baseEnvs[e.Id] = e
	}
	seenEnv := map[string]bool{}
	for _, re := range remote.Environments {
		seenEnv[re.Id] = true
		le, exists := localEnvs[re.Id]
		if !exists {
			out.Environments = append(out.Environments, re)
			report.Pulled++
			continue
		}
		merged := le
		be, hasBase := baseEnvs[re.Id]
		if hasBase {
			name, nameConflict := scalar3(le.Name, re.Name, be.Name)
			merged.Name = name
			lv, rv, bv := jsonValue(le.Variables), jsonValue(re.Variables), jsonValue(be.Variables)
			varVal, varConflict := scalar3(lv, rv, bv)
			switch varVal {
			case lv:
				// 本地胜出（含无变化）
			case rv:
				merged.Variables = re.Variables
			}
			merged.UpdatedAt = maxInt64(le.UpdatedAt, re.UpdatedAt)
			if !reflect.DeepEqual(merged, le) {
				report.Pulled++
			} else if !reflect.DeepEqual(merged, re) {
				report.Pushed++
			}
			if nameConflict {
				rec.add("environment", re.Id, le.Name, "name", le.Name, re.Name)
			}
			if varConflict {
				rec.add("environment", re.Id, le.Name, "variables", le.Variables, re.Variables)
			}
		} else {
			// 无基线：实体级 LWW（与旧 merge 一致）
			if re.UpdatedAt > le.UpdatedAt {
				merged = re
				report.Pulled++
			} else if le.UpdatedAt > re.UpdatedAt {
				report.Pushed++
			}
		}
		out.Environments = append(out.Environments, merged)
	}
	for _, le := range local.Environments {
		if !seenEnv[le.Id] {
			out.Environments = append(out.Environments, le)
			report.Pushed++
		}
	}

	// ── 全局变量（ADR-017 维持整体 LWW，不逐字段）──
	if remote.GlobalsRev > local.GlobalsRev {
		out.Globals = remote.Globals
		out.GlobalsRev = remote.GlobalsRev
		report.Pulled++
	} else {
		out.Globals = local.Globals
		out.GlobalsRev = local.GlobalsRev
		if local.GlobalsRev > remote.GlobalsRev {
			report.Pushed++
		}
	}
	return out, report, rec.list
}

// mergeNodeThreeWay 合并双端同存的节点：结构（父节点/墓碑）实体级 LWW，
// 内容字段（name/request/auth/variables/scripts/sortOrder）三路逐字段。
func mergeNodeThreeWay(ln, rn, bn SyncNode, rec *conflictRecorder) SyncNode {
	survivor := ln
	if rn.rev() > ln.rev() {
		survivor = rn
	}
	// 基线缺失或墓碑胜出：整实体取 survivor（与既有 LWW 语义一致，不做内容合并）
	if bn.Id == "" || survivor.DeletedAt > 0 {
		return survivor
	}

	merged := survivor // 结构字段（ParentId/Kind/Id/CreatedAt/WorkspaceId）随 rev 新者
	if name, conflict := scalar3(ln.Name, rn.Name, bn.Name); conflict {
		merged.Name = name
		rec.add("node", ln.Id, ln.Name, "name", ln.Name, rn.Name)
	} else {
		merged.Name = name
	}
	if order, conflict := scalar3(ln.SortOrder, rn.SortOrder, bn.SortOrder); conflict {
		merged.SortOrder = order
		rec.add("node", ln.Id, ln.Name, "sortOrder", ln.SortOrder, rn.SortOrder)
	} else {
		merged.SortOrder = order
	}
	merged.UpdatedAt = maxInt64(ln.UpdatedAt, rn.UpdatedAt)
	merged.Request = mergeNodeRequest(ln, rn, bn, rec)
	merged.Auth = mergeFieldPointer(ln.Auth, rn.Auth, bn.Auth, "auth", ln, rec)
	merged.Variables = mergeFieldVariables(ln, rn, bn, rec)
	if pre, conflict := scalar3(ln.PreScript, rn.PreScript, bn.PreScript); conflict {
		rec.add("node", ln.Id, ln.Name, "preScript", ln.PreScript, rn.PreScript)
		merged.PreScript = pre
	} else {
		merged.PreScript = pre
	}
	if test, conflict := scalar3(ln.TestScript, rn.TestScript, bn.TestScript); conflict {
		rec.add("node", ln.Id, ln.Name, "testScript", ln.TestScript, rn.TestScript)
		merged.TestScript = test
	} else {
		merged.TestScript = test
	}
	return merged
}

// mergeNodeRequest 合并 request 指针字段：整体缺失语义 + 对象逐键递归。
func mergeNodeRequest(ln, rn, bn SyncNode, rec *conflictRecorder) *model.HttpRequest {
	if ln.Request == nil && rn.Request == nil {
		return nil
	}
	if ln.Request == nil || rn.Request == nil || bn.Request == nil {
		lv, rv, bv := jsonValue(ln.Request), jsonValue(rn.Request), jsonValue(bn.Request)
		val, conflict := scalar3(lv, rv, bv)
		switch val {
		case lv:
			if conflict {
				rec.add("node", ln.Id, ln.Name, "request", lv, rv)
			}
			return ln.Request
		case rv:
			if conflict {
				rec.add("node", ln.Id, ln.Name, "request", lv, rv)
			}
			return rn.Request
		default: // bv：双侧都无 request
			return nil
		}
	}
	mergedAny := mergeJSONValue(toMap(ln.Request), toMap(rn.Request), toMap(bn.Request), "request", ln, rec)
	if m, ok := mergedAny.(map[string]any); ok {
		b, err := json.Marshal(m)
		if err == nil {
			var req model.HttpRequest
			if err := json.Unmarshal(b, &req); err == nil {
				return &req
			}
		}
	}
	// 不可重建（理论上不可达）：退回 rev 新者的 request
	if rn.rev() > ln.rev() {
		return rn.Request
	}
	return ln.Request
}

// mergeJSONValue 通用三路 JSON 合并：对象逐键递归，数组/标量原子。
// 冲突即 rec.add（本地胜出）。
func mergeJSONValue(l, r, b any, path string, entity SyncNode, rec *conflictRecorder) any {
	lm, lok := l.(map[string]any)
	rm, rok := r.(map[string]any)
	bm, bok := b.(map[string]any)
	if lok && rok && bok {
		keys := map[string]bool{}
		for k := range lm {
			keys[k] = true
		}
		for k := range rm {
			keys[k] = true
		}
		sorted := make([]string, 0, len(keys))
		for k := range keys {
			sorted = append(sorted, k)
		}
		sort.Strings(sorted)
		out := map[string]any{}
		for _, k := range sorted {
			merged := mergeJSONValue(lm[k], rm[k], bm[k], path+"."+k, entity, rec)
			if merged != nil {
				out[k] = merged
			}
		}
		return out
	}
	if reflect.DeepEqual(l, r) {
		return l
	}
	if reflect.DeepEqual(l, b) {
		return r
	}
	if reflect.DeepEqual(r, b) {
		return l
	}
	rec.add("node", entity.Id, entity.Name, path, l, r)
	return l
}

// mergeFieldPointer auth 等指针字段：整体为一个字段值的三路
func mergeFieldPointer[T any](l, r, b *T, field string, entity SyncNode, rec *conflictRecorder) *T {
	lv, rv, bv := jsonValue(l), jsonValue(r), jsonValue(b)
	val, conflict := scalar3(lv, rv, bv)
	switch val {
	case lv:
		if conflict {
			rec.add("node", entity.Id, entity.Name, field, lv, rv)
		}
		return l
	case rv:
		if conflict {
			rec.add("node", entity.Id, entity.Name, field, lv, rv)
		}
		return r
	default:
		return nil
	}
}

func mergeFieldVariables(ln, rn, bn SyncNode, rec *conflictRecorder) []model.Variable {
	lv, rv, bv := jsonValue(ln.Variables), jsonValue(rn.Variables), jsonValue(bn.Variables)
	val, conflict := scalar3(lv, rv, bv)
	switch val {
	case lv:
		if conflict {
			rec.add("node", ln.Id, ln.Name, "variables", ln.Variables, rn.Variables)
		}
		return ln.Variables
	case rv:
		if conflict {
			rec.add("node", ln.Id, ln.Name, "variables", ln.Variables, rn.Variables)
		}
		return rn.Variables
	default:
		return nil
	}
}

func jsonValue(v any) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func toMap(v any) map[string]any {
	b, err := json.Marshal(v)
	if err != nil {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return map[string]any{}
	}
	return m
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// loadSyncBase 读取可用基线：不存在、版本不符或内容损坏时返回 nil（并清掉坏基线），
// 该轮回退实体级 LWW。
func loadSyncBase(store *storage.Store, workspaceId string) *Snapshot {
	version, raw, ok, err := store.GetSyncBase(workspaceId)
	if err != nil || !ok {
		return nil
	}
	if version != snapshotSchemaVersion {
		_ = store.DeleteSyncBase(workspaceId)
		return nil
	}
	var base Snapshot
	if err := json.Unmarshal([]byte(raw), &base); err != nil || validateSnapshot(&base) != nil {
		_ = store.DeleteSyncBase(workspaceId)
		return nil
	}
	return &base
}
