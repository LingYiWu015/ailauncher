//go:build !windows

package core

// terminalArgv 按终端类型拼「拉起 tui agent」的命令行（Unix 版）。
// 目前按 Alacritty 约定（--working-directory + sh -lc）；其余终端待按发行版适配。
func terminalArgv(terminal, wd, cmdline string) []string {
	return []string{terminal, "--working-directory", wd, "-e", "sh", "-lc", cmdline}
}
