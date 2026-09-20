# 进度日志 08 · TUI 交互直达模式 + state 播种文档（2026-08-10 下午 6:23）

- **时间**：2026-08-10 18:23
- **会话**：ailauncher-v3-rewrite（后台 job abc440da）续
- **性质**：承接 step7，本轮完成 PROJECT_OVERVIEW「B. 短期可做」第 2、5 条：**TUI 交互直达模式** + **state 播种文档说明**。B 项至此全部清完。

## 背景

- B2（交互直达）：`ailauncher <agent> [path]` 一直走 CLI 非交互 launch（直接拉起）；Node 原版会进参数页。计划加「跳过选工具页（可选跳过目录页）但进参数页」的交互直达变体。
- B5（播种文档）：data.json 只有 claude/opencode，codex/reasonix/cc-switch 首次进 TUI 会自动播种，不算 bug，但没人说明。

## 设计决策（实现前想清楚的三点）

1. **`-i` 的参数面**：`ailauncher -i <agent> [path]`，path 恒为第 2 个 token（与 CLI 直达 `<agent> [path]` 一致），不解析 KEY=VALUE/附加参数——要传参去参数页里敲。避免「path 还是参数」的歧义启发式，保持接口可预期。**做不到的**：一条命令同时给 path 又预填参数（Node 版也不支持）。
2. **复用一个 runTuiAgent**：完整模式与直达模式的「选目录→输参数→启动」是同一段逻辑，抽 `runTuiAgent(agent, preWorkdir, stay)`：preWorkdir 非空跳过目录页、stay=true 启动后循环回参数页（直达）vs false 启动一次即回外层（完整）。`Run()` 与 `RunDirect()` 共用，`Run()` 行为逐字节不变。
3. **可单测性**：`runTuiAgent` 会真实拉起进程（`ailauncher.Launch`），测试没法跑真 TTY。给 `session` 加 `launch` 注入字段（默认 `ailauncher.Launch`），测试桩掉它，就能驱动「跳过目录页」「stay 循环参数页」「目录/参数页取消」这些流程断言。与既有 `newBytesKeyReader`/丢弃输出 Screen 的注入风格一致。

## 做了什么

1. **`internal/tui/run.go` 重构**：
   - `session` 新增 `launch` 字段（注入点）。
   - 抽 `newSession() (*session, cleanup, error)`：raw 模式 + 配置/状态加载 + 光标/清屏，`Run`/`RunDirect` 共用。
   - 抽 `session.save()`（原 `Run` 里的闭包）。
   - 新增 `runTuiAgent(agent, preWorkdir, stay)`。
   - 新增 `RunDirect(agentName, path)`：`ResolveAgent` 解析（name 或 display）、path 非空做 `dirExists` 校验、gui 型直接拉起、tui 型走 `runTuiAgent(agent, path, true)`。
2. **`cmd/ailauncher/main.go`**：拦截 `-i`/`--interactive`（参数不足/多余报用法），调 `tui.RunDirect`。
3. **`pkg/ailauncher/cli.go`**：usage 补 `-i` 行，顺手把过时的「TUI/GUI 后续接入」改成「CLI 与 TUI 都是 pkg/ailauncher 的前端」。
4. **新增 `internal/tui/direct_test.go`**（6 例）：launch 桩驱动 runTuiAgent——
   - preWorkdir 非空跳过目录页直接用；
   - 无 preWorkdir 走完整目录选择（选根→项目列表→参数页）；
   - stay=true 启动两次后 q 取消退出；
   - 参数页 q 取消不启动；
   - 目录页两段取消（选根 q→手动列表 q）不启动；
   - preWorkdir 不存在时 runTuiAgent 不校验、照传（校验在 RunDirect，职责分层）。
5. **文档**：README（`-i` 用法 + TUI 节直达说明 + 数据文件节播种说明）、PROJECT_OVERVIEW（架构树/已交付/测试数 36/B2、B5 勾销/维护速查）、CLAUDE.md（运行节/测试行/run.go 行/主流程段）同步更新。

## 后果 / 验证

- 完整模式 `Run()` 行为不变：gui 短路、选工具循环、取消退出均保持原样，只是换用 `runTuiAgent(agent,"",false)` 内部走同一路径。
- **36 单测**（pkg 15 + tui 21，含 6 新）+ `go vet`/`gofmt`/windows+linux+darwin(amd64/arm64)+freebsd 交叉编译全绿。
- 非 TTY 冒烟：`ailauncher.exe -i` → 用法提示 exit 1；`ailauncher.exe --interactive nosuchagent` → 「需要交互式终端」exit 1（先于 agent 解析，与 `Run()` 一致，非 TTY 本来也进不了 TUI）。无 panic。
- **已知边界**：`-i` 传参只能在参数页手敲，不支持命令行预填；直达模式在参数页取消即整体退出（不会退回选工具页——本模式语义就是「直接进，取消走」）。

## 下一步

- B 项（短期可做）已全部完成 ✅。C 挂账远期（daemon/GUI/adapter/Recent+Favorites/macOS 发行）需设计或需真人，不在自动环境推进。
- 真机待办不变：TUI 交互验收（含新 `-i`）、termios 真机、Windows Terminal/WezTerm 终端分发真机验证。
- 窗口独立性：仍搁置（WMI 逃逸已实现，用户真机两次验收仍死，方案见 devlog 04/05/06）。
