# 可扩展性与插件接口

相关文档：[文档索引](./index.md) · [导入导出与代码生成](./interop.md) · [多协议适配器](./protocols.md)

即便 v1 不开放第三方插件，内部也以"注册表 + interface（Go 接口）"组织可扩展点，降低耦合、便于测试与后续开放：

| 扩展点 | 接口 | 注册方式 |
|--------|------|---------|
| 导入格式 | `Importer` | 按 format id 注册到 `ImporterRegistry` |
| 导出格式 | `Exporter` | 同上 |
| 代码生成 | `CodeGen` | 按 `(lang,client)` 注册 |
| 认证类型 | `AuthProvider` | 按 auth type 注册，负责签名/注入 |
| 动态变量 | `DynamicVar` | 按 `$name` 注册生成器 |
| 协议 | `ProtocolSession` | 按 scheme 注册 |
| 断言库 | 注入脚本沙箱的 JS 模块 | 引擎启动时装载 |

- 所有扩展点输入输出均为 IR/纯数据，天然可单测。
- **未来开放策略**：若开放第三方，插件运行在 WASM 或独立进程沙箱中，经受限 host API 通信，不直接触碰文件系统/网络。
