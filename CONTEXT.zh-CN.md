# ApiRequest 领域上下文

[English](./CONTEXT.md) | 简体中文

## 核心模块

- **工作区会话（Workspace Session）**：按工作区隔离的编辑器状态，包含标签、活动标签与可恢复草稿。每个标签只属于一个工作区。
- **密钥保险库（Secret Vault）**：唯一允许持久化结构化凭据字段的模块。SQLite 只保存不透明的 `secret://` 引用；运行时通过系统 keychain Adapter 或加密文件 Adapter 解析。
- **脱敏器（Redactor）**：Secret Vault 的策略边界；数据跨越审计、历史、日志、导出或省略密钥的同步边界前，不可逆地替换凭据值。
- **历史摘要（History Summary）**：历史列表使用的轻量分页投影，不包含请求快照、响应 Header、测试结果或响应 Body。
- **历史详情（History Detail）**：按需加载的单条重放记录；请求凭据在持久化前已经脱敏。
- **响应 Blob（Response Blob）**：存放在 SQLite 外部的响应体。消费者读取元数据、有界范围或流式保存到用户选择的文件，不假定完整 Blob 可装入内存。
- **请求进度（Request Progress）**：一次发送过程的生命周期状态：`sending`、`ttfb`、`downloading` 与 `done`，可携带已接收字节数。
- **操作生命周期（Operation Lifecycle）**：Request Execution 与 Collection Runner 共享的、由注册表管理的身份、父级取消、完成与 shutdown 语义。活动操作 ID 在完成前必须唯一。

## 不变量

1. 持久化的结构化密钥字段（Auth 密钥参数、`type=secret` 变量与同步密码）必须是不透明的 `secret://keyring/` 或 `secret://file/` 引用。URL、普通 Header、Body 与脚本中的任意字符串仍是请求内容，不自动判定为凭据。
2. 历史与日志是审计面，不是密钥恢复面；其中的脱敏不可逆。
3. 节点身份包含工作区所有权。更新、移动、祖先查询、请求发送与环境选择必须拒绝跨工作区引用。
4. 列表接口只返回有界摘要；大详情与响应体必须通过显式详情接口或范围接口读取。
5. 关闭脏草稿必须由用户明确决定。可恢复草稿按工作区会话保存，前端持久化副本会省略结构化 Auth 凭据。
6. 取消父操作必须传播到其活动子请求。应用 shutdown 时停止接收新操作、取消活动任务，并在关闭存储前等待任务完成。

## Adapter

- **系统 Keychain Adapter**：首选的 Secret Vault 实现，由操作系统凭据存储提供支持。
- **加密文件 Adapter**：回退实现，使用 Argon2id 派生密钥和 AES-GCM 加密。主密码只存在于内存中，进程重启后 Adapter 保持锁定。
- **原生对话框 Adapter**：Wails 文件与目录对话框，用于必须由桌面宿主选择或写入的路径。
