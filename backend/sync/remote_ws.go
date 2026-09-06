// 远端工作区发现与导入（docs/sync.md）：新设备以远端快照为源头建立本地工作区，
// 工作区 id 与远端快照文件保持一致（远端路径由 workspaceId 决定），导入后即可常规双向同步。
package sync

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"apirequest/backend/model"
	"apirequest/backend/storage"
)

// RemoteWorkspaceInfo 远端工作区快照摘要
type RemoteWorkspaceInfo struct {
	WorkspaceId string `json:"workspaceId"`
	Name        string `json:"name"`
	SyncedAt    int64  `json:"syncedAt"`
}

// DiscoverRemoteWorkspaces 通过 PROPFIND 列出远端 ApiRequest/ 目录下的工作区快照，
// 并逐个 GET 读取名称与同步时间（单个快照损坏只跳过该条，不影响整体发现）。
func DiscoverRemoteWorkspaces(cfg DavConfig) ([]RemoteWorkspaceInfo, error) {
	infos, err := discoverWithClient(context.Background(), cfg, nil)
	if err != nil {
		return nil, err
	}
	return infos, nil
}

func discoverWithClient(ctx context.Context, cfg DavConfig, httpClient *http.Client) ([]RemoteWorkspaceInfo, error) {
	client, err := newDavClientWithHTTP(cfg, httpClient)
	if err != nil {
		return nil, err
	}
	hrefs, err := client.propfind(ctx, "ApiRequest/")
	if err != nil {
		return nil, err
	}
	var infos []RemoteWorkspaceInfo
	for _, href := range hrefs {
		id, ok := workspaceIdFromHref(href)
		if !ok {
			continue
		}
		data, exists, _, err := client.Get(ctx, remotePath(id))
		if err != nil || !exists {
			continue // 快照暂时读不到：跳过而非整体失败
		}
		var head struct {
			SchemaVersion int    `json:"schemaVersion"`
			WorkspaceName string `json:"workspaceName"`
			SyncedAt      int64  `json:"syncedAt"`
		}
		if err := json.Unmarshal(data, &head); err != nil {
			continue
		}
		infos = append(infos, RemoteWorkspaceInfo{
			WorkspaceId: id,
			Name:        head.WorkspaceName,
			SyncedAt:    head.SyncedAt,
		})
	}
	return infos, nil
}

// ImportWorkspace 把远端快照导入为本地工作区：id 沿用远端（保证后续同步命中同一
// 快照路径），实体经 applyToLocal 落库，并写入合并基线（下轮起即字段级三路合并）。
// 远端已存在快照，导入后无需回推。
func ImportWorkspace(store *storage.Store, cfg DavConfig, workspaceId string) (*Report, error) {
	return importWithClient(context.Background(), store, cfg, workspaceId, nil)
}

func importWithClient(ctx context.Context, store *storage.Store, cfg DavConfig, workspaceId string, httpClient *http.Client) (*Report, error) {
	if strings.TrimSpace(workspaceId) == "" {
		return nil, model.NewError(model.KindValidation, "workspaceId is required")
	}
	client, err := newDavClientWithHTTP(cfg, httpClient)
	if err != nil {
		return nil, err
	}
	data, exists, _, err := client.Get(ctx, remotePath(workspaceId))
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, model.NewError(model.KindValidation, "remote workspace not found: "+workspaceId)
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, model.NewError(model.KindImport, "remote snapshot corrupt: "+err.Error())
	}
	if snap.SchemaVersion > snapshotSchemaVersion {
		return nil, model.NewError(model.KindValidation,
			"remote snapshot from newer app version; please upgrade")
	}
	if err := validateSnapshot(&snap); err != nil {
		return nil, model.NewError(model.KindImport, "remote snapshot invalid: "+err.Error())
	}
	// 已存在同 id 本地工作区：拒绝重复导入（用普通同步接入即可）
	workspaces, err := store.ListWorkspaces()
	if err != nil {
		return nil, model.WrapError(model.KindStorage, err)
	}
	for _, w := range workspaces {
		if w.Id == workspaceId {
			return nil, model.NewError(model.KindValidation,
				"workspace already exists locally: "+workspaceId)
		}
	}
	// 归一归属：导入即"收编"该快照的所有实体到目标工作区（与本地重建的快照
	// 保持一致，否则基线与首轮合并会因 workspaceId 差异产生伪变更）
	for i := range snap.Nodes {
		snap.Nodes[i].WorkspaceId = workspaceId
	}
	for i := range snap.Environments {
		snap.Environments[i].WorkspaceId = workspaceId
	}
	if err := store.EnsureWorkspace(workspaceId, snap.WorkspaceName); err != nil {
		return nil, model.WrapError(model.KindStorage, err)
	}
	if err := applyToLocal(store, workspaceId, &snap); err != nil {
		return nil, model.WrapError(model.KindStorage, err)
	}
	baseJSON, err := json.Marshal(&snap)
	if err == nil {
		_ = store.PutSyncBase(workspaceId, snapshotSchemaVersion, string(baseJSON))
	}
	return &Report{
		Pulled:   len(snap.Nodes) + len(snap.Environments),
		SyncedAt: time.Now().UnixMilli(),
		Remote:   remotePath(workspaceId),
	}, nil
}

// propfind Depth:1 列目录，返回成员 href（目录自身也在内，由调用方过滤）
func (c *davClient) propfind(ctx context.Context, rel string) ([]string, error) {
	resp, err := c.do(ctx, "PROPFIND", rel, nil, davTransferTimeout(0), map[string]string{"Depth": "1"})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil // 目录未建立 = 远端还没有任何工作区
	}
	if resp.StatusCode >= 300 {
		return nil, davError("PROPFIND", rel, resp)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, model.WrapError(model.KindNetwork, err)
	}
	var hrefs []string
	decoder := xml.NewDecoder(strings.NewReader(string(data)))
	depth := 0
	inHref := false
	var current string
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, model.NewError(model.KindImport, "WebDAV listing invalid: "+err.Error())
		}
		switch t := token.(type) {
		case xml.StartElement:
			depth++
			if t.Name.Local == "href" {
				inHref = true
				current = ""
			}
		case xml.EndElement:
			if inHref && t.Name.Local == "href" {
				inHref = false
				hrefs = append(hrefs, current)
			}
			depth--
		case xml.CharData:
			if inHref {
				current += string(t)
			}
		}
	}
	return hrefs, nil
}

// workspaceIdFromHref 从 multistatus href 提取工作区 id：
// 形如 /dav/ApiRequest/workspace-<id>.json（href 可能是 URL 编码的）
func workspaceIdFromHref(href string) (string, bool) {
	unescaped, err := url.PathUnescape(href)
	if err != nil {
		return "", false
	}
	name := path.Base(unescaped)
	if !strings.HasPrefix(name, "workspace-") || !strings.HasSuffix(name, ".json") {
		return "", false
	}
	id := strings.TrimSuffix(strings.TrimPrefix(name, "workspace-"), ".json")
	if id == "" || id == "/" || strings.Contains(id, "/") {
		return "", false
	}
	return id, true
}
