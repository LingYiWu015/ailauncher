# AILauncher v4

零依赖的 AI 编程工具启动器，并包含一个**实验性 Agent Workspace**：传统模式在单个终端里**选 AI 工具 → 选工作目录 → 传高级参数 → 拉起**；Workspace 则只消费 agent 的机器可读后端协议，由 AILauncher 自己渲染会话文本。架构拆为三个独立 Go 项目（core / cli / ui），依赖单向 `ui → cli → core`。

- **纯标准库、零外部依赖**：可离线编译；TUI 为手写 ANSI 全屏重绘，无 bubbletea/readline。
- **三层架构**：`core` 逻辑处理层（每条逻辑 = 自注册单元，带 stage/declared）、`cli` 纯路由端口
  （脚本面与 TUI 共享同一路由）、`ui` 前端（交互/体验优化全归本层）。
- **生命周期分级**：`draft → checking → experimental → stable → abandoned`；自研直进 experimental，
  晋升/回退只改单元声明里的 stage。

## 构建与运行

需要 Go 1.26+。工作区 `go.work`（use ./core ./cli ./ui），无第三方依赖，离线即可构建。

```bash
go build -o ailauncher.exe ./ui/cmd/ailauncher   # 构建单二进制
./ailauncher                                    # 交互式 TUI：主菜单（启动工具 / Agent 会话 / 历史会话）
./ailauncher -i <agent> [path]                  # 交互直达（跳过选工具页，走 legacy 启动流程）
./ailauncher chat <agent> [path]                # Claude Code 风格流式会话（workspace 为兼容别名）
./ailauncher history-replay <session-id>        # 只读回放一条本地 transcript
./ailauncher run <agent> [path] [KEY=V | arg...] # 启动 agent；path 为工作目录
./ailauncher dsh [web|headless] [KEY=V|arg...]   # DeepSeek Harness：web 起后台服务 / headless 跑一次性任务
./ailauncher list                               # 列出全部 agent（JSON，可被脚本解析）
./ailauncher resolve <name|display>             # 解析 agent（JSON）
./ailauncher cap                                # 能力清单（stage 过滤后，JSON）
./ailauncher config                             # 当前配置（JSON）
./ailauncher state get <agent>                  # agent 运行时状态（JSON）
./ailauncher state put <agent>                  # 写状态（JSON 自 stdin）
./ailauncher version / help [cmd] / migrate
```

验证：`go test ./core/... ./cli/... ./ui/...` + `go vet ./core/... ./cli/... ./ui/...`。
退出码：`0` 成功 / `1` 运行时错误 / `2` 未知命令·用法（未知命令返回详细信息：能力清单 + cap/help 提示）。
开发期启用实验单元：`AILAUNCHER_EXPERIMENTAL=1 ailauncher cap`。

## Agent Workspace（ACP 会话，用户面）

`chat` 是统一会话宿主（`workspace` 为兼容别名）；它不会嵌入或解析 agent 自己的 ANSI TUI。
当前实现 **ACP JSON-RPC/stdio** adapter（`opencode acp` 已真机验证：协议 v1，model/mode 下拉、
9 个可用命令、load/list/resume/close/fork 能力），以统一 session event 驱动 Claude Code 风格
TUI（备用屏 + 脏行 diff + 50ms 分批渲染，中英文双宽感知）：

```bash
ailauncher chat opencode C:\path\to\your-project
ailauncher history-replay <local-session-id>
ailauncher session list
ailauncher session get <local-session-id>
ailauncher session delete <local-session-id>   # 只删本地 transcript
ailauncher session probe opencode              # 就绪态自检（只握手不建会话）
```

- 主菜单显式分三项：启动工具（legacy 拉起）、Agent 会话（流式 chat）、历史会话（只读回放，`d` 可删本地记录）。
- 选 agent 页即时出现：摘要零 IO 先列，后台并行探测逐行刷新（opencode 约 1s，dsh 冷启动十几秒不等）；同一次 TUI 内结果缓存，进出不再重复握手。
- 进会话/读远端时转 spinner 反馈（可 ESC 取消/跳过），不再干 freeze。
- 会话渲染已完成块行缓存（只拼装不重算），思考块按 `t` 折叠；落盘走内存索引 Append O(1)，长会话不越跑越卡。
- `config.json` 已给 `opencode` 配好开箱 adapter（`opencode acp` + `permission: ask`），`dsh` 配好 `demo:acp`；需先 `opencode auth login`。新 agent 只要命令支持 ACP stdio，Core 内置推导自动配对，无需手写配置。
- `agent list` 看全部接入摘要（adapter 来源/命令是否安装），`agent doctor [name]` 做真握手体检，`agent enable/disable <name>` 控制 TUI 是否展示。
- 会话内：`Enter` 发送，`/` 进命令（`/help /mode /model /commands /usage /title /quit`，mode/model 用统一列表页选后端下发项）；权限请求挂起时 `数字/y/n/ESC` 直接回应；`Ctrl-C` 运行中取消（挂起权限按 cancelled 回应）。
- `permission` 支持 `allow` / `reject` / `ask`（默认 reject）：`ask` 走 TUI 交互确认，其余按静态策略自动回应并落可见事件。
- agent 可反向调用 `fs/read_text_file`、`fs/write_text_file`、`terminal/*`（本地执行，输出 1MB 截头留尾）、`elicitation/create`（TUI 确认，默认拒绝）。
- 进会话前可选恢复远端上下文（后端有 list/resume 能力才展示该页，否则直进新建）。
- `sessions.jsonl` 保存本地 transcript（消息、状态、错误、时间、标题、模式、用量），**不保存** adapter env、密钥或后端原始帧；标题/用量/模式同步到记录头，列表页直接可读。
- 旧 `run`、`-i` 和启动工具流程保持可用；CLI（`session/*`、`chat` 参数）是开发者排障面，用户面全在 TUI。


本地集成 DeepSeek 官方 agent harness（`~/.dsh` 存数据，Web UI 默认在 `127.0.0.1:3080`，无 TUI）。

```bash
ailauncher dsh [web] [KEY=V|arg...]     # 起后台 web 服务（detached，默认浏览器打开 3080；--port N 可改端口）
ailauncher dsh headless "任务" [KEY=V]  # 前台跑一次性任务，阻塞到出答案
ailauncher dsh help                     # 用法
```

- 仓库根必须可指出：`config.json` 的 `extension.dsh.repo`（或环境变量 `DSH_REPO`），cwd 强制在仓库根（`pnpm dsh …`）。`dsh help` 只查看用法，不要求仓库已配置或存在。
- `KEY=V` 注入环境变量，如 `DEEPSEEK_API_KEY=xxx`、`DEEPSEEK_BASE_URL=...`（省略即走官方）。
- web 是 detached 后台服务：ailauncher 立即退出，进程独立存活，首次 ready 约需 20~25 秒（tsx 编译）。
- **TUI 集成**：`type: "gui"` + `extension.dsh.repo` 的 dsh agent 会出现在选工具列表，选中即
  detached 起 web 服务并展示访问地址（`run dsh` 同样生效；`--port N` 时提示同步改）。

## 数据文件

| 文件 | 角色 |
|---|---|
| `config.json` | 静态配置（只读种子）：`terminal`（拉起 TUI 工具的终端）、`defaultDirectories`、`agents[]` |
| `data.json` | 运行时状态，程序自动读写：按 agent 分 key，每 agent `{ directories, removedDirs, commands, removedCmds, seeded, rootDir, ... }` |

配置/状态文件默认在当前工作目录查找；若 cwd 没有则回退到可执行文件所在目录。v4 移除旧 `tools[]`
兼容读取（`migrate` 遇旧格式会报错提示按 `config.sample.json` 手工迁移到 `agents[]`）。

> **state 播种说明**：首次进入某个 agent 的 TUI（选目录页）时，会把该 agent 的 `directories[]` 与
> 配置 `defaultDirectories` 合并、过滤已存在项后写入 `data.json`（此后 `data.json` 是唯一事实来源）。

### agent 字段

```jsonc
{
  "name": "claude",            // 唯一名（CLI 匹配用）
  "display": "claude (Claude Code)", // 列表显示
  "desc": "Anthropic Claude Code CLI",
  "exec": "claude",            // 可执行文件
  "type": "tui",               // "tui" 套终端拉起 | "gui" 直接 detached 拉起
  "args": "",                  // 固定附加参数
  "env": { "ANTHROPIC_MODEL": "opus" }, // 固定环境变量
  "extension": { "adapter": { "kind": "x" } }, // 通用向前兼容槽
  // 仓库服务型（dsh 等）：key 放 agent.extension.dsh.repo，cwd 强制仓库根、起后台 web 服务
  "rootDir": "D:\\Coding\\Projects\\Claude_Projects",
  "directories": [ "...", "..." ]  // 首次播种用；之后 data.json 是唯一事实来源
}
```

## 交互（TUI，全页统一，见 `ui/tui/keymap.go`）

- **列表**：`↑↓`/`jk` 移动、`1-9` 跳转、`Home/End/PgUp/Dn` 翻页、`Enter/空格` 确认、`ESC`/`Ctrl-C`/`q` 返回；双栏再加 `Tab` 切栏、`d` 移除/`u` 移回、`H/L` 排序、`M` 手动输入。超长列表做窗口分页（`↑ 还有 N 项`）。
- **输入框**（单行 prompt / 参数输入 / 会话输入同一手感）：`←→`/`Home/End`/`Ctrl-A/E` 移动、`Backspace/Delete` 删除、`Ctrl-K` 删到行尾、`Ctrl-U` 清空、`Ctrl-W` 删词、`Enter` 确认、`ESC`/`Ctrl-C` 取消；`q` 与数字照常落字。
- **主菜单**：`↑↓`/`jk` 选择、`1-3` 快选直接进入、`Enter` 进入、`ESC`/`q` 退出。
- **参数页**：输入框 + 历史 + 已移除三区，`Tab` 循环；输入框内 `↑↓` 按 shell 习惯召回历史。
- **会话页**：焦点恒在输入框，`↑↓` 回忆历史，`PgUp/Dn` 滚动，`Ctrl-D`（空行）退出；`ESC`/`Ctrl-C` 运行中取消、空闲时退出。
- **回放页**：只读，`↑↓`/`jk`/`空格` 滚动，`g` 回顶部，`ESC`/`q` 退出。
- **选目录**：三层——①首次播种（合并 agent + 默认目录，去重过滤存在）→ ②选根（候选根 = agent
  rootDir + 历史目录父目录 + 默认目录；`M` 手动 / `Q` 跳过退化手动模式）→ ③项目列表（每次重新扫描
  根下子目录；`N` 新建 / `R` 切根）。
- 需要交互式 TTY；Windows 请在原生 Windows Terminal、PowerShell 或 cmd 中运行 TUI，Git Bash/某些 IDE 内置 Bash 的 stdin 不是 Windows 控制台，无法进入 raw 模式。非 TTY 直接报「需要交互式终端」退出；这些环境仍可使用 `run`、`list`、`cap` 等 CLI 子命令。
- **交互直达（`-i <agent> [path]`）**：跳过选工具页直接进该 agent 流程；给了 `path`（已存在目录）
  则连目录页也跳过，直接进参数页。启动后循环回参数页直到取消。`gui` 型 agent 直接拉起。

## 启动语义

- **gui**：`exec.Command(...).Start()` detached，不套终端；env 合并 `agent.Env` + 运行时 `KEY=V`。
  `extension.dsh.repo`（仓库服务型）cwd 强制仓库根，忽略传入 workdir。
- **tui**：拼 `set K=V && … && <exec> <args> <extra>` 命令串，用 `config.terminal` 拉起。启动参数按
  终端类型分发（Alacritty `--working-directory` / wt `-d` / wezterm `start --cwd`，未知终端按 Alacritty
  兜底），**换终端改 `config.json` 的 `terminal` 即可，无需改代码**。
- **窗口独立于启动终端**：Windows 下直拉带 `CREATE_BREAKAWAY_FROM_JOB` 逃出终端的 kill-on-close Job；
  Job 拒绝时自动改经 **WMI `Win32_Process.Create`** 拉起（进程由系统服务 WmiPrvSE 创建，确定在 Job 外），
  explorer 逃逸作第二选择。**从任意终端启动、杀掉启动终端，弹出的 agent 窗口不受影响。**
- 参数里的 `KEY=V` 被解析为环境变量（仅对 TUI 生效），其余为附加参数。`run` 的 path 是首个非 KEY=V 的 token。

## 平台

Windows 10/11（主目标，已实测构建）与 Linux（交叉编译验证）为发布目标；macOS 代码支持不发布；
FreeBSD 顺带支持。平台差异用 build tag 文件隔离（`term_windows/linux/bsd.go`、`procattr_*.go`）。

## 目录结构

```
core/                逻辑处理层（module core）：registry + stage + 数据模型 + 平台服务 + 命令/查询单元
cli/                 纯路由端口（module cli）：Router.Dispatch/Format/ExitCode + proto-lab 实验位
ui/                  前端（module ui）：cmd/ailauncher（唯一入口）+ tui/（页面 + cli 端口）
go.work              工作区：use ./core ./cli ./ui
config.sample.json   v4 配置样例
PROJECT_OVERVIEW.md  项目总览（含未完成事项/可扩展方向）
devlog/              开发存档（每步：做了什么/为什么/后果）
```

详细架构与修改约定见 `CLAUDE.md`。
