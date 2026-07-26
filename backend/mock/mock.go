// Package mock 实现 Mock Server（docs/advanced.md §1）。
// 以 collection 为单位启停，Example 为数据源，按路径/方法打分匹配。
package mock

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"apirequest/backend/model"
	"apirequest/backend/template"
)

// route 一个可匹配的请求节点及其 examples
type route struct {
	nodeId   string
	name     string
	method   string
	segments []string // path 段；{{var}} 与 :param 为通配段
	examples []model.Example
}

// LogFunc 命中日志回调（binding 层转 mock:log 事件）
type LogFunc func(method, path, matched string, status int)

// Server 单个集合的 mock 服务
type Server struct {
	collectionId string
	httpSrv      *http.Server
	Addr         string
	routes       []route
	delayMs      int
	onLog        LogFunc
}

// Options 启动选项
type Options struct {
	Port    int `json:"port,omitempty"`    // 0 = 自动从 3600 起探测
	DelayMs int `json:"delayMs,omitempty"` // 模拟延迟
}

// Manager 管理全部运行中的 mock server
type Manager struct {
	mu      sync.Mutex
	servers map[string]*Server // collectionId → server
}

// NewManager 构造
func NewManager() *Manager {
	return &Manager{servers: map[string]*Server{}}
}

// Start 启动集合的 mock（已运行则先停）。
// nodes 为集合的全部后代节点，examples 为其下全部示例。
func (m *Manager) Start(collectionId string, nodes []model.Node, examples []model.Example, opts Options, onLog LogFunc) (*Server, error) {
	m.Stop(collectionId)

	// 组路由：request 节点 + 其 examples
	exByNode := map[string][]model.Example{}
	for _, e := range examples {
		exByNode[e.NodeId] = append(exByNode[e.NodeId], e)
	}
	var routes []route
	for _, n := range nodes {
		if n.Kind != "request" || n.Request == nil {
			continue
		}
		exs := exByNode[n.Id]
		if len(exs) == 0 {
			continue // 无示例的请求不可 mock
		}
		routes = append(routes, route{
			nodeId:   n.Id,
			name:     n.Name,
			method:   strings.ToUpper(n.Request.Method),
			segments: pathSegments(n.Request.Url),
			examples: exs,
		})
	}
	if len(routes) == 0 {
		return nil, model.NewError(model.KindValidation, "collection has no examples to mock (save a response as example first)")
	}

	// 端口：指定或从 3600 探测
	port := opts.Port
	var ln net.Listener
	var err error
	if port > 0 {
		ln, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			return nil, model.WrapError(model.KindNetwork, err)
		}
	} else {
		for p := 3600; p < 3700; p++ {
			ln, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
			if err == nil {
				break
			}
		}
		if ln == nil {
			return nil, model.NewError(model.KindNetwork, "no free port in 3600-3699")
		}
	}

	srv := &Server{
		collectionId: collectionId,
		routes:       routes,
		delayMs:      opts.DelayMs,
		Addr:         "http://" + ln.Addr().String(),
		onLog:        onLog,
	}
	srv.httpSrv = &http.Server{Handler: srv}
	go srv.httpSrv.Serve(ln)

	m.mu.Lock()
	m.servers[collectionId] = srv
	m.mu.Unlock()
	return srv, nil
}

// Stop 停止集合的 mock（未运行为 no-op）
func (m *Manager) Stop(collectionId string) {
	m.mu.Lock()
	srv, ok := m.servers[collectionId]
	delete(m.servers, collectionId)
	m.mu.Unlock()
	if ok {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		srv.httpSrv.Shutdown(ctx)
		cancel()
	}
}

// StopAll 应用退出时统一关闭
func (m *Manager) StopAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.servers))
	for id := range m.servers {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.Stop(id)
	}
}

// Running 返回运行中的 collectionId → 地址
func (m *Manager) Running() map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]string{}
	for id, s := range m.servers {
		out[id] = s.Addr
	}
	return out
}

// ServeHTTP 匹配算法见 docs/advanced.md §1.2
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// CORS 宽松放行（mock 的典型消费方是本地前端开发）
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "*")
	w.Header().Set("Access-Control-Allow-Headers", "*")
	if r.Method == http.MethodOptions {
		w.WriteHeader(204)
		return
	}

	if s.delayMs > 0 {
		time.Sleep(time.Duration(s.delayMs) * time.Millisecond)
	}

	rt := s.match(r.Method, r.URL.Path)
	if rt == nil {
		s.log(r, "", 404)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(404)
		json.NewEncoder(w).Encode(map[string]any{
			"error":      "no mock matched",
			"method":     r.Method,
			"path":       r.URL.Path,
			"candidates": s.candidatePaths(),
		})
		return
	}

	ex := pickExample(rt.examples, r)
	for _, h := range ex.Headers {
		if h.Key != "" {
			w.Header().Set(h.Key, h.Value)
		}
	}
	status := ex.Status
	if status == 0 {
		status = 200
	}
	s.log(r, rt.name, status)
	w.WriteHeader(status)
	// body 中的 {{$...}} 动态变量渲染；普通 {{var}} 无环境上下文不解析
	w.Write([]byte(template.Resolve(ex.Body, template.NewScope())))
}

func (s *Server) log(r *http.Request, matched string, status int) {
	if s.onLog != nil {
		s.onLog(r.Method, r.URL.Path, matched, status)
	}
}

func (s *Server) candidatePaths() []string {
	var out []string
	for _, rt := range s.routes {
		out = append(out, rt.method+" /"+strings.Join(rt.segments, "/"))
	}
	sort.Strings(out)
	return out
}

// match 打分匹配：method 相同 → 字面段多者优先 → 段数长者优先
func (s *Server) match(method, path string) *route {
	segs := splitPath(path)
	type scored struct {
		rt      *route
		literal int
		total   int
	}
	var best *scored
	consider := func(rt *route, methodMatch bool) {
		if !matchSegments(rt.segments, segs) {
			return
		}
		literal := 0
		for _, s := range rt.segments {
			if !isWildcard(s) {
				literal++
			}
		}
		c := &scored{rt: rt, literal: literal, total: len(rt.segments)}
		if best == nil ||
			c.literal > best.literal ||
			(c.literal == best.literal && c.total > best.total) {
			best = c
		}
		_ = methodMatch
	}
	// 先严格 method 匹配
	for i := range s.routes {
		if s.routes[i].method == strings.ToUpper(method) {
			consider(&s.routes[i], true)
		}
	}
	if best != nil {
		return best.rt
	}
	// 放宽：任意 method（docs/advanced.md：无匹配时降级）
	for i := range s.routes {
		consider(&s.routes[i], false)
	}
	if best != nil {
		return best.rt
	}
	return nil
}

// pickExample 选择响应（docs/advanced.md §1.2 第 4 步）
func pickExample(examples []model.Example, r *http.Request) model.Example {
	if name := r.Header.Get("x-mock-response-name"); name != "" {
		for _, e := range examples {
			if e.Name == name {
				return e
			}
		}
	}
	if code := r.Header.Get("x-mock-response-code"); code != "" {
		for _, e := range examples {
			if fmt.Sprintf("%d", e.Status) == code {
				return e
			}
		}
	}
	return examples[0]
}

// pathSegments 从请求 URL 提取 path 段
func pathSegments(rawUrl string) []string {
	// {{var}} 先占位替换避免 url.Parse 失败
	safe := strings.NewReplacer("{{", "__V", "}}", "V__").Replace(rawUrl)
	u, err := url.Parse(safe)
	path := safe
	if err == nil && u.Path != "" {
		path = u.Path
	} else if i := strings.Index(safe, "://"); i >= 0 {
		if j := strings.Index(safe[i+3:], "/"); j >= 0 {
			path = safe[i+3+j:]
		} else {
			path = "/"
		}
	}
	segs := splitPath(path)
	for i, s := range segs {
		if strings.Contains(s, "__V") {
			segs[i] = "*" // 通配段
		}
	}
	return segs
}

func splitPath(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}

func isWildcard(seg string) bool {
	return seg == "*" || strings.HasPrefix(seg, ":")
}

func matchSegments(pattern, actual []string) bool {
	if len(pattern) != len(actual) {
		return false
	}
	for i := range pattern {
		if !isWildcard(pattern[i]) && pattern[i] != actual[i] {
			return false
		}
	}
	return true
}
