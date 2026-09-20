package core

import (
	"context"
	"fmt"
	"sync"
)

// SessionManager 负责 live adapter 与本地 transcript 的顺序一致性。
type SessionManager struct {
	base     string
	registry *AdapterRegistry
	store    SessionStore
}

func NewSessionManager(base string, registry *AdapterRegistry) *SessionManager {
	if registry == nil {
		registry = DefaultAdapterRegistry()
	}
	return &SessionManager{base: base, registry: registry, store: NewFileSessionStore(base)}
}

func (m *SessionManager) SetBaseDir(base string) {
	if m == nil {
		return
	}
	m.base = base
	m.store = NewFileSessionStore(base)
}

func (m *SessionManager) Store() SessionStore {
	if m == nil {
		return nil
	}
	return m.store
}

func (m *SessionManager) Open(ctx context.Context, cfg *Config, req SessionRequest) (Session, error) {
	if m == nil || m.registry == nil || m.store == nil {
		return nil, fmt.Errorf("workspace session manager 未初始化")
	}
	if cfg == nil {
		return nil, fmt.Errorf("未加载配置")
	}
	agent := cfg.ResolveAgent(req.AgentName)
	if agent == nil {
		return nil, fmt.Errorf("未知 agent %q（可用 list 查看）", req.AgentName)
	}
	live, spec, err := m.registry.Open(ctx, agent, req)
	if err != nil {
		return nil, err
	}
	record := SessionRecord{ID: live.ID(), Agent: agent.Name, Adapter: spec.Kind, Workdir: req.Workdir, Title: req.Title, RemoteID: live.RemoteID(), Mode: live.CurrentMode(), Status: SessionReady}
	if err := m.store.Create(record); err != nil {
		_ = live.Close()
		return nil, fmt.Errorf("创建本地 session transcript 失败：%w", err)
	}
	return newManagedSession(live, m.store), nil
}

// Probe 对单个 agent 做就绪态自检（只握手不建会话）：显式配置优先，缺失试内置推导。
func (m *SessionManager) Probe(ctx context.Context, agent *Agent) (AgentProbe, error) {
	if m == nil || m.registry == nil {
		return AgentProbe{}, fmt.Errorf("workspace session manager 未初始化")
	}
	if agent == nil {
		return AgentProbe{}, fmt.Errorf("未指定 agent")
	}
	spec, _, err := EffectiveAdapter(agent)
	if err != nil {
		return AgentProbe{Agent: agent.Name, Display: displayOrName(agent), Error: err.Error(), CommandLine: ""}, nil
	}
	return ProbeAgent(ctx, agent, spec), nil
}

type managedSession struct {
	live   Session
	store  SessionStore
	events chan SessionEvent
	done   chan struct{}
	sendMu sync.Mutex
	once   sync.Once
}

func newManagedSession(live Session, store SessionStore) *managedSession {
	s := &managedSession{live: live, store: store, events: make(chan SessionEvent, 128), done: make(chan struct{})}
	go s.pump()
	return s
}

func (s *managedSession) ID() SessionID { return s.live.ID() }
func (s *managedSession) RemoteID() string {
	if s == nil || s.live == nil {
		return ""
	}
	return s.live.RemoteID()
}
func (s *managedSession) Capabilities() SessionCapabilities {
	if s == nil || s.live == nil {
		return SessionCapabilities{}
	}
	return s.live.Capabilities()
}
func (s *managedSession) Modes() []SessionMode {
	if s == nil || s.live == nil {
		return nil
	}
	return s.live.Modes()
}
func (s *managedSession) CurrentMode() string {
	if s == nil || s.live == nil {
		return ""
	}
	return s.live.CurrentMode()
}
func (s *managedSession) ConfigOptions() []SessionConfigOption {
	if s == nil || s.live == nil {
		return nil
	}
	return s.live.ConfigOptions()
}
func (s *managedSession) Usage() SessionUsage {
	if s == nil || s.live == nil {
		return SessionUsage{}
	}
	return s.live.Usage()
}
func (s *managedSession) Events() <-chan SessionEvent { return s.events }

func (s *managedSession) Send(ctx context.Context, text string) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	// The manager serializes callers, so a second user message cannot race adapter busy
	// admission. Persist before the backend can publish a synchronous assistant event.
	e := SessionEvent{SessionID: s.ID(), Kind: EventUserText, Text: text, Status: SessionRunning}
	if err := s.store.Append(s.ID(), e); err != nil {
		return fmt.Errorf("保存用户消息失败：%w", err)
	}
	if err := s.live.Send(ctx, text); err != nil {
		return err
	}
	return nil
}

func (s *managedSession) Cancel(ctx context.Context) error {
	if s == nil || s.live == nil {
		return nil
	}
	return s.live.Cancel(ctx)
}

func (s *managedSession) SetMode(ctx context.Context, modeID string) error {
	if s == nil || s.live == nil {
		return fmt.Errorf("会话未初始化")
	}
	return s.live.SetMode(ctx, modeID)
}

func (s *managedSession) SetConfig(ctx context.Context, configID, value string) error {
	if s == nil || s.live == nil {
		return fmt.Errorf("会话未初始化")
	}
	return s.live.SetConfig(ctx, configID, value)
}

func (s *managedSession) LoadRemote(ctx context.Context, remoteID string) error {
	if s == nil || s.live == nil {
		return fmt.Errorf("会话未初始化")
	}
	return s.live.LoadRemote(ctx, remoteID)
}

func (s *managedSession) DecidePermission(ctx context.Context, permID, optionID string) error {
	if s == nil || s.live == nil {
		return fmt.Errorf("会话未初始化")
	}
	return s.live.DecidePermission(ctx, permID, optionID)
}

func (s *managedSession) DecideElicitation(ctx context.Context, permID string, content map[string]string) error {
	if s == nil || s.live == nil {
		return fmt.Errorf("会话未初始化")
	}
	return s.live.DecideElicitation(ctx, permID, content)
}

func (s *managedSession) AvailableCommands() []AvailableCommand {
	if s == nil || s.live == nil {
		return nil
	}
	return s.live.AvailableCommands()
}

func (s *managedSession) SessionTitle() string {
	if s == nil || s.live == nil {
		return ""
	}
	return s.live.SessionTitle()
}

func (s *managedSession) Close() error {
	var err error
	s.once.Do(func() {
		err = s.live.Close()
		close(s.done)
	})
	return err
}

func (s *managedSession) pump() {
	defer close(s.events)
	for event := range s.live.Events() {
		// The adapter may emit user text too; manager owns the durable user event to avoid duplicate rows.
		if event.Kind == EventUserText {
			continue
		}
		if err := s.store.Append(s.ID(), event); err != nil {
			event = SessionEvent{SessionID: s.ID(), Kind: EventError, Status: SessionFailed, Error: "保存 session 事件失败: " + err.Error()}
		} else {
			s.syncMeta(event)
		}
		select {
		case s.events <- event:
		case <-s.done:
			return
		}
	}
}

// syncMeta 把标题/用量/模式等元数据同步到 transcript 记录头（列表页直接可读）。
func (s *managedSession) syncMeta(event SessionEvent) {
	var patch SessionPatch
	dirty := false
	if event.Kind == EventTitle && event.Text != "" {
		patch.Title = &event.Text
		dirty = true
	}
	if event.Kind == EventMode && event.Text != "" {
		patch.Mode = &event.Text
		dirty = true
	}
	if event.Kind == EventUsage && s.live != nil {
		u := s.live.Usage()
		patch.Usage = &u
		dirty = true
	}
	if dirty {
		_ = s.store.Update(s.ID(), patch)
	}
}
