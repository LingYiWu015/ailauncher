# 进度日志 07 · 终端适配层 + 测试加固（2026-08-10 下午 1:40）

- **时间**：2026-08-10 13:40
- **会话**：ailauncher-v3-rewrite（后台 job abc440da）续
- **性质**：承接 step6，本轮完成 PROJECT_OVERVIEW「B. 短期可做」第 3、4 条：**终端适配层** + **GUI/WMI 启动冒烟测试**。

## 一、终端适配层（B3）

### 问题
`launch.go` 的 tui 路径硬编码 Alacritty：`alacritty --working-directory <wd> -e cmd /k <cmd>`。换终端要改代码；且代码里 `runtime.GOOS` 内联判断拼 argv，不可单测。

### 做了什么
1. **新增 `terminal_windows.go`**（`//go:build windows`）：`terminalArgv(terminal, wd, cmdline)` 按终端 basename（小写）分发：
   - Alacritty：`--working-directory <wd> -e cmd /k <cmd>`（默认，与历史字节一致）
   - Windows Terminal（wt.exe / wt）：`-d <wd> cmd /k <cmd>`
   - WezTerm：`start --cwd <wd> -- cmd /k <cmd>`
   - 未知终端：Alacritty 兜底
2. **新增 `terminal_other.go`**（`//go:build !windows`）：Unix 版，Alacritty 约定（`--working-directory` + `sh -lc`），跨平台编译占位。
3. **`launch.go`**：tui 路径删掉 `runtime.GOOS` 拼 argv 的内联分支，改为 `argv := terminalArgv(terminal, wd, b.String())`。env 拼串（`set K=V &&` vs `K=V`）保留原逻辑不变。
4. **新增 `terminal_windows_test.go`**：4 个分支断言（Alacritty 带空格路径字节一致 / wt / wezterm 大小写不敏感 / 未知终端兜底）。

### 后果
- Alacritty 输出与迁移前**逐字节一致**（单测钉死），现有流程零回归。
- 换终端从此改 `config.json` 的 `terminal` 即可，无需动代码；新终端在 `terminal_windows.go` 加一个 case。
- 与窗口独立性修复正交：WezTerm 分支是为将来 `config.terminal` 指向 wezterm 时准备。

## 二、测试加固（B4）

1. **`launch_windows_smoke_test.go`**（`//go:build windows`，真实拉起进程、无需 TTY）：
   - `TestWmiSmoke`（上轮遗留，本轮转正）：真实跑 `writeWmiVBS` + wscript + WMI Create，验证 WmiPrvSE 真能创建进程、wscript 退出码为 0。**这是 devlog 05 抓到 VBS 括号语法错误的那个测试**，留作回归防线。
   - 新增 `TestLaunchGuiDetached`：走 `Launch(gui)` 完整链路（无害 `cmd /c echo > marker`），验证 detached 启动真能拉起进程（直拉或 WMI 兜底任一成功即可），轮询等 marker。

### 后果
- GUI 启动从「无自动化测试」→ 有 Windows 冒烟覆盖。
- 全量：30 单测（pkg 15 + tui 15，含 2 Windows 冒烟），`go vet`、`gofmt`、Windows/linux/darwin/freebsd 交叉编译全绿。

## 下一步

- 剩余 B 项：**TUI 交互直达模式**（B2，需设计，暂缓）、**state 播种文档说明**（B5，小事）。
- 真机仍待：TUI 交互验收、termios 真机、Windows Terminal/WezTerm 终端分发真机验证。
- 窗口独立性：搁置中，见 devlog 06「下一步」。
