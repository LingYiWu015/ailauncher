# step16 · Agent Workspace MVP（ACP 后端兼容层 + 自渲染会话）

> 2026-08-26。AILauncher 从纯“拉起外部 Agent”开始演进为“宿主自己渲染会话、Agent 只提供后端能力”的双路径产品。

## 做了什么

### 1. 选择机器协议，不抓取终端画面

- 审阅本地 `deepseek-harness` 的 `@deepseek-ai/dsh-acp` README、SDK 类型和集成测试。
- 确认 ACP 是 newline-delimited JSON-RPC/stdio：`initialize` → `session/new`（绝对 cwd）→ `session/prompt`；输出是 `session/update` 内的 committed `agent_message_chunk`。
- 确认 dsh ACP 的边界：fresh session、每 session 单个 in-flight prompt、取消和一次性 permission request；没有远端 resume、工具/思考/Diff/Markdown token 流。
- 因此没有尝试嵌入或解析 Agent 自己的 ANSI TUI，也没有用 dsh 的 `stream-json` 测试设施作为产品协议。

### 2. core：统一会话层与 ACP adapter

新增：

- `core/session.go`：provider-neutral `Session`、`SessionRequest`、状态与 canonical 事件（user text / assistant text / status / error）。
- `core/adapter.go`：从 `agent.extension.adapter` 解码配置，adapter registry；缺失、无效或未知 kind 给清晰错误，绝不静默回退。
- `core/adapter_acp.go`：零依赖 ACP JSON-RPC stdio 客户端，按真实 dsh 协议处理 initialize/new/prompt/update/cancel/permission/EOF；子进程不是 detached legacy window，关闭时由 Workspace 管理。
- `core/session_store.go`：BaseDir 下 `sessions.jsonl`。事件每行追加、进程内串行化、末尾崩溃半行忽略、中间损坏报错；不保存 env、API key、原始协议帧。
- `core/session_manager.go`：连接 live adapter 与 transcript，确保用户输入及 backend event 落盘。
- `core/cmd_session.go`：`session list` / `session get <id>` JSON 查询。

保留不变：`launch*.go`、`cmd_run.go`、窗口 escape/WMI fallback、无参数 TUI、`-i`。它们仍是正式 legacy 路径。

### 3. cli：可选实时会话端口

- 现有 `cli.Port{Dispatch}` 原样保留；老 TUI 和测试桩不需要改接口。
- 增加 `cli.SessionPort` 和 session 类型别名，`*cli.Router` 通过 `SessionManager` 提供 Open/List/Get。
- `SetBaseDir()` 同步 session store，因此测试和不同配置目录不会共享 transcript。

### 4. ui：独立 Workspace 页面

新增：

- `ui/tui/conversation.go`：纯 conversation model/reducer/render，安全文本折行、assistant chunk 合并、draft、滚动、状态和错误。
- `ui/tui/workspace.go`：只通过 `cli.SessionPort` 打开会话和消费事件；不 import core、不碰 exec、不解析 JSON-RPC。
- 新入口：`ailauncher workspace <agent> [path]`。这是显式 opt-in，不改无参数入口。
- 键位：Enter 发送；busy 时拒绝重复提交；Ctrl-C/ESC 取消运行中的任务、idle 时退出；↑↓/Page 滚动。

### 5. 配置、文档和测试

- `config.sample.json` 为 dsh 添加完整 `extension.adapter` ACP 示例：`pnpm --dir <repo> run demo:acp`、cwd、空 env 和默认 reject permission。
- README、CLAUDE、PROJECT_OVERVIEW 说明 Workspace 与 legacy 双路径、adapter contract、`sessions.jsonl`、安全边界及 MVP 限制。
- 核心新增 adapter/store/session command 测试；UI 新增 conversation/reducer/key 行为测试；既有 launcher 测试仍完整运行。
- 审查后补强：ACP string/number JSON-RPC request ID、一过性 allow_once/reject_once 权限选择、caller cancel 同步远端 cancel、stderr 持续 drain、协议 EOF/超限终止 pending RPC、正常子进程退出终态、同一进程多 store 共享锁、重复 session ID 拒绝和 manager event channel close。

## 为什么

用户需要 AILauncher 包进 Agent 的渲染，只把 Agent 当作后端。ANSI screen-scraping 会重新引入 Agent 各自的 raw TUI、光标重绘、PTY、窗口生命周期和终端尺寸问题；ACP 提供语义事件，因此是首个垂直切片最安全的入口。

同时，现有启动器已经有稳定用户路径。Workspace 在 ACP 真机验收前必须 opt-in，绝不能让无 adapter 配置的 Agent 被悄悄换成另一种启动语义。

## 影响与已知限制

- Workspace 当前只渲染 committed assistant text，不是 token 级流式输出。
- 静态 `permission: "allow"|"reject"` 只用于 ACP 一次性 permission request；没有人工确认 UI。
- `sessions.jsonl` 是本地 transcript，不等于后端 session resume token；进程退出后只能查看历史，不能恢复远端上下文。
- Claude Code、Codex、OpenCode adapter、工具卡片、reasoning、Diff、Markdown、附件、PTY fallback 与 GUI 都尚未开始。
- 自动环境未启动真实 dsh ACP/web/headless，也没有读取或记录任何 key；真实 ACP 需要用户在自己的 dsh 配置与原生 TTY 环境验收。

## 验证

完成：

```text
gofmt -w core cli ui                         PASS
go test ./core/... ./cli/... ./ui/...       PASS
go vet ./core/... ./cli/... ./ui/...        PASS
go build ./core/... ./cli/... ./ui/...      PASS
GOOS=windows/linux/darwin/freebsd build      PASS
```

## 下一步

1. 在原生 Windows Terminal 用已配置 dsh ACP 的 agent 执行 `ailauncher workspace dsh <project>`，验收中文输入、cancel、EOF 和进程清理。
2. 为 ACP 增加 fixture 子进程测试，覆盖精确 wire ordering、permission response 和 EOF。
3. 在正式机器协议基础上增加 Claude Code/Codex/OpenCode adapter；每个只输出 canonical events，不把 provider 类型泄漏到 UI。
4. 扩充 block renderer（工具调用、权限、Diff、Markdown），等协议能力足够后再讨论默认入口迁移。
