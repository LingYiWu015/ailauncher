// Package cli 是 AILauncher v4 的纯路由微前端：不持有逻辑。
// 命令进来先问 core（按 stage 过滤）→ proto-lab → 都不认返回详细信息。
// 结果格式化也在此层：Text 走人类可读，JSON 走脚本。
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"core"
)

// Router 是命令分发器：只维护注入的单元列表，逻辑全在 core。
type Router struct {
	core     []*core.Unit // core 逻辑（stable 常驻，experimental 按环境变量开关）
	proto    []*core.Unit // proto-lab 接缝（cli 本地实验位）
	base     string       // 数据目录；空串 → core.BaseDir()（测试可指定临时目录）
	sessions *core.SessionManager
}

// New 用注入的单元列表构建路由。测试传合成列表；生产用 core.Snapshot() + protoLab()。
func New(coreUnits, proto []*core.Unit) *Router {
	return &Router{core: coreUnits, proto: proto}
}

// NewDefault 构建绑定 core 注册表 + 空 proto-lab 的默认路由。
func NewDefault() *Router {
	r := New(core.Snapshot(), protoLab())
	r.sessions = core.NewSessionManager(core.BaseDir(), core.DefaultAdapterRegistry())
	return r
}

// SetBaseDir 指定数据目录（覆盖 core.BaseDir()）；空串恢复默认。
func (r *Router) SetBaseDir(dir string) {
	r.base = dir
	if r.sessions != nil {
		base := dir
		if base == "" {
			base = core.BaseDir()
		}
		r.sessions.SetBaseDir(base)
	}
}

func (r *Router) sessionStore(base string) core.SessionStore {
	if r.sessions == nil {
		return core.NewFileSessionStore(base)
	}
	return r.sessions.Store()
}

// OpenSession 通过 core adapter registry 创建一个 live Workspace session。
func (r *Router) OpenSession(ctx context.Context, req core.SessionRequest) (core.Session, error) {
	if r.sessions == nil {
		base := r.base
		if base == "" {
			base = core.BaseDir()
		}
		r.sessions = core.NewSessionManager(base, core.DefaultAdapterRegistry())
	}
	base := r.base
	if base == "" {
		base = core.BaseDir()
	}
	configPath, _ := core.Paths(base)
	cfg, err := core.LoadConfig(configPath)
	if err != nil {
		return nil, err
	}
	return r.sessions.Open(ctx, cfg, req)
}

func (r *Router) ListSessions() ([]core.SessionRecord, error) {
	base := r.base
	if base == "" {
		base = core.BaseDir()
	}
	return r.sessionStore(base).List()
}

func (r *Router) GetSession(id core.SessionID) (*core.SessionRecord, error) {
	base := r.base
	if base == "" {
		base = core.BaseDir()
	}
	return r.sessionStore(base).Get(id)
}

func (r *Router) DeleteSession(id core.SessionID) error {
	base := r.base
	if base == "" {
		base = core.BaseDir()
	}
	return r.sessionStore(base).Delete(id)
}

func (r *Router) ensureSessions() *core.SessionManager {
	if r.sessions == nil {
		base := r.base
		if base == "" {
			base = core.BaseDir()
		}
		r.sessions = core.NewSessionManager(base, core.DefaultAdapterRegistry())
	}
	return r.sessions
}

func (r *Router) loadConfig() (*core.Config, error) {
	base := r.base
	if base == "" {
		base = core.BaseDir()
	}
	configPath, _ := core.Paths(base)
	return core.LoadConfig(configPath)
}

// ProbeAgent 对单个 agent 做就绪态自检（只握手不建会话，供 TUI 展示可用性）。
func (r *Router) ProbeAgent(ctx context.Context, agentName string) (core.AgentProbe, error) {
	cfg, err := r.loadConfig()
	if err != nil {
		return core.AgentProbe{}, err
	}
	agent := cfg.ResolveAgent(agentName)
	if agent == nil {
		return core.AgentProbe{}, fmt.Errorf("未知 agent %q（可用 list 查看）", agentName)
	}
	return r.ensureSessions().Probe(ctx, agent)
}

// ListRemoteSessions 列出 agent 远端会话（需后端 list 能力）。
func (r *Router) ListRemoteSessions(ctx context.Context, agentName string) ([]core.SessionInfo, error) {
	cfg, err := r.loadConfig()
	if err != nil {
		return nil, err
	}
	agent := cfg.ResolveAgent(agentName)
	if agent == nil {
		return nil, fmt.Errorf("未知 agent %q（可用 list 查看）", agentName)
	}
	return core.ListRemoteSessions(ctx, agent)
}

// DeleteRemoteSession 删除一条远端会话（需后端 delete 能力）。
func (r *Router) DeleteRemoteSession(ctx context.Context, agentName, remoteID string) error {
	cfg, err := r.loadConfig()
	if err != nil {
		return err
	}
	agent := cfg.ResolveAgent(agentName)
	if agent == nil {
		return fmt.Errorf("未知 agent %q（可用 list 查看）", agentName)
	}
	return core.DeleteRemoteSession(ctx, agent, remoteID)
}

// Dispatch 按 argv 路由到逻辑单元并执行。路由顺序：
// core 稳定 → core 实验（AILAUNCHER_EXPERIMENTAL=1 时）→ proto-lab → 详细报错。
// argv 为空视为用法错误。
func (r *Router) Dispatch(argv []string, in io.Reader) (*core.Result, error) {
	if len(argv) == 0 {
		return nil, core.Usagef("未提供命令（可用 help 查看）")
	}
	u := r.lookup(argv[0])
	if u == nil {
		return nil, r.unknownCommand(argv[0])
	}

	base := r.base
	if base == "" {
		base = core.BaseDir()
	}
	configPath, statePath := core.Paths(base)
	cfg, err := core.LoadConfig(configPath)
	if err != nil {
		return nil, err
	}
	st, err := core.LoadState(statePath)
	if err != nil {
		return nil, err
	}
	ctx := &core.Ctx{
		BaseDir:    base,
		ConfigPath: configPath,
		StatePath:  statePath,
		Cfg:        cfg,
		St:         st,
		Sessions:   r.sessionStore(base),
		In:         in,
	}
	return u.Run(ctx, argv[1:])
}

// visibleUnits 返回按路由顺序的可见单元（core 稳定 → core 实验启用 → proto-lab），
// 未暴露 stage（draft/checking/abandoned 及默认 experimental）被过滤。
func (r *Router) visibleUnits() []*core.Unit {
	var out []*core.Unit
	add := func(units []*core.Unit) {
		for _, u := range units {
			if u != nil && u.Stage.Visible() {
				out = append(out, u)
			}
		}
	}
	add(r.core)
	add(r.proto)
	return out
}

func (r *Router) lookup(name string) *core.Unit {
	for _, u := range r.visibleUnits() {
		if u.Name == name {
			return u
		}
	}
	return nil
}

// capabilities 按路由顺序返回可见单元的能力清单（详细报错与 cap 的信息源）。
func (r *Router) capabilities() []core.Capability {
	units := r.visibleUnits()
	out := make([]core.Capability, 0, len(units))
	for _, u := range units {
		out = append(out, core.Capability{
			Name: u.Name, Stage: u.Stage, Kind: u.Kind, Declared: u.Declared,
		})
	}
	return out
}

// unknownError 携带未知命令的详细信息（名字 + 能力清单 + 提示）。
type unknownError struct{ msg string }

func (e *unknownError) Error() string { return e.msg }

func (r *Router) unknownCommand(name string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "未知命令或 agent：%s\n", name)
	caps := r.capabilities()
	if len(caps) == 0 {
		b.WriteString("（当前无可见逻辑单元）")
	} else {
		b.WriteString("可用命令（stage / kind）：\n")
		for _, c := range caps {
			fmt.Fprintf(&b, "  %-12s [%s/%s] %s\n", c.Name, c.Stage, c.Kind, c.Declared)
		}
	}
	b.WriteString("提示：cap 查看能力清单（JSON）；help 查看用法。")
	return &unknownError{msg: b.String()}
}

// IsUnknown 报告错误是否为未知命令（exit 2）。
func IsUnknown(err error) bool {
	var u *unknownError
	return errors.As(err, &u)
}

// ExitCode 计算命令执行应返回的进程退出码：
// nil → 0；未知命令 / 用法错误 → 2；其余运行时错误 → 1。
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	if IsUnknown(err) || errors.Is(err, core.ErrUsage) {
		return 2
	}
	return 1
}

// Format 把逻辑结果格式化为 stdout 字节（Text 原样；JSON 缩进序列化）。
func Format(res *core.Result) ([]byte, error) {
	switch res.Kind {
	case core.ResultJSON:
		b, err := json.MarshalIndent(res.Data, "", "  ")
		if err != nil {
			return nil, err
		}
		return append(b, '\n'), nil
	default:
		return []byte(res.Text + "\n"), nil
	}
}
