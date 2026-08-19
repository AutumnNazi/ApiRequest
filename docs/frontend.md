# 前端状态与 UI/UX 规范

[English](./en/frontend.md) | 简体中文

相关文档：[文档索引](./index.md) · [概述与架构](./overview.md) · [接口约定](./api-contract.md)

---

## 1. 状态分层

| 状态种类 | 归属 | 说明 |
|---------|------|------|
| 持久业务数据（集合/环境/历史） | TanStack Query + Go | 以 Go 为真源，Query 做缓存/失效 |
| UI 会话状态（打开的标签、激活标签） | Zustand | 按工作区隔离；可恢复部分写入 WebView `localStorage` |
| 编辑中的草稿（未保存的请求改动） | Zustand（per-tab） | 与已保存态分离，支持"脏"标记、关闭确认与恢复 |
| 实时流（progress/ws 消息/日志） | 事件订阅 → 局部 store | Wails 事件 → 归并到对应标签页 |

`localStorage` 不是 Secret Vault。序列化会清空已知认证类型的密钥参数，未知认证类型则清空全部参数；URL、普通 Header、Body 与脚本仍按请求内容保存，不做猜测式脱敏。需要保护的凭据应放入 Auth 编辑器或 `type=secret` 变量，避免硬编码到普通请求字段。

---

## 2. 请求发送的数据流

```
用户点 Send
  → 组装 SendContext（当前 tab 草稿 + 激活环境 + 本地覆盖）
  → 调 ipc 层 sendRequest(...)（底层为 Wails 绑定 SendRequest）// 乐观置 tab 为 sending
  → 订阅 request:progress 事件更新进度条/计时
  → 收到 ResponseResult → 写入 tab 响应态 + 刷新 history 查询缓存
  → 出错 → 结构化 AppError → 内联错误面板（不弹全局 toast）
```

---

## 3. IPC typed wrapper

所有 `invoke`（即 Wails 生成绑定函数的调用）经由 `frontend/src/ipc/` 下按领域封装的函数，参数/返回类型引用[共享类型](./data-model.md#3-前后端共享类型契约)，禁止组件层直接裸调 Wails 绑定，保证：类型安全、便于 mock 测试、统一错误转换。

---

## 4. UI / UX 交互规范（要点）

- **整体布局**：顶部工作区/环境切换条 → 左侧集合/历史侧栏（可拖拽调整宽度）→ 中央多标签编辑区 → 请求下方的响应区（可拖拽调整高度）。
- **多标签**：每个请求一个标签；未保存态用圆点标记；支持拖拽重排、中键关闭。
- **键值表格**：Header/Query/表单统一交互——末行自动新增空行、勾选启用、批量粘贴（从剪贴板的 `k: v` 或表格自动拆列）、`Bulk Edit` 文本模式切换。
- **变量提示**：`{{` 触发变量补全下拉；已定义变量 hover 显示来源与值（密钥掩码）；未定义变量红色下划线。
- **响应区**：Pretty/Raw/Preview 切换与搜索高亮；正文最多渲染 500,000 个字符，blob 只允许有界分块查看，完整内容通过原生文件对话框流式另存。HTML blob 不把预览片段当完整文档渲染。

### 快捷键（默认，可改）

| 操作 | Win/Linux | macOS |
|------|-----------|-------|
| 发送请求 | Ctrl+Enter | Cmd+Enter |
| 保存 | Ctrl+S | Cmd+S |
| 新建标签 | Ctrl+T | Cmd+T |
| 关闭标签 | Ctrl+W | Cmd+W |
| 切换环境 | Ctrl+E | Cmd+E |

- **主题与可访问性**：明/暗/跟随系统；表单和主要操作提供可见焦点与键盘访问。
- **平台一致性**：快捷键按上表映射；文件选择、拖放、剪贴板和系统浏览器均经 Wails 运行时能力调用，不依赖 WebView 私有 API。相关关键路径纳入 Windows/macOS 冒烟测试。
- **空状态与引导**：无活动标签时提示通过 Ctrl/Cmd+T 新建请求。
