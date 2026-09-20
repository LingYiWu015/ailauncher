package core

import (
	"context"
	"time"
)

// SessionID 是 AILauncher 本地会话标识；它与后端协议的会话 ID 分离。
type SessionID string

// SessionStatus 描述宿主可见的会话状态。
type SessionStatus string

const (
	SessionConnecting SessionStatus = "connecting"
	SessionReady      SessionStatus = "ready"
	SessionRunning    SessionStatus = "running"
	SessionIdle       SessionStatus = "idle"
	SessionCancelled  SessionStatus = "cancelled"
	SessionFailed     SessionStatus = "failed"
	SessionClosed     SessionStatus = "closed"
)

// EventKind 是 adapter 输出到 renderer 的最小语义事件集合。
// 纯文本三件套保留兼容；新增 thinking/tool/permission/usage 供 Claude Code 风格 TUI
// 做流式块渲染。老 transcript 行缺失新字段时按 omitempty 零值解码。
type EventKind string

const (
	EventSessionStarted    EventKind = "session_started"
	EventUserText          EventKind = "user_text"
	EventAssistantText     EventKind = "assistant_text"
	EventAssistantThinking EventKind = "assistant_thinking"
	EventToolCall          EventKind = "tool_call"
	EventToolResult        EventKind = "tool_result"
	EventPermission        EventKind = "permission"
	EventUsage             EventKind = "usage"
	EventStatus            EventKind = "status"
	EventError             EventKind = "error"
	EventCommand           EventKind = "command"
	EventMode              EventKind = "mode"
	EventTitle             EventKind = "title"
	EventPlan              EventKind = "plan"
	EventDiff              EventKind = "diff"
	EventElicitation       EventKind = "elicitation"
)

// SessionEvent 不泄漏任一 provider 的私有 wire 类型。Metadata 只能存安全诊断数据。
// Title/Name/Detail/ToolID 是工具块与权限提示的结构化载荷，避免把 JSON 拼进 Text。
// PermID/Options 只用于 live 权限与 elicitation 交互（不写入 transcript）。
type SessionEvent struct {
	SessionID SessionID          `json:"sessionId"`
	Kind      EventKind          `json:"kind"`
	Text      string             `json:"text,omitempty"`
	Status    SessionStatus      `json:"status,omitempty"`
	Error     string             `json:"error,omitempty"`
	Title     string             `json:"title,omitempty"`
	Name      string             `json:"name,omitempty"`
	Detail    string             `json:"detail,omitempty"`
	ToolID    string             `json:"toolId,omitempty"`
	PermID    string             `json:"permId,omitempty"`
	Options   []PermissionOption `json:"options,omitempty"`
	Metadata  map[string]string  `json:"metadata,omitempty"`
	At        time.Time          `json:"at"`
}

// SessionRequest 是创建一个 fresh live session 所需的非敏感参数。
// ModeID/ResumeID 是用户面选项：mode 切换 agent 模式，resume 复用远端会话
// （后端不支持时 Open 明确报错，不静默降级为 fresh）。
type SessionRequest struct {
	AgentName string `json:"agentName"`
	Workdir   string `json:"workdir,omitempty"`
	Title     string `json:"title,omitempty"`
	ModeID    string `json:"modeId,omitempty"`
	ResumeID  string `json:"resumeId,omitempty"`
}

// SessionCapabilities 是握手后 agent 声明的能力快照，供 TUI 决定展示哪些入口
// （如 resume/close/mode/config）。不支持的能力不展示、不调用。
type SessionCapabilities struct {
	LoadSession   bool `json:"loadSession"`
	ListSessions  bool `json:"listSessions"`
	ResumeSession bool `json:"resumeSession"`
	CloseSession  bool `json:"closeSession"`
	DeleteSession bool `json:"deleteSession"`
	ForkSession   bool `json:"forkSession"`
	HasModes      bool `json:"hasModes"`
	HasConfig     bool `json:"hasConfig"`
}

// SessionMode 是 agent 声明的可切换模式（如 ask/architect/code）。
type SessionMode struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Desc string `json:"desc,omitempty"`
}

// SessionConfigOption 是下发的会话配置项（如下拉/开关），TUI 只读展示+透传设置。
type SessionConfigOption struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Desc    string   `json:"desc,omitempty"`
	Kind    string   `json:"kind"`
	Value   string   `json:"value,omitempty"`
	Options []string `json:"options,omitempty"`
}

// SessionUsage 是上下文/费用水位，供状态栏展示。
type SessionUsage struct {
	Used  uint64 `json:"used"`
	Size  uint64 `json:"size"`
	Cost  string `json:"cost,omitempty"`
	Title string `json:"title,omitempty"`
}

// PermissionOption 是 permission 请求里用户可选的一项。
type PermissionOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// SessionInfo 是 agent 远端会话列表的一项（session/list）。
type SessionInfo struct {
	ID      string `json:"id"`
	Title   string `json:"title,omitempty"`
	Cwd     string `json:"cwd,omitempty"`
	Updated string `json:"updated,omitempty"`
}

// Session 是前端消费的 live session。每个 session 同时只允许一个 Send。
type Session interface {
	ID() SessionID
	RemoteID() string
	Capabilities() SessionCapabilities
	Modes() []SessionMode
	CurrentMode() string
	ConfigOptions() []SessionConfigOption
	Usage() SessionUsage
	Events() <-chan SessionEvent
	Send(context.Context, string) error
	Cancel(context.Context) error
	SetMode(context.Context, string) error
	SetConfig(context.Context, string, string) error
	LoadRemote(context.Context, string) error
	// DecidePermission 回应一次交互式权限请求：optionID 为后端 options 中的 optionId；
	// 空串表示拒绝（优先选 reject 类选项，无则按 cancelled 回应）。
	DecidePermission(ctx context.Context, permID, optionID string) error
	// DecideElicitation 回应一次结构化输入请求：content 为空表示拒绝。
	DecideElicitation(ctx context.Context, permID string, content map[string]string) error
	// AvailableCommands 返回 agent 下发的可用命令（TUI /command 菜单用）。
	AvailableCommands() []AvailableCommand
	// SessionTitle 返回 agent 下发的会话标题（可为空）。
	SessionTitle() string
	Close() error
}

// AgentProbe 是 TUI 就绪态自检的结果：一次 initialize 握手能拿到的全部信息。
// Reachable=false 时 Error 告诉用户怎么修（如登录、安装依赖），而不是静默摆设。
type AgentProbe struct {
	Agent        string              `json:"agent"`
	Display      string              `json:"display,omitempty"`
	Reachable    bool                `json:"reachable"`
	Error        string              `json:"error,omitempty"`
	Protocol     int                 `json:"protocol,omitempty"`
	AgentName    string              `json:"agentName,omitempty"`
	AgentVersion string              `json:"agentVersion,omitempty"`
	Capabilities SessionCapabilities `json:"capabilities"`
	Modes        []SessionMode       `json:"modes,omitempty"`
	AuthRequired bool                `json:"authRequired,omitempty"`
	AuthHint     string              `json:"authHint,omitempty"`
	CommandLine  string              `json:"commandLine,omitempty"`
}

// SessionAdapter 把一个 agent 的私有后端协议映射为统一 Session。
type SessionAdapter interface {
	Kind() string
	Open(context.Context, *Agent, AdapterSpec, SessionRequest) (Session, error)
}
