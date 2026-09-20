package core

func init() {
	Register(&Unit{
		Name:     "migrate",
		Stage:    StageStable,
		Kind:     UnitCommand,
		Declared: "数据归一化：清理 data.json null 数组，校验 config 为 v4 agents[] 形态",
		Run:      migrateUnit,
	})
}

func migrateUnit(ctx *Ctx, args []string) (*Result, error) {
	if len(args) > 0 {
		return nil, Usagef("migrate 不接受参数")
	}
	cfgChanged, err := MigrateConfig(ctx.ConfigPath)
	if err != nil {
		return nil, err
	}
	stChanged, err := MigrateState(ctx.StatePath)
	if err != nil {
		return nil, err
	}
	if !cfgChanged && !stChanged {
		return Text("config/data 已是 v4 形态，无需迁移"), nil
	}
	var msg string
	if cfgChanged {
		msg += "config.json 已归一化\n"
	}
	if stChanged {
		msg += "data.json null 数组已归一化为 []\n"
	}
	return Text(msg), nil
}
