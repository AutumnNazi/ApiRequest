# Mock Server 与 Collection Runner

[English](./en/advanced.md) | 简体中文

相关文档：[文档索引](./index.md) · [请求生命周期](./request-lifecycle.md)

---

## 1. Mock Server

用 Go `net/http` 起本地 HTTP 服务，以集合中请求下挂的 [Example](./data-model.md#2-数据库-schema-设计) 作为数据源，按路径/方法匹配返回 mock 数据。

### 1.1 生命周期与端口

- 以 **collection 为单位**启停（`StartMockServer(collectionId, opts)`），每个集合一个独立 `http.Server` 实例；状态经 `mock:status` 事件推前端。
- 端口默认从固定基准（如 3600）向上探测空闲端口，也可在 opts 指定；启动成功后返回实际监听地址供 UI 展示与复制。
- 应用退出时统一 `Shutdown`（带超时的优雅关闭）；集合被删除时联动停止其 mock。

### 1.2 匹配算法

对每个入站请求 `(method, path)`，在集合的所有 request 节点中按以下顺序打分匹配：

```
1. 展开集合内全部 request，取其 URL 的 path 部分作为路由模板
   （{{var}} 与 :param 段视为通配段）
2. 过滤 method 相同者（无匹配时再放宽为任意 method，标记降级）
3. 按"字面段数多者优先 > 段数长者优先"排序，取最优 request
4. 在该 request 的 examples 中选择响应：
   - 请求带 `x-mock-response-name: <example名>` header → 精确指定
   - 请求带 `x-mock-response-code: <status>` header → 按状态码选
   - 否则取第一个（按创建顺序）example
5. 无任何匹配 → 404 + JSON 错误体（列出最接近的候选路径，便于调试）
```

### 1.3 响应生成

- 返回 example 的 status/headers/body；body 中的 `{{$...}}` 动态变量在返回前渲染（复用[模板引擎](./request-lifecycle.md#2-变量解析与模板引擎)），普通 `{{var}}` 不解析（mock 无环境上下文）。
- 支持 opts 级延迟模拟（固定或区间随机毫秒），模拟慢网络。
- 每次命中以 `mock:log` 事件推前端，形成请求日志时间线。
- CORS：默认对所有来源返回宽松 CORS 头（mock 的典型消费方是本地前端开发）。


---

- **响应脚本（已实现）**：示例可携带一段 JS（`Example.MockScript`，goja 沙箱，每次请求新建 Runtime，2s 看门狗中断）。脚本内可用只读 `mockRequest`（`method`/`path`/`query`/`queryFirst(name)`/`headers`/`body`）并调用 `respond({ status, headers, body, delayMs })` 动态生成响应；未调用 `respond` 回退静态示例，脚本抛错返回 500。Mock 面板「响应脚本」页即示例管理器：可编辑名称/状态码/响应头/响应体与脚本，删除需确认；列表展示所属请求名（集合级查询附带）。
## 2. Collection Runner

顺序/迭代执行集合中的请求，支持绑定 CSV/JSON 数据文件驱动多轮。汇总每个请求的测试结果，生成运行报告（通过/失败/耗时）。

### 2.1 执行引擎

```
run_collection(target, options):
  iterations = load_data_file()?  // CSV/JSON，无则单轮
  for row in iterations:          // 每行注入为 data 作用域变量
    for request in flatten_ordered(target):
      if request.disabled: continue
      result = SendRequest(request, ctx.with_data(row))
      report.push({request, status, tests, duration})
      if options.stopOnError && result.failed: break
  return report  // 汇总：通过/失败/跳过/总耗时 + 每请求明细
```

- **执行顺序**：按树的显示顺序展开；脚本内可用 `pm.setNextRequest(name)` 改变流转（后期）。
- **数据驱动**：每轮把数据文件一行注入 `data` 作用域（优先级见[变量解析](./request-lifecycle.md#2-变量解析与模板引擎)）。
- **并发**：默认串行（多数接口有状态依赖）；可选有限并发用于压测型场景。
- **取消**：每次运行拥有唯一 `runId`；取消会传播到当前 HTTP 请求并阻止后续迭代。关闭运行中的 Runner 前必须确认并取消，不能留下无主后台任务。
- **报告**：结构化结果可导出 JSON/HTML，供 CI 消费；CLI 模式的退出码反映失败数（见下节）。

### 2.2 CLI 无头运行（apirequest-cli）

`cmd/cli` 与桌面端复用同一 core 与本地库（见 [decisions.md](./decisions.md) OPEN-001）：

```bash
apirequest-cli run --collection <名称|id> [--workspace <名称|id>]
  [--data <file>] [--iterations N] [--stop-on-error]
  [--env <名称|id>] [--env-file env.json]
  [--report report.json] [--junit junit.xml] [--db <dir>]
```

- **环境选择**：`--env` 按名称或 id 选择工作区内环境（重名时报歧义错误）；缺省用该工作区的激活环境。CI 场景推荐 `--env-file`：JSON 对象 `{"KEY": "value"}`，非字符串值显式拒绝（提示加引号）。变量优先级：数据行 > env-file > 环境变量 > 全局变量。
- **CI 集成**：退出码 = 失败请求数（上限 100），2 = 用法/准备错误；`--junit` 输出 JUnit XML（GitHub Actions test-report 等可直接消费），因 `--stop-on-error` 或取消跳过的请求计入 `skipped` 属性、不产生 testcase；`--report` 输出完整 JSON。
- `apirequest-cli list` 列出工作区与集合（含请求计数），用于发现 `--collection` 参数。
