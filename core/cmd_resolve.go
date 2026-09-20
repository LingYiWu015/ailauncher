package core

import "fmt"

func init() {
	Register(&Unit{
		Name:     "resolve",
		Stage:    StageStable,
		Kind:     UnitQuery,
		Declared: "解析 agent：resolve <name|display>（JSON）",
		Run:      resolveUnit,
	})
}

func resolveUnit(ctx *Ctx, args []string) (*Result, error) {
	if len(args) != 1 {
		return nil, Usagef("resolve 需要一个 <name>：resolve <name|display>")
	}
	a := ctx.Cfg.ResolveAgent(args[0])
	if a == nil {
		return nil, fmt.Errorf("未知 agent %q（可用 list 查看）", args[0])
	}
	return JSON(a), nil
}
