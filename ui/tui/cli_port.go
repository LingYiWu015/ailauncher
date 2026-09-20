package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"cli"
)

// 本包只依赖 cli（端口 + 值类型别名），不直接 import core。
// 所有用户触发的逻辑（config/state/run/resolve……）都经 cli.Port 完成。

// dispatchNoIn 便捷封装：忽略 stdin 的一次命令调用。
func dispatchNoIn(p cli.Port, argv []string) (*cli.Result, error) {
	return p.Dispatch(argv, nil)
}

// fetchConfig 经 cli config 单元引导配置。
func fetchConfig(p cli.Port) (*cli.Config, error) {
	res, err := dispatchNoIn(p, []string{"config"})
	if err != nil {
		return nil, err
	}
	cfg, ok := res.Data.(*cli.Config)
	if !ok {
		return nil, errPortType("config", res.Data)
	}
	return cfg, nil
}

// resolveAgent 经 cli resolve 单元按 name/display 解析 agent。
func resolveAgent(p cli.Port, name string) (*cli.Agent, error) {
	res, err := dispatchNoIn(p, []string{"resolve", name})
	if err != nil {
		return nil, err
	}
	a, ok := res.Data.(*cli.Agent)
	if !ok {
		return nil, errPortType("resolve", res.Data)
	}
	return a, nil
}

// loadState 经 cli state get 单元读取 agent 运行时状态。
func loadState(p cli.Port, name string) (*cli.AgentState, error) {
	res, err := dispatchNoIn(p, []string{"state", "get", name})
	if err != nil {
		return nil, err
	}
	td, ok := res.Data.(*cli.AgentState)
	if !ok {
		return nil, errPortType("state get", res.Data)
	}
	return td, nil
}

// saveState 经 cli state put 单元把 agent 状态落盘：JSON 经 in 传入，
// 避开 raw 模式下 os.Stdin 已被 TUI 占用的问题。
func saveState(p cli.Port, name string, td *cli.AgentState) error {
	b, err := json.Marshal(td)
	if err != nil {
		return err
	}
	_, err = p.Dispatch([]string{"state", "put", name}, strings.NewReader(string(b)))
	return err
}

// errPortType 报告 cli 端口返回的数据类型不符合预期。
func errPortType(op string, data any) error {
	return fmt.Errorf("%s 返回类型异常：%T", op, data)
}
