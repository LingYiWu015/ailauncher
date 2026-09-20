package core

import (
	"encoding/json"
	"fmt"
	"os"
)

func init() {
	Register(&Unit{
		Name:     "state",
		Stage:    StageStable,
		Kind:     UnitCommand, // 含查询子命令 get；整体按有副作用登记
		Declared: "读写 agent 运行时状态：state get <agent> / state put <agent>（JSON 自 stdin）",
		Run:      stateUnit,
	})
}

func stateUnit(ctx *Ctx, args []string) (*Result, error) {
	if len(args) < 1 {
		return nil, Usagef("state 需要子命令：state get <agent> | state put <agent>")
	}
	switch args[0] {
	case "get":
		return stateGet(ctx, args[1:])
	case "put":
		return statePut(ctx, args[1:])
	default:
		return nil, Usagef("未知 state 子命令 %q（可用 get/put）", args[0])
	}
}

func stateGet(ctx *Ctx, args []string) (*Result, error) {
	if len(args) != 1 {
		return nil, Usagef("state get 需要一个 <agent>")
	}
	return JSON(ctx.St.Get(args[0])), nil
}

func statePut(ctx *Ctx, args []string) (*Result, error) {
	if len(args) != 1 {
		return nil, Usagef("state put 需要一个 <agent>")
	}
	in := ctx.In
	if in == nil {
		in = os.Stdin
	}
	var st AgentState
	if err := json.NewDecoder(in).Decode(&st); err != nil {
		return nil, fmt.Errorf("state put JSON 解析失败：%v", err)
	}
	st.normalize() // nil 切片归一化为 []，避免落盘 null 数组
	ctx.St[args[0]] = &st
	if err := ctx.St.Save(ctx.StatePath); err != nil {
		return nil, fmt.Errorf("state put 保存失败：%v", err)
	}
	return Text(fmt.Sprintf("已保存 %s 状态", args[0])), nil
}
