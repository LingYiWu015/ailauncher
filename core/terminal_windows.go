//go:build windows

package core

import (
	"path/filepath"
	"strings"
)

// terminalArgv 按终端类型拼「拉起 tui agent」的命令行（Windows 版）。
// 各终端的工作目录参数约定不同：
//   - Alacritty：--working-directory <wd> -e cmd /k <cmd>
//   - Windows Terminal（wt）：-d <wd> cmd /k <cmd>
//   - WezTerm：start --cwd <wd> -- cmd /k <cmd>
//   - 未知终端：按 Alacritty 约定兜底（换终端无需再改 launch 层）
//
// 终端名按可执行文件 basename 小写匹配，与大小写无关。
func terminalArgv(terminal, wd, cmdline string) []string {
	name := strings.ToLower(filepath.Base(terminal))
	switch {
	case name == "wt.exe" || name == "wt":
		return []string{terminal, "-d", wd, "cmd", "/k", cmdline}
	case strings.HasPrefix(name, "wezterm"):
		return []string{terminal, "start", "--cwd", wd, "--", "cmd", "/k", cmdline}
	default: // alacritty 及未知终端
		return []string{terminal, "--working-directory", wd, "-e", "cmd", "/k", cmdline}
	}
}
