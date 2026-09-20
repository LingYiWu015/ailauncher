package core

func init() {
	Register(&Unit{
		Name:     "config",
		Stage:    StageStable,
		Kind:     UnitQuery,
		Declared: "当前配置（JSON）",
		Run:      configUnit,
	})
}

func configUnit(ctx *Ctx, args []string) (*Result, error) {
	if len(args) > 0 {
		return nil, Usagef("config 不接受参数")
	}
	return JSON(ctx.Cfg), nil
}
