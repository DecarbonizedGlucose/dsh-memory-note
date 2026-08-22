# One-shot Command Protocol v1

本文定义 dsh-memory-note 核心程序的公开命令协议。它规定每个 subcommand 传入什么、返回什么，以及失败时如何判断。存储层的表结构、SQL 语句与不变式由 `sql-standard.md` 另行规定；两者冲突时以本文档为准。

该协议不是 JSON-RPC 2.0。subcommand 已经表示 method，因此 request 不再增加 `jsonrpc`、`method`、`params`、`request_id` 等公共包装。

## 1. 调用方式

```text
dsh-memory-note <subcommand> '<request-json>'
```

- 每次进程只执行一个 subcommand；
- 业务命令必须恰好收到一个 JSON object argument；
- TS adapter 必须使用无 shell 的 process API，并把 JSON 作为单独 argv 传入；
- stdout 只输出一个 JSON response；
- stderr 只用于诊断；
- request JSON 最大 96 KiB。

`version` 和 `help` 是核心程序自身命令，不作为 agent tool，也不走 JSON 协议：

- `version`：向 stdout 输出一行应用版本字符串（形如 `1.0.0`，核心与适配层同版本），退出码 `0`；输出不是 JSON；
- `help`：向 stderr 输出 usage 文本，退出码 `0`；输出不是 JSON；
- argv 形状错误：向 stderr 输出 usage，退出码 `2`，不输出 JSON response。

## 2. Response envelope

成功：

```json
{
  "ok": true,
  "data": {}
}
```

失败：

```json
{
  "ok": false,
  "error": {
    "code": "invalid_request",
    "message": "content is required"
  }
}
```

- `ok: true` 时必须有 `data`，不得有 `error`；`data` 必须是 JSON object；
- `ok: false` 时必须有 `error`，不得有 `data`；
- 调用方只按 `error.code` 分支，`message` 只供人阅读；
- 调用方必须忽略 response 中不认识的新字段（向前兼容）；
- 成功退出码为 `0`，已输出合法错误 response 时退出码为 `1`；
- argv 形状错误或进程无法生成 response 时，必须使用退出码 `2`，且不输出 JSON response。

进程超时、被 signal 终止或 stdout 不是合法 response 时，结果为 unknown。写命令不得自动重试，应先读取当前状态。

## 3. JSON 通用规则

- 输入必须是单个 JSON object，其后只能有 whitespace；
- 必须是合法 UTF-8；
- 除 `metadata` 内部外，所有层级拒绝 unknown field；
- 所有 object 都拒绝重复 key；
- request 字段除非明确说明，否则不接受 `null`；
- integer 必须是 JSON 整数字面量，不接受小数和指数；
- `workspace_id` 和 `version` 范围为 `1..9007199254740991`；
- 时间输入使用 RFC 3339，精确到秒，必须携带 UTC 偏移（`Z` 视同 `+00:00`），不接受小数秒；
- 时间输出统一为 `YYYY-MM-DDTHH:MM:SS±HH:MM`，其中偏移是事件发生时记录的 UTC 偏移（类 git commit 时间的语义），`+00:00` 不简写为 `Z`；所有时间比较按 UTC 时刻进行，与记录的偏移无关；
- 所有字符串长度限制均按 UTF-8 byte 计算；
- 所有 string 字段（含 `metadata` 的 key 与 string value）允许 `\t`、`\n`、`\r`，拒绝其余控制字符（U+0000–U+0008、U+000B、U+000C、U+000E–U+001F 以及 U+007F）。

字段“可选”表示可以省略，不表示可以传入错误类型。

## 4. 公共类型

### 4.1 Memory

完整 Memory response 固定包含以下字段：

```json
{
  "memory_id": "mem_...",
  "workspace_id": 1,
  "content": "Use SQLite.",
  "type": "decision",
  "scope": "storage",
  "source": ["conversation:123"],
  "metadata": {"reason": "local-first"},
  "state": "active",
  "version": 1,
  "supersedes": null,
  "superseded_by": null,
  "created_at": "2026-08-21T01:02:03+08:00",
  "updated_at": "2026-08-21T01:02:03+08:00"
}
```

| 字段 | 类型 | 规则 |
|---|---|---|
| `memory_id` | string | Go 生成的不透明 ID；调用方不得解析格式。仅在工作区内唯一，跨工作区可能重复，因此所有读取或修改必须同时给出 `workspace_id` 与 `memory_id`。 |
| `workspace_id` | integer | 所属 WID。 |
| `content` | string | memory 正文。 |
| `type` | string 或 null | 调用方定义的分类。未设置时为 null。 |
| `scope` | string 或 null | 调用方定义的范围。未设置时为 null。 |
| `source` | string[] | 来源标识。未设置时为 `[]`。 |
| `metadata` | object | 调用方扩展信息。未设置时为 `{}`。 |
| `state` | string | `active`、`superseded` 或 `invalid`。 |
| `version` | integer | 从 1 开始。每次成功修改当前记录时加 1。 |
| `supersedes` | string 或 null | 本记录取代的旧 ID。 |
| `superseded_by` | string 或 null | 取代本记录的新 ID。 |
| `created_at` | string | 创建时间。格式见 §3。 |
| `updated_at` | string | 最近修改时间。格式见 §3。 |

Response 不得因空值省略上述字段。

### 4.2 Memory input

create 和 supersede 的新内容使用相同字段：

| 字段 | 必填 | 规则 |
|---|---:|---|
| `content` | 是 | 保存原文；去除首尾空白后不能为空；最大 64 KiB。 |
| `type` | 否 | trim 后保存；空字符串表示未设置；最大 128 bytes。 |
| `scope` | 否 | trim 后保存；空字符串表示未设置；最大 256 bytes。 |
| `source` | 否 | trim、去空、去重并保序；最多 64 项，每项最大 512 bytes。 |
| `metadata` | 否 | 任意 JSON object；按紧凑序列化（无多余空白）计最大 16 KiB；嵌套深度最大 32 层。 |

Go 不解释 type、scope、source 或 metadata 的自然语言含义。

### 4.3 Workspace

```json
{
  "workspace_id": 1,
  "path": "/canonical/workspace/path",
  "created_at": "2026-08-21T01:02:03+08:00",
  "updated_at": "2026-08-21T01:02:03+08:00"
}
```

path 必须是现有 directory 的绝对路径，否则返回 `invalid_request`。Go 执行 absolute、clean 和 symlink resolution 后存储 canonical path。

## 5. Command 列表

| subcommand | 读/写 | 用户批准 |
|---|---|---|
| `workspace-resolve` | 读 | 否 |
| `workspace-register` | 写 | 是 |
| `workspace-rebind` | 写 | 是 |
| `workspace-clear` | 写 | 是 |
| `workspace-delete` | 写 | 是 |
| `memory-search` | 读 | 否 |
| `memory-list` | 读 | 否 |
| `memory-get` | 读 | 否 |
| `memory-create` | 写 | 是 |
| `memory-update` | 写 | 是 |
| `memory-supersede` | 写 | 是 |
| `memory-invalidate` | 写 | 是 |
| `memory-delete` | 写 | 是 |

## 6. Workspace commands

### 6.1 `workspace-resolve`

Request：

```json
{"path": "/absolute/workspace/path"}
```

Response data：

```json
{"workspace": null}
```

- path 校验与 §4.3 相同：不存在、不是 directory 或不是绝对路径时返回 `invalid_request`；
- 已注册时 `workspace` 为完整 Workspace object，未注册时为 null；
- 若 HOME 尚不存在，本次 invocation 先按整体设计原子初始化 HOME，再返回 `workspace: null`；该技术初始化不注册 workspace，也不创建 memory。

### 6.2 `workspace-register`

Request：

```json
{"path": "/absolute/workspace/path"}
```

Response data：

```json
{
  "workspace": {
    "workspace_id": 1,
    "path": "/canonical/workspace/path",
    "created_at": "2026-08-21T01:02:03+08:00",
    "updated_at": "2026-08-21T01:02:03+08:00"
  },
  "created": true
}
```

- path 校验与 §4.3 相同，否则返回 `invalid_request`；
- HOME 不存在时，Session 先原子初始化完整存储布局，再注册 workspace；
- 同一 canonical path 已注册时返回原 Workspace 和 `created: false`；
- 一个 canonical path 不能属于两个 WID。

### 6.3 `workspace-rebind`

Request：

```json
{
  "workspace_id": 1,
  "path": "/new/workspace/path"
}
```

Response data：

```json
{"workspace": {"workspace_id": 1, "path": "/new/workspace/path", "created_at": "2026-08-20T01:02:03+08:00", "updated_at": "2026-08-21T01:02:03+08:00"}}
```

- path 校验与 §4.3 相同，否则返回 `invalid_request`；
- 只修改 `meta.db` 的 path mapping，不移动 memory DB；
- 新 path 已属于另一 WID 时返回 `workspace_path_used`；
- 新 canonical path 与当前 path 相同时视为 no-op：成功返回原 Workspace，`updated_at` 不变。

### 6.4 `workspace-clear`

Request：

```json
{"workspace_id": 1}
```

Response data：

```json
{"deleted_count": 12}
```

删除该 workspace 的全部 memory 及其全部历史版本，保留 WID、path 和 DB 文件。空 workspace 返回 `deleted_count: 0`。mapping 存在但对应 memory DB 缺失或损坏时返回 `workspace_broken`。

### 6.5 `workspace-delete`

Request：

```json
{"workspace_id": 1}
```

Response data：

```json
{"deleted": true}
```

删除 workspace mapping、memory DB 以及对应 WAL/SHM。WID 不得复用。mapping 存在但对应 memory DB 缺失或损坏时仍然成功：这是清理半损坏 workspace 的唯一通道。该操作不删除用户的工作区目录。

## 7. Memory commands

### 7.1 `memory-search`

Request：

```json
{
  "workspace_id": 1,
  "query": "database sqlite",
  "filter": {
    "types": ["decision"],
    "scopes": ["storage"],
    "created_after": "2026-01-01T00:00:00+08:00",
    "created_before": "2026-12-31T23:59:59+08:00",
    "updated_after": "2026-01-01T00:00:00+08:00",
    "updated_before": "2026-12-31T23:59:59+08:00"
  },
  "limit": 8
}
```

- `workspace_id` 必填；
- query 和 filter 至少有一个有效条件；
- 空白 query 不算有效条件；空数组或空 filter 返回 `invalid_request`（不是被忽略）；
- limit 可选，默认 8，范围 1..20；
- 时间边界包含；`after` 不得晚于 `before`；时间比较按 UTC 时刻进行；
- 只返回 active memory。

filter 规则：

- `types` 与 `scopes` 是两个相互独立的调用方自定义维度，Go 不解释其含义；
- `types` 精确匹配 memory 的 `type`，`scopes` 精确匹配 memory 的 `scope`；
- 匹配是对 trim 后存储值的精确相等比较，大小写敏感；
- 同一维度内多个值是 OR；两个维度之间是 AND；filter 与 query 之间是 AND；
- `type` 为 null 的记忆不会命中任何非空 `types` 过滤；`scope` 同理。

query 规则：

1. 按连续 Unicode letter/number（类别 L\* 与 N\*）切分关键词；不做 Unicode 归一化；
2. 大小写不敏感仅作用于 ASCII（A–Z ↔ a–z）；其余字符按字节精确比较；
3. 关键词在 content、type、scope 中做 substring match；
4. 命中任意关键词即可；
5. score 为命中关键词数除以关键词总数；没有 query 时 score 固定为 0；
6. 按 `score DESC, updated_at DESC, memory_id ASC` 排序（`memory_id` 按 UTF-8 byte 序比较），再应用 limit。

Response data：

```json
{
  "memories": [
    {
      "memory_id": "mem_...",
      "type": "decision",
      "scope": "storage",
      "version": 2,
      "snippet": "Use SQLite in WAL mode.",
      "score": 1,
      "updated_at": "2026-08-21T01:02:03+08:00"
    }
  ]
}
```

SearchHit 的 type/scope 未设置时为 null。snippet 取 content 开头最多 240 bytes，若截断则停在 UTF-8 编码边界（不产生半个字符）。无结果返回空数组。

### 7.2 `memory-get`

Request：

```json
{"workspace_id": 1, "memory_id": "mem_..."}
```

Response data：

```json
{"memory": {"memory_id": "mem_...", "workspace_id": 1, "content": "Use SQLite.", "type": null, "scope": null, "source": [], "metadata": {}, "state": "active", "version": 1, "supersedes": null, "superseded_by": null, "created_at": "2026-08-21T01:02:03+08:00", "updated_at": "2026-08-21T01:02:03+08:00"}}
```

可以读取任意 state。不存在返回 `memory_not_found`。

### 7.3 `memory-list`

Request：

```json
{"workspace_id": 1, "limit": 50, "cursor": "..."}
```

- `workspace_id` 必填；
- limit 可选，默认 50，范围 1..200；
- cursor 可选，是不透明字符串：调用方必须原样传回，不得解析或修改；被改动的 cursor 返回 `invalid_request`；
- 列出该 workspace 任意 state（active、superseded、invalid）的全部记忆，按 `updated_at DESC, memory_id ASC`（UTF-8 byte 序）排序；
- 行内容不含 content、source、metadata；完整记录使用 `memory-get`；
- 当可能存在更多行时，response 附带 `next_cursor`；否则不带。

Response data：

```json
{
  "memories": [
    {
      "memory_id": "mem_...",
      "type": "decision",
      "scope": null,
      "state": "active",
      "version": 2,
      "supersedes": null,
      "superseded_by": null,
      "created_at": "2026-08-21T01:02:03+08:00",
      "updated_at": "2026-08-21T01:02:03+08:00"
    }
  ],
  "next_cursor": "..."
}
```

type/scope/supersedes/superseded_by 未设置时为 null。

### 7.4 `memory-create`

Request：

```json
{
  "workspace_id": 1,
  "content": "Use SQLite.",
  "type": "decision",
  "scope": "storage",
  "source": ["conversation:123"],
  "metadata": {"reason": "local-first"}
}
```

Response data：`{"memory": <完整 Memory>}`。

新记录 state 为 active，version 为 1。

### 7.5 `memory-update`

Request：

```json
{
  "workspace_id": 1,
  "memory_id": "mem_...",
  "expected_version": 1,
  "content": "Use SQLite in WAL mode.",
  "type": "decision",
  "scope": "storage",
  "source": ["conversation:456"],
  "metadata": {"reason": "better concurrency"}
}
```

- `workspace_id`、`memory_id`、`expected_version` 必填；
- content/type/scope/source/metadata 至少出现一个；
- 字段缺失表示保持原值；
- `type: ""` 或 `scope: ""` 表示清除；
- `source: []` 或 `metadata: {}` 表示清空；
- 所有字段都是整体替换，不做 merge 或 append；
- 目标必须为 active，版本必须匹配；
- 成功后 memory ID 不变，version 加 1；
- 被替换的旧值归档为内部历史版本，不通过协议暴露。

Response data：`{"memory": <完整 Memory>}`。

这就是 update command 自身的语义，不再增加名为 `patch` 的嵌套 object。

### 7.6 `memory-supersede`

Request：

```json
{
  "workspace_id": 1,
  "memory_id": "mem_old",
  "expected_version": 2,
  "new": {
    "content": "Use PostgreSQL.",
    "type": "decision",
    "scope": "storage"
  }
}
```

- 旧 memory 必须为 active 且版本匹配；
- `new` 遵守 Memory input 规则；
- 一个 SQLite transaction 内完成旧记录变更、新记录创建和双向关系；
- 旧记录变为 superseded 且 version 加 1，其被替换的旧值归档为内部历史版本；
- 新记录为 active、version 1。

Response data 固定包含 `old` 和 `new` 两个字段，二者都必须是完整 Memory object。

### 7.7 `memory-invalidate`

Request：

```json
{"workspace_id": 1, "memory_id": "mem_...", "expected_version": 1}
```

目标必须为 active 且版本匹配。成功后 state 为 invalid，version 加 1，被替换的旧值归档为内部历史版本。

Response data：`{"memory": <完整 Memory>}`。

### 7.8 `memory-delete`

Request：

```json
{"workspace_id": 1, "memory_id": "mem_...", "expected_version": 2}
```

- 任意 state 都可以删除，但版本必须匹配；
- 存在 supersede relationship 时，必须同时清除另一端指向被删除 ID 的字段，且另一端 version 加 1、updated_at 更新（该变化通过下次 `memory-get` 可见），不留下 dangling reference；
- 该 memory_id 的全部内部历史版本一并删除。

Response data：

```json
{"deleted": true}
```

这里的 delete 是从当前 SQLite 数据库删除，不表示底层介质的安全擦除。

## 8. Version 与 state 冲突

memory 写命令按以下顺序检查：

1. memory 是否存在；
2. expected version 是否匹配；
3. 当前 state 是否允许该操作。

因此一个已经被其他操作修改的旧请求首先得到 `version_conflict`。调用方应重新 `memory-get`，不得自动把 expected version 改成新值。

`memory-delete` 不检查 state（任意 state 可删），但仍按上述顺序先检查存在性与版本。

## 9. Error codes

| code | 含义 |
|---|---|
| `invalid_request` | JSON、字段、类型、范围、控制字符、时间格式、路径不存在或非 directory、空 filter、无效 cursor 或业务输入不合法。 |
| `home_broken` | HOME 已存在但缺失必要文件、类型错误或不可识别；程序不会自动重建。 |
| `schema_mismatch` | SQLite schema 版本不受当前核心程序支持。 |
| `workspace_not_found` | WID 未注册。 |
| `workspace_path_used` | canonical path 已属于另一 WID。 |
| `workspace_busy` | 等待目标 WID 的跨进程读写锁超时。 |
| `workspace_broken` | mapping 存在，但对应 memory DB 缺失或损坏；memory 命令与 `workspace-clear` 返回此码，`workspace-delete` 不返回此码（照常清理）。 |
| `memory_not_found` | 指定 WID 下没有该 memory ID。 |
| `version_conflict` | expected version 与当前 version 不同。 |
| `invalid_memory_state` | 当前 state 不允许该命令。 |
| `internal_error` | 未分类的 SQLite、文件系统或程序错误。 |

错误 response 不同时返回成功 data。`message` 不得包含 memory content、SQL 或 stack trace。

## 10. 批准边界

Go request 中不存在 `approved`、`confirmed` 等字段。

LLM 可以直接调用 read command（`workspace-resolve`、`memory-search`、`memory-list`、`memory-get`），也可以提出 write command。TS adapter 必须在真正启动写 command 前向用户展示目标和变化，并取得 Harness approval。

如果一次写 invocation 的结果 unknown，adapter 不自动重试。应先使用 workspace-resolve、memory-search、memory-list 或 memory-get 检查当前状态，再由用户决定是否发起新的写操作。

## 11. 协议维护

- 每个 subcommand 必须有独立 Go request/response struct 和 TS type；
- Go 和 TS 共用一组 valid/invalid JSON fixtures；
- unknown field、duplicate key、trailing JSON、空值、控制字符、时间格式、cursor 和版本冲突必须测试；
- 改变已有字段含义、默认值、返回结构、排序或错误码属于 protocol breaking change；
- 协议版本跟随应用版本的主版本：`1.x.x` 实现协议 `v1`；协议发生 breaking change 时升为 `v2`，核心与适配层的应用版本同步升到 `2.0.0`。SQL schema 版本是内部迁移计数器，不随协议或应用版本变化（见 `sql-standard.md` 与 README「版本与兼容性」）；
- README 只介绍用法，不重复维护完整协议；
- 存储层语义由 `sql-standard.md` 规定，两者冲突时以本文档为准。
