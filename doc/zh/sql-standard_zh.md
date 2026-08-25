# SQL 标准

本文档规范 dsh-memory-note 存储层的表结构、SQL 语句、事务边界与不变式。它是 `overall-design_zh.md` 与 `protocol-v2-proposal_zh.md` 在存储层的落实；三者冲突时，以协议文档为准。

适用范围：HOME 下的 `meta.db`，以及 `memory/` 下的每个 `workspace-{WID}-memory.db`（下文简称 memory DB）。

## 1. 通用规则

- 每个 SQLite 连接必须设置：`PRAGMA foreign_keys = ON`、`PRAGMA busy_timeout = 1000`（毫秒）、`PRAGMA journal_mode = WAL`。
- 所有 SQL 值一律使用绑定参数，禁止字符串拼接 SQL。
- 所有 TEXT 比较与排序使用 BINARY collation（SQLite 默认）；协议中的 “`memory_id` 升序”即按 UTF-8 byte 序比较。
- 数据库文件创建权限为 `0600`，目录创建权限为 `0700`。
- 库标识：
  - `meta.db`：`PRAGMA application_id = 0x44534D4D`（"DSMM"），`PRAGMA user_version = 4`；
  - memory DB：`PRAGMA application_id = 0x44534D57`（"DSMW"），`PRAGMA user_version = 4`。
- 时间列：一律存为两个 INTEGER——epoch seconds（UTC 时刻）与 offset minutes（该时刻的 UTC 偏移，范围 -840..840）。SQL 内的比较、范围过滤与排序只使用 epoch seconds；对外输出格式由协议 §3 规定。
- JSON 列（`source_json`、`metadata_json`）：一律使用紧凑序列化（无多余空白）存储，保证协议的长度上限与存储字节数一致。
- 事务模型：memory 写命令在 memory DB 内使用单个写事务，`meta.db` 只承担短事务（mapping 校验、锁的获取/释放、workspace 表变更）。memory DB 写事务提交之后才释放 WID 锁并结束 meta 协调；若锁释放或收尾失败，命令返回 `internal_error`，但 memory 变更已生效；调用方按协议的 unknown 结果纪律处理（先读状态，再决定下一步）。

## 2. meta.db

### 2.1 表结构

```sql
CREATE TABLE meta_info (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  schema_version INTEGER NOT NULL CHECK (schema_version = 4),
  cursor_key TEXT NOT NULL
);

CREATE TABLE workspaces (
  workspace_id INTEGER PRIMARY KEY AUTOINCREMENT
    CHECK (workspace_id BETWEEN 1 AND 9007199254740991),
  path TEXT NOT NULL UNIQUE,
  created_at INTEGER NOT NULL,
  created_offset INTEGER NOT NULL CHECK (created_offset BETWEEN -840 AND 840),
  updated_at INTEGER NOT NULL,
  updated_offset INTEGER NOT NULL CHECK (updated_offset BETWEEN -840 AND 840)
);

CREATE TABLE workspace_locks (
  workspace_id INTEGER NOT NULL,
  mode TEXT NOT NULL CHECK (mode IN ('read', 'write')),
  session_id TEXT NOT NULL,
  acquired_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  PRIMARY KEY (workspace_id, session_id)
);

CREATE UNIQUE INDEX workspace_locks_single_writer
  ON workspace_locks(workspace_id) WHERE mode = 'write';

CREATE INDEX workspace_locks_expiry ON workspace_locks(expires_at);
```

说明：

- `workspace_id` 由 SQLite AUTOINCREMENT 分配；删除后 `sqlite_sequence` 不回退，因此 WID 永不复用。
- `path` 是应用层计算好的 canonical path（绝对、clean、symlink resolved）；UNIQUE 约束保证一个 path 只属于一个 WID。
- `workspace_locks` 实现总体设计 §3 的 per-WID 读/写锁。写锁互斥由部分唯一索引兜底；读锁允许多条共存。每个 Session 对每个 WID 至多持有一行（主键保证）。
- `session_id` 来自 Session 的随机会话 ID，与工作区路径无关。

### 2.2 锁协议

时间单位一律为 Unix 秒。租约 TTL 为 30 秒：`expires_at = acquired_at + 30`。

获取锁（在一个短 `meta.db` 事务内原子完成；该事务使用 `BEGIN IMMEDIATE`，并发写者在 SQLite busy_timeout 内排队而非撞快照冲突，超时按 `workspace_busy` 报告）：

1. 回收过期锁：`DELETE FROM workspace_locks WHERE expires_at <= ?`（崩溃兜底）；
2. 冲突检查：
   - 目标为写锁：该 WID 存在任意未过期锁行即冲突（`SELECT 1 FROM workspace_locks WHERE workspace_id = ? AND expires_at > ? LIMIT 1`）；
   - 目标为读锁：该 WID 存在未过期写锁行即冲突（`SELECT 1 FROM workspace_locks WHERE workspace_id = ? AND mode = 'write' AND expires_at > ? LIMIT 1`）；
3. 无冲突则 INSERT 自己的锁行并提交。并发写锁 INSERT 违反部分唯一索引同样视为冲突。

冲突时回滚短事务，等待后重试，总预算 5 秒（含 busy_timeout 的等待时间）；超预算返回 `workspace_busy`。

释放：正常完成时执行 `DELETE FROM workspace_locks WHERE workspace_id = ? AND session_id = ?`。Session 结束前必须释放其全部锁行。

续租：操作可能逼近过期时间且仍是持有者时，执行 `UPDATE workspace_locks SET expires_at = ? WHERE workspace_id = ? AND session_id = ? AND expires_at > ?`；影响行数为 0 表示所有权已丢失，当前操作必须中止且不得开始新的写入。

锁协调的是进程级互斥；数据原子性由 memory DB 的 SQLite 事务保证，两者不可互相替代。

### 2.3 workspace 语句

- resolve：`SELECT workspace_id, path, created_at, created_offset, updated_at, updated_offset FROM workspaces WHERE path = ?`；无行返回 `workspace: null`。
- register：应用层先校验 canonical path；随后在 `BEGIN IMMEDIATE` 写事务内按 path 查询，已存在则提交并返回现有行（`created: false`）；不存在则 INSERT 并提交，然后按 §3.1 创建 memory DB 文件。并发注册同一 path 由 `path` UNIQUE 约束仲裁（INSERT 唯一冲突的一方改为返回现有行）；WID 由 AUTOINCREMENT 分配，两个进程不会拿到同一 WID，DB 文件名冲突不可能发生。
- rebind：`BEGIN IMMEDIATE` 写事务内读目标行（无行 → `workspace_not_found`）→ 新 path 与当前 path 相同则提交并返回原行（no-op，不动 `updated_at`）→ 按新 path 查询（存在 → `workspace_path_used`）→ `UPDATE workspaces SET path = ?, updated_at = ?, updated_offset = ? WHERE workspace_id = ?` → 提交。
- delete：`BEGIN IMMEDIATE` 写事务内 `DELETE FROM workspaces WHERE workspace_id = ?`（影响行数为 0 → `workspace_not_found`，回滚）→ 提交 → 按 §3.5 删除文件。

## 3. Memory DB

### 3.1 创建

文件名只取决于十进制 WID：`memory/workspace-{WID}-memory.db`。以 `O_CREATE|O_EXCL` 创建（权限 0600）；文件已存在属于不应发生的情况（WID 不复用），返回 `workspace_broken`。建表并插入表头行后 fsync 文件。

### 3.2 表结构

```sql
CREATE TABLE memory_info (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  workspace_id INTEGER NOT NULL
    CHECK (workspace_id BETWEEN 1 AND 9007199254740991),
  schema_version INTEGER NOT NULL CHECK (schema_version = 4)
);

CREATE TABLE memories (
  workspace_id INTEGER NOT NULL
    CHECK (workspace_id BETWEEN 1 AND 9007199254740991),
  memory_id TEXT NOT NULL,
  content TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('fact', 'note')),
  label TEXT,
  source_json TEXT NOT NULL,
  metadata_json TEXT NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('active', 'superseded', 'invalid')),
  version INTEGER NOT NULL CHECK (version BETWEEN 1 AND 9007199254740991),
  supersedes TEXT,
  superseded_by TEXT,
  created_at INTEGER NOT NULL,
  created_offset INTEGER NOT NULL CHECK (created_offset BETWEEN -840 AND 840),
  updated_at INTEGER NOT NULL,
  updated_offset INTEGER NOT NULL CHECK (updated_offset BETWEEN -840 AND 840),
  PRIMARY KEY (workspace_id, memory_id),
  FOREIGN KEY (workspace_id, supersedes) REFERENCES memories(workspace_id, memory_id),
  FOREIGN KEY (workspace_id, superseded_by) REFERENCES memories(workspace_id, memory_id)
);

CREATE TABLE memory_history (
  workspace_id INTEGER NOT NULL,
  memory_id TEXT NOT NULL,
  version INTEGER NOT NULL,
  action TEXT NOT NULL CHECK (action IN ('update', 'supersede', 'invalidate')),
  content TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('fact', 'note')),
  label TEXT,
  source_json TEXT NOT NULL,
  metadata_json TEXT NOT NULL,
  state TEXT NOT NULL,
  supersedes TEXT,
  superseded_by TEXT,
  created_at INTEGER NOT NULL,
  created_offset INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  updated_offset INTEGER NOT NULL,
  archived_at INTEGER NOT NULL,
  PRIMARY KEY (workspace_id, memory_id, version)
);

CREATE TABLE memory_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  workspace_id INTEGER NOT NULL,
  memory_id TEXT NOT NULL,
  action TEXT NOT NULL CHECK (action IN ('create', 'update', 'supersede', 'invalidate', 'delete')),
  from_version INTEGER,
  to_version INTEGER,
  related_memory_id TEXT,
  reason TEXT,
  created_at INTEGER NOT NULL,
  created_offset INTEGER NOT NULL CHECK (created_offset BETWEEN -840 AND 840)
);

CREATE INDEX memory_events_by_memory
  ON memory_events(workspace_id, memory_id, id);

CREATE INDEX memories_search ON memories(state, kind, label, updated_at, memory_id);
CREATE INDEX memories_list ON memories(workspace_id, updated_at, memory_id);
```

说明：

- `memory_id` 仅在 WID 内唯一（复合主键），跨工作区可以重复；由应用层生成，格式不透明。
- 复合外键保证替换链不跨工作区；默认 RESTRICT 动作要求删除前先清除另一端指针（见 §3.3 delete 的顺序）。
- `memory_history` 保存每次成功 `update`/`supersede`/`invalidate` 前的完整旧行，并记录产生该归档的 `action`；`version` 为归档时旧行的 version。不设外键，因为归档快照可能引用后来已不存在的行。历史行可通过 `memory-get` 的 `version` 参数与 `memory-history` 读取（协议 §7.2、§7.9）。
- `memory_events` 是变更日志：每次 mutation 一行，不存正文。`from_version`/`to_version` 为变更前/后的版本（无意义时为 NULL：create 无 from）；`related_memory_id` 在 supersede 时指向新记录；`reason` 承载协议可选 reason。事件与 mutation 在同一事务内写入；本版本不通过任何读取命令暴露，供审计、变更摘要与未来统计使用。delete 不写事件——该 memory 的事件随其一并抹除。

### 3.3 状态迁移语句

所有写命令在同一个 memory DB 写事务内完成，检查顺序为：存在性 → expected version → state（协议 §8）。语句中 `WHERE ... AND version = ?` 影响行数不为 1 时返回 `version_conflict` 并回滚。每次 mutation 在同一事务内写入一行 `memory_events`。

- create：`INSERT INTO memories(...) VALUES(...)`；`state = 'active'`、`version = 1`、`supersedes` 为新记录前身 ID 或 NULL、`superseded_by = NULL`。事件：`action = 'create'`、`to_version = 1`、`reason` 来自请求。
- update：
  1. SELECT 旧行（无行 → `memory_not_found`）；
  2. 旧 version 不匹配 → `version_conflict`；state 非 active → `invalid_memory_state`；
  3. INSERT 旧行到 `memory_history`（`action = 'update'`、`archived_at = now`）；
  4. `UPDATE memories SET content = ?, kind = ?, label = ?, source_json = ?, metadata_json = ?, version = version + 1, updated_at = ?, updated_offset = ? WHERE workspace_id = ? AND memory_id = ? AND version = ?`。
  事件：`action = 'update'`、`from_version` = 旧 version、`to_version` = 旧 version + 1、`reason` 来自请求。
- supersede：
  1. SELECT 旧行并检查存在性、version、state（同 update）；
  2. INSERT 旧行到 `memory_history`（`action = 'supersede'`）；
  3. INSERT 新行（`state = 'active'`、`version = 1`、`supersedes = 旧id`）；
  4. `UPDATE memories SET state = 'superseded', superseded_by = ?, version = version + 1, updated_at = 新记录 created_at, updated_offset = 新记录 created_offset WHERE workspace_id = ? AND memory_id = ? AND version = 旧version`。
  事件：旧 memory 一条（`action = 'supersede'`、`from_version` = 旧 version、`to_version` = 旧 version + 1、`related_memory_id` = 新 id、`reason` 来自请求）+ 新 memory 一条（`action = 'create'`、`to_version = 1`）。
- invalidate：
  1. SELECT 旧行并检查（同 update）；
  2. INSERT 旧行到 `memory_history`（`action = 'invalidate'`）；
  3. `UPDATE memories SET state = 'invalid', version = version + 1, updated_at = ?, updated_offset = ? WHERE workspace_id = ? AND memory_id = ? AND version = ?`。
  事件：`action = 'invalidate'`、`from_version` = 旧 version、`to_version` = 旧 version + 1、`reason` 来自请求。
- delete：
  1. SELECT 旧行；无行 → `memory_not_found`；version 不匹配 → `version_conflict`。不检查 state；
  2. `DELETE FROM memory_history WHERE workspace_id = ? AND memory_id = ?`（删除全部历史版本）；
  3. `DELETE FROM memory_events WHERE workspace_id = ? AND memory_id = ?`（删除全部事件行——delete 是彻底抹除，不留 delete 事件）；
  4. 若旧行的 `supersedes` 或 `superseded_by` 非 NULL，清除另一端指针并给另一端 version 加 1：
     `UPDATE memories SET supersedes = CASE WHEN supersedes = ? THEN NULL ELSE supersedes END, superseded_by = CASE WHEN superseded_by = ? THEN NULL ELSE superseded_by END, version = version + 1, updated_at = ?, updated_offset = ? WHERE workspace_id = ? AND (supersedes = ? OR superseded_by = ?)`；
     另一端 version 加 1 保证持有旧版本号的写入者得到 `version_conflict`；
  5. `DELETE FROM memories WHERE workspace_id = ? AND memory_id = ? AND version = ?`（影响行数不为 1 → `version_conflict`）。
  顺序保证先清指针再删行，复合外键（RESTRICT）不会阻止删除。
- clear：同一事务内 `DELETE FROM memories WHERE workspace_id = ?`（行数即 `deleted_count`），随后 `DELETE FROM memory_history WHERE workspace_id = ?`，再 `DELETE FROM memory_events WHERE workspace_id = ?`。

**回滚**没有专用语句：它就是一次普通 `update`，其 content 来自事先读取的历史版本（协议 §7.2、§7.5）。回滚永不删除版本。

### 3.4 读取语句

- get：`SELECT ... FROM memories WHERE workspace_id = ? AND memory_id = ?`；任意 state。带 `version` 参数时：若该版本等于当前行 version，直接返回当前行；否则 `SELECT ... FROM memory_history WHERE workspace_id = ? AND memory_id = ? AND version = ?`；无行 → `memory_not_found`。
- history：从两张表拼装版本列表——当前版本取 `SELECT version, action, state, updated_at, updated_offset FROM memories WHERE workspace_id = ? AND memory_id = ?`（其 `action` 为最新事件的 action，`archived_at` 为 NULL），历史版本取 `SELECT version, action, state, updated_at, updated_offset, archived_at FROM memory_history WHERE workspace_id = ? AND memory_id = ? ORDER BY version ASC`——按 `version` 升序合并。归档行的 `action` 是归档该行的操作，也就是产生*下一个*版本的操作；因此拼装后的列表中，版本 1 的 action 恒为 `create`，其后每个版本的 action 取前一个归档行的 action。
- diff：读取两个请求的版本（当前行或历史行，同上）并在应用层比较 content/kind/label/source_json/metadata_json/state，只返回发生变化的字段。
- search：检索语句为 `SELECT ... FROM memories WHERE workspace_id = ? AND state = 'active' ORDER BY updated_at DESC, memory_id ASC`；时间范围、`kind`/`label` 精确过滤、关键词子串匹配与 score 全部由应用层按协议 §7.1 的确定算法在这些返回行上计算（过滤条件不下推 SQL 也不改变结果集合的语义；`label IS NULL` 的行不命中任何非空 `labels` 过滤）。
- list：keyset 分页。首页：`SELECT ... FROM memories WHERE workspace_id = ? ORDER BY updated_at DESC, memory_id ASC LIMIT ?`；后续页：`SELECT ... FROM memories WHERE workspace_id = ? AND (updated_at < ? OR (updated_at = ? AND memory_id > ?)) ORDER BY updated_at DESC, memory_id ASC LIMIT ?`。每次取 `limit + 1` 行判断是否还有下一页；cursor 编码上一页最后一行的 `(updated_at, memory_id)`，为 opaque 字符串。cursor 载荷用 `meta_info.cursor_key` 作为密钥做 HMAC-SHA256 认证；无法解码或 HMAC 校验不通过的 cursor 返回 `invalid_request`，调用方无法伪造 cursor 来改变分页起点。

### 3.5 文件删除

`workspace-delete` 在 meta 事务提交后删除 `workspace-{WID}-memory.db` 及其 `-wal`、`-shm` 文件：先尽力删除 `-wal`/`-shm`，再删除主文件；主文件必须删除成功，否则返回 `internal_error`。mapping 已删除不可回滚，残留文件因 WID 不复用而永远不会再被访问。

## 4. 打开校验与损坏处理

- `meta.db` 与 memory DB 路径必须是 regular file 且非 symlink，否则分别返回 `home_broken` / `workspace_broken`。
- `PRAGMA application_id` 与 `PRAGMA user_version` 必须匹配 §1 的标识：application_id 错误 → `home_broken` / `workspace_broken`；user_version 不受支持 → `schema_mismatch`。
- 表头行必须存在：`meta_info` 的 `schema_version = 4`，且其 `cursor_key` 必须是合法的 32 字节十六进制值；memory DB 的 `memory_info` 中 `workspace_id` 必须与本次打开的 WID 一致且 `schema_version = 4`。
- `memory/` 目录内每个条目都必须是 regular file 且非 symlink，名字符合 `workspace-{十进制WID}-memory.db`、`-wal`、`-shm` 模式；出现任何其他条目 → `workspace_broken`。名字符合模式但 WID 未注册的文件属于 `workspace-delete` 留下的残留，不视为异常，也永远不会被打开。
- 不要求每次打开运行 `PRAGMA integrity_check`；检测到损坏时按 `workspace_broken` / `home_broken` 报告，绝不静默重建或清空数据。`workspace-delete` 是清理半损坏 workspace 的唯一通道。

## 5. 命令-事务矩阵

| 命令 | WID 锁 | meta 事务 | memory DB 事务 | 提交顺序 |
|---|---|---|---|---|
| `workspace-resolve` | 无 | 短读 | 无 | — |
| `workspace-register` | 无（由 meta 写事务 + O_EXCL 建库协调） | 写 | 建库（无事务） | meta 提交后建库 |
| `workspace-rebind` | 写 | 写 | 无 | — |
| `workspace-clear` | 写 | 读（校验 mapping） | 写 | memory 提交后释放锁 |
| `workspace-delete` | 写 | 写（删 mapping） | 无（删文件） | meta 提交后删文件 |
| `memory-search` | 读 | 读（校验 mapping） | 读 | — |
| `memory-list` | 读 | 读（校验 mapping） | 读 | — |
| `memory-get` | 读 | 读（校验 mapping） | 读 | — |
| `memory-history` | 读 | 读（校验 mapping） | 读 | — |
| `memory-diff` | 读 | 读（校验 mapping） | 读 | — |
| `memory-create` / `update` / `supersede` / `invalidate` / `delete` | 写 | 读（校验 mapping） | 写 | memory 提交后释放锁 |

锁的持有范围覆盖命令的整个执行过程；读命令持有读锁，写命令持有写锁，workspace 级写命令的写锁排斥该 WID 的一切 memory 访问。
