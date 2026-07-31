# 请求生命周期、变量、脚本与 HTTP 引擎

[English](./en/request-lifecycle.md) | 简体中文

本篇覆盖一次请求从发起到落库的完整流程、变量解析与模板引擎、脚本引擎执行模型，以及 HTTP 引擎内核设计。

相关文档：[文档索引](./index.md) · [数据模型](./data-model.md) · [认证](./auth.md) · [接口约定](./api-contract.md)

---

## 1. 请求生命周期（一次发送的完整流程）

```
1. 收集上下文       → 合并变量作用域，加载 Cookie Jar
2. 前置脚本执行     → goja 沙箱运行 preRequestScript
                      脚本可读写变量、修改即将发送的请求
3. 变量解析/模板渲染 → 替换 URL/Header/Body 中的 {{var}}
4. 构造原生请求     → 应用 auth、编码 body、设置 headers
5. 发送（HTTP 引擎）→ net/http 执行，采集分阶段计时
6. 接收响应         → 流式读取，解析 Cookie，写回 Jar
7. 测试脚本执行     → 运行 testScript，pm.test/pm.expect 断言
8. 结果落库         → 写 History，返回响应 + 测试结果给前端
```

每个阶段以 Wails 事件（`runtime.EventsEmit` / 前端 `EventsOn`）向前端推送进度（发送中/首字节/完成），支持取消。

---

## 2. 变量解析与模板引擎

### 2.1 解析算法

`{{var}}` 语法，支持嵌套与动态变量。解析在 Go 侧完成（脚本执行后、构造请求前）：

```
1. 构建作用域链（Map<name, value>），按优先级从低到高叠加覆盖：
   global → collection(含继承) → environment → data-file → local/override
2. 扫描目标字符串，匹配 {{ ... }} token
3. 对每个 token：
   - 以 $ 开头 → 动态变量，调用生成器（$guid/$timestamp/$randomInt/$randomEmail...）
   - 否则查作用域链
4. 支持一层间接引用（值本身含 {{x}} 时再解析），设最大深度防循环
5. 未定义变量：保留原样 or 报错（可配置），并在 UI 高亮提示
```

### 2.2 动态变量清单（内置生成器）

`$guid` `$uuid` `$timestamp` `$isoTimestamp` `$randomInt` `$randomUUID` `$randomEmail` `$randomFirstName` `$randomIP` `$randomBoolean` 等——对齐 Postman 的 `{{$...}}` 命名，降低迁移成本。

### 2.3 解析发生的位置

变量替换作用于：URL、query、header 键值、body（raw/urlencoded/formdata 的文本部分）、auth 参数。**binary body 与文件路径不做模板替换**（除路径本身）。

---

## 3. 脚本引擎执行模型

用 `goja`（纯 Go 的 JS 引擎）运行沙箱 JS，注入 `pm` 对象，兼容 Postman 常用 API 子集。脚本对变量的修改通过绑定方法返回值回传，由 Go 统一提交，避免竞态。

### 3.1 执行时序

```
SendRequest 内部：
  ctx := collectContext()                     // 变量作用域 + cookie
  // ── 前置脚本 ──
  sandbox := script.NewSandbox(5 * time.Second)
  sandbox.InjectPM(ctx, request)              // 注入 pm 对象
  sandbox.Eval(preScript)                     // 用户脚本可改 request / 设变量
  request = sandbox.MutatedRequest()
  ctx = sandbox.MutatedCtx()                  // 变量变更收集
  // ── 发送 ──
  resolved := template.Resolve(request, ctx)
  resp := httpengine.Send(resolved)
  // ── 测试脚本 ──
  sandbox.InjectResponse(resp)
  sandbox.Eval(testScript)                    // pm.test / pm.expect
  results := sandbox.TestResults()
  persistVariableChanges(ctx)                 // Go 统一提交，避免竞态
```

### 3.2 `pm.*` API 映射表（首批目标）

| Postman API | 实现方式 | 阶段 |
|-------------|---------|------|
| `pm.environment.get/set/unset` | 桥接到 Go 变量作用域，变更缓冲后回传 | 必做 |
| `pm.variables.get/replaceIn` | 同上，只读合并视图 | 必做 |
| `pm.collectionVariables.*` | 同上 | 必做 |
| `pm.request.*` | 前置脚本中暴露可变请求对象 | 必做 |
| `pm.response.json()/.text()/.code/.headers` | 测试脚本中只读响应 | 必做 |
| `pm.test(name, fn)` | 收集断言结果 | 必做 |
| `pm.expect` | 注入精简 chai（BDD assert 子集） | 必做 |
| `pm.sendRequest(req, cb)` | 走受控通道回调 Go 的 http_engine | 次做 |
| `pm.cookies.*` | 桥接 Cookie Jar | 次做 |
| `console.log/warn/error` | 收集到 `scriptLogs` 返回前端 | 必做 |

### 3.3 沙箱约束

- 无 `require`/`import`、无 `fs`、无直接 `fetch`；网络仅经 `pm.sendRequest` 受控通道。
- CPU 时间与 wall-clock 双重超时；内存上限；单请求内脚本互相隔离。
- 每次执行新建 context，避免全局状态跨请求泄漏。

脚本兼容度是关键风险点：Postman `pm.*` API 面很大，全兼容成本高。策略是先覆盖高频子集，按需扩展，并在文档标注已支持范围。

---

## 4. HTTP 引擎内核设计

引擎是"解析后的请求 → 响应结果 + 计时"的纯粹执行单元，不感知集合/变量等上层概念。

### 4.1 为什么请求执行放在 Go 侧

- **绕开 CORS**：WebView 里 `fetch` 受同源策略限制，Postman 类工具必须能请求任意目标。放到 Go 用 `net/http` 直接发原生请求。
- **完整网络控制**：自定义 TLS 证书、客户端证书、代理、重定向策略、原始 Header 顺序、超时。
- **精确计时**：DNS / 连接 / TLS 握手 / TTFB / 下载各阶段耗时，需在原生层采集。
- **大响应流式处理**：避免把大 body 一次性塞进 WebView。

### 4.2 分阶段计时采集

Go 标准库 `net/http/httptrace` 直接暴露分阶段 hook（DNS/connect/TLS/TTFB 都有回调），无需额外封装。采集方案：
- 用 `net/http/httptrace` 的 `ClientTrace` 回调（`DNSStart/DNSDone`、`ConnectStart/ConnectDone`、`TLSHandshakeStart/TLSHandshakeDone`、`GotFirstResponseByte`），配合 `time.Now()` 打点采集 DNS/connect/TLS/TTFB/download。
- 首选：把 `httptrace.WithClientTrace` 注入请求 `context`，配合流式 body 读取记录首字节(TTFB)与结束时刻。
- 若精度不足，回退到基于自定义 `http.Transport`（`DialContext`/`DialTLSContext`）的打点保证 `Timing` 各字段可填。

### 4.3 关键能力清单

- **原始 Header 顺序/大小写保留**：Go 的 `http.Header` 是 map（键会被规范化、顺序不保留）；需要时用自定义 `http.Transport` 或 header 保序结构底层构造并禁用自动排序，保留用户输入顺序（对某些签名类接口关键）。
- **重定向策略**：可配置最大跳数、是否跟随、跨域是否携带 Authorization。
- **压缩**：自动 gzip/br/deflate 解码，同时保留原始 `Content-Encoding` 展示。
- **代理**：系统代理 / 手动 / PAC（后期）/ 绕过列表；支持 http/https/socks5。
- **TLS**：Go 标准库 `crypto/tls` 为主；自定义 CA、客户端证书(mTLS)、可选关闭校验(强警告)。
- **流式与取消**：body 分块读取，超阈值不内联；用 `context.Context` 取消支持 `CancelRequest`（取消语义见[接口约定](./api-contract.md#3-当前方法签名以生成-binding-为准)）。
- **连接复用**：`http.Client` 全局单例（含 `http.Transport` 连接池）；per-host 可配置。

### 4.4 请求超大 body 处理

- 上行 binary 与 multipart 文件：从文件路径流式上传，不整块读进内存；可重放 body 提供重新打开能力，用于重定向、Digest 重试与 AWS SigV4 流式哈希。
- 下行大响应：边收边写 `blobs/`，前端先拿摘要 + ref，按需分段拉取渲染。
