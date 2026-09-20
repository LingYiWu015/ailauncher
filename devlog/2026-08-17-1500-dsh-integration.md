# step12 · DeepSeek Harness 集成到 core：dsh 单元（web 后台服务 / headless 任务）

> 2026-08-17。用户在本地发现 deepseek-harness（DeepSeek 官方 agent harness，developer preview，
> clone 于 `D:\Coding\Projects\Claude_Projects\New_Project\deepseek-harness`），要求
> 「添加到 core 中」。经澄清：**深seek 专用单元**（非通用 agent 型），命令面接入。

## 做了什么

- **core/cmd_dsh.go**：自注册命令单元 `dsh`（StageStable）：
  - `ailauncher dsh [web] [KEY=V|arg...]` — 起 **web UI 后台服务**（detached，浏览器打开
    `http://127.0.0.1:3080`）；`--port N` 等参数透传
  - `ailauncher dsh headless "任务" [KEY=V]` — 前台跑一次性任务，阻塞透传输出到出答案
  - `dsh help` — 用法
  - 仓库根：`config.json` 的 `extension.dsh.repo` 优先，回退环境变量 `DSH_REPO`，
    都无/路径不存在报错（这是 extension 首个被消费的键）
  - `KEY=V` 经现有 `ParseArgs` 抽为环境变量（如 `DEEPSEEK_API_KEY`）
- **core/launch_windows.go / launch_other.go**：新增 `launchDetachedEnv(argv, wd, env)`——
  与 `startDetached` 同构但显式注入 env，供 web 后台服务 detached 拉起（Windows 复用
  BREAKAWAY → WMI/explorer 逃逸链）。
- **可注入点**：`dshLaunchFn` / `dshHeadlessFn`（仿 `launchFn` 模式），单测换桩。
- **配置**：根与生产 `config.json` 都加了 `extension.dsh.repo`；`config.sample.json` 补示例。
- **测试**：cmd_dsh_test.go 13 例（注册/仓库解析 6 + web 启动 4 + headless 2 + 未知子命令/help）。
  - 修坑：测试构造配置 JSON 时 Windows 路径反斜杠必须先经 `json.Marshal` 转义（否则 `\U`
    触发非法 JSON 转义）。

## 为什么

- **形态确认**：用户澄清点是「后台 web 服务」是否唤醒 terminal emulator 在宿主里返回信息——
  否，AILauncher 是 detached 拉起后台进程、宿主立即退出，浏览器自己开 3080，不托管输出。
- **专用单元而非 agent 型**：deepseek-harness 无 TUI（官方已移除 dsh-tui），唯一交互是 Web，
  且 `pnpm dsh` 必须 cwd 在仓库根、无全局 bin——现有 `Agent.Type`(tui/gui) 表达不了，也不该
  混进 TUI 选工具列表；命令面 `dsh` 最贴合。
- **不引入硬编码路径**：仓库根走 `extension.dsh.repo`（配置驱动），非代码里写死。

## 后果

- 三模块 build/vet/test 全绿，四平台交叉编译通过。`dsh` 自注册即自动进入 cap/help/未知命令
  详细报错（cli 走 `core.Snapshot()`，零接线改动）。
- **真实集成验证（本机 Windows）**：`ailauncher dsh web --port 3099` detached 后约 20~25 秒
  dsh web 真实监听（tsx 首次编译较慢），已清理进程与端口。
- 生产副本 `D:\Coding\Scripts\ai-launcher\`：二进制已更新 + config 已加 extension.dsh.repo。
- 注意事项：`dsh`/`dsh web` 在配置了 repo 的目录会**真实 detach 拉起服务**——冒烟验证时要
  避开（用 `dsh help`/`cap` 或删 repo 的临时副本），否则会起后台进程（本次踩了两次，均清理）。

## 下一步

- **真机验收**：给 `DEEPSEEK_API_KEY` 后真跑 `ailauncher dsh` 开 web 用一轮；headless 任务同理。
- 需要时给 web 模式加启动后自动打开浏览器（ui/命令面 UX 层，core 不动）。
- adapter 层仍挂账（`extension.adapter` 无消费）。

## 验证命令

```bash
go test ./core/... ./cli/... ./ui/...
go vet ./core/... ./cli/... ./ui/...
GOOS=linux/darwin/freebsd go build ./core/... ./cli/... ./ui/...   # 四平台交叉编译
go build -o ailauncher.exe ./ui/cmd/ailauncher
./ailauncher dsh help                        # 用法
./ailauncher cap | grep dsh                  # 已暴露
```