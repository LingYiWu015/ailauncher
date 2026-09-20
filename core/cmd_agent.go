package core

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func init() {
	Register(&Unit{
		Name:     "agent",
		Stage:    StageStable,
		Kind:     UnitCommand,
		Declared: "管理 agent 接入：agent list / enable <name> / disable <name> / doctor [name]",
		Run:      agentUnit,
	})
}

func agentUnit(ctx *Ctx, args []string) (*Result, error) {
	if len(args) < 1 {
		return nil, Usagef("agent 需要子命令：agent list | enable <name> | disable <name> | doctor [name]")
	}
	switch args[0] {
	case "list":
		if len(args) != 1 {
			return nil, Usagef("agent list 不接受额外参数")
		}
		return JSON(agentSummaries(ctx.Cfg)), nil
	case "enable", "disable":
		if len(args) != 2 {
			return nil, Usagef("agent %s 需要一个 <name>", args[0])
		}
		return agentSetEnabled(ctx, args[1], args[0] == "enable")
	case "doctor":
		if len(args) > 2 {
			return nil, Usagef("agent doctor 只接受零或一个 <name>")
		}
		name := ""
		if len(args) == 2 {
			name = args[1]
		}
		return agentDoctor(ctx, name)
	default:
		return nil, Usagef("未知 agent 子命令 %q（可用 list/enable/disable/doctor）", args[0])
	}
}

// AgentSummary 是 agent 接入状态的一行摘要（TUI/CLI 共用）。
type AgentSummary struct {
	Name      string `json:"name"`
	Display   string `json:"display,omitempty"`
	Enabled   bool   `json:"enabled"`
	Adapter   string `json:"adapter"`
	Source    string `json:"source"`
	Installed bool   `json:"installed"`
}

func agentSummaries(cfg *Config) []AgentSummary {
	var out []AgentSummary
	if cfg == nil {
		return out
	}
	for _, a := range cfg.Agents {
		if a == nil {
			continue
		}
		spec, source, err := EffectiveAdapter(a)
		summary := AgentSummary{Name: a.Name, Display: displayOrName(a), Enabled: a.IsEnabled(), Source: source}
		if err != nil {
			summary.Adapter = "none"
			summary.Source = ""
		} else {
			summary.Adapter = spec.Kind
			summary.Installed = commandExists(spec.Command)
		}
		out = append(out, summary)
	}
	return out
}

func agentSetEnabled(ctx *Ctx, name string, enabled bool) (*Result, error) {
	if ctx.Cfg == nil {
		return nil, fmt.Errorf("未加载配置")
	}
	agent := ctx.Cfg.ResolveAgent(name)
	if agent == nil {
		return nil, fmt.Errorf("未知 agent %q（可用 list 查看）", name)
	}
	agent.Enabled = &enabled
	if err := ctx.Cfg.Save(ctx.ConfigPath); err != nil {
		return nil, fmt.Errorf("保存配置失败：%w", err)
	}
	state := "已禁用"
	if enabled {
		state = "已启用"
	}
	return Text(fmt.Sprintf("%s %s", state, displayOrName(agent))), nil
}

// agentDoctor 对全部（或单个）agent 做接入体检：内置可用性 + 显式配置 + 真握手。
func agentDoctor(ctx *Ctx, name string) (*Result, error) {
	if ctx.Cfg == nil {
		return nil, fmt.Errorf("未加载配置")
	}
	var agents []*Agent
	if name != "" {
		agent := ctx.Cfg.ResolveAgent(name)
		if agent == nil {
			return nil, fmt.Errorf("未知 agent %q（可用 list 查看）", name)
		}
		agents = []*Agent{agent}
	} else {
		agents = ctx.Cfg.Agents
	}
	type report struct {
		AgentSummary
		Reachable    bool   `json:"reachable"`
		Error        string `json:"error,omitempty"`
		Protocol     int    `json:"protocol,omitempty"`
		AgentVersion string `json:"agentVersion,omitempty"`
		Hint         string `json:"hint,omitempty"`
	}
	var out []report
	for _, a := range agents {
		if a == nil {
			continue
		}
		summary := AgentSummary{Name: a.Name, Display: displayOrName(a), Enabled: a.IsEnabled()}
		spec, source, err := EffectiveAdapter(a)
		if err != nil {
			summary.Adapter = "none"
			out = append(out, report{AgentSummary: summary, Hint: "走 run 原启动模式，或等内置接入"})
			continue
		}
		summary.Adapter = spec.Kind
		summary.Source = source
		summary.Installed = commandExists(spec.Command)
		if !summary.Installed {
			out = append(out, report{AgentSummary: summary, Hint: fmt.Sprintf("未找到命令 %q：先安装对应 CLI", spec.Command)})
			continue
		}
		probeCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		probe := ProbeAgent(probeCtx, a, spec)
		cancel()
		r := report{AgentSummary: summary, Reachable: probe.Reachable, Error: probe.Error, Protocol: probe.Protocol, AgentVersion: probe.AgentVersion}
		if !probe.Reachable {
			r.Hint = probeHintLine(a, probe)
		}
		out = append(out, r)
	}
	return JSON(out), nil
}

func probeHintLine(a *Agent, probe AgentProbe) string {
	if probe.AuthRequired {
		hint := "需先登录"
		if probe.AuthHint != "" {
			hint += "：" + probe.AuthHint
		}
		return hint
	}
	if strings.Contains(probe.Error, "PATH") {
		return probe.Error
	}
	if a != nil && strings.EqualFold(a.Name, "opencode") {
		return probe.Error + "（先跑 opencode auth login）"
	}
	return probe.Error
}
