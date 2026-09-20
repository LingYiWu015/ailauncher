# 进度日志 01 · 上下文恢复 + 记忆固化 + Go v3 骨架 + CLI 核心 + 设置

- **时间**：2026-08-10 01:40 – 02:20
- **会话**：ailauncher-v3-rewrite（后台 job abc440da）

## 做了什么

1. **排查"记忆丢失"**：用户担心持久记忆被清空。核查 `memory/` 目录 + 全部会话日志后确认——该目录**从未写过任何记忆文件**（无 `/remember`、无 Write 到 memory），因此没有"清空已有记忆"这回事。
2. **从旧会话恢复方案**：上次主会话 `0923a095`（476KB）完整无损，从中恢复已敲定决定：把 Node 版重写为 **Go v3 中间层**（不是微调结构）。
3. **固化记忆**：写入 `memory/ailauncher-v3-rewrite.md`（项目方向）、`memory/ail-work-preferences.md`（协作偏好）、`memory/MEMORY.md`（索引）；CLAUDE.md 顶部加提示。以后 `/clear` 也丢不了。
4. **搭 Go v3 骨架 + CLI 核心**：`cmd/ailauncher/main.go` 薄入口 + `pkg/ailauncher/` 核心库（`agent.go` Agent 含 adapter 缓冲、`config.go` Config 读写 + legacy `tools[]` 兼容 + ResolveAgent、`state.go` State/AgentState、`launch.go` gui/tui 启动 + ParseArgs + 平台分支、`procattr_*.go` detached、`cli.go` --list/--version/launch/直达）。
5. **测试与产物**：4 组单测全过；`go build`/`go vet` 通过；`ailauncher.exe` 构建成功；`--list` 从旧 config.json 正确列出 5 个 agent；未知 agent 退出码 1。
6. **会话重命名**为 `ailauncher-v3-rewrite`；**CLAUDE.md 重写**为 v3 文档。
7. **权限一次性申请**：`.claude/settings.local.json` 加入 15 条 allow 规则（`go`/`node`/`npm`/`npx` 统配 + 具体命令），JSON 校验通过，通宵无需逐个确认。
8. **网络探测**：`go get bubbletea/bubbles/lipgloss` 失败——`proxy.golang.org` DNS 解析失败，**外网不可达**。

## 为什么

- 用户丢失了当前会话上下文，需要找回；且用户要求"早上一把做完"，夜间自动开发需要记忆 + 权限双保障。
- 恢复出的既有决定就是 Go v3 方向，无需再议，直接续工。
- 用户要求：每步写清"做了什么/为什么/后果"存档，早起查看。

## 后果 / 影响

- 仓库现状 = **旧 Node 版（可用、顶着用）** + **Go v3 骨架（CLI 核心可用）**。
- **外网不可达 → TUI 只能纯 stdlib 实现**（与 Node 版零依赖哲学一致；bubbletea 方案作废）。
- 通宵续工 cron 已挂（job 1e6dde88，每 20 分钟自动唤醒）。
- 权限已配齐，后续命令不再弹窗。

## 下一步

- 实现**零依赖 TUI**：终端 raw 模式（Windows/Unix 平台分支）+ 按键解析 + 双栏列表页（选工具/选目录）+ 参数页 + 主流程；再接入目录播种与 State 流程；补测试；linux/mac 平台分支。
