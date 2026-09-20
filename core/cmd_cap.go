package core

func init() {
	Register(&Unit{
		Name:     "cap",
		Stage:    StageStable,
		Kind:     UnitQuery,
		Declared: "能力清单：当前可见（stage 过滤后）逻辑单元（JSON）",
		Run:      capUnit,
	})
}

func capUnit(ctx *Ctx, args []string) (*Result, error) {
	if len(args) > 0 {
		return nil, Usagef("cap 不接受参数")
	}
	return JSON(Capabilities()), nil
}
