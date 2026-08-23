# dsh-memory-note

[English](README.md) | **中文**

DeepSeek Harness 的轻量、本地、跨会话记忆核心：一个一次性（one-shot）Go
可执行文件 + 一层薄 TypeScript 适配器 bundle。

```text
DeepSeek Harness -> 适配层（bundle）-> Go 核心 -> SQLite
```

## 快速开始

依赖：`go`、`pnpm`、`dsh` 都在 PATH 里。

**Unix 类（Linux / macOS / BSD）：**

```sh
git clone https://github.com/DecarbonizedGlucose/dsh-memory-note
cd dsh-memory-note
scripts/install.sh [profile]    # profile 默认 web
```

**Windows（PowerShell）：**

```powershell
git clone https://github.com/DecarbonizedGlucose/dsh-memory-note
cd dsh-memory-note
.\scripts\install.ps1 [-Profile web]
```

安装脚本把 Go 核心构建进 `GOBIN`，构建 adapter bundle，再通过
`dsh plugin ... add link:` 注册进 profile。

安装后请确认 `GOBIN`（`$(go env GOPATH)/bin`）在 PATH 里，并重启 profile
（`dsh --profile <name>`）以加载工具。

卸载：

```sh
scripts/uninstall.sh [profile]          # Unix 类
```

```powershell
.\scripts\uninstall.ps1 [-Profile web]  # Windows
```

卸载脚本移除 bundle 与核心二进制，然后询问是否删除整个记忆数据目录（此前
先做硬性安全检查——只有确认确实是本工具数据目录才会删除）。

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

完整 JSON 契约见
[`doc/zh/protocol-v1-proposal.md`](doc/zh/protocol-v1-proposal.md)。

## 存储结构

```
DSH_MEMORY_NOTE_HOME/             # 默认值见下方平台说明
    meta.db                       # 元数据
    memory/
        workspace-WID-memory.db   # 各工作区记忆
```

- Unix 类（Linux / macOS / BSD）：`~/.local/share/dsh-memory-note`
- Windows：`%LOCALAPPDATA%\dsh-memory-note`（回退
  `%USERPROFILE%\AppData\Local\dsh-memory-note`）

可用 `DSH_MEMORY_NOTE_HOME` 覆盖。
