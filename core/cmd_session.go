package core

import (
	"context"
	"fmt"
)

func init() {
	Register(&Unit{
		Name:     "session",
		Stage:    StageStable,
		Kind:     UnitQuery,
		Declared: "管理本地 Workspace transcript：session list / get <id> / delete <id> / probe <agent>",
		Run:      sessionUnit,
	})
}

func sessionUnit(ctx *Ctx, args []string) (*Result, error) {
	if ctx.Sessions == nil {
		return nil, fmt.Errorf("session store 未初始化")
	}
	if len(args) < 1 {
		return nil, Usagef("session 需要子命令：session list | get <id> | delete <id> | probe <agent>")
	}
	switch args[0] {
	case "list":
		if len(args) != 1 {
			return nil, Usagef("session list 不接受额外参数")
		}
		records, err := ctx.Sessions.List()
		if err != nil {
			return nil, fmt.Errorf("读取 session list 失败：%w", err)
		}
		return JSON(records), nil
	case "get":
		if len(args) != 2 {
			return nil, Usagef("session get 需要一个 <id>")
		}
		record, err := ctx.Sessions.Get(SessionID(args[1]))
		if err != nil {
			return nil, fmt.Errorf("读取 session 失败：%w", err)
		}
		if record == nil {
			return nil, fmt.Errorf("未找到本地 session %q", args[1])
		}
		return JSON(record), nil
	case "delete":
		if len(args) != 2 {
			return nil, Usagef("session delete 需要一个 <id>（只删本地 transcript，不碰远端）")
		}
		if err := ctx.Sessions.Delete(SessionID(args[1])); err != nil {
			return nil, fmt.Errorf("删除 session 失败：%w", err)
		}
		return Text("已删除本地 session " + args[1]), nil
	case "probe":
		if len(args) != 2 {
			return nil, Usagef("session probe 需要一个 <agent>（只握手不建会话）")
		}
		if ctx.Cfg == nil {
			return nil, fmt.Errorf("未加载配置")
		}
		agent := ctx.Cfg.ResolveAgent(args[1])
		if agent == nil {
			return nil, fmt.Errorf("未知 agent %q（可用 list 查看）", args[1])
		}
		spec, err := DecodeAdapter(agent)
		if err != nil {
			return JSON(AgentProbe{Agent: agent.Name, Display: displayOrName(agent), Error: err.Error()}), nil
		}
		probe := ProbeAgent(context.Background(), agent, spec)
		return JSON(probe), nil
	default:
		return nil, Usagef("未知 session 子命令 %q（可用 list/get/delete/probe）", args[0])
	}
}
