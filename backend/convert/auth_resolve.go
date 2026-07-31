package convert

import (
	"apirequest/backend/model"
)

// resolveAuth 解析"可继承"的 Auth：请求级 none 是显式关闭，只有空值/inherit
// 才按父链向上查找；父节点的 none 与运行时 resolveInheritedAuth 一致，视为未配置。
// 若到 collection 根都无有效 Auth，返回零值 Auth（Type=""）。
//
// 用途：导出（OpenAPI/cURL）需要把集合/文件夹级 Auth 落到每个请求上，
// 而内部 model.Node.Auth 是 *Auth（collection/folder 的可继承 Auth 配置）；
// request 节点本身的 Request.Auth 是 Auth（值类型，type=inherit 表示用上层）。
func resolveAuth(req model.Node, byId map[string]model.Node, collection model.Node) model.Auth {
	// 1) request 自身的 Request.Auth
	if req.Request != nil {
		a := req.Request.Auth
		if a.Type == "none" {
			return a
		}
		if a.Type != "" && a.Type != "inherit" {
			return a
		}
	}
	// 2) 从该 request 节点本身开始，按 ParentId 链向上找 *Node.Auth
	cur := req
	for {
		if cur.Auth != nil {
			t := cur.Auth.Type
			if t != "" && t != "none" && t != "inherit" {
				return *cur.Auth
			}
		}
		if cur.ParentId == "" || cur.ParentId == collection.Id {
			break
		}
		parent, ok := byId[cur.ParentId]
		if !ok {
			break
		}
		cur = parent
	}
	// 3) collection 根自身的 Auth
	if collection.Auth != nil {
		t := collection.Auth.Type
		if t != "" && t != "none" && t != "inherit" {
			return *collection.Auth
		}
	}
	return model.Auth{}
}
