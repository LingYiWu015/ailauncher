package core

func init() {
	Register(&Unit{
		Name:     "list",
		Stage:    StageStable,
		Kind:     UnitQuery,
		Declared: "列出全部 agent（JSON）",
		Run:      listUnit,
	})
}

func listUnit(ctx *Ctx, args []string) (*Result, error) {
	if len(args) > 0 {
		return nil, Usagef("list 不接受参数")
	}
	return JSON(ctx.Cfg.Agents), nil
}
