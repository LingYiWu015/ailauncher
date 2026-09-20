package core

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// AdapterSpec 是 agent.extension.adapter 的受控配置。Env 仅传给子进程，绝不写入 transcript。
type AdapterSpec struct {
	Kind       string            `json:"kind"`
	Command    string            `json:"command"`
	Args       []string          `json:"args,omitempty"`
	Cwd        string            `json:"cwd,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	Permission string            `json:"permission,omitempty"` // allow | reject | ask，默认 reject
}

// DecodeAdapter 解析 agent 的 adapter 扩展，缺失或非法配置一律明确报错。
func DecodeAdapter(agent *Agent) (AdapterSpec, error) {
	if agent == nil {
		return AdapterSpec{}, fmt.Errorf("未指定 agent")
	}
	if !agent.Extension.Has("adapter") {
		return AdapterSpec{}, fmt.Errorf("agent %q 未配置 workspace adapter；请使用 run %s 走原启动模式", agent.Name, agent.Name)
	}
	var spec AdapterSpec
	if err := agent.Extension.Decode("adapter", &spec); err != nil {
		return AdapterSpec{}, fmt.Errorf("agent %q 的 adapter 配置无效：%w", agent.Name, err)
	}
	spec.Kind = strings.TrimSpace(spec.Kind)
	spec.Command = strings.TrimSpace(spec.Command)
	if spec.Kind == "" || spec.Kind == "none" {
		return AdapterSpec{}, fmt.Errorf("agent %q 未启用 workspace adapter；请使用 run %s 走原启动模式", agent.Name, agent.Name)
	}
	if spec.Command == "" {
		return AdapterSpec{}, fmt.Errorf("agent %q 的 adapter.command 不能为空", agent.Name)
	}
	if spec.Permission == "" {
		spec.Permission = "reject"
	}
	if spec.Permission != "allow" && spec.Permission != "reject" && spec.Permission != "ask" {
		return AdapterSpec{}, fmt.Errorf("agent %q 的 adapter.permission 必须为 allow、reject 或 ask", agent.Name)
	}
	return spec, nil
}

// builtinAdapter 按 agent 名字推导开箱即用的 ACP 配置（用户零配置即可配对）。
// 目前只有 opencode 原生支持 `opencode acp`（stdio JSON-RPC）；其他 agent 返回 ok=false，
// TUI 展示“暂不支持 ACP”而不是“未配 adapter”，并指引走 run。
func builtinAdapter(agent *Agent) (spec AdapterSpec, ok bool) {
	if agent == nil {
		return AdapterSpec{}, false
	}
	name := strings.ToLower(strings.TrimSpace(agent.Name))
	execBase := strings.ToLower(strings.TrimSpace(filepath.Base(agent.Exec)))
	if name == "opencode" || execBase == "opencode" || execBase == "opencode.exe" {
		return AdapterSpec{Kind: "acp", Command: "opencode", Args: []string{"acp"}, Permission: "ask"}, true
	}
	return AdapterSpec{}, false
}

// EffectiveAdapter 返回 agent 生效的 adapter：显式配置优先，缺失时试内置推导。
// 返回的 source 为 "config" | "builtin"，TUI 据此展示“已配置”或“一键接入”。
func EffectiveAdapter(agent *Agent) (spec AdapterSpec, source string, err error) {
	if agent == nil {
		return AdapterSpec{}, "", fmt.Errorf("未指定 agent")
	}
	if agent.Extension.Has("adapter") {
		spec, err := DecodeAdapter(agent)
		if err != nil {
			return AdapterSpec{}, "", err
		}
		if spec.Kind == "none" {
			if builtin, ok := builtinAdapter(agent); ok {
				return builtin, "builtin", nil
			}
		}
		return spec, "config", nil
	}
	if builtin, ok := builtinAdapter(agent); ok {
		return builtin, "builtin", nil
	}
	return AdapterSpec{}, "", fmt.Errorf("agent %q 未配置 workspace adapter；请使用 run %s 走原启动模式", agent.Name, agent.Name)
}

// ProbeBuiltin 只用内置推导做就绪态自检（TUI 一键接入前先验证真能握手）。
func ProbeBuiltin(ctx context.Context, agent *Agent) (AgentProbe, error) {
	builtin, ok := builtinAdapter(agent)
	if !ok {
		return AgentProbe{}, fmt.Errorf("agent %q 暂无内置 ACP 接入", agent.Name)
	}
	return ProbeAgent(ctx, agent, builtin), nil
}

// commandExists 报告命令是否在 PATH 或按绝对路径可执行。
func commandExists(command string) bool {
	if command == "" {
		return false
	}
	if filepath.IsAbs(command) {
		info, err := os.Stat(command)
		return err == nil && !info.IsDir()
	}
	_, err := exec.LookPath(command)
	return err == nil
}

// BuiltinStatus 报告内置接入的预检状态：命令是否存在（不握手，纯本地检查）。
func BuiltinStatus(agent *Agent) (spec AdapterSpec, installed bool) {
	builtin, ok := builtinAdapter(agent)
	if !ok {
		return AdapterSpec{}, false
	}
	return builtin, commandExists(builtin.Command)
}

// AdapterRegistry 以 kind 管理明确注册的 adapter factory。
type AdapterRegistry struct {
	mu       sync.RWMutex
	adapters map[string]SessionAdapter
}

func NewAdapterRegistry() *AdapterRegistry {
	return &AdapterRegistry{adapters: map[string]SessionAdapter{}}
}

func DefaultAdapterRegistry() *AdapterRegistry {
	r := NewAdapterRegistry()
	r.Register(newACPAdapter())
	return r
}

func (r *AdapterRegistry) Register(adapter SessionAdapter) {
	if r == nil || adapter == nil || adapter.Kind() == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[adapter.Kind()] = adapter
}

func (r *AdapterRegistry) Open(ctx context.Context, agent *Agent, req SessionRequest) (Session, AdapterSpec, error) {
	spec, _, err := EffectiveAdapter(agent)
	if err != nil {
		return nil, AdapterSpec{}, err
	}
	if r == nil {
		return nil, AdapterSpec{}, fmt.Errorf("workspace adapter registry 未初始化")
	}
	r.mu.RLock()
	adapter := r.adapters[spec.Kind]
	r.mu.RUnlock()
	if adapter == nil {
		return nil, AdapterSpec{}, fmt.Errorf("agent %q 的 adapter kind %q 尚不受支持；请使用 run %s", agent.Name, spec.Kind, agent.Name)
	}
	s, err := adapter.Open(ctx, agent, spec, req)
	if err != nil {
		return nil, AdapterSpec{}, err
	}
	return s, spec, nil
}
