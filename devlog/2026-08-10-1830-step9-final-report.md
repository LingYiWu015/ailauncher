# 进度日志 09 · 本轮最终报告：B 项全部完成，v3 功能面收口（2026-08-10 下午 6:30）

- **时间**：2026-08-10 18:30
- **会话**：ailauncher-v3-rewrite（后台 job abc440da）续
- **性质**：承接 step8，PROJECT_OVERVIEW「B. 短期可做」5 条全部清完。本报告供用户回来查看「做到哪、剩什么、下一步」，结构对齐 step3。

## 一、本轮（step4→step8）做了什么

| 篇 | 交付 |
|---|---|
| 04 | 窗口独立：直拉加 CREATE_BREAKAWAY_FROM_JOB，逃出 kill-on-close Job |
| 05 | 换确定性 WMI Win32_Process.Create 逃逸（explorer 真机不可靠）；抓出 VBS 括号语法坑 |
| 06 | `--migrate`：config tools[]→agents[] + data.json null 归一化，已应用到真实文件（有备份） |
| 07 | 终端适配层（Alacritty/wt/wezterm 分发，Alacritty 逐字节不变）+ GUI/WMI 冒烟测试 |
| 08 | `-i` 交互直达模式（跳过选工具/目录页，进参数页，stay 循环）+ runTuiAgent 复用抽取 + launch 注入单测 + state 播种文档 |

## 二、B 项（短期可做）结算

| # | 项 | 状态 |
|---|---|---|
| B1 | config/data 迁移 v3 | ✅ `--migrate` 幂等可逆，已应用 |
| B2 | TUI 交互直达模式 | ✅ `-i <agent> [path]` |
| B3 | 终端适配层 | ✅ `config.terminal` 分发 |
| B4 | 测试加固 | ⏳ GUI/WMI 冒烟已加；**termios raw 模式各平台仍待真机**（唯一剩项） |
| B5 | state 播种文档 | ✅ README「数据文件」节说明 |

## 三、v3 现状总览

- **功能面已齐**：TUI（完整 + 交互直达）、CLI（--list/--version/--migrate/launch/直达）、终端适配、配置迁移、分层启动（gui/tui + WMI 逃逸）。
- **测试 36**（pkg 15 + tui 21，含 2 Windows 冒烟），fmt/vet/build + linux/darwin(amd64/arm64)/freebsd 交叉编译全绿。
- **数据文件**：`config.json` 已是 v3 `agents[]`（5 agent）、`data.json` 无 null 残留；备份 `config.json.bak-v3`/`data.json.bak-v3` 在，确认无碍后可删。
- **零外部依赖**保持：纯 stdlib，离线可编译。

## 四、真机验收清单（需真人，自动环境无 TTY）

1. **TUI 交互**：`.\ailauncher.exe` 走完整流程——方向键/中文输入/ESC 取消即时性/配色排版/Alacritty 带目录拉起。
2. **`-i` 交互直达**：`.\ailauncher.exe -i claude`（进参数页）与 `-i claude D:\Coding\Projects\Claude_Projects`（跳过目录页），启动后循环回参数页、`Q` 退出。
3. **终端分发**：`config.json` 改 `terminal` 为 wt/wezterm 后拉起正常。
4. **termios**：Linux/macOS raw 模式真机（目前仅交叉编译保证编译过）。
5. 窗口独立性：**搁置**（WMI 逃逸已实现但用户真机两次验收仍随 WezTerm 关标签被杀；重启方向见六）。

## 五、踩坑记录（本轮新增）

1. **explorer 逃逸不可靠**（devlog 05）：ShellExecute 委托给 explorer 不保证在 Job 外，真机失效 → 换 WMI `Win32_Process.Create`（WmiPrvSE 系统服务创建，确定性在 Job 外）。
2. **VBS 括号语法**（devlog 05）：`rc = obj.Method a, b` 赋返回值时必须加括号 `.Create(a, b, ...)`，否则 wscript「语句未结束」。**字符串单测测不出来，必须真跑**。
3. **`State.Get` nil map panic**（step8）：`State{}` 的零值 map 上 `Get` 会赋值 panic，测试须用非 nil 空 map `State{}`。

## 六、下一步（留给真人/挂账）

- **自动环境无可推进项**：B 项清完；C（daemon/GUI/adapter/Recent+Favorites/macOS 发行）挂账远期，需设计定夺；A 需真机。
- **用户回来优先做**：①真机走一遍完整 TUI + `-i` 验收；②删不删 `.bak-v3` 备份；③决定窗口独立性问题要不要重启（备选：深挖 WezTerm Job 关联机制 / schtasks / COM 自建进程）。
- 常用命令：`go build -o ailauncher.exe ./cmd/ailauncher`、`go test ./...`、`.\ailauncher.exe -i claude`、`.\ailauncher.exe --list`。
