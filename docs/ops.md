# 安全、质量与运维

[English](./en/ops.md) | 简体中文

> 汇总横切关注点：安全、错误模型、测试策略、打包发布、性能预算。
> 返回 [文档索引](./index.md)。

---

## 1. 安全考量

- **凭证保护**：密钥类变量与 OAuth token 默认存入系统 keychain（Windows Credential Manager / macOS Keychain）；系统后端不可用时，使用用户主密码经 Argon2id 派生密钥并用 AES-GCM 加密存储。UI 默认掩码显示，历史和脚本日志统一脱敏。
- **识别边界**：Vault 保护 Auth 模型中的密钥参数、`type=secret` 变量和同步密码。URL、普通 Header、Body 与脚本是用户控制的请求内容，不自动推断其中是否含密钥；凭据应通过结构化认证字段或密钥变量引用。
- **脚本沙箱**：JS 脚本无法访问宿主文件系统与任意网络，执行有超时上限（见 [请求生命周期](./request-lifecycle.md)）。
- **证书信任**：允许自定义 CA 与客户端证书，但对"关闭 SSL 校验"给出明确风险提示。
- **导入内容视为不可信**：解析导入文件时防注入，不因导入内容触发脚本自动执行。

---

## 2. 错误模型与用户反馈

Go 侧统一错误类型，序列化为结构化错误返回前端，避免裸字符串：

```go
// 错误分类：Network/Tls/Script/Storage/Import/Validation
type ErrorKind string

const (
    KindNetwork    ErrorKind = "network" // dns/connect/tls/timeout/...
    KindTls        ErrorKind = "tls"
    KindScript     ErrorKind = "script"
    KindStorage    ErrorKind = "storage"
    KindImport     ErrorKind = "import"
    KindValidation ErrorKind = "validation"
)

// sentinel error，配合 errors.As 做分类判断
type AppError struct {
    Kind    ErrorKind `json:"kind"`
    Detail  string    `json:"detail"`
    Phase   string    `json:"phase,omitempty"`  // 脚本错误阶段 pre/test
    Line    *uint32   `json:"line,omitempty"`   // 脚本错误行号
    Format  string    `json:"format,omitempty"` // 导入错误格式
}

func (e *AppError) Error() string { return string(e.Kind) + ": " + e.Detail }
```

- **网络错误**分类展示（DNS 失败 / 连接被拒 / TLS 握手失败 / 超时），并给可操作建议。
- **脚本错误**带阶段（pre/test）与行号，直接定位到编辑器。
- 前端错误边界：单个请求失败不影响其他标签页；全局错误 toast + 日志留痕。

---

## 3. 测试策略

| 层次 | 范围 | 工具 |
|------|------|------|
| Go 单元测试 | 变量解析、模板渲染、auth 签名、导入导出转换器、代码生成 | `go test` |
| Go 集成测试 | SendRequest 全流程（对 mock HTTP server） | `go test` + `net/http/httptest` |
| 脚本引擎测试 | `pm.*` API 行为、断言、超时、隔离 | `go test` |
| 前端单元测试 | store 逻辑、IPC wrapper、纯组件 | Vitest + Testing Library |
| E2E | 关键用户路径（建请求→发送→看响应→存集合） | Wails + Playwright/WebDriver |
| 跨平台冒烟 | Windows / macOS 构建产物可启动、可发一次请求、可读写应用数据目录 | CI matrix |

核心不变量放在 Go 纯函数层，保证脱离 UI 可高覆盖单测。

### 3.1 Windows / macOS 冒烟门禁

每次合并到主分支及每个发布候选版本，均在 Windows 和 macOS 的原生 runner 上执行：

1. 构建对应安装包并安装 / 启动应用。
2. 创建工作区和请求，向本地 mock server 发起一次 HTTP 请求。
3. 验证 SQLite、`blobs/` 与日志均写入 Wails 运行时路径 + Go `path/filepath` 解析的应用数据目录。
4. 写入并读取一项密钥变量；系统 keychain 不可用的 CI 环境使用主密码加密回退。
5. 验证 Ctrl/Cmd+Enter 发送快捷键和文件选择对话框可用。
6. 验证签名状态；macOS 发布产物额外验证公证票据。

---

## 4. 跨平台支持与发布

### 4.1 CI 构建矩阵与产物

| 平台 | GitHub Actions runner | 架构 | 产物 | 发布前检查 |
|------|------------------------|------|------|------------|
| Windows | `windows-latest` | x64 | `wails build` 产出 `.exe`（可附 NSIS `.msi`） | Authenticode 签名；确保 WebView2 Runtime 可用或由安装器引导安装 |
| macOS | `macos-latest` | Apple Silicon + Intel（universal 或分架构） | `wails build` 产出 `.app`/`.dmg` | Developer ID 签名、Hardened Runtime、notarization 与 stapling |
| Linux | `ubuntu-latest` | x64 | `.AppImage` / `.deb` | best-effort 构建与启动检查 |

- **更新链路**：Wails 无内置 updater。每个 stable release 发布 `SHA256SUMS` 与 `update-manifest.json`，设置页当前只打开官方 release 下载页；在实现签名校验、回滚和原子替换前，不执行静默自更新。
- **签名与标识**：Windows/macOS secrets 齐全时由 CI 执行 Authenticode、Developer ID 签名、公证与 stapling；未配置时产物文件名带 `-unsigned`，并附平台级 `SIGNING_STATUS-*.txt`。
- **崩溃与遥测**：可选、默认关闭、明确告知；本地日志滚动留存便于排障。

### 4.2 平台实现边界

- 业务模块不得通过 build tags（`//go:build` / `runtime.GOOS`）直接散落 OS 分支访问 OS API；所有平台差异统一经 [platform 抽象](./overview.md#4-平台抽象层platform) 提供。
- 应用数据、缓存、日志、数据库与 `blobs/` 根目录均由 Wails 运行时路径 API + Go `path/filepath` 获取；不使用硬编码用户目录或字符串拼接路径。
- 系统代理、证书库和 keychain 读取失败时，返回可诊断的结构化错误并保留手动配置 / 主密码回退路径；不得静默降级为不安全配置。
- UI 对 WebView 差异采用 Wails/Radix 提供的统一能力；涉及快捷键、拖放、文件对话框和剪贴板的关键路径必须纳入双端冒烟。

---

## 5. 非功能性指标（性能预算）

| 指标 | 目标 |
|------|------|
| 冷启动到可交互 | < 1.5s |
| 发送简单请求的额外开销（除网络本身） | < 20ms |
| 1MB JSON 响应格式化渲染 | < 300ms（超阈值走虚拟化/折叠） |
| 空闲内存占用 | < 200MB |
| 10 万条历史记录列表滚动 | 稳定 60fps（虚拟列表） |
| 安装包体积 | 单平台 < 30MB（优先 CM6 而非 Monaco；Go 二进制 + 系统 WebView，远小于 Electron） |
