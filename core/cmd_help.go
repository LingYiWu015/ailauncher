package core

import (
	"fmt"
	"strings"
)

func init() {
	Register(&Unit{
		Name:     "help",
		Stage:    StageStable,
		Kind:     UnitQuery,
		Declared: "显示帮助；help [cmd] 显示单条命令",
		Run:      helpUnit,
	})
}

func helpUnit(ctx *Ctx, args []string) (*Result, error) {
	if len(args) > 1 {
		return nil, Usagef("help 最多接受一个命令名")
	}
	if len(args) == 1 {
		u := Lookup(args[0])
		if u == nil || !u.Stage.Visible() {
			return nil, fmt.Errorf("未知命令 %q（可用 help 查看全部）", args[0])
		}
		return Text(fmt.Sprintf("%s  [%s / %s]\n%s", u.Name, u.Stage, u.Kind, u.Declared)), nil
	}
	var b strings.Builder
	b.WriteString("AILauncher v" + Version + " —— 命令面：\n\n")
	for _, c := range Capabilities() {
		fmt.Fprintf(&b, "  %-12s %s\n", c.Name, c.Declared)
	}
	b.WriteString("\n无参数运行进入交互式 TUI；cap 查看能力清单；未知命令返回详细报错。")
	return Text(b.String()), nil
}
