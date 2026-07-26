# 导入 / 导出与代码生成

相关文档：[文档索引](./index.md) · [数据模型](./data-model.md) · [可扩展性](./extensibility.md)

转换器统一走 `外部格式 → 内部模型(IR) → 外部格式`，IR 即[共享类型契约](./data-model.md#前后端共享类型契约)。

---

## 1. 概述

以"转换器（Adapter）"接口统一实现：

- **导入**：Postman v2.1、OpenAPI 3.x / Swagger 2、cURL 命令、HAR、Insomnia。
- **导出**：Postman v2.1、OpenAPI、cURL、代码片段。

内部模型 → 各语言 HTTP 代码：cURL、JavaScript(fetch/axios)、Python(requests)、Go、Java、Rust、PHP 等。以模板 + 生成器接口扩展。

---

## 2. 导入 / 导出的字段级映射

以下为关键格式的字段对应，避免实现时反复查文档。

### 2.1 Postman Collection v2.1 ↔ IR

| Postman v2.1 | 内部模型 | 备注 |
|--------------|---------|------|
| `info.name` / `info._postman_id` | collection node `name` / `id` | id 冲突时重新生成并记映射 |
| `item[]`（含嵌套 `item`） | `node` 树（folder/request） | 递归；有 `request` 字段者为 request |
| `request.method` / `request.url.raw` | `method` / `url` | url 也可能是对象，需拼 `raw` |
| `request.header[]` | `headers: KV[]` | `disabled` → `enabled=false` |
| `request.url.query[]` | `params: KV[]` | 与 url 中 query 去重合并 |
| `request.body.mode` | `body.kind` | raw/urlencoded/formdata/file/graphql 一一对应 |
| `request.body.raw` + `options.raw.language` | `body.text` + `language` | 缺省语言按 header 猜测 |
| `request.auth` | `auth` | type 名对齐，参数数组转对象 |
| `event[]`（prerequest/test） | `pre_script` / `test_script` | `listen` 区分阶段，`script.exec[]` join 为文本 |
| collection/folder `variable[]` | node `variables` | |
| `{{var}}` 占位符 | 原样保留 | 语法一致，无需转换 |

导出为反向映射；不可表达的 IR 字段（如请求级 SSL 覆盖）降级为最接近语义或写入 `description` 注记。

### 2.2 OpenAPI 3.x / Swagger 2 → IR（单向导入）

| OpenAPI | 内部模型 |
|---------|---------|
| `info.title` | collection name |
| `tags` | 一级 folder 分组 |
| `paths./x.{method}` | request（`operationId` 或 `summary` 作名） |
| `servers[0].url` + path | url（服务器变量转 `{{var}}`） |
| `parameters(in=query/header/path)` | params / headers / path 段占位 |
| `requestBody.content` | body（按 media-type 选 raw/formdata/urlencoded） |
| `security` + `securitySchemes` | auth（bearer/apiKey/oauth2 映射） |
| `components.examples` / `responses` | 保存为 Example（供 Mock 用） |

- **$ref 解析**：先完整解引用（含远程/相对 `$ref`）再转换。
- **oneOf/anyOf/allOf**：body 示例取首个可用 schema 生成样例值。
- 服务器变量与 path 参数统一落为环境变量，导入后提示用户填值。

### 2.3 cURL → IR（命令行解析）

解析 `-X/--request`、`-H/--header`、`-d/--data*`、`-F/--form`、`-u/--user`、`--url`、`-b/--cookie`、`--compressed`、`-k/--insecure` 等：

- `-d` 多次出现 → 合并；有 `Content-Type: json` 时 body.kind=raw(json)，否则 urlencoded。
- `-F` → formdata（`@file` 前缀识别为文件项）。
- `-u user:pass` → Basic auth。
- 反向（IR → cURL）复用代码生成器的 curl 目标。

### 2.4 HAR → IR

`log.entries[].request` 逐条转为 request（method/url/headers/queryString/postData）；同一 host 归入一个 folder；可选把 `response` 存为 Example。用于"抓包回放"场景。

---

## 3. 代码生成器架构

`IR(HttpRequest, 已解析变量?) → 目标语言片段`。生成器接口统一，按 `(language, client)` 二元组注册：

```go
type CodeGen interface {
    Id() string                                          // "javascript-fetch" / "python-requests" ...
    Generate(req *HttpRequest, opts *GenOptions) string
}
```

- **目标矩阵**：curl、JavaScript(fetch/axios)、Python(requests/http.client)、Go(net/http)、Java(OkHttp/HttpClient)、Rust(reqwest)、PHP(curl/Guzzle)、C#(HttpClient)、Node(native)、Shell(httpie)。
- **GenOptions**：是否保留 `{{var}}` 占位 vs 内联已解析值、缩进风格、是否含 auth、是否含注释。
- **正确性要点**：各语言的转义规则不同（引号、多行 body、二进制），生成后对每个目标各留一组快照测试（golden test）确保稳定。
- 与导出解耦：代码生成面向"复制片段"，导出面向"文件交换"，但底层 IR 同源。
