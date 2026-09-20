# 进度日志 02 · 零依赖 TUI 前端完成 + 全平台交叉编译 + CLAUDE.md 落定

- **时间**：2026-08-10 02:20 – 03:10
- **会话**：ailauncher-v3-rewrite（后台 job abc440da）

## 做了什么

1. **TUI 地基（纯 stdlib）**：
   - `internal/tui/key.go`：按键解析——`readKey(*bufio.Reader)` 把单字节/多字节 UTF-8/转义序列映射为命名键（up/down/return/tab/escape/backspace/char…），含 CSI(`[A`)、SS3(`OA`)、带修饰(`[1;5C`)、`[3~` delete、`[5~`/`[6~` 翻页、Alt+字符。
   - `internal/tui/key_test.go`：18 组注入字节流用例，纯函数可单测，无需 TTY。
   - `internal/tui/screen.go`：ANSI 全屏重绘原语（`Screen.Clear/Line/Invert/Center/Raw`），常量集中在顶部。
   - raw 模式平台分支（build tag 隔离）：
     - `term_windows.go`：`kernel32` LazyDLL 的 `GetStdHandle/GetConsoleMode/SetConsoleMode`（stdlib syscall 只暴露 Get，Set 需 LazyDLL；仍零依赖）；关掉 `PROCESSED|LINE|ECHO`，开 `VIRTUAL_TERMINAL_INPUT/OUTPUT`；非控制台句柄报错 = 非交互 TTY 检测。
     - `term_linux.go`：termios `TCGETS/TCSETS`。
     - `term_bsd.go`：`TIOCGETA/TIOCSETA`（darwin/freebsd/netbsd/openbsd）。

2. **TUI 页面层**：
   - `internal/tui/list_page.go`：通用双栏列表页（忠实移植 Node `dualListPage`）——数字键跳转、`extraKeys` 钩子（N 新建/R 切根）、Tab 换栏、↑↓/k j、H/L 排序、d/u 软删除/移回、M 手动输入、Enter 选中、ESC/Q 取消；列表以指针传入，页面 splice 后写回 + `onChange` 持久化。
   - `internal/tui/prompt.go`：单行输入（raw 模式手动收集字符，与主循环一致；ESC 取消返回空串）。
   - `internal/tui/args_page.go`：参数页三区焦点（0=输入框/1=历史/2=已移除），回车即运行并去重置顶记历史，Tab 循环切换。
   - `internal/tui/select_dir.go`：目录选择三层页——播种（首次合并 agent+默认目录去重过滤写 state）→ 选根（候选根+`M` 手动+`Q` 跳过退化手动模式）→ 项目列表（重新扫描根下子目录，`N` 新建/`R` 切根）；nil save 防御。
   - `internal/tui/run.go`：`Run()` 主流程循环（选工具→选目录→输参数→启动），gui 短路；非 TTY 报「需要交互式终端」。
   - `cmd/ailauncher/main.go`：无参数 → TUI，有参数 → CLI（与 Node「无参数交互式 / 有参数直达」一致）。

3. **TUI 逻辑单测**：`internal/tui/pages_test.go` 11 例——用「注入字节流 + 丢弃输出的 Screen」驱动 `listPage`/`runArgs`/`selectDir`（数字跳转、↑↓、移除/移回、取消、手动输入、额外键、args 输入/历史/取消、目录播种与选根、nil save 不 panic）。共 13 例（含 key 测试）全过。

4. **交叉编译验证**（本机无法运行非 Windows，至少保证编译）：
   - 首次 darwin 编译失败：`syscall.TCGETS/TCSETS` 不存在 → 拆成 `term_linux.go`(TCGETS) 与 `term_bsd.go`(TIOCGETA)，linux/darwin/freebsd amd64 + darwin arm64 + windows 全部编译通过。

5. **冒烟测试**：重建 `ailauncher.exe`；`--version`→0.3.0-dev；`--list`→5 个 agent 的 JSON；`echo | ./ailauncher.exe`→「需要交互式终端」退出码 1。

6. **CLAUDE.md 落定**：修正过时的「bubbletea 再联网」描述为「零依赖 TUI 已完成」；补 TUI 前端架构表、TUI 页面流、修改约定（raw 模式手动收集、build tag、指针写回持久化等）。任务 #6 完成。

## 为什么

- 网络探测已确认不可达 → TUI 只能 stdlib；这也正合 Node 版零依赖哲学。
- 页面逻辑先做成**可注入字节流驱动**的形式，才能在没有 TTY 的本机把它测掉（而不是盲写）。
- 平台差异必须 build tag 隔离 + 交叉编译验证，否则 Linux/mac 分支是「写了但编译不过」的假完成。
- 与 Node 行为保持一字不差地移植（含 q 取消、gui 短路、播种/移除语义），降低用户迁移成本。

## 后果 / 影响

- **v3 现在可交互使用**：`./ailauncher` 就是完整 TUI（Windows 上已可跑，raw 模式+VT 已启用）；CLI 无参数时也进 TUI，有参数走 CLI。
- 测试 13 例全绿；`go build`/`go vet`/`go test ./...` 全过；四平台交叉编译通过。
- 已知边界（未阻塞）：Windows 旧代码页（GBK）下中文输入依赖控制台 UTF-8，现代终端默认无碍；GUI/TUI 的实际窗口拉起需真人验证（自动测试无法开窗）。
- CLAUDE.md 已成为 v3 真实架构文档，不再是「计划」。

## 下一步

- 真人交互验收 Windows TUI（选工具→选目录→输参数→启动 → Alacritty 里拉起 claude）。
- 可选：把直达模式也做成「跳过目录页但进参数页」的 TUI 变体（现 CLI 直达是纯非交互 launch，与 Node 交互直达略不同）。
- 迁移 config.json/data.json 到 v3 schema 的落地脚本（legacy 兼容已在）。
- 收尾：README、最终验收报告。

## 补充（03:10 复查发现并修复一个真 bug）

- **现象**：`readEscape` 对「单独 ESC」的判定是阻塞 `ReadRune` 等后续字节——用户按一次 ESC（取消键）无后续字节时会**永久挂起**，直到再按别的键（且会被误读成 Alt+字符）。
- **修复**：把输入从 `bufio.Reader` 换成 `keyReader`（goroutine 从 os.Stdin 读原始字节推通道 + 带超时的 `get(timeout)`）；ESC 后用 `select + time.After`（~100ms，与 Node readline 一致）判定单独 ESC vs 转义序列。多字节 UTF-8 用 `utf8.FullRune` 解码。
- **后果**：单测扩到 16 例（新增 `TestReadKeyBareEscape`、渲染内容断言 `TestListPageRenderContent`）；build/vet/test 全过，四平台交叉编译过。取消键 ESC 不再挂起。
