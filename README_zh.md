# dsh-memory-note

DeepSeek Harness 的轻量、本地、跨会话记忆核心：一个一次性（one-shot）Go
可执行文件 + 一层薄 TypeScript 适配器 bundle。

```text
DeepSeek Harness -> 适配层（bundle）-> Go 核心 -> SQLite
```

## 存储结构

```
DSH_MEMORY_NOTE_HOME/             # 默认~/.local/share/dsh-memory-note
    meta.db                       # 元数据
    memory/
        workspace-WID-memory.db   # 各工作区记忆
```

## 用法

一次调用只执行一个子命令：

```text
dsh-memory-note <subcommand> '<json-request>'
```

- 工作区：`workspace-resolve`、`workspace-register`、`workspace-rebind`、
  `workspace-clear`、`workspace-delete`
- 记忆：`memory-search`、`memory-list`、`memory-get`、`memory-create`、
  `memory-update`、`memory-supersede`、`memory-invalidate`、`memory-delete`
- 内部命令：`version`、`help`

记忆数据存放在 `~/.local/share/dsh-memory-note`（可用
`DSH_MEMORY_NOTE_HOME` 覆盖）。完整 JSON 契约见
[`doc/zh/protocol-v1-proposal.md`](doc/zh/protocol-v1-proposal.md)。

## 安装 / 卸载

依赖：`go`、`pnpm`、`dsh` 都在 PATH 里。

```sh
git clone <仓库地址>
cd dsh-memory-note
scripts/install.sh [profile]    # profile 默认 web
scripts/uninstall.sh [profile]
```

`install.sh` 把 Go 核心构建进 `GOBIN`、构建 adapter bundle，再通过
`dsh plugin ... add link:` 注册进 profile。`uninstall.sh` 移除 bundle 与
核心二进制；记忆数据不会被删除。

安装后请确认 `$(go env GOPATH)/bin` 在 PATH 里，并重启 profile
（`dsh --profile <name>`）以加载工具。
