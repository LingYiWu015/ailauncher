package core

// Version 是当前构建版本。
const Version = "0.4.0-dev"

func init() {
	Register(&Unit{
		Name:     "version",
		Stage:    StageStable,
		Kind:     UnitQuery,
		Declared: "显示版本",
		Run:      versionUnit,
	})
}

func versionUnit(ctx *Ctx, args []string) (*Result, error) {
	if len(args) > 0 {
		return nil, Usagef("version 不接受参数")
	}
	return Text(Version), nil
}
