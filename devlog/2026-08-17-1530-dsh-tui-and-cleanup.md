# step13 · dsh 动态端口 + TUI 集成 + 移除测试注入点

> 2026-08-17。承接 step12（dsh 单元落地）。用户三连问：
> ① web 成功文案为什么写死 3080；② 直接更新 TUI 要试用；③ 把测试时留的代码移除或替换
> （指 step12 加的 `dshLaunchFn`/`dshHeadlessFn` 注入点）。

## 做了什么

### 1. 端口动态化（答「为什么写死端口」）

- step12 的 `dshUnit` web 分支成功文案硬编码 `http://127.0.0.1:3080`，与 `--port N` 脱节。
  现在经 `dshWebPort(extra)` 解析透传参数里的 `--port`（默认 `3080`，`--port 0` 走系统分配），
  文案用解析结果拼 URL。
- `run` 单元启动「仓库服务型 agent」（有 `extension.dsh.repo`）后追加提示行 `dshAgentHint(extra)`：
  「浏览器打开 http://127.0.0.1:<port> 使用 DeepSeek Harness」——`--port 0` 时给说明不给死端口。

### 2. TUI 集成（直接试用）

- **config 加 dsh gui agent**（根 / 生产 / sample 三份 config）：`name:"dsh"`、`type:"gui"`、
  `exec:"pnpm"`、`args:"dsh web"`、`extension.dsh.repo` 指仓库根。TUI 选工具列表自动出现
  「DeepSeek Harness」，选中即走 gui 短路 → `run dsh` → detached 起 web 服务。
- **`agent.Args` 拆词修复**：gui 拉起是 `exec.Command(exec, args...)`，而 `args` 以前
  把 `agent.Args` 整串当一个 argv（会去找名为 `"dsh web"` 的可执行文件）。现在
  launchGUI 两端都 `strings.Fields(agent.Args)` 拆词（TUI 的命令串本就按空格解析，不受影响）。
  这是本轮真正搬掉的隐性 bug。
- **`showResult(res, fallback)`**：TUI 的 `runGui`/`runTuiAgent` 之前丢弃 Dispatch 结果、只印
  固定的「已启动 X」。现在渲染 `Result.Text` 多行文案（gui 停留 2s 让人读、tui 0.8s）——所以
  dsh 的访问地址在 TUI 里直接可见。

### 3. 移除注入点 + 测试重写

- 删掉 `dshLaunchFn`/`dshHeadlessFn` 变量：web 直接调 `launchDetachedEnv`，headless 直接
  `dshHeadlessCmd(...).Run()`。逻辑抽成纯函数：`dshWebSpec` / `dshWebPort` / `dshHeadlessCmd` /
  `dshRepoFromAgent` / `dshAgentHint`。
- dsh 仓库根覆盖从平台 launchGUI 上移到共享纯函数 **`expandWorkdir(agent, workdir)`**
  （launch.go）：`extension.dsh.repo` 优先强制 cwd，再 workdir，回退 USERPROFILE/当前目录。
  这样 GUI/TUI 都生效，且可纯函数单测（不用桩 launchFn 链）。
- cmd_dsh_test.go 重写：去掉 stub 注入，改测纯函数（webSpec/webPort/headlessCmd/repoFromAgent）
  + 既有 repoDir 各分支 + 分发错误路径（help/未知子命令/headless 无任务/仓库未配置）。

## 为什么

- **端口**：`--port N` 是真实透传参数，成功文案必须与之同步，否则用户按文案开错端口。
- **TUI 集成**：step12 曾判断「dsh 不该混进 TUI 选工具列表」——那是指 deepseek-harness 无自己的
  TUI 客户端、不能当 tui/gui 直接套终端跑交互；但作为 **gui 仓库服务型 agent**（起 web 后台服务）
  进启动器列表正是它的自然用法：零代码，config 驱动，选一下即起。结论修正并写入本 log。
- **移除注入点**：step12 的 `dshLaunchFn`/`dshHeadlessFn` 是为单测换桩临时加的，属于用户点名要
  清掉的「测试时留的代码」。抽纯函数后把「真 exec」留在单元里、断言全走纯函数，注入点不再必要。

## 后果

- 全模块 build/vet/test 绿（core 64 + cli 14 + ui 27 = 105），四平台交叉编译通过。
- 冒烟（生产副本，**避开真起 web**）：`cap` 含 dsh；`list`/`resolve dsh` 显示带
  `extension.dsh.repo` 的 dsh agent；`dsh help` 正常。未触发真实后台进程。
- 生产副本更新：二进制 + config（已加 dsh agent）。
- gofmt 全过；测试配置 JSON 一律 `json.Marshal` 构造（Windows 路径反斜杠转义坑，step12 已记）。

## 下一步

- **你本人在真机 TUI 试用**：`ailauncher.exe` → 选「DeepSeek Harness」→ 确认起服务 + 地址提示；
  `-i dsh` 直达同理。
- 得空可给 dsh web 加「启动后自动开浏览器」（UX 层，core 不动）。

## 验证命令

```bash
go test ./core/... ./cli/... ./ui/...
go vet ./core/... ./cli/... ./ui/...
GOOS=linux/darwin/freebsd go build ./core/... ./cli/... ./ui/...
go build -o ailauncher.exe ./ui/cmd/ailauncher
./ailauncher list | grep -i dsh     # agent 在列
./ailauncher resolve dsh            # 带 extension.dsh.repo
./ailauncher dsh help               # 用法（不拉服务）
```