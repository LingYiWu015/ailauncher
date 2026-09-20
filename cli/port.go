package cli

import (
	"context"
	"io"

	"core"
)

// 值类型别名：前端（TUI/未来 GUI）只经 cli 接触 core 数据模型，不直接 import core。
// cli 是逻辑层的唯一前端面——这些别名让 ui 以最小耦合获得数据模型（仍是同一类型，零拷贝）。
type (
	Config              = core.Config
	Agent               = core.Agent
	AgentState          = core.AgentState
	Result              = core.Result
	SessionID           = core.SessionID
	Session             = core.Session
	SessionRequest      = core.SessionRequest
	SessionRecord       = core.SessionRecord
	SessionEvent        = core.SessionEvent
	StoredEvent         = core.StoredEvent
	SessionStatus       = core.SessionStatus
	SessionCapabilities = core.SessionCapabilities
	SessionMode         = core.SessionMode
	SessionConfigOption = core.SessionConfigOption
	SessionUsage        = core.SessionUsage
	SessionInfo         = core.SessionInfo
	PermissionOption    = core.PermissionOption
	AvailableCommand    = core.AvailableCommand
	AgentProbe          = core.AgentProbe
	AgentSummary        = core.AgentSummary
	EventKind           = core.EventKind
)

const (
	EventUserText          = core.EventUserText
	EventAssistantText     = core.EventAssistantText
	EventAssistantThinking = core.EventAssistantThinking
	EventToolCall          = core.EventToolCall
	EventToolResult        = core.EventToolResult
	EventPermission        = core.EventPermission
	EventUsage             = core.EventUsage
	EventStatus            = core.EventStatus
	EventError             = core.EventError
	EventCommand           = core.EventCommand
	EventMode              = core.EventMode
	EventTitle             = core.EventTitle
	EventPlan              = core.EventPlan
	EventDiff              = core.EventDiff
	EventElicitation       = core.EventElicitation
)

const (
	SessionConnecting = core.SessionConnecting
	SessionReady      = core.SessionReady
	SessionRunning    = core.SessionRunning
	SessionIdle       = core.SessionIdle
	SessionCancelled  = core.SessionCancelled
	SessionFailed     = core.SessionFailed
	SessionClosed     = core.SessionClosed
)

// Port 是逻辑端口：前端持有它完成所有逻辑（config/state/run/resolve……），
// 本包不持有逻辑，只按 argv 路由到 core（按 stage 过滤）→ proto-lab → 详细报错。
// *Router 满足本接口；前端测试注入桩，逻辑单测无需 core。
type Port interface {
	// Dispatch 按 argv 路由到逻辑单元并执行；in 供需要 stdin 的单元
	// （state put 读 JSON）使用，无 stdin 需求传 nil。
	Dispatch(argv []string, in io.Reader) (*Result, error)
}

// SessionPort 是 Workspace 的可选 live 会话供应端口。它不扩展 Port，
// 保证既有 launcher TUI 和测试桩不必实现实时会话能力。
type SessionPort interface {
	Port
	OpenSession(ctx context.Context, req SessionRequest) (Session, error)
	ProbeAgent(ctx context.Context, agentName string) (AgentProbe, error)
	ListSessions() ([]SessionRecord, error)
	GetSession(id SessionID) (*SessionRecord, error)
	DeleteSession(id SessionID) error
	ListRemoteSessions(ctx context.Context, agentName string) ([]SessionInfo, error)
	DeleteRemoteSession(ctx context.Context, agentName, remoteID string) error
}

// Text / JSON 构造前端可读的结果值（等价 core.Text / core.JSON），
// 供测试桩与未来前端面构造 Result，不必触及 core 包。
func Text(s string) *Result { return core.Text(s) }

func JSON(data any) *Result { return core.JSON(data) }
