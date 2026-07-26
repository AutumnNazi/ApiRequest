# 术语表

> 返回 [文档索引](./index.md)。

| 术语 | 含义 |
|------|------|
| Workspace | 工作区，隔离集合/环境的顶层容器 |
| Collection | 集合，请求的分组根节点 |
| Environment | 环境，一组可切换的变量集 |
| Runner | 集合批量执行器，支持数据驱动 |
| Cookie Jar | 按域/路径管理 Cookie 的本地存储 |
| Node（本设计） | 集合/文件夹/请求统一的树节点实体 |
| Example | 挂在请求下的示例响应，"保存为示例"的产物，Mock Server 的数据源 |
| oplog | 操作日志，同步与冲突排序的基础 |
| PKCE | OAuth 2.0 授权码模式的防截获扩展 |
| TTFB | Time To First Byte，首字节到达耗时 |
| mTLS | 双向 TLS，客户端也提供证书 |
| IR | Intermediate Representation，导入导出/代码生成共用的内部模型 |
| Adapter / Converter | 外部格式与内部模型互转的转换器 |
