# step15 · Windows 非控制台启动错误修复

> 2026-08-22。用户从 Bash 运行无参数 `ailauncher` 时收到 `需要交互式终端: The handle is invalid.`。

## 原因

Windows TUI 的 `enterRaw` 通过 `GetStdHandle` / `GetConsoleMode` 获取控制台句柄。原实现只判断句柄为 `0`，没有判断 Windows API 的 `INVALID_HANDLE_VALUE`（全 1 句柄）。从 Git Bash、Claude Bash 或重定向 stdin 启动时，标准输入不是 Win32 控制台，API 返回无效句柄，最终把英文系统错误直接暴露给用户。

## 修复

- 增加 `INVALID_HANDLE_VALUE` 检查，输入和输出句柄均覆盖 `0` 与无效句柄。
- 将错误改为明确中文提示：请在 Windows Terminal、PowerShell 或 cmd 中运行 TUI。
- `GetConsoleMode` 失败时同样标明对应的输入/输出不是 Windows 控制台，并保留底层错误便于诊断。
- README 与 PROJECT_OVERVIEW 增加环境说明：Git Bash/某些 IDE 内置 Bash 不适合 Windows raw TUI，但 `run`、`list`、`cap`、`dsh help` 等 CLI 子命令仍可使用。

## 验证

```text
go test ./core/... ./cli/... ./ui/...  PASS
go vet ./core/... ./cli/... ./ui/...   PASS
go build -o D:\\Coding\\Scripts\\ai-launcher\\ailauncher.exe ./ui/cmd/ailauncher  PASS
```

生产目录二进制已更新。TUI 请从原生 Windows Terminal、PowerShell 或 cmd 启动，例如：

```powershell
cd D:\Coding\Scripts\ai-launcher
.\ailauncher.exe
```

本轮没有启动 dsh web 服务。
