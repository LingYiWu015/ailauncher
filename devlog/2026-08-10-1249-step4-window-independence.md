# 进度日志 04 · 拉起的 agent 窗口独立于启动终端（2026-08-10 中午 12:49）

- **时间**：2026-08-10 12:49
- **会话**：ailauncher-v3-rewrite（后台 job abc440da）续
- **性质**：用户反馈「从 WezTerm 启动 ailauncher、弹出的 Alacritty 窗口会随 WezTerm 关标签一起被杀」→ 定位 + 修复。

## 现象

从**任意终端**启动 ailauncher → 选中 agent → 弹新 Alacritty 窗口。杀掉启动终端后，弹出的窗口**整窗消失**。用户环境是 **WezTerm**，关标签页/点 × 都会触发。

## 为什么（根因）

1. 先排除「控制台挂接」：实测 `Alacritty.exe` PE 头 **Subsystem=2（Windows GUI）**，GUI 进程本来就不挂接任何控制台；当前代码与旧 Node 版也都用了 `DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP`。控制台关闭的 `CTRL_CLOSE_EVENT` 杀不到它。
2. 真正的机制是 **kill-on-close Job Object**：WezTerm 给标签页进程树建了 Job（关标签要杀整棵树）。`DETACHED_PROCESS` 逃得出控制台，**逃不出 Job**——Alacritty 是 ailauncher 的后代，自动加入同一 Job，关标签时一起被杀。用户「没挂到 PID 1」的直觉对应 Windows 的「不在 Job / 不在进程树里」。

## 做了什么

三层回退（Windows 专属，`pkg/ailauncher`）：

1. **`procattr_windows.go`**：`detachProcAttr()` 增加 `CREATE_BREAKAWAY_FROM_JOB`（0x01000000），尝试让子进程逃出 Job；新增不带 BREAKAWAY 的 `detachProcAttrNoBreakaway()` 兜底。
2. **新增 `launch_windows.go`**（`//go:build windows`）：`buildCmdline`（复用 `syscall.EscapeArg` 拼命令行）→ `writeVBS`（临时 .vbs：wscript 隐藏窗口样式 0 运行，避免 cmd 闪烁）→ `launchWindowsFallback`（改由 **explorer.exe** 拉起）。explorer 是桌面 shell、不在 WezTerm 的 Job/进程树里，它拉起的 wscript→alacritty 继承到 Job 外，关标签杀不到。
3. **新增 `launch_other.go`**（`//go:build !windows`）：占位空实现，保证跨平台编译。
4. **`launch.go`**：tui / gui 两个路径接入「直拉(BREAKAWAY) → explorer 逃逸 → 无 BREAKAWAY 直拉兜底」。tui 的 env 已含在命令串里无需额外处理；gui 兜底不注入 env（当前 gui agent env 均为空，已知边界）。
5. **`launch_windows_test.go`**：`TestBuildCmdline`（转义）+ `TestWriteVBS`（引号翻倍）。

## 后果 / 验证

- 全平台构建通过：Windows / linux / darwin / freebsd；`go test ./...` 全绿（原 16 例 + 新 2 例）；`go vet`、`gofmt` 通过。
- `ailauncher.exe --version` → 0.3.0-dev、`--list` → JSON，正常。
- 分层语义：cmd / Windows Terminal 等无 kill Job 的终端 → 直拉照旧，无感；WezTerm → BREAKAWAY 被拒 → explorer 逃逸拉起；逃逸也失败 → 无 BREAKAWAY 兜底（至少能启动）。
- **真机待验收**（自动环境无法复现 WezTerm Job）：用户需在 WezTerm 里走一遍完整流程 + 关标签验证 Alacritty 窗口存活。

## 下一步

- 用户真机验收：WezTerm 里启动 → 关标签 → 确认 Alacritty 窗口和 agent 存活且可交互；cmd/Windows Terminal 各验一遍直拉无回归。
- 若 explorer 逃逸在某些环境不生效（explorer 委托行为不保证），备选方案：改走 **WMI `Win32_Process.Create`**（进程由 wmiprvse 创建，确定在 Job 外），代价是慢 ~0.5s。
