// Command ailauncher 是 AILauncher v4 唯一入口：无参数 → TUI；
// -i <agent> [path] → 交互直达；否则 → cli 路由（脚本面）。
package main

import (
	"fmt"
	"os"

	"cli"
	"ui/tui"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(argv []string) int {
	router := cli.NewDefault()

	// 无参数 → TUI
	if len(argv) == 0 {
		if err := tui.Run(router); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}

	// chat 是 Claude Code 风格的自渲染会话入口；workspace 保留兼容（同 chat）。
	if argv[0] == "chat" || argv[0] == "workspace" {
		if len(argv) < 2 || len(argv) > 3 {
			fmt.Fprintln(os.Stderr, "用法: ailauncher chat <agent> [path]")
			return 2
		}
		path := ""
		if len(argv) == 3 {
			path = argv[2]
		}
		if err := tui.RunChat(router, argv[1], path); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}

	// history-replay 只读回放一条本地 transcript。
	if argv[0] == "history-replay" {
		if len(argv) != 2 {
			fmt.Fprintln(os.Stderr, "用法: ailauncher history-replay <session-id>")
			return 2
		}
		if err := tui.RunHistoryReplay(router, argv[1]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}

	// -i / --interactive → 交互直达
	if argv[0] == "-i" || argv[0] == "--interactive" {
		if len(argv) < 2 || len(argv) > 3 {
			fmt.Fprintln(os.Stderr, "用法: ailauncher -i <agent> [path]")
			return 2
		}
		path := ""
		if len(argv) == 3 {
			path = argv[2]
		}
		if err := tui.RunDirect(router, argv[1], path); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}

	// 其余 → cli 路由（脚本面，与 TUI 同一端口）
	res, err := router.Dispatch(argv, os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return cli.ExitCode(err)
	}
	b, err := cli.Format(res)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	os.Stdout.Write(b)
	if res.Exit != 0 {
		return res.Exit
	}
	return 0
}
