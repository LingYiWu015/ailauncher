# AILauncher v4 项目总览

> 更新：2026-08-26，加入实验性 Agent Workspace MVP（dsh ACP backend + 本地 transcript）。供快速了解「项目是什么、做到哪、还能往哪长」。架构细节见
> `CLAUDE.md`，使用见 `README.md`，开发存档见 `devlog/`。

## 1. 定位

**零依赖的 AI 编程工具启动器 + 实验性 Agent Workspace（v4 三层架构）**。传统路径是在单个终端里选 AI 工具 → 选工作目录 → 传高级参数 → 拉起；Workspace 路径消费 agent 的机器协议，由 AILauncher 自己渲染会话文本并持久化本地 transcript。两条路径并存，legacy `run` 永远保留。架构拆为三个独立 Go 项目，依赖单向 `ui → cli → core`：

- **core = 逻辑处理层**：每条逻辑是一个自注册单元（`stage` + `declared` 声明），命令与查询皆有；
  数据模型（Config/State/Agent/Extension）+ 平台服务（launch/terminal/procattr）也在此层。
- **cli = 纯路由微前端**：命令进来先问 core（按 stage 过滤）→ proto-lab 实验位 → 都不认返回
  **详细信息**（机器可读能力清单）。不持有逻辑。
- **ui = 前端**（TUI 先，GUI 未来）：交互/体验优化全归本层；所有逻辑经 `cli.Port` 端口完成，只 import cli。

- **技术**：Go 1.26.5、纯标准库、零外部依赖（网络不可达 → TUI 手写 ANSI 全屏重绘，无 bubbletea/readline）。
- **版本**：`0.4.0-dev`（`version` 命令）。
- **平台**：Windows 10/11（主目标，已实测构建）、Linux（交叉编译验证）、macOS（代码支持不发行）、FreeBSD（顺带支持）。
- **数据现状**：`config.json` 为 v4 `agents[]` 格式（6 个 agent：opencode/claude/codex/reasonix/cc-switch/dsh）；
  `data.json` 无 null 数组残留（`AgentState.normalize()` 单点归一化，state put 与 migrate 共用）。
  旧 v3 源码在 `../AI_LAUNCHER_v3_archive/`。

## 2. 架构

```
go.work                    工作区：use ./core ./cli ./ui（根不是 module，用 ./core/... ./cli/... ./ui/...）
core/                      逻辑处理层（module core）
  registry.go   Unit{Name,Stage,Kind,Declared,Run} + Register/Snapshot/Lookup/Capabilities + Result/Ctx
  stage.go      Stage 生命周期 + 可见性过滤（stable 默认 / experimental 需环境变量）
  config.go / state.go / agent.go / extension.go  数据模型 + Load/Save/Resolve + Extension 兼容槽
  launch.go + launch_windows/other.go              gui detached / tui 套终端拉起 + ParseArgs + 平台回退
  terminal_windows/other.go  按 config.terminal 分发终端参数（Alacritty/wt/wezterm）
  procattr_windows/other.go  detached 平台属性（Windows 含 CREATE_BREAKAWAY_FROM_JOB）
  migrate.go / cmd_*.go     命令与查询单元（init 自注册，声明中文 declared）
  cmd_dsh.go                DeepSeek Harness 集成单元（web 后台服务 / headless 任务，仓库根走 extension.dsh.repo）
  session.go / adapter*.go  provider-neutral Session + adapter registry + ACP JSON-RPC/stdio 适配器
  session_store/manager.go  sessions.jsonl transcript + live 生命周期（本地回放，不是 remote resume）
  cmd_session.go            session list/get 本地 transcript 查询
cli/                       纯路由端口（module cli）
  port.go     Port + 可选 SessionPort + core 数据模型别名（ui 只 import cli 即可接触会话）
  router.go    New/Dispatch/Format/ExitCode/IsUnknown + SessionManager + visibleUnits（stage 过滤）+ 详细报错
  protolab.go  proto-lab 实验位（A 阶段留空）
ui/                        前端（module ui）
  cmd/ailauncher/main.go   唯一入口：无参数→TUI；-i→直达；否则→cli 路由
  tui/cli_port.go   cli.Port 消费侧接线（fetchConfig/resolveAgent/loadState/saveState，参数 `p cli.Port`）
  tui/run.go / list_page.go / prompt.go / args_page.go / select_dir.go / screen.go / key.go / input.go / term_*.go
  tui/workspace.go / conversation.go   显式 workspace 的 session 控制器 + canonical 事件文本渲染
config.sample.json         v4 配置样例
```

**关键设计**：Config（只读种子）与 State（运行时）分离；stage 生命周期 + 运行时分级（A 阶段，构建期
剪包 B 挂账）；cli 纯路由不持有逻辑，脚本面与 TUI 共享同一端口；`cli.Port` 供应侧接口 + 数据模型别名，
ui 只 import cli，依赖单向 `ui → cli → core` 完全成立。

## 3. 已交付

| 项 | 状态 |
|---|---|
| 三层骨架（core/cli/ui 独立 module + go.work） | ✅ 依赖单向，任一模块独立 build |
| stage 生命周期 + 运行时分级（A 阶段） | ✅ 默认只暴露 stable；experimental 打包但需 `AILAUNCHER_EXPERIMENTAL=1` |
| 自注册单元 registry（命令/查询 + 中文 declared） | ✅ 无集中表，逻辑跟注册同文件 |
| 命令面全新（run/list/version/help/migrate/resolve/cap/config/state get\|put + dsh） | ✅ 退出码 0/1/2，未知命令返回详细信息 |
| 数据模型 + Extension 兼容槽 + state put/migrate 归一化 | ✅ `AgentState.normalize()` 单点，落盘无 null 数组 |
| 启动逻辑 gui/tui + Windows Job 逃逸（WMI/explorer） | ✅ 从 v3 移植，launchFn 可注入桩 |
| TUI 全部逻辑经 cli 端口（config/state/run/resolve） | ✅ session 持 `cli.Port` + `*cli.Config`，页测注入 stub CLI |
| 测试 | ✅ core + cli + ui 全量用例绿；build/vet/gofmt 全过；本轮补充 dsh help 无仓库配置、nil State 访问回归测试 |
| DeepSeek Harness 集成 | ✅ `dsh` 单元（web 后台服务 detached / headless 一次性任务）+ `run`/TUI 仓库服务型 agent（`extension.dsh.repo` 强制 cwd 仓库根、起 web 服务、动态端口提示）；真机实测 detached 起服务并监听 3080 |
| 实验性 Agent Workspace | ✅ 显式 `workspace <agent> [path]` + provider-neutral Session/SessionPort + dsh ACP JSON-RPC/stdio adapter + `sessions.jsonl` 本地 transcript；首版仅 committed text、取消、静态 permission policy，不替换 legacy run/TUI |
| 跨平台编译 | ✅ windows/linux/darwin/freebsd 三模块均过 |
| 文档 | ✅ CLAUDE.md + README + PROJECT_OVERVIEW + devlog v4 首篇 |
| 部署副本 | ✅ 已部署 v4 二进制到日常启动目录并做数据验证（目录由 `release.ps1` 的 `AILAUNCHER_DEPLOY_DIR` 指定） |

## 4. 未完成事项

### A. 必须真人完成（自动环境无 TTY，只能真机验证）

1. **Windows 真机 TUI 交互验收**：请在原生 Windows Terminal、PowerShell 或 cmd 运行 `ailauncher.exe`；Git Bash/某些 IDE 内置 Bash 的 stdin 不是 Windows 控制台，不能用于 raw TUI。重点看方向键/中文输入/ESC 取消/配色排版/Alacritty 是否正确带目录拉起；再试 `ailauncher.exe -i claude [path]` 交互直达，以及
   **选「DeepSeek Harness」拉起后台 web 服务 + 访问地址提示**。代码逻辑与渲染均有单测，真实终端行为只能人确认。
2. **Linux/macOS 真机 raw 模式验证**：目前只交叉编译保证编译通过，termios 行为没在真机跑过。

### B. 短期可做（随时可继续，自动环境能完成）

1. **实验性 TUI 迭代**：真机试用后按手感优化页面 UX（选工具布局、目录页、参数页），全在 ui 层，core/cli 不用动。
2. **proto-lab 首个实验单元**：把某个待定逻辑放 `cli/protolab.go` 试跑，稳定后晋升 core（改 stage + 挪文件）。
3. **新命令/查询单元**：在 core 建 `cmd_*.go` 自注册即可，cap/详细报错自动跟上。
4. **测试加固**：termios raw 模式各平台真机测试；更多 TUI 流程测试。

### C. 挂账远期（架构已预留，未实现）

1. **B 阶段：构建期按 stage 剪包**（`go:generate` 生成裁剪包）——A 阶段运行时分级跑稳后再叠加。
2. **常驻后台 daemon（类 CC-Switch）**：监听全局热键/hooks 唤起 GUI。架构预留：State 独立、CLI 端口
   接口、Recent/Favorites 字段。
3. **GUI 前端**：实现 `tui.CLI` 同款端口（或直接复用它），复用 cli 路由，ui 层新增。
4. **adapter 实际适配层**：`extension.adapter` 字段存在但无消费逻辑（`extension.dsh.repo` 是首个被消费的
   extension 键）。
5. **Recent/Favorites 接入 TUI**：AgentState 预留字段，UI 未用。
6. **macOS 发行**：代码支持（交叉编译过），无发布目标。

### D. 已知边界 / 设计取舍（有意为之或待解）

1. ~~ui/tui import core~~ ✅ **已解决**：`cli.Port` 供应侧接口 + 数据模型类型别名（`cli/port.go`），
   ui 只 import cli，依赖单向完全成立，ui 不再引用 core。
2. **`run` 的 path 语义**：首个非 KEY=V 的 token 是 path；不需要 path 时传 `""` 占位（TUI 内部
   `buildRunArgv` 保证，CLI 用户可显式传 `""`）。
3. **中文输入依赖控制台 UTF-8**：现代终端默认 OK；旧代码页（GBK）控制台可能乱码。
4. **参数页输入框按 `q` 取消**：Node 原版同款行为，为对齐保留。
5. **命令拼串不 shell-escape**：`set K=V && … && <exec> <args> <extra>` 若值含特殊字符可能有坑；工作目录走终端参数规避。
6. **配置查找**：BaseDir cwd 优先，找不到回退可执行文件所在目录。

## 5. 可扩展方向（挂钩点）

| 方向 | 挂钩点 | 现状 |
|---|---|---|
| 新 agent | `config.json` 的 `agents[]` 加一个对象 | 零代码，配置驱动 |
| 新命令/查询单元 | core 建 `cmd_*.go`，`init()` 自注册（stage + 中文 declared） | cap/详细报错自动跟上 |
| 新终端 | `terminal_*.go` 加一个 case（Alacritty/wt/wezterm 已有） | 模式已固化 |
| 新 TUI 页面 | 复用 `listPage`（`removedTitle:""` 即单栏）+ `prompt` + `runArgs` | 模块化，别另起渲染循环 |
| proto-lab 实验位 | `cli/protolab.go` 返回实验单元列表 | A 阶段留空，随时可填 |
| GUI 前端 | 实现 `cli.Port` 端口（或复用 `*cli.Router`），复用 cli 路由 | 接口已就绪 |
| daemon 复用 | 复用 cli 路由 + core 数据模型 | 已按复用设计 |
| B 阶段剪包 | stage 声明已齐，`go:generate` 挂账 | A 阶段跑稳后叠加 |

## 6. 维护速查

```bash
go build -o ailauncher.exe ./ui/cmd/ailauncher    # 构建单二进制
go test ./core/... ./cli/... ./ui/...             # 测试（零依赖，离线可跑）
go vet ./core/... ./cli/... ./ui/...              # 检查
.\ailauncher.exe                                   # 交互 TUI
.\ailauncher.exe -i <agent> [path]                 # 交互直达模式
.\ailauncher.exe run <agent> [path] [KEY=V|arg...] # 启动 agent
.\ailauncher.exe dsh [web|headless] [KEY=V|arg...] # DeepSeek Harness（web 后台服务 / headless 任务）
.\ailauncher.exe cap / list / resolve / state get  # 查询类（JSON）
GOOS=linux go build ./core/... ./cli/... ./ui/...  # 交叉编译（linux/darwin/freebsd）
```

- 改 agent 文案 → `config.json`；改启动语义 → `core/launch*.go`；改 TUI 页面 → `ui/tui/`。
- 新增逻辑单元 → `core/cmd_*.go` 自注册；实验逻辑 → `cli/protolab.go`。
- 平台差异一律 build tag 文件隔离；改 raw 模式先交叉编译各平台。
- 配置/状态文件：cwd 优先，回退 exe 目录；部署副本目录由 `release.ps1` 的 `AILAUNCHER_DEPLOY_DIR` 指定。
