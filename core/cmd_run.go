package core

import (
	"fmt"
	"strings"
)

func init() {
	Register(&Unit{
		Name:     "run",
		Stage:    StageStable,
		Kind:     UnitCommand,
		Declared: "启动 agent：run <agent> [path] [KEY=V | arg...]",
		Run:      runUnit,
	})
}

func runUnit(ctx *Ctx, args []string) (*Result, error) {
	if len(args) < 1 {
		return nil, Usagef("run 需要 <agent>：run <agent> [path] [KEY=V | arg...]")
	}
	agent := ctx.Cfg.ResolveAgent(args[0])
	if agent == nil {
		return nil, fmt.Errorf("未知 agent %q（可用 list 查看）", args[0])
	}
	rest := args[1:]

	// path：第一个非 KEY=V 的 token；其后 KEY=V → 环境变量，其余 → 附加参数。
	wd := ""
	if len(rest) > 0 && !isEnvToken(rest[0]) {
		wd = rest[0]
		rest = rest[1:]
	}
	if wd != "" && !dirExists(wd) {
		return nil, fmt.Errorf("工作目录不存在：%s", wd)
	}

	envs, extra := ParseArgs(rest)
	if err := launchFn(agent, wd, envs, extra, ctx.Cfg.Terminal); err != nil {
		return nil, fmt.Errorf("启动 %s 失败：%v", agent.Name, err)
	}
	msg := fmt.Sprintf("已启动 %s", displayName(agent))
	if wd != "" {
		msg += fmt.Sprintf("（工作目录 %s）", wd)
	}
	if hint := dshAgentHint(agent, extra); hint != "" {
		msg += hint
	}
	return Text(msg), nil
}

func isEnvToken(s string) bool {
	if k, _, ok := strings.Cut(s, "="); ok && isEnvKey(k) {
		return true
	}
	return false
}

func displayName(a *Agent) string {
	if a.Display != "" {
		return a.Display
	}
	return a.Name
}
