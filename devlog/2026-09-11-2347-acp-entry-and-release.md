# step17 · ACP 入口可用化 + TUI 卡顿治理 + 发布工作流

> 2026-09-11。把「TUI 上的 Agent 会话入口」从摆设做成可用，治掉实测出来的四处卡顿，并把「同步到生产目录」固化成一条命令。

## 做了什么

### 1. 起因：入口是摆设

真机截图显示 7 个 agent 全部标 `run 模式（未配 adapter）` —— TUI 的 Agent 会话入口等于不可用。

排查出两个原因：

- 旧二进制（9/4 构建）+ 生产 `config.json` 里 opencode 根本没配 adapter。
- ui 侧调用了不存在的 `effectiveAdapterOf`，`go vet` 都过不去。

### 2. core：内置推导 + agent 管理命令

新增 / 改动：

- `core/adapter.go`：`builtinAdapter()` 按 agent 名/exec 推导开箱配置（目前只有 opencode 原生支持 `opencode acp`）；`EffectiveAdapter()` 显式配置优先、缺失才试内置，返回 `source` 为 `config`|`builtin`；`commandExists()` 本地预检。
- `core/agent.go`：新增 `Enabled *bool` + `IsEnabled()`（缺省启用，老配置零迁移）。
- `core/cmd_agent.go`（新）：`agent list`（接入摘要）/ `agent enable|disable <name>` / `agent doctor [name]`（真握手体检 + 可操作 hint）。
- `core/session_manager.go`：`Probe` 改走 `EffectiveAdapter`，`Open` 同步改（`adapter.go`）。
- `cli/port.go`：透出 `AgentSummary` 类型别名。ui 不碰 core 逻辑，摘要经 `agent list` 拿。

### 3. config：开箱即用

- `opencode`：`extension.adapter = {kind: acp, command: opencode, args: [acp], permission: ask}`。
- `dsh`：补回丢失的 adapter（`pnpm --dir <repo> run demo:acp`，cwd 仓库根）。

### 4. 性能：四处卡顿（先实测，再动手）

实测数据：`session probe opencode` **1.1s**；`agent doctor` 全量 **14s**（dsh 的 `demo:acp` 冷启动 tsx 编译极慢）。

- **选单同步串行 probe** → `listPage` 拆出 `listPageLive(o, updates)`：主循环 select 按键 + 外部更新通道，`startProbes()` 后台并行探测、每完成一项发一条 `listUpdate` 逐行刷新；`probeCache` 让同一次 TUI 内进出不重复握手。按键处理抽成 `listKey()` 纯函数（`budget` 传入以保留 PgUp/PgDn 精确步进），`listKeys()` 保持原同步语义给单测。
- **建连/远端 list 黑屏阻塞** → `openSessionSplash()`（run.go）、`pickRemoteResume` 后台化、`openChatSession`（chat_loop.go）三处加 spinner + ESC 取消/跳过。
- **落盘全量读文件** → `FileSessionStore` 加内存索引（`ensureLocked` 按 mtime+size 失效重载），`Append` 降到 O(1)（只追加内存 + 写一行），Create/Update/Delete/List/Get 全切索引。
- **渲染每帧全量重算** → `chatBlock.cached` 行缓存（已完成块渲染一次常驻，layout 只拼装）；`renderBlocks` 拆出 `renderOneBlock`；思考块按 `t` 折叠（`thinkingCollapsed`）。

### 5. 发布工作流

- `release.ps1`（新）：`gofmt` → `vet` → `test` 三模块全绿才构建 → 同步 `ailauncher.exe` + `config.json` 到 `D:\Coding\Scripts\ai-launcher`，覆盖前按时间戳备份，生产进程在跑时**拒绝发布**，发布后自动 `version` 烟雾测试。支持 `-SkipTests` / `-WhatIf` / `-Target`。
- `CLAUDE.md` 部署节重写成显式触发式工作流（小版本迭代 / 功能增加 / Release / Git 分支创建）。

## 为什么

- 「TUI 入口不是摆设」：CLI 是给开发者排障的，普通用户只会走 TUI。入口标满「未配 adapter」等于产品没做出来。所以做**内置推导 + 真就绪态自检**：能跑的标就绪（可一键接入），不能跑的给出可操作原因，而不是抛一句配置术语。
- 卡顿必须**先量再改**。1.1s / 14s 是量出来的，不是猜的；据此才决定「异步化 + 缓存」而不是盲目优化渲染。
- 发布动作此前只散落一句「复制到 D:\...」，无触发时机、无门槛、无保护。踩过「覆盖正在运行的生产 exe」和「生产 config 缺 adapter」两次，所以做成脚本。

## 影响与已知限制

- **只有 opencode / dsh 能走 ACP**。claude / codex / reasonix / cc-switch / cmdc 五个无 ACP 面，仍标 `run 模式（暂不支持 ACP）`，走原启动路径。
- dsh 的 `demo:acp` 冷启动十几秒（tsx 编译），已用后台 + spinner 兜住体验，但首次仍慢。
- 生产 `README.md` 仍是 8/12 版（本次只同步 exe + config）。
- 生产 `config.json` 与项目**逐字节一致**（hash 校验），二者差异只剩 adapter 两处，无生产专有内容被覆盖。

## 验证

- `gofmt` 干净；`go vet` 通过；`go test ./core/... ./cli/... ./ui/...` 三模块全绿。
- 交叉编译 linux / darwin 通过。
- 单测新增：`TestEffectiveAdapterBuiltin`、`TestAgentEnableDisable`、`TestSessionStoreMemIndex`、`TestFetchAgentSummaries`、`TestEnabledAgentsFiltersDisabled`、`TestRenderBlocksCachesFinished`、`TestThinkingCollapse`。
- 生产 `ailauncher.exe agent doctor opencode` → `reachable: true, protocol: 1, agentVersion: 1.18.13`。
- 发布记录：`ailauncher.exe` 4,631,040 字节（原 4,062,208）；备份 `ailauncher.exe.bak-20260911-234724`、`config.json.bak-20260911-234724`；`data.json` 原样保留。

## 下一步

- 待真机验收：进 Agent 会话看列表是否秒出、opencode 约 1s 变就绪、发一句话看流式渲染 + `/mode` + 权限确认手感。
- 生产 `README.md` 待同步（或纳入 release.ps1 的可选清单）。
- Git 未初始化，「分支创建」触发的发布暂不适用；`git init` 后纳入。
- 五个无 ACP 的 agent 若要接入，需它们各自提供 stdio ACP 面，再往 `builtinAdapter` 里加推导。
