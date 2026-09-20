# step14 · 全项目重读与持续打磨

> 2026-08-22。承接 step13，按用户要求重新阅读 core / cli / ui 全部源码、测试、配置和文档，修复审计中确认的问题并完成全量验证。

## 做了什么

### 1. 完成全项目重读与一致性核对

- 重读三个独立 Go module 的源码、平台分支和测试：`core/`、`cli/`、`ui/`。
- 核对根配置、状态文件、样例配置、README、CLAUDE、PROJECT_OVERVIEW、既有 devlog。
- 修正文档滞后内容：根配置实际已有 6 个 agent（新增 dsh），PROJECT_OVERVIEW 不再写 5 个；同步当前 dsh 默认端口和最近 devlog 指针。

### 2. 修复 dsh help 的配置依赖

- 原 `dshUnit` 一进入就解析仓库路径，导致仅查看 `dsh help` 时也要求 `extension.dsh.repo` 存在。
- 现在先解析子命令并处理 `help`，只有真正执行 web/headless 时才解析并校验仓库。
- `dsh help` 因此成为安全的只读帮助命令：未配置仓库、仓库路径不存在时仍能正常显示用法。
- 补充回归测试，覆盖无 repo 配置的 help。

### 3. 加固 State.Get 公共 API

- 原 `State.Get` 在 nil map 上写入会 panic，注释虽说明由路由保证非 nil，但公共数据模型不应把调用者限制在内部加载路径。
- 现在 nil `State` 返回初始化好的独立 `AgentState`；非 nil map 仍保持缺失 agent 自动登记的原行为。
- 补充 nil map 不 panic 的回归测试，并明确记录值接收器无法把新 key 写回 nil map 的 Go 语义。

### 4. 不再静默吞掉 TUI 保存错误

- `listPage` 的 `onChange` 从无返回值改为 `func() error`，列表排序、软删除、恢复、手动添加和额外按键操作可以把持久化错误向上返回。
- `selectDir` 的播种、切根、项目列表同步、手动模式同步和最终保存均检查 `save()` 返回值。
- `runArgs` 的参数移除、恢复和排序保存均检查 `save()` 返回值。
- 保存失败时 TUI 立即结束当前流程并交给上层显示错误，不再继续呈现“看似已保存”的状态。
- 补充 listPage 与 runArgs 保存失败回归测试。

## 验证

```text
go test ./core/... ./cli/... ./ui/...       PASS
go vet ./core/... ./cli/... ./ui/...        PASS
go build ./core/... ./cli/... ./ui/...     PASS
GOOS=windows go build ./core/... ./cli/... ./ui/...  PASS
GOOS=linux go build ./core/... ./cli/... ./ui/...    PASS
GOOS=darwin go build ./core/... ./cli/... ./ui/...   PASS
GOOS=freebsd go build ./core/... ./cli/... ./ui/...  PASS
```

只读 CLI 冒烟全部通过：`cap` 含 dsh、`list` 含 DeepSeek Harness agent、`resolve dsh` 返回 repo extension、`dsh help` 在当前配置下正常。没有执行 `dsh web`，没有创建后台服务。

## 后果

- dsh 的帮助和实际启动路径职责分离，缺配置时更容易诊断。
- State 与 TUI 状态持久化失败路径更健壮。
- 文档与当前 6-agent 配置、dsh 动态端口实现保持一致。
- 本轮源码和文档留在项目目录，生产日常启动目录 `D:\\Coding\\Scripts\\ai-launcher\\` 已更新构建后的二进制并完成只读核对；真实 Windows TTY 交互、dsh 带 key 的 web/headless 试用仍需真人验收。

## 下一步

1. 用户在 Windows 真机运行 TUI，重点验收方向键、中文、ESC、配色、目录页和 DeepSeek Harness 地址提示。
2. 给 dsh 配置有效 key 后分别试用 web 与 headless；首次 web 启动需等待 tsx 编译完成。
3. 后续可继续做 TUI 尺寸适配、keyReader 生命周期收尾和 proto-lab 首个实验单元，但不影响当前稳定命令面。
