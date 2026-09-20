# 开发进度日志（夜间自动重构存档）

本目录记录 2026-08-10 夜间 AILauncher v3 重构每一步的详细存档。**给用户早起查看用。**

每篇日志包含：
- **时间**：什么时间做的
- **做了什么**：具体动作（文件、命令、改动）
- **为什么**：当时的判断依据
- **后果/影响**：结果、验证情况、留下什么
- **下一步**：接下来计划

## 索引

| 篇 | 时间 | 主题 |
|---|---|---|
| [01](2026-08-10-0200-step1-context-recovery-cli-core.md) | 2026-08-10 01:40–02:20 | 上下文恢复 + 记忆固化 + Go v3 骨架 + CLI 核心 + 会话/文档/权限设置 |
| [02](2026-08-10-0300-step2-zero-dep-tui.md) | 2026-08-10 02:20–03:10 | 零依赖 TUI 前端完成（raw 模式/按键解析/三页面/主流程）+ 13 单测 + 四平台交叉编译 + CLAUDE.md 落定 |
| [03](2026-08-10-0330-step3-final-report.md) | 2026-08-10 03:30 | **最终报告**：v3 主体完工总览 + 真机验收清单 + 踩坑记录 + 下一步 |
| [04](2026-08-10-1249-step4-window-independence.md) | 2026-08-10 12:49 | 拉起的 agent 窗口独立于启动终端（WezTerm kill-on-close Job → BREAKAWAY + explorer 逃逸三层回退） |
| [05](2026-08-10-1320-step5-wmi-escape.md) | 2026-08-10 13:20 | 真机反馈 explorer 逃逸不生效 → 换确定性 WMI Win32_Process.Create（WmiPrvSE 创建进程）；VBS 括号语法坑 |
| [06](2026-08-10-1334-step6-migrate.md) | 2026-08-10 13:34 | `--migrate`：config tools[]→agents[] + data.json null 归一化，已应用到真实文件（有备份）；窗口独立性问题搁置 |
| [07](2026-08-10-1340-step7-terminal-adapter-tests.md) | 2026-08-10 13:40 | 终端适配层（Alacritty/wt/wezterm 分发）+ GUI/WMI 启动冒烟测试 |
| [08](2026-08-10-1823-step8-direct-mode.md) | 2026-08-10 18:23 | `-i` 交互直达模式（跳过选工具/目录页，进参数页，stay 循环）+ runTuiAgent 抽取 + launch 注入单测 + state 播种文档 |
| [09](2026-08-10-1830-step9-final-report.md) | 2026-08-10 18:30 | **最终报告**：B 项全部完成（5/5），v3 功能面收口总览 + 真机验收清单 + 本轮踩坑 + 下一步 |
| [10](2026-08-10-1855-step10-deploy.md) | 2026-08-10 18:55 | 部署 v3 到日常启动目录 `D:\Coding\Scripts\ai-launcher`（路径纠正）+ 就地迁移 + 归档旧 Node 脚本 |
| [11](2026-08-12-1700-v4-skeleton.md) | 2026-08-12 17:00 | v4 骨架重构：core/cli/ui 三层独立项目 + go.work | 依赖单向 + stage 生命周期 + 全新命令面 + TUI 全逻辑经 cli 端口 |
| [12](2026-08-17-1500-dsh-integration.md) | 2026-08-17 15:00 | DeepSeek Harness 集成：`dsh` 单元（web 后台服务 / headless 任务）+ `extension.dsh.repo` 首个消费键 |
| [13](2026-08-17-1530-dsh-tui-and-cleanup.md) | 2026-08-17 15:30 | dsh 端口动态化 + TUI 集成（仓库服务型 agent 进选工具列表）+ 移除测试注入点 + exec 拆词修复 |
| [14](2026-08-22-2200-full-audit-polish.md) | 2026-08-22 22:00 | 全项目重读与持续打磨：dsh help 解耦仓库配置、State nil map 加固、TUI 保存错误传播、文档一致性与全量验证 |
| [15](2026-08-22-2230-windows-console-handle.md) | 2026-08-22 22:30 | Windows 非控制台启动错误：识别 INVALID_HANDLE_VALUE、明确 TUI 终端要求、更新生产二进制 |
| [16](2026-08-26-1200-agent-workspace-mvp.md) | 2026-08-26 12:00 | Agent Workspace MVP：dsh ACP JSON-RPC 后端 adapter + 统一 session/event + 自渲染 TUI + 本地 transcript，legacy launcher 保持不变 |
| [17](2026-09-11-2347-acp-entry-and-release.md) | 2026-09-11 23:47 | ACP 入口可用化（内置推导 + agent list/doctor/enable/disable + opencode/dsh 开箱配置）+ TUI 卡顿治理（后台并行探测/建连 splash/store 内存索引/渲染块缓存/思考折叠）+ `release.ps1` 发布工作流与首次正式发布 |
