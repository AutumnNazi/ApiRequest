# 贡献指南

感谢你为 ApiRequest 做出贡献。

本仓库以 `dev` 作为默认集成分支，稳定版本通过 tag（`vX.Y.Z`）发布。**请勿直接向 `dev` 推送提交** — 所有变更一律通过 Pull Request 合入。

[English](CONTRIBUTING.md)

---

## 分支模型

- `dev`：默认分支，日常集成分支
- `release/*`：维护者的发布准备分支
- `main`：稳定发布分支（从 `release/*` 合入）
- 贡献者推荐分支命名：
  - `fix/*`：Bug 修复
  - `feature/*`：新功能或增强
  - `docs/*`：纯文档变更

维护者发布流程：

```text
feature/* / fix/* -> dev -> release/* -> main -> tag(vX.Y.Z)
```

---

## 如何提交 Pull Request

无论你的分支是 `fix/*` 还是 `feature/*`，Pull Request 请**提向 `dev` 分支**。

原因：

- `dev` 是活跃集成分支，变更能与进行中的工作在同一通道内评审
- 合入 `dev` 会触发 Dev Build 工作流，持续验证合入的变更
- 维护者可以直接从 `dev` 切出 `release/*`，无需先同步外部变更

推荐流程：

1. Fork 本仓库
2. 将 fork 与 `dev` 同步，并从 `dev` 创建分支（推荐 `fix/*` 或 `feature/*`）
3. 完成修改并自检：
   ```bash
   go test ./backend/...          # Go 核心测试
   cd frontend && npm run build   # 类型检查 + 打包
   ```
4. 将分支推送到你的 fork
5. 向本仓库的 `dev` 分支发起 Pull Request

---

## Pull Request 要求

保持每个 PR 聚焦、可评审、易验证：

- 一个 PR 只解决一个逻辑变更
- 标题清晰说明目的
- 描述中包含：
  - 背景与问题说明
  - 关键变更
  - 影响范围
  - 验证方式
- UI 变更建议附截图或录屏
- 新的后端行为应附带测试；已有测试必须保持全绿
- 若改动了 Wails 绑定面（`backend/binding` 的导出方法），请运行 `wails generate module` 并提交重新生成的 `frontend/wailsjs/` 文件

提交信息遵循项目规范：`emoji type(scope): 中文描述`（参考 `git log` 中的示例）。

---

## 维护者合并策略

合入 `dev` 的 PR 原则上使用 **Squash and merge**：

- 保持 `dev` 历史在活跃迭代期可读
- 每个 PR 对应 `dev` 上的一个集成提交
- 降低创建 `release/*` 前的 cherry-pick 与冲突成本

---

## 维护者发布流程

1. 从 `dev` 切出 `release/x.y.z`；只做稳定化（版本号、文档、修复）
2. 将 `release/x.y.z` 合入 `main`
3. 在 `main` 上打 `vX.Y.Z` tag — Release 工作流自动构建 Windows / macOS / Linux 产物并发布 GitHub Release
4. 若发布期间有仅在 release 分支的修复，将 `main` 回并到 `dev`

---

## 反馈问题

Bug、功能请求与文档问题请使用 GitHub Issues。技术类报告请尽量附上版本、操作系统与复现步骤。
