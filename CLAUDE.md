# CLAUDE.md

AILauncher v4 —— 零依赖 Go AI 编程工具启动器。架构拆为**三个独立 Go 项目**（core / cli / ui），
依赖单向 `ui → cli → core`，任何层不得反向依赖。纯标准库，无外部依赖（`proxy.golang.org` 不可达，
TUI 为手写 ANSI 全屏重绘）。旧 v3 源码在 `../AI_LAUNCHER_v3_archive/`，本目录从零重建。

## 运行 / 验证

```bash
go build -o ailauncher.exe ./ui/cmd/ailauncher   # 构建单二进制（Go 1.26.5）
./ailauncher                                    # 无参数 → 显式 TUI 主菜单（启动工具 / Agent 会话 / 历史会话）
./ailauncher -i <agent> [path]                  # TUI 交互直达（legacy 启动流程）
./ailauncher chat <agent> [path]                # Claude Code 风格流式会话（workspace 为兼容别名，需 adapter）
./ailauncher history-replay <session-id>        # 只读回放一条本地 transcript
./ailauncher run <agent> [path] [KEY=V | arg...] # 命令类：启动 agent
./ailauncher cap                                # 能力清单（JSON）
```

工作区在根 `go.work`（`use ./core ./cli ./ui`）。**根目录不是 module**：`go build ./...` 不可用，
一律用 `go test ./core/... ./cli/... ./ui/...`。验证：build + vet + test 三模块全绿 +
四平台（windows/linux/darwin/freebsd）交叉编译。

## 架构（三层，依赖单向）

```
用户 → ui（TUI 前端，唯一入口）→ cli（纯路由/api 端口）→ core（逻辑处理层）
脚本 → cli（同一路由，另类 api）
```

| 模块 | 角色 | 关键文件 |
|---|---|---|
| `core/` | **逻辑处理层**：每条逻辑是一个自注册单元（`stage` + `declared` 声明），命令与查询皆有 | `registry.go`（Unit/Register/Snapshot/Capabilities）、`stage.go`、`config.go`/`state.go`/`agent.go`/`extension.go`、`launch*.go`/`terminal*.go`/`procattr*.go`（build tag 平台隔离）、`cmd_*.go`（命令/查询单元，init 自注册） |
| `cli/` | **纯路由微前端**：命令进来先问 core（按 stage 过滤）→ proto-lab → 都不认返回**详细信息**（机器可读能力清单）。不持有逻辑。**对外供应端口**：`cli.Port` 接口 + core 数据模型别名 | `port.go`（Port/别名/Text/JSON）、`router.go`（Dispatch/Format/ExitCode/IsUnknown）、`protolab.go`（本地实验位） |
| `ui/` | **前端**（TUI 先，GUI 未来）：交互/体验优化全归本层；所有逻辑经 `cli.Port` 端口，**只 import cli** | `cmd/ailauncher/main.go`（唯一入口）、`tui/`（run.go + 页面 + cli_port.go + term_*.go 平台分支） |

依赖方向：`ui → cli → core`，**任何层不得反向依赖**。`cli/port.go` 定义**供应侧端口**
`type Port interface { Dispatch(argv []string, in io.Reader) (*Result, error) }` 并把 core 数据模型
`Config/Agent/AgentState/Result` 用**类型别名**导出（别名即同一类型，零拷贝）。`*cli.Router` 满足
`Port`；ui **只 import cli**，经 cli 别名命名 core 数据模型，不 import core、不碰 core 逻辑。
`cli.Text`/`cli.JSON` 构造 Result，ui 测试桩无需引用 core。core 不 import 任何层。实时会话另用可选
`cli.SessionPort`（嵌入但不修改 `Port`），`*cli.Router` 供应 `OpenSession/ListSessions/GetSession`；ui 仍只经 cli
类型别名拿 `Session/SessionEvent`，不得接触 adapter、子进程或 JSON-RPC。

## 生命周期与 stage

- `draft → checking → experimental → stable → abandoned`。自研直进 experimental。
- **A 阶段 = 运行时分级**：默认只暴露 `stable`；`experimental` 打包但不默认暴露
  （`AILAUNCHER_EXPERIMENTAL=1` 开启，供试用/cap 展示）；draft/checking 仅存在不暴露；
  abandoned 不参与路由。晋升/回退只改单元声明里的 stage，无需动其他。
- cli 的 `visibleUnits()` 统一过滤；`cap` 与未知命令详细报错共用同一信息源（`capabilities()`）。
- B 阶段构建期按 stage 剪包（go:generate）挂账，未做。

## 命令面（v4 全新，无旧兼容）

```
ailauncher                      → TUI（无参数，主菜单）
ailauncher -i <agent> [path]    → TUI 交互直达（legacy 启动流程）
ailauncher chat <agent> [path]  → ACP 会话（workspace 为兼容别名，需 adapter）
ailauncher history-replay <session-id> → 只读回放一条本地 transcript
ailauncher run <agent> [path] [KEY=V | arg...]  命令类：启动（path 为工作目录）
ailauncher dsh [web|headless] [KEY=V | arg...]  命令类：DeepSeek Harness（web 起后台服务 / headless 跑一次性任务）
ailauncher list / version / migrate / help [cmd]  命令类
ailauncher resolve <name|display> / cap / config / state get <agent>  查询类（JSON）
ailauncher state put <agent>    → 命令类：写状态（JSON 自 stdin）
ailauncher session list / get <id> / delete <id> / probe <agent>  会话管理（JSON；CLI 是开发者排障面）
```

- 退出码：`0` 成功 / `1` 运行时错误 / `2` 未知命令·用法。未知命令返回详细信息（名字 + 能力清单 + cap/help 提示）。
- `run` 的 path = **首个非 KEY=V 的 token**；不需要 path 时传空串 `""` 占位（TUI 内部如此，`buildRunArgv` 保证）。
- `KEY=V` token 解析为环境变量，其余为附加参数（`ParseArgs`）。

## 数据模型（关键）

- **Config 只读种子** `{ terminal, defaultDirectories, agents[] }`。v4 **移除 legacy tools[] 兼容读取**
  （`migrate` 遇旧格式报错提示手工迁移）；`ResolveAgent` 按 name 或 display 精确匹配。
- **State 运行时** 按 agent name 分 key，每 agent `{ directories, removedDirs, commands, removedCmds,
  seeded, rootDir, recent, favorites, extension }`；`State.Get(name)` 缺失自动补默认结构。
- **Extension** `map[string]json.RawMessage` 通用向前兼容槽（adapter 字段并入此约定）。消费方：
  - `extension.dsh.repo`（`dsh` 单元 + `run`/TUI 的仓库服务型 agent，见 `cmd_dsh.go`）。
  - agent 级 `extension.dsh.repo`：`expandWorkdir` 强制 cwd 仓库根（`run dsh` / TUI 选 dsh agent 起后台 web 服务）。
  - agent 级 `extension.adapter`：ACP 会话后端配置。`kind:"acp"`，使用 `command` + `args` argv、可选
    `cwd/env` 与 `permission:"allow"|"reject"|"ask"`（默认 reject；ask 走 TUI 交互确认）。不可把 env 或 token 写入日志/错误。
- Workspace transcript 独立写 BaseDir 的 `sessions.jsonl`，不扩展 `AgentState/data.json`；记录头存标题/远端ID/模式/用量快照，
  事件行存 canonical event（老行缺新字段按零值解码）。本地 transcript 可回放，不等于远端 resume（resume 另调后端能力）。
- `AgentState.normalize()`：nil 切片→`[]`（单点，state put 与 migrate 共用，避免落盘 null 数组）。
- 数据文件留在项目根：`config.json` / `data.json` / `config.sample.json`。BaseDir：cwd 优先回退 exe 目录。

## 启动语义（core）

- **gui**：`exec.Command(exec, args...).Start()` detached，不套终端；env 合并 `agent.Env` + 运行时 envs；
  `agent.Args` 按空白拆词拼 argv。
- **tui**：拼 `set K=V && … && <exec> <args> <extra>`（Windows）或 `K=V … <exec> …`（Unix），
  用 `config.terminal` 拉起；启动参数按终端类型分发（`terminal_*.go`：Alacritty 默认 / wt / wezterm）。
- **cwd 决定**：`expandWorkdir`（launch.go 纯函数）——`extension.dsh.repo`（仓库服务型）优先强制仓库根，
  再传入 workdir，回退 USERPROFILE/当前目录。
- **独立性（Windows）**：直拉带 `CREATE_BREAKAWAY_FROM_JOB` 逃出 kill-on-close Job；被拒时走
  `launchWindowsFallback` 两层逃逸：① WMI `Win32_Process.Create`（wscript 触发，WmiPrvSE 创建进程，
  确定性在 Job 外）② explorer.exe；再失败不带 BREAKAWAY 直拉兜底。
- `var launchFn = Launch`（core）可注入桩便于单测；`syscall.EscapeArg` 用于 Windows argv。

## TUI 前端（ui/tui）

纯 stdlib 高效渲染：无参数进**显式主菜单**（启动工具 / Agent 会话 / 历史会话）。
session 持 `cli.Port` + `*cli.Config`（引导数据）；agent 状态逐个 `state get/put`，启动走 `run`。
交互直达 `RunDirect` 跳过选工具页（legacy 启动流程）。

`chat <agent> [path]`（`workspace` 为兼容别名）是 ACP 会话宿主：只依赖 `cli.SessionPort`，
渲染 canonical user/assistant/thinking/tool/permission/usage/command/mode/title/plan/diff/elicitation/status/error
event。备用屏 + 脏行 diff + 50ms 分批渲染，中英文双宽感知；Enter 发送，`/` 进命令
（help/mode/model/commands/usage/title/quit），权限挂起时数字/y/n/ESC 直接回应，
Ctrl-C/ESC 运行中取消（挂起权限按 cancelled 回应）、空闲时退出，↑↓历史、PgUp/Dn 滚动。
选 agent 前先 probe 就绪态（只握手不建会话），进会话前可选恢复远端上下文。
不得让会话页 import core、调用 exec、解析 JSON-RPC，或污染 legacy launcher 的 `session` 结构。
不解析后端 ANSI，不宣称 Markdown 或附件。

- **选工具**：单栏列表；`gui` 型 agent 直接拉起短路（不选目录/参数）。
- **选目录**（`selectDir`）：①播种（首次 agent.Directories + defaultDirectories 合并去重过滤写 state，
  state 是唯一事实来源）→ ②选根（候选根 = agent.RootDir + 历史目录父目录 + 默认目录，M 手动 / Q 跳过）
  → ③项目列表（每次扫描根下子目录，N 新建 / R 切根）。
- **参数页**（`runArgs`）三区焦点：0=输入框（回车即运行并去重置顶进历史）、1=历史、2=已移除；Tab 循环。
- 移除列表是软删除（d 移入已移除 / u 移回）；页面以指针改内存态，变更经 save 闭包 → cli `state put` 落盘。
- 输入由 goroutine 从 os.Stdin 读原始字节推入通道（`keyReader`），ESC 用 ~100ms 窗口判定单独 ESC vs 转义序列。
- 非 TTY 时 `enterRaw` 失败 → 报「需要交互式终端」退出。

## 修改约定

- Go 代码 `gofmt` 对齐；纯函数优先配单测（stdlib `testing`）。TUI 页面用「注入字节流 + 丢弃输出 Screen +
  stub CLI」驱动（见 `ui/tui/*_test.go`），无需 TTY。
- 平台差异用 build tag 文件隔离（`*_windows.go` / `*_linux.go` / `*_bsd.go` / `*_other.go`）；
  改 raw 模式 / 平台服务先交叉编译各平台验证。
- 可打印字符（含中文/空格/路径分隔符）必须在 raw 模式下手动收集；**不要引入 readline interface**。
- 任何列表变更都要触发 save（经 cli `state put`）；手动输入目录做 `dirExists` 校验。
- 退出路径统一恢复光标/清屏/退 raw 模式（`Run`/`RunDirect` 里 `defer cleanup()`）。
- agent 文案（display/desc）在 config 里改，别动代码。
- 新增逻辑单元：在 core 建 `cmd_*.go` 自注册（`init()` 里 `Register`），声明 stage + declared；
  逻辑跟注册同文件，不维护集中表。实验逻辑先进 `cli/protolab.go`。
- 新逻辑声明中文 declared；跨模块改动注意依赖单向（ui 不 import cli 包，core 不 import 任何层）。

## 部署

**触发时机**（满足任一即执行发布，把最新功能二进制同步到生产目录）：

- 小版本迭代（改动落地后）
- 功能增加
- 版本发布（Release）
- Git 支链创建/切换

**一条命令**：

```powershell
pwsh -File .\release.ps1              # gofmt + vet + test 全绿才构建 → 同步
pwsh -File .\release.ps1 -SkipTests   # 仅紧急同步时用
pwsh -File .\release.ps1 -WhatIf      # 只验证+构建，不同步
```

- **部署目录不在仓库里**：由 `-Target` 参数或 `AILAUNCHER_DEPLOY_DIR` 环境变量给出。解析不到时脚本
  明确报错退出（exit 2），**绝不静默部署到别处**。设一次环境变量即可无参运行。
- 生产数据（`config.json` / `data.json`）在部署目录，已是 v4 agents[] 格式。
- 脚本同步 `ailauncher.exe` + `config.json`，覆盖前按时间戳备份为 `*.bak-yyyyMMdd-HHmmss`。
- 生产 `ailauncher.exe` 在跑时脚本**拒绝发布**（复制会失败），先关闭再执行。
- 发布后自动跑 `ailauncher.exe version` 烟雾测试。
- 变更 schema 保留幂等 migrate（`migrate` 单元），旧数据先 `migrate` 再发布。

## 仓库与提交

- 远端：<https://github.com/LingYiWu015/ailauncher>（公开）。默认分支 `main`。
- **个人数据一律不入库**（`.gitignore` 强制排除）：`config.json`、`data.json`、`sessions.jsonl`、
  `ailauncher.exe`、`.claude/`、`.commandcode/`。它们含个人绝对路径、运行时状态与会话记录；
  仓库只提供 `config.sample.json` 作为通用模板。**新增配置类文件前先判断是否属于「个人数据」**。
- **勿在 `.gitignore` 写裸 `ailauncher`**：不带斜杠的模式会匹配任意层级的同名目录，把
  `ui/cmd/ailauncher/` 源目录一起忽略、静默丢掉程序入口。限定根目录要写 `/ailauncher`。
- `.gitattributes` 统一换行（源码 LF、`*.ps1` CRLF）：Windows 默认 `core.autocrlf=true`，
  不锁定会让仓库内容随平台漂移。
- 提交与发布是两件事：`git push` 只更新远端源码，**不等于**发布；`release.ps1` 负责把二进制
  同步到部署目录，也不要求先推送。改动落地后两者按需各自执行。
- **提交信息不加任何工具署名**：不得自行附加 `Co-authored-by` 之类的 bot/工具 trailer。
  提交历史只体现人类作者；是否需要署名由仓库所有者决定，不由工具代劳。
