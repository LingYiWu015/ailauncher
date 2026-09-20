package core

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	acpProtocolVersion = 1
	acpMaxLineBytes    = 1 << 20
	acpMaxStderrBytes  = 16 << 10
	acpOpenTimeout     = 30 * time.Second
	acpCloseTimeout    = 5 * time.Second
	acpFSMaxBytes      = 1 << 20
)

type acpAdapter struct{}

func newACPAdapter() SessionAdapter { return acpAdapter{} }
func (acpAdapter) Kind() string     { return "acp" }

func (acpAdapter) Open(ctx context.Context, agent *Agent, spec AdapterSpec, req SessionRequest) (Session, error) {
	wd := req.Workdir
	if wd == "" {
		wd = spec.Cwd
	}
	if wd == "" {
		wd = expandWorkdir(agent, "")
	}
	abs, err := filepath.Abs(wd)
	if err != nil {
		return nil, fmt.Errorf("解析 workspace 目录失败：%w", err)
	}
	if !dirExists(abs) {
		return nil, fmt.Errorf("workspace 目录不存在：%s", abs)
	}

	ctx, cancel := context.WithTimeout(ctx, acpOpenTimeout)
	defer cancel()

	cmd := exec.Command(spec.Command, spec.Args...)
	cmd.Dir = spec.Cwd
	if cmd.Dir == "" {
		cmd.Dir = abs
	}
	cmd.Env = append(os.Environ(), envPairs(spec.Env)...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("创建 ACP stdin 失败：%w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("创建 ACP stdout 失败：%w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("创建 ACP stderr 失败：%w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动 ACP adapter %q 失败：%w", spec.Command, err)
	}

	s := &acpSession{
		id:          SessionID(newLocalSessionID()),
		cmd:         cmd,
		stdin:       stdin,
		pending:     map[uint64]chan acpResponse{},
		pendingPerm: map[string]*pendingPerm{},
		events:      make(chan SessionEvent, 128),
		permission:  spec.Permission,
		done:        make(chan struct{}),
	}
	go s.readStderr(stderr)
	go s.readLoop(stdout)
	go s.waitLoop()

	initResult, err := s.request(ctx, "initialize", map[string]any{
		"protocolVersion":    acpProtocolVersion,
		"clientCapabilities": map[string]any{"fs": map[string]any{"readTextFile": true, "writeTextFile": true}},
		"clientInfo":         map[string]any{"name": "AILauncher", "version": "0.4.0-dev"},
	})
	if err != nil {
		s.Close()
		return nil, fmt.Errorf("ACP initialize 失败：%w", err)
	}
	s.applyInit(initResult, agent)

	if req.ResumeID != "" {
		if err := s.loadRemoteLocked(ctx, abs, req.ResumeID); err != nil {
			s.Close()
			return nil, err
		}
	} else {
		result, err := s.request(ctx, "session/new", map[string]any{"cwd": abs, "mcpServers": []any{}})
		if err != nil {
			s.Close()
			if isAuthRequired(err) {
				return nil, fmt.Errorf("ACP 需要登录：%s", s.authHintText())
			}
			return nil, fmt.Errorf("ACP session/new 失败：%w", err)
		}
		var created struct {
			SessionID     string             `json:"sessionId"`
			Modes         *sessionModesWire  `json:"modes"`
			ConfigOptions []configOptionWire `json:"configOptions"`
		}
		if err := json.Unmarshal(result, &created); err != nil || created.SessionID == "" {
			s.Close()
			if err != nil {
				return nil, fmt.Errorf("ACP session/new 返回无效：%w", err)
			}
			return nil, fmt.Errorf("ACP session/new 未返回 sessionId")
		}
		s.remoteID = created.SessionID
		s.applyModes(created.Modes)
		s.applyConfigOptions(created.ConfigOptions)
	}
	if req.ModeID != "" {
		if err := s.SetMode(ctx, req.ModeID); err != nil {
			s.Close()
			return nil, err
		}
	}
	s.emit(EventSessionStarted, "", SessionReady, "", map[string]string{"adapter": "acp", "agent": agent.Name})
	return s, nil
}

func isAuthRequired(err error) bool {
	if err == nil {
		return false
	}
	var rpcErr *acpError
	if as := asACPError(err, &rpcErr); as && rpcErr.Code == -32000 {
		return true
	}
	return strings.Contains(err.Error(), "auth")
}

type acpError struct {
	Code    int
	Message string
}

func (e *acpError) Error() string { return fmt.Sprintf("[%d] %s", e.Code, e.Message) }

func asACPError(err error, target **acpError) bool {
	for err != nil {
		if e, ok := err.(*acpError); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

type authMethodWire struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Desc string `json:"description"`
}

type sessionModesWire struct {
	Current   string `json:"currentModeId"`
	Available []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Desc string `json:"description"`
	} `json:"availableModes"`
}

type configOptionWire struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Desc    string `json:"description"`
	Kind    string `json:"type"`
	Value   string `json:"currentValue"`
	Options []struct {
		Value string `json:"value"`
		Name  string `json:"name"`
		Desc  string `json:"description"`
	} `json:"options"`
}

type acpResponse struct {
	result json.RawMessage
	err    error
}

type pendingPerm struct {
	id        string
	options   []PermissionOption
	toolCall  json.RawMessage
	responded bool
}

type acpSession struct {
	id         SessionID
	remoteID   string
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	permission string

	mu          sync.Mutex
	writeMu     sync.Mutex
	emitMu      sync.Mutex
	nextID      uint64
	nextTerm    uint64
	pending     map[uint64]chan acpResponse
	pendingPerm map[string]*pendingPerm
	terminals   map[string]*acpTerminal
	busy        bool
	closed      bool
	finished    bool
	events      chan SessionEvent
	done        chan struct{}
	once        sync.Once
	endOnce     sync.Once

	caps      SessionCapabilities
	agentName string
	agentVer  string
	protocol  int
	authHint  string
	authReq   bool
	modes     []SessionMode
	curMode   string
	configs   []SessionConfigOption
	usage     SessionUsage
	commands  []AvailableCommand
	title     string
}

func (s *acpSession) ID() SessionID               { return s.id }
func (s *acpSession) RemoteID() string            { return s.remoteID }
func (s *acpSession) Events() <-chan SessionEvent { return s.events }

func (s *acpSession) Capabilities() SessionCapabilities {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.caps
}

func (s *acpSession) Modes() []SessionMode {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]SessionMode(nil), s.modes...)
}

func (s *acpSession) CurrentMode() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.curMode
}

func (s *acpSession) ConfigOptions() []SessionConfigOption {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]SessionConfigOption(nil), s.configs...)
}

func (s *acpSession) Usage() SessionUsage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.usage
}

// AvailableCommands 返回 agent 下发的可用命令（/command 入口用）。
func (s *acpSession) AvailableCommands() []AvailableCommand {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]AvailableCommand(nil), s.commands...)
}

func (s *acpSession) SessionTitle() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.title
}

// applyInit 解析 initialize 结果：协议版本、能力、auth 方法、agent 信息。
func (s *acpSession) applyInit(result json.RawMessage, agent *Agent) {
	var init struct {
		Protocol int `json:"protocolVersion"`
		Caps     struct {
			LoadSession *bool `json:"loadSession"`
			SessionCaps *struct {
				List   *struct{} `json:"list"`
				Resume *struct{} `json:"resume"`
				Close  *struct{} `json:"close"`
				Delete *struct{} `json:"delete"`
				Fork   *struct{} `json:"fork"`
			} `json:"sessionCapabilities"`
		} `json:"agentCapabilities"`
		Auth []authMethodWire `json:"authMethods"`
		Info *struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"agentInfo"`
	}
	if json.Unmarshal(result, &init) != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.protocol = init.Protocol
	if init.Caps.LoadSession != nil {
		s.caps.LoadSession = *init.Caps.LoadSession
	}
	if sc := init.Caps.SessionCaps; sc != nil {
		s.caps.ListSessions = sc.List != nil
		s.caps.ResumeSession = sc.Resume != nil
		s.caps.CloseSession = sc.Close != nil
		s.caps.DeleteSession = sc.Delete != nil
		s.caps.ForkSession = sc.Fork != nil
	}
	if init.Info != nil {
		s.agentName, s.agentVer = init.Info.Name, init.Info.Version
	}
	for _, m := range init.Auth {
		if s.authHint == "" {
			s.authHint = m.Name
			if m.Desc != "" {
				s.authHint += "：" + m.Desc
			}
		} else {
			s.authHint += " / " + m.Name
		}
	}
	_ = agent
}

func (s *acpSession) authHintText() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.authHint != "" {
		return s.authHint
	}
	return "请先在终端完成登录"
}

func (s *acpSession) applyModes(modes *sessionModesWire) {
	if modes == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.curMode = modes.Current
	s.modes = s.modes[:0]
	for _, m := range modes.Available {
		s.modes = append(s.modes, SessionMode{ID: m.ID, Name: m.Name, Desc: m.Desc})
	}
	if len(s.modes) > 0 {
		s.caps.HasModes = true
	}
}

func (s *acpSession) applyConfigOptions(opts []configOptionWire) {
	if len(opts) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.configs = s.configs[:0]
	for _, o := range opts {
		opt := SessionConfigOption{ID: o.ID, Name: o.Name, Desc: o.Desc, Kind: o.Kind, Value: o.Value}
		if opt.Kind == "" {
			opt.Kind = "select"
		}
		for _, v := range o.Options {
			label := v.Name
			if label == "" {
				label = v.Value
			}
			if v.Desc != "" {
				label += "（" + v.Desc + "）"
			}
			opt.Options = append(opt.Options, v.Value+"|"+label)
		}
		s.configs = append(s.configs, opt)
	}
	s.caps.HasConfig = len(s.configs) > 0
}

func (s *acpSession) Send(ctx context.Context, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("不能发送空消息")
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("会话已关闭")
	}
	if s.busy {
		s.mu.Unlock()
		return fmt.Errorf("当前 prompt 尚未完成")
	}
	s.busy = true
	remoteID := s.remoteID
	s.mu.Unlock()

	s.emit(EventUserText, text, SessionRunning, "", nil)
	s.emit(EventStatus, "", SessionRunning, "", nil)
	promptDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			// A caller cancellation must also cancel the remote turn. Keep busy until its
			// prompt response arrives, so another Send cannot overlap it.
			_ = s.Cancel(context.Background())
		case <-promptDone:
		case <-s.done:
		}
	}()
	go func() {
		defer close(promptDone)
		result, err := s.request(context.Background(), "session/prompt", map[string]any{
			"sessionId": remoteID,
			"prompt":    []map[string]string{{"type": "text", "text": text}},
		})
		s.mu.Lock()
		s.busy = false
		closed := s.closed
		s.mu.Unlock()
		if err != nil {
			if !closed {
				s.emit(EventError, "", SessionFailed, "ACP prompt 失败: "+err.Error(), nil)
			}
			return
		}
		var response struct {
			StopReason string `json:"stopReason"`
		}
		_ = json.Unmarshal(result, &response)
		status := SessionIdle
		if response.StopReason == "cancelled" {
			status = SessionCancelled
		}
		s.emit(EventStatus, "", status, "", map[string]string{"stopReason": response.StopReason})
	}()
	return nil
}

func (s *acpSession) Cancel(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	remoteID, busy := s.remoteID, s.busy
	s.mu.Unlock()
	// 协议要求：cancel 时所有挂起的 permission 按 cancelled 回应。
	s.cancelPendingPermissions()
	if !busy {
		return nil
	}
	if err := s.notify(ctx, "session/cancel", map[string]any{"sessionId": remoteID}); err != nil {
		return fmt.Errorf("ACP cancel 失败：%w", err)
	}
	s.emit(EventStatus, "", SessionCancelled, "", nil)
	return nil
}

// SetMode 切换 agent 模式（如下拉里的 build/plan）。后端不支持时明确报错。
func (s *acpSession) SetMode(ctx context.Context, modeID string) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("会话已关闭")
	}
	remoteID := s.remoteID
	s.mu.Unlock()
	if modeID == "" {
		return fmt.Errorf("模式不能为空")
	}
	if _, err := s.request(ctx, "session/set_mode", map[string]any{"sessionId": remoteID, "modeId": modeID}); err != nil {
		return fmt.Errorf("切换模式失败：%w", err)
	}
	s.mu.Lock()
	s.curMode = modeID
	s.mu.Unlock()
	s.emit(EventMode, modeID, SessionRunning, "", nil)
	return nil
}

// SetConfig 设置会话配置项（如 model 下拉）。value 透传给后端。
func (s *acpSession) SetConfig(ctx context.Context, configID, value string) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("会话已关闭")
	}
	remoteID := s.remoteID
	s.mu.Unlock()
	if configID == "" {
		return fmt.Errorf("配置项不能为空")
	}
	result, err := s.request(ctx, "session/set_config_option", map[string]any{"sessionId": remoteID, "configId": configID, "value": value})
	if err != nil {
		return fmt.Errorf("设置配置失败：%w", err)
	}
	var updated struct {
		Options []configOptionWire `json:"configOptions"`
	}
	if json.Unmarshal(result, &updated) == nil && len(updated.Options) > 0 {
		s.applyConfigOptions(updated.Options)
	}
	return nil
}

// LoadRemote 用远端会话 ID 恢复上下文（resume 不回放历史，load 回放全文）。
// 后端不支持时明确报错，TUI 不展示该入口。
func (s *acpSession) LoadRemote(ctx context.Context, remoteID string) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("会话已关闭")
	}
	s.mu.Unlock()
	if remoteID == "" {
		return fmt.Errorf("远端会话 ID 不能为空")
	}
	return s.loadRemoteLocked(ctx, "", remoteID)
}

func (s *acpSession) loadRemoteLocked(ctx context.Context, abs, remoteID string) error {
	caps := s.Capabilities()
	method := ""
	if caps.ResumeSession {
		method = "session/resume"
	} else if caps.LoadSession {
		method = "session/load"
	} else {
		return fmt.Errorf("后端不支持恢复会话（无 resume/load 能力）")
	}
	params := map[string]any{"sessionId": remoteID, "mcpServers": []any{}}
	if method == "session/load" || method == "session/resume" {
		if abs == "" {
			s.mu.Lock()
			abs = s.workdir()
			s.mu.Unlock()
		}
		if abs != "" {
			params["cwd"] = abs
		}
	}
	result, err := s.request(ctx, method, params)
	if err != nil {
		return fmt.Errorf("恢复远端会话失败：%w", err)
	}
	var out struct {
		SessionID     string             `json:"sessionId"`
		Modes         *sessionModesWire  `json:"modes"`
		ConfigOptions []configOptionWire `json:"configOptions"`
	}
	if json.Unmarshal(result, &out) == nil {
		if out.SessionID != "" {
			s.mu.Lock()
			s.remoteID = out.SessionID
			s.mu.Unlock()
		}
		s.applyModes(out.Modes)
		s.applyConfigOptions(out.ConfigOptions)
	}
	s.emit(EventStatus, "", SessionReady, "", map[string]string{"resumed": remoteID, "method": method})
	return nil
}

// DecidePermission 回应一次交互式权限请求。optionID 为空表示拒绝。
func (s *acpSession) DecidePermission(ctx context.Context, permID, optionID string) error {
	s.mu.Lock()
	p := s.pendingPerm[permID]
	if p != nil && p.responded {
		p = nil
	}
	s.mu.Unlock()
	if p == nil {
		return fmt.Errorf("权限请求已处理或不存在：%s", permID)
	}
	outcome := map[string]any{"outcome": "cancelled"}
	if optionID != "" {
		outcome = map[string]any{"outcome": "selected", "optionId": optionID}
	} else {
		for _, o := range p.options {
			if strings.HasPrefix(o.Kind, "reject") {
				outcome = map[string]any{"outcome": "selected", "optionId": o.ID}
				break
			}
		}
	}
	s.mu.Lock()
	if pp := s.pendingPerm[permID]; pp != nil {
		pp.responded = true
		delete(s.pendingPerm, permID)
	}
	s.mu.Unlock()
	var rawID json.RawMessage
	if json.Unmarshal([]byte(permID), &rawID) != nil {
		rawID = json.RawMessage(`"` + permID + `"`)
	}
	if err := s.sendFrame(map[string]any{"jsonrpc": "2.0", "id": rawID, "result": outcome}); err != nil {
		return fmt.Errorf("回应权限请求失败：%w", err)
	}
	return nil
}

func (s *acpSession) workdir() string {
	if s.cmd != nil && s.cmd.Dir != "" {
		return s.cmd.Dir
	}
	return ""
}

func (s *acpSession) Close() error {
	var closeErr error
	s.once.Do(func() {
		s.mu.Lock()
		s.closed = true
		stdin := s.stdin
		cmd := s.cmd
		remoteID := s.remoteID
		canClose := s.caps.CloseSession
		for _, ch := range s.pending {
			ch <- acpResponse{err: fmt.Errorf("会话已关闭")}
			close(ch)
		}
		s.pending = map[uint64]chan acpResponse{}
		for id := range s.pendingPerm {
			delete(s.pendingPerm, id)
		}
		s.mu.Unlock()
		if canClose && remoteID != "" {
			ctx, cancel := context.WithTimeout(context.Background(), acpCloseTimeout)
			_ = s.notify(ctx, "session/close", map[string]any{"sessionId": remoteID})
			cancel()
		}
		if stdin != nil {
			_ = stdin.Close()
		}
		if cmd != nil && cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		s.emit(EventStatus, "", SessionClosed, "", nil)
		s.finish()
	})
	return closeErr
}

func (s *acpSession) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, fmt.Errorf("会话已关闭")
	}
	s.nextID++
	id := s.nextID
	ch := make(chan acpResponse, 1)
	s.pending[id] = ch
	s.mu.Unlock()
	if err := s.sendFrame(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		s.removePending(id)
		return nil, err
	}
	select {
	case response, ok := <-ch:
		if !ok {
			return nil, fmt.Errorf("ACP 连接已关闭")
		}
		return response.result, response.err
	case <-ctx.Done():
		s.removePending(id)
		return nil, ctx.Err()
	case <-s.done:
		return nil, fmt.Errorf("ACP 连接已关闭")
	}
}

func (s *acpSession) failPending(id uint64, err error) {
	s.mu.Lock()
	ch := s.pending[id]
	delete(s.pending, id)
	s.mu.Unlock()
	if ch != nil {
		ch <- acpResponse{err: err}
		close(ch)
	}
}

func (s *acpSession) notify(ctx context.Context, method string, params any) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	return s.sendFrame(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (s *acpSession) sendFrame(frame any) error {
	b, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.mu.Lock()
	if s.closed || s.stdin == nil {
		s.mu.Unlock()
		return fmt.Errorf("ACP stdin 已关闭")
	}
	stdin := s.stdin
	s.mu.Unlock()
	_, err = stdin.Write(append(b, '\n'))
	return err
}

func (s *acpSession) removePending(id uint64) {
	s.mu.Lock()
	delete(s.pending, id)
	s.mu.Unlock()
}

func (s *acpSession) readLoop(stdout io.Reader) {
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 4096), acpMaxLineBytes)
	for sc.Scan() {
		var frame struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(sc.Bytes(), &frame); err != nil {
			s.abortProtocol("ACP stdout 包含无效 JSON-RPC: " + err.Error())
			return
		}
		if len(frame.ID) > 0 && string(frame.ID) != "null" {
			var numericID uint64
			if json.Unmarshal(frame.ID, &numericID) == nil {
				s.mu.Lock()
				ch := s.pending[numericID]
				delete(s.pending, numericID)
				s.mu.Unlock()
				if ch != nil {
					if frame.Error != nil {
						ch <- acpResponse{err: &acpError{Code: frame.Error.Code, Message: frame.Error.Message}}
					} else {
						ch <- acpResponse{result: frame.Result}
					}
					close(ch)
					continue
				}
			}
			s.handleInboundRequest(frame.ID, frame.Method, frame.Params)
			continue
		}
		s.handleNotification(frame.Method, frame.Params)
	}
	if err := sc.Err(); err != nil {
		s.abortProtocol("读取 ACP stdout 失败: " + err.Error())
		return
	}
	if s.hasPending() {
		s.abortProtocol("ACP stdout 已关闭")
	}
}

func (s *acpSession) hasPending() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.pending) > 0
}
func (s *acpSession) abortProtocol(message string) {
	s.emit(EventError, "", SessionFailed, message, nil)
	s.mu.Lock()
	for _, ch := range s.pending {
		ch <- acpResponse{err: fmt.Errorf("%s", message)}
		close(ch)
	}
	s.pending = map[uint64]chan acpResponse{}
	cmd := s.cmd
	s.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

func (s *acpSession) handleNotification(method string, params json.RawMessage) {
	if method != "session/update" {
		return
	}
	var envelope struct {
		SessionID string          `json:"sessionId"`
		Update    json.RawMessage `json:"update"`
	}
	if json.Unmarshal(params, &envelope) != nil || envelope.SessionID != s.remoteID || len(envelope.Update) == 0 {
		return
	}
	var header struct {
		SessionUpdate string `json:"sessionUpdate"`
	}
	if json.Unmarshal(envelope.Update, &header) != nil {
		return
	}
	switch header.SessionUpdate {
	case "agent_message_chunk", "agent_thought_chunk", "user_message_chunk":
		text, _ := contentText(envelope.Update)
		if text == "" {
			return
		}
		kind := EventAssistantText
		if header.SessionUpdate == "agent_thought_chunk" {
			kind = EventAssistantThinking
		}
		if header.SessionUpdate == "user_message_chunk" {
			kind = EventUserText
		}
		s.emit(kind, text, SessionRunning, "", nil)
	case "tool_call":
		s.handleToolCall(envelope.Update, false)
	case "tool_call_update":
		s.handleToolCall(envelope.Update, true)
	case "plan":
		if entries := planText(envelope.Update); entries != "" {
			s.emit(EventPlan, entries, SessionRunning, "", nil)
		}
	case "available_commands_update":
		if cmds := availableCommands(envelope.Update); len(cmds) > 0 {
			s.mu.Lock()
			s.commands = cmds
			s.mu.Unlock()
			var names []string
			for _, c := range cmds {
				names = append(names, c.Name)
			}
			s.emit(EventCommand, strings.Join(names, ", "), SessionRunning, "", nil)
		}
	case "current_mode_update":
		if modeID := strField(envelope.Update, "currentModeId"); modeID != "" {
			s.mu.Lock()
			s.curMode = modeID
			s.mu.Unlock()
			s.emit(EventMode, modeID, SessionRunning, "", nil)
		}
	case "session_mode_update":
		if text := textOf(envelope.Update, "text"); text != "" {
			s.emit(EventMode, text, SessionRunning, "", nil)
		}
	case "config_option_update":
		var updated struct {
			Options []configOptionWire `json:"configOptions"`
		}
		if json.Unmarshal(envelope.Update, &updated) == nil && len(updated.Options) > 0 {
			s.applyConfigOptions(updated.Options)
			s.emit(EventUsage, "配置已更新", SessionRunning, "", nil)
		}
	case "session_info_update":
		title := strField(envelope.Update, "title")
		if title == "" {
			var nullable struct {
				Title *string `json:"title"`
			}
			if json.Unmarshal(envelope.Update, &nullable) == nil && nullable.Title != nil {
				title = *nullable.Title
			}
		}
		s.mu.Lock()
		s.title = title
		s.mu.Unlock()
		s.emit(EventTitle, title, SessionRunning, "", nil)
	case "usage_update":
		var usage struct {
			Used uint64 `json:"used"`
			Size uint64 `json:"size"`
			Cost *struct {
				Amount   float64 `json:"amount"`
				Currency string  `json:"currency"`
			} `json:"cost"`
		}
		if json.Unmarshal(envelope.Update, &usage) == nil {
			s.mu.Lock()
			s.usage.Used, s.usage.Size = usage.Used, usage.Size
			if usage.Cost != nil {
				s.usage.Cost = fmt.Sprintf("%.4g %s", usage.Cost.Amount, usage.Cost.Currency)
			}
			u := s.usage
			s.mu.Unlock()
			s.emit(EventUsage, usageText(u), SessionRunning, "", nil)
		}
	}
}

// AvailableCommand 是 agent 下发的可用命令（TUI /command 菜单用）。
type AvailableCommand struct {
	Name  string `json:"name"`
	Desc  string `json:"desc,omitempty"`
	Input string `json:"input,omitempty"`
}

func usageText(u SessionUsage) string {
	if u.Size == 0 {
		if u.Cost != "" {
			return "累计 " + u.Cost
		}
		return ""
	}
	pct := u.Used * 100 / u.Size
	text := fmt.Sprintf("上下文 %d/%d (%d%%)", u.Used, u.Size, pct)
	if u.Cost != "" {
		text += " · 累计 " + u.Cost
	}
	return text
}

// contentText 按官方 schema 提取 content 块里的文本：content 单块或数组，
// 每项可能是 text / resource / resource_link，逐项拼行。
func contentText(raw json.RawMessage) (string, []string) {
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return "", nil
	}
	if content, ok := obj["content"]; ok {
		if text, files := blockText(content); text != "" || len(files) > 0 {
			return text, files
		}
		if text, files := blocksText(content); text != "" || len(files) > 0 {
			return text, files
		}
	}
	for _, k := range []string{"text", "title"} {
		var str string
		if json.Unmarshal(obj[k], &str) == nil && str != "" {
			return str, nil
		}
	}
	return "", nil
}

func blocksText(raw json.RawMessage) (string, []string) {
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) != nil {
		return "", nil
	}
	var texts []string
	var files []string
	for _, item := range list {
		text, f := blockText(item)
		if text != "" {
			texts = append(texts, text)
		}
		files = append(files, f...)
	}
	return strings.Join(texts, "\n"), files
}

func blockText(raw json.RawMessage) (string, []string) {
	var probe struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(raw, &probe) != nil || probe.Type == "" {
		var str string
		if json.Unmarshal(raw, &str) == nil {
			return str, nil
		}
		return "", nil
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return "", nil
	}
	strVal := func(key string) string {
		var str string
		if json.Unmarshal(obj[key], &str) != nil {
			return ""
		}
		return str
	}
	switch probe.Type {
	case "text":
		return strVal("text"), nil
	case "resource_link", "resource":
		name := strVal("name")
		if name == "" {
			name = strVal("title")
		}
		uri := strVal("uri")
		label := name
		if label == "" {
			label = uri
		}
		if inner, ok := obj["resource"]; ok {
			if text, _ := blockText(inner); text != "" {
				if label != "" {
					return label + "：\n" + text, []string{label}
				}
				return text, []string{label}
			}
		}
		if label != "" {
			return "", []string{label}
		}
		return "", nil
	case "content":
		if inner, ok := obj["content"]; ok {
			return blockText(inner)
		}
		return "", nil
	case "diff":
		path := strVal("path")
		oldText := strVal("oldText")
		newText := strVal("newText")
		return diffText(path, oldText, newText), []string{path}
	case "terminal":
		if id := strVal("terminalId"); id != "" {
			return "", []string{"终端 " + id}
		}
		return "", nil
	case "image", "audio":
		mime := strVal("mimeType")
		if mime == "" {
			mime = probe.Type
		}
		return "", []string{"[" + mime + "]"}
	}
	return "", nil
}

// diffText 把 tool 内容里的 diff 块压成 TUI 可读的短摘要（全文进 detail）。
func diffText(path, oldText, newText string) string {
	oldLines, newLines := countLines(oldText), countLines(newText)
	if path == "" {
		path = "文件"
	}
	return fmt.Sprintf("%s −%d/+%d 行", path, oldLines, newLines)
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

// toolContent 把 tool_call 的 content 数组转成 TUI 文本：text 拼行，diff 转摘要，
// 文件/终端引用记到 files（标题后缀展示）。
func toolContent(raw json.RawMessage) (text string, files []string, diffs []string) {
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return "", nil, nil
	}
	content, ok := obj["content"]
	if !ok {
		return "", nil, nil
	}
	var list []json.RawMessage
	if json.Unmarshal(content, &list) != nil {
		if t, f := blockText(content); t != "" || len(f) > 0 {
			return t, f, nil
		}
		return "", nil, nil
	}
	var texts []string
	for _, item := range list {
		var probe struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(item, &probe) != nil {
			continue
		}
		if probe.Type == "diff" {
			var d struct {
				Path    string `json:"path"`
				OldText string `json:"oldText"`
				NewText string `json:"newText"`
			}
			if json.Unmarshal(item, &d) == nil {
				diffs = append(diffs, d.Path+"\n---\n"+d.OldText+"\n+++\n"+d.NewText)
				texts = append(texts, diffText(d.Path, d.OldText, d.NewText))
				if d.Path != "" {
					files = append(files, d.Path)
				}
			}
			continue
		}
		t, f := blockText(item)
		if t != "" {
			texts = append(texts, t)
		}
		files = append(files, f...)
	}
	return strings.Join(texts, "\n"), files, diffs
}

func planText(raw json.RawMessage) string {
	var plan struct {
		Entries []struct {
			Content  string `json:"content"`
			Status   string `json:"status"`
			Priority string `json:"priority"`
		} `json:"entries"`
	}
	if json.Unmarshal(raw, &plan) != nil || len(plan.Entries) == 0 {
		return ""
	}
	var lines []string
	for _, e := range plan.Entries {
		mark := "○"
		switch e.Status {
		case "in_progress":
			mark = "◐"
		case "completed":
			mark = "●"
		}
		line := mark + " " + e.Content
		if e.Priority == "high" {
			line += " !"
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func availableCommands(raw json.RawMessage) []AvailableCommand {
	var update struct {
		Commands []struct {
			Name  string `json:"name"`
			Desc  string `json:"description"`
			Input *struct {
				Hint string `json:"hint"`
			} `json:"input"`
		} `json:"availableCommands"`
	}
	if json.Unmarshal(raw, &update) != nil {
		return nil
	}
	var out []AvailableCommand
	for _, c := range update.Commands {
		cmd := AvailableCommand{Name: c.Name, Desc: c.Desc}
		if c.Input != nil {
			cmd.Input = c.Input.Hint
		}
		out = append(out, cmd)
	}
	return out
}

func (s *acpSession) handleToolCall(raw json.RawMessage, isUpdate bool) {
	toolID := strField(raw, "toolCallId")
	title := strField(raw, "title")
	kind := strField(raw, "kind")
	status := strField(raw, "status")
	if title == "" {
		title = kind
	}
	if title == "" {
		title = "tool"
	}
	text, files, diffs := toolContent(raw)
	if text == "" {
		text = textOf(raw, "rawInput", "text")
	}
	detail := text
	if len(diffs) > 0 {
		detail = strings.Join(diffs, "\n\n")
	}
	for _, loc := range toolLocations(raw) {
		dup := false
		for _, f := range files {
			if f == loc {
				dup = true
				break
			}
		}
		if !dup {
			files = append(files, loc)
		}
	}
	if len(files) > 0 && len(files) <= 3 {
		title += " · " + strings.Join(files, ", ")
	} else if len(files) > 3 {
		title += fmt.Sprintf(" · %s 等 %d 个文件", files[0], len(files))
	}
	done := status == "completed" || status == "failed" || status == "done"
	meta := map[string]string{"status": status, "kind": kind}
	if isUpdate && !done && title == "" && detail == "" && toolID == "" {
		return
	}
	if done || (isUpdate && status != "" && status != "pending" && status != "in_progress") {
		if status == "failed" {
			meta["failed"] = "1"
		}
		s.emit(EventToolResult, detail, SessionRunning, "", meta, withToolPayload(title, kind, detail, toolID))
		return
	}
	s.emit(EventToolCall, "", SessionRunning, "", meta, withToolPayload(title, kind, detail, toolID))
}

func toolLocations(raw json.RawMessage) []string {
	var obj struct {
		Locations []struct {
			Path string `json:"path"`
			Line *int   `json:"line"`
		} `json:"locations"`
	}
	if json.Unmarshal(raw, &obj) != nil {
		return nil
	}
	var out []string
	for _, l := range obj.Locations {
		if l.Path == "" {
			continue
		}
		if l.Line != nil {
			out = append(out, fmt.Sprintf("%s:%d", l.Path, *l.Line))
		} else {
			out = append(out, l.Path)
		}
	}
	return out
}

func withToolPayload(title, name, detail, toolID string) SessionEvent {
	return SessionEvent{Title: truncateRunes(title, 80), Name: truncateRunes(name, 80), Detail: truncateRunes(detail, 500), ToolID: toolID}
}

func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

func textOf(raw json.RawMessage, keys ...string) string {
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return ""
	}
	for _, k := range keys {
		var str string
		if json.Unmarshal(obj[k], &str) == nil && str != "" {
			return str
		}
	}
	if text, _ := contentText(raw); text != "" {
		return text
	}
	return ""
}

func strField(raw json.RawMessage, key string) string {
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return ""
	}
	var str string
	if json.Unmarshal(obj[key], &str) != nil {
		return ""
	}
	return str
}

func (s *acpSession) handleInboundRequest(id json.RawMessage, method string, params json.RawMessage) {
	switch method {
	case "session/request_permission":
		s.handlePermissionRequest(id, params)
	case "fs/read_text_file":
		s.handleFSRead(id, params)
	case "fs/write_text_file":
		s.handleFSWrite(id, params)
	case "elicitation/create":
		s.handleElicitation(id, params)
	case "terminal/create":
		s.handleTermCreate(id, params)
	case "terminal/output":
		s.handleTermOutput(id, params)
	case "terminal/wait_for_exit":
		s.handleTermWait(id, params)
	case "terminal/kill":
		s.handleTermKill(id, params)
	case "terminal/release":
		s.handleTermRelease(id, params)
	default:
		_ = s.sendFrame(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32601, "message": "method not supported"}})
	}
}

func permKey(id json.RawMessage) string { return string(id) }

// handlePermissionRequest 先按静态策略自动回应（allow/reject）；
// 策略为 ask 或无匹配选项时挂起为交互式请求，TUI 通过 DecidePermission 回应。
// Cancel 时所有挂起请求按 cancelled 回应（协议要求）。
func (s *acpSession) handlePermissionRequest(id json.RawMessage, params json.RawMessage) {
	var request struct {
		SessionID string          `json:"sessionId"`
		ToolCall  json.RawMessage `json:"toolCall"`
		Options   []struct {
			OptionID string `json:"optionId"`
			Name     string `json:"name"`
			Kind     string `json:"kind"`
		} `json:"options"`
	}
	if err := json.Unmarshal(params, &request); err != nil || len(request.Options) == 0 {
		_ = s.sendFrame(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32602, "message": "invalid permission request"}})
		return
	}
	var options []PermissionOption
	for _, o := range request.Options {
		name := o.Name
		if name == "" {
			name = o.Kind
		}
		options = append(options, PermissionOption{ID: o.OptionID, Name: name, Kind: o.Kind})
	}
	toolTitle := toolCallTitle(request.ToolCall)
	detail := toolTitle
	if detail == "" {
		var kinds []string
		for _, o := range options {
			kinds = append(kinds, o.Kind+"/"+o.ID)
		}
		detail = "选项: " + strings.Join(kinds, ", ")
	}
	permID := permKey(id)
	if s.permission == "allow" || s.permission == "reject" {
		want := "reject_once"
		if s.permission == "allow" {
			want = "allow_once"
		}
		for _, o := range request.Options {
			if o.Kind == want {
				s.emit(EventPermission, "", SessionRunning, "", map[string]string{"policy": s.permission, "auto": "1"},
					SessionEvent{Title: toolTitle, Name: s.permission, Detail: detail, PermID: permID, Options: options})
				_ = s.sendFrame(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"outcome": "selected", "optionId": o.OptionID}})
				return
			}
		}
	}
	s.mu.Lock()
	if s.pendingPerm == nil {
		s.pendingPerm = map[string]*pendingPerm{}
	}
	s.pendingPerm[permID] = &pendingPerm{id: permID, options: options, toolCall: request.ToolCall}
	s.mu.Unlock()
	s.emit(EventPermission, "", SessionRunning, "", map[string]string{"policy": s.permission},
		SessionEvent{Title: toolTitle, Name: "ask", Detail: detail, PermID: permID, Options: options})
}

func toolCallTitle(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	title := strField(raw, "title")
	if title != "" {
		return title
	}
	return strField(raw, "kind")
}

// cancelPendingPermissions 按 cancelled 回应所有挂起的权限请求（Cancel 时调用）。
func (s *acpSession) cancelPendingPermissions() {
	s.mu.Lock()
	ids := make([]string, 0, len(s.pendingPerm))
	for id, p := range s.pendingPerm {
		if !p.responded {
			p.responded = true
			ids = append(ids, id)
		}
	}
	for _, id := range ids {
		delete(s.pendingPerm, id)
	}
	s.mu.Unlock()
	for _, id := range ids {
		var rawID json.RawMessage
		if json.Unmarshal([]byte(id), &rawID) != nil {
			rawID = json.RawMessage(`"` + id + `"`)
		}
		_ = s.sendFrame(map[string]any{"jsonrpc": "2.0", "id": rawID, "result": map[string]any{"outcome": "cancelled"}})
	}
}

// handleFSRead 直接读本地文件回给 agent（路径按 agent 传的绝对路径）。
func (s *acpSession) handleFSRead(id json.RawMessage, params json.RawMessage) {
	var req struct {
		Path  string `json:"path"`
		Line  *int   `json:"line"`
		Limit *int   `json:"limit"`
	}
	if json.Unmarshal(params, &req) != nil || req.Path == "" {
		_ = s.sendFrame(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32602, "message": "invalid fs/read_text_file params"}})
		return
	}
	content, err := os.ReadFile(req.Path)
	if err != nil {
		_ = s.sendFrame(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32002, "message": "读取文件失败: " + err.Error()}})
		return
	}
	text := string(content)
	if len(text) > acpFSMaxBytes {
		text = text[:acpFSMaxBytes] + "\n…（已截断）"
	}
	lines := strings.Split(text, "\n")
	if req.Line != nil && *req.Line > 1 {
		start := *req.Line - 1
		if start >= len(lines) {
			lines = nil
		} else {
			lines = lines[start:]
		}
	}
	if req.Limit != nil && *req.Limit >= 0 && *req.Limit < len(lines) {
		lines = lines[:*req.Limit]
	}
	_ = s.sendFrame(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"content": strings.Join(lines, "\n")}})
}

// handleFSWrite 直接写本地文件（目录不存在自动创建）。
func (s *acpSession) handleFSWrite(id json.RawMessage, params json.RawMessage) {
	var req struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if json.Unmarshal(params, &req) != nil || req.Path == "" {
		_ = s.sendFrame(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32602, "message": "invalid fs/write_text_file params"}})
		return
	}
	if dir := filepath.Dir(req.Path); dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	if err := os.WriteFile(req.Path, []byte(req.Content), 0o644); err != nil {
		_ = s.sendFrame(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32603, "message": "写入文件失败: " + err.Error()}})
		return
	}
	s.emit(EventStatus, "", SessionRunning, "", map[string]string{"fsWrite": req.Path})
	_ = s.sendFrame(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{}})
}

// handleElicitation 把 agent 的结构化输入请求转成 TUI 可见事件并挂起，
// 用户在 TUI 确认/拒绝后经 DecideElicitation 回应（默认拒绝，保证安全）。
func (s *acpSession) handleElicitation(id json.RawMessage, params json.RawMessage) {
	permID := permKey(id)
	s.mu.Lock()
	if s.pendingPerm == nil {
		s.pendingPerm = map[string]*pendingPerm{}
	}
	s.pendingPerm[permID] = &pendingPerm{id: permID, toolCall: params}
	s.mu.Unlock()
	msg := strField(params, "message")
	if msg == "" {
		msg = "需要你提供输入"
	}
	s.emit(EventElicitation, msg, SessionRunning, "", nil,
		SessionEvent{Title: "输入请求", Detail: truncateRunes(msg, 500), PermID: permID})
}

// DecideElicitation 回应一次 elicitation：accept 带 content，空 content 表示拒绝。
func (s *acpSession) DecideElicitation(ctx context.Context, permID string, content map[string]string) error {
	s.mu.Lock()
	p := s.pendingPerm[permID]
	if p != nil && p.responded {
		p = nil
	}
	s.mu.Unlock()
	if p == nil {
		return fmt.Errorf("输入请求已处理或不存在：%s", permID)
	}
	var result any
	if len(content) == 0 {
		result = map[string]any{"action": "decline"}
	} else {
		result = map[string]any{"action": "accept", "content": content}
	}
	s.mu.Lock()
	if pp := s.pendingPerm[permID]; pp != nil {
		pp.responded = true
		delete(s.pendingPerm, permID)
	}
	s.mu.Unlock()
	var rawID json.RawMessage
	if json.Unmarshal([]byte(permID), &rawID) != nil {
		rawID = json.RawMessage(`"` + permID + `"`)
	}
	if err := s.sendFrame(map[string]any{"jsonrpc": "2.0", "id": rawID, "result": result}); err != nil {
		return fmt.Errorf("回应输入请求失败：%w", err)
	}
	return nil
}

type acpTerminal struct {
	cmd    *exec.Cmd
	output []byte
	mu     sync.Mutex
	done   chan struct{}
	code   *int
	signal string
}

// handleTermCreate 起一个本地命令，输出进内存缓冲（上限 1MB，截头留尾）。
func (s *acpSession) handleTermCreate(id json.RawMessage, params json.RawMessage) {
	var req struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
		Cwd     string   `json:"cwd"`
		Env     []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"env"`
	}
	if json.Unmarshal(params, &req) != nil || req.Command == "" {
		_ = s.sendFrame(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32602, "message": "invalid terminal/create params"}})
		return
	}
	cmd := exec.Command(req.Command, req.Args...)
	if req.Cwd != "" {
		cmd.Dir = req.Cwd
	} else {
		cmd.Dir = s.workdir()
	}
	if len(req.Env) > 0 {
		env := os.Environ()
		for _, e := range req.Env {
			env = append(env, e.Name+"="+e.Value)
		}
		cmd.Env = env
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = s.sendFrame(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32603, "message": "创建终端失败: " + err.Error()}})
		return
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		_ = s.sendFrame(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32603, "message": "启动命令失败: " + err.Error()}})
		return
	}
	t := &acpTerminal{cmd: cmd, done: make(chan struct{})}
	s.mu.Lock()
	if s.terminals == nil {
		s.terminals = map[string]*acpTerminal{}
	}
	s.nextTerm++
	termID := fmt.Sprintf("term-%d", s.nextTerm)
	s.terminals[termID] = t
	s.mu.Unlock()
	go func() {
		defer close(t.done)
		buf := make([]byte, 4096)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				t.mu.Lock()
				t.output = append(t.output, buf[:n]...)
				if len(t.output) > acpFSMaxBytes {
					t.output = t.output[len(t.output)-acpFSMaxBytes:]
				}
				t.mu.Unlock()
			}
			if err != nil {
				break
			}
		}
		err = cmd.Wait()
		t.mu.Lock()
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				code := exitErr.ExitCode()
				t.code = &code
			} else {
				t.signal = err.Error()
			}
		} else {
			zero := 0
			t.code = &zero
		}
		t.mu.Unlock()
	}()
	s.emit(EventStatus, "", SessionRunning, "", map[string]string{"terminal": termID, "command": req.Command})
	_ = s.sendFrame(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"terminalId": termID}})
}

func (s *acpSession) termOf(termID string) *acpTerminal {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.terminals[termID]
}

func (s *acpSession) handleTermOutput(id json.RawMessage, params json.RawMessage) {
	var req struct {
		TerminalID string `json:"terminalId"`
	}
	if json.Unmarshal(params, &req) != nil || req.TerminalID == "" {
		_ = s.sendFrame(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32602, "message": "invalid terminal/output params"}})
		return
	}
	t := s.termOf(req.TerminalID)
	if t == nil {
		_ = s.sendFrame(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32002, "message": "终端不存在"}})
		return
	}
	t.mu.Lock()
	out := string(t.output)
	code, signal := t.code, t.signal
	t.mu.Unlock()
	result := map[string]any{"output": out, "truncated": false}
	if code != nil {
		result["exitStatus"] = map[string]any{"exitCode": *code}
	} else if signal != "" {
		result["exitStatus"] = map[string]any{"signal": signal}
	}
	_ = s.sendFrame(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func (s *acpSession) handleTermWait(id json.RawMessage, params json.RawMessage) {
	var req struct {
		TerminalID string `json:"terminalId"`
	}
	if json.Unmarshal(params, &req) != nil || req.TerminalID == "" {
		_ = s.sendFrame(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32602, "message": "invalid terminal/wait_for_exit params"}})
		return
	}
	t := s.termOf(req.TerminalID)
	if t == nil {
		_ = s.sendFrame(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32002, "message": "终端不存在"}})
		return
	}
	select {
	case <-t.done:
	case <-s.done:
		_ = s.sendFrame(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32800, "message": "cancelled"}})
		return
	}
	t.mu.Lock()
	code, signal := t.code, t.signal
	t.mu.Unlock()
	result := map[string]any{}
	if code != nil {
		result["exitCode"] = *code
	} else if signal != "" {
		result["signal"] = signal
	}
	_ = s.sendFrame(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func (s *acpSession) handleTermKill(id json.RawMessage, params json.RawMessage) {
	var req struct {
		TerminalID string `json:"terminalId"`
	}
	if json.Unmarshal(params, &req) != nil || req.TerminalID == "" {
		_ = s.sendFrame(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32602, "message": "invalid terminal/kill params"}})
		return
	}
	t := s.termOf(req.TerminalID)
	if t == nil {
		_ = s.sendFrame(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32002, "message": "终端不存在"}})
		return
	}
	if t.cmd != nil && t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
	}
	_ = s.sendFrame(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{}})
}

func (s *acpSession) handleTermRelease(id json.RawMessage, params json.RawMessage) {
	var req struct {
		TerminalID string `json:"terminalId"`
	}
	if json.Unmarshal(params, &req) != nil || req.TerminalID == "" {
		_ = s.sendFrame(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32602, "message": "invalid terminal/release params"}})
		return
	}
	s.mu.Lock()
	t := s.terminals[req.TerminalID]
	delete(s.terminals, req.TerminalID)
	s.mu.Unlock()
	if t != nil && t.cmd != nil && t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
	}
	_ = s.sendFrame(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{}})
}

func (s *acpSession) readStderr(r io.Reader) {
	const chunkSize = 4096
	buf := make([]byte, chunkSize)
	kept := make([]byte, 0, acpMaxStderrBytes)
	for {
		n, err := r.Read(buf)
		if n > 0 && len(kept) < acpMaxStderrBytes {
			remain := acpMaxStderrBytes - len(kept)
			if n > remain {
				n = remain
			}
			kept = append(kept, buf[:n]...)
		}
		if err != nil {
			if err != io.EOF {
				s.emit(EventStatus, "", SessionConnecting, "", map[string]string{"stderr": "读取 stderr 失败: " + err.Error()})
			}
			break
		}
	}
	if len(kept) > 0 {
		if len(kept) == acpMaxStderrBytes {
			kept = append(kept, []byte("…")...)
		}
		s.emit(EventStatus, "", SessionConnecting, "", map[string]string{"stderr": string(kept)})
	}
}

func (s *acpSession) waitLoop() {
	err := s.cmd.Wait()
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if !closed {
		if err != nil {
			s.emit(EventError, "", SessionFailed, "ACP 子进程退出: "+err.Error(), nil)
		} else {
			s.emit(EventStatus, "", SessionClosed, "", nil)
		}
	}
	s.finish()
}

func (s *acpSession) finish() {
	s.endOnce.Do(func() {
		s.emitMu.Lock()
		defer s.emitMu.Unlock()
		s.mu.Lock()
		s.finished = true
		close(s.done)
		close(s.events)
		s.mu.Unlock()
	})
}

func (s *acpSession) emit(kind EventKind, text string, status SessionStatus, msg string, meta map[string]string, payload ...SessionEvent) {
	e := SessionEvent{SessionID: s.id, Kind: kind, Text: text, Status: status, Error: msg, Metadata: meta, At: time.Now().UTC()}
	if len(payload) > 0 {
		e.Title, e.Name, e.Detail, e.ToolID = payload[0].Title, payload[0].Name, payload[0].Detail, payload[0].ToolID
		e.PermID, e.Options = payload[0].PermID, payload[0].Options
	}
	s.emitMu.Lock()
	defer s.emitMu.Unlock()
	s.mu.Lock()
	if s.finished {
		s.mu.Unlock()
		return
	}
	events := s.events
	done := s.done
	s.mu.Unlock()
	select {
	case events <- e:
	case <-done:
	}
}

func newLocalSessionID() string { return fmt.Sprintf("ws-%d", time.Now().UnixNano()) }

// ProbeAgent 只做 initialize 握手并取能力快照：TUI 就绪态用，不建会话。
// 后端不可达/握手失败/auth_required 都转为 Reachable=false + 可操作提示。
func ProbeAgent(ctx context.Context, agent *Agent, spec AdapterSpec) AgentProbe {
	probe := AgentProbe{Agent: agent.Name, Display: displayOrName(agent)}
	probe.CommandLine = spec.Command + " " + strings.Join(spec.Args, " ")
	if spec.Kind == "" || spec.Kind == "none" {
		probe.Error = "未配置 adapter（extension.adapter.kind）"
		return probe
	}
	if spec.Command == "" {
		probe.Error = "adapter.command 为空"
		return probe
	}
	ctx, cancel := context.WithTimeout(ctx, acpOpenTimeout)
	defer cancel()
	cmd := exec.Command(spec.Command, spec.Args...)
	if spec.Cwd != "" {
		cmd.Dir = spec.Cwd
	}
	cmd.Env = append(os.Environ(), envPairs(spec.Env)...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		probe.Error = "创建 stdin 失败：" + err.Error()
		return probe
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		probe.Error = "创建 stdout 失败：" + err.Error()
		return probe
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		probe.Error = "创建 stderr 失败：" + err.Error()
		return probe
	}
	if err := cmd.Start(); err != nil {
		probe.Error = "启动失败：" + probeHint(err.Error())
		return probe
	}
	defer func() {
		_ = stdin.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}()
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := stderr.Read(buf); err != nil {
				return
			}
		}
	}()
	s := &acpSession{
		id:          SessionID(newLocalSessionID()),
		cmd:         cmd,
		stdin:       stdin,
		pending:     map[uint64]chan acpResponse{},
		pendingPerm: map[string]*pendingPerm{},
		events:      make(chan SessionEvent, 4),
		done:        make(chan struct{}),
	}
	go s.readLoop(stdout)
	go s.waitLoop()
	defer s.finish()
	result, err := s.request(ctx, "initialize", map[string]any{
		"protocolVersion":    acpProtocolVersion,
		"clientCapabilities": map[string]any{"fs": map[string]any{"readTextFile": true, "writeTextFile": true}},
		"clientInfo":         map[string]any{"name": "AILauncher", "version": "0.4.0-dev"},
	})
	if err != nil {
		if isAuthRequired(err) {
			probe.AuthRequired = true
			probe.AuthHint = s.authHintText()
			probe.Error = "需要登录：" + probe.AuthHint
			return probe
		}
		probe.Error = "握手失败：" + probeHint(err.Error())
		return probe
	}
	s.applyInit(result, agent)
	s.mu.Lock()
	probe.Protocol = s.protocol
	probe.AgentName, probe.AgentVersion = s.agentName, s.agentVer
	probe.Capabilities = s.caps
	probe.Modes = append([]SessionMode(nil), s.modes...)
	probe.AuthHint = s.authHint
	s.mu.Unlock()
	probe.Reachable = true
	return probe
}

func probeHint(err string) string {
	if strings.Contains(err, "executable file not found") || strings.Contains(err, "not found") {
		return err + "（命令不存在：检查 adapter.command 是否安装并在 PATH）"
	}
	if strings.Contains(err, "deadline exceeded") || strings.Contains(err, "timeout") {
		return err + "（握手超时：后端无响应，检查命令是否支持 ACP stdio）"
	}
	return err
}

func displayOrName(agent *Agent) string {
	if agent == nil {
		return ""
	}
	if agent.Display != "" {
		return agent.Display
	}
	return agent.Name
}

// openEphemeral 为一次性远端操作建连接（list/delete）：握手后只调目标方法即关。
func openEphemeral(ctx context.Context, agent *Agent, spec AdapterSpec) (*acpSession, context.CancelFunc, error) {
	cwd := spec.Cwd
	if cwd == "" {
		cwd = expandWorkdir(agent, "")
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return nil, nil, fmt.Errorf("解析目录失败：%w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, acpOpenTimeout)
	cmd := exec.Command(spec.Command, spec.Args...)
	cmd.Dir = spec.Cwd
	if cmd.Dir == "" {
		cmd.Dir = abs
	}
	cmd.Env = append(os.Environ(), envPairs(spec.Env)...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("创建 ACP stdin 失败：%w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("创建 ACP stdout 失败：%w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("创建 ACP stderr 失败：%w", err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, nil, fmt.Errorf("启动 ACP adapter %q 失败：%w", spec.Command, err)
	}
	s := &acpSession{
		id:          SessionID(newLocalSessionID()),
		cmd:         cmd,
		stdin:       stdin,
		pending:     map[uint64]chan acpResponse{},
		pendingPerm: map[string]*pendingPerm{},
		events:      make(chan SessionEvent, 4),
		permission:  spec.Permission,
		done:        make(chan struct{}),
	}
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := stderr.Read(buf); err != nil {
				return
			}
		}
	}()
	go s.readLoop(stdout)
	go s.waitLoop()
	initResult, err := s.request(ctx, "initialize", map[string]any{
		"protocolVersion":    acpProtocolVersion,
		"clientCapabilities": map[string]any{"fs": map[string]any{"readTextFile": true, "writeTextFile": true}},
		"clientInfo":         map[string]any{"name": "AILauncher", "version": "0.4.0-dev"},
	})
	if err != nil {
		s.Close()
		cancel()
		if isAuthRequired(err) {
			return nil, nil, fmt.Errorf("ACP 需要登录：%s", s.authHintText())
		}
		return nil, nil, fmt.Errorf("ACP initialize 失败：%w", err)
	}
	s.applyInit(initResult, agent)
	return s, func() {
		s.Close()
		cancel()
	}, nil
}

// ListRemoteSessions 列出 agent 远端会话（需 list 能力；dsh demo 等不支持会明确报错）。
func ListRemoteSessions(ctx context.Context, agent *Agent) ([]SessionInfo, error) {
	spec, err := DecodeAdapter(agent)
	if err != nil {
		return nil, err
	}
	s, cleanup, err := openEphemeral(ctx, agent, spec)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	if !s.Capabilities().ListSessions {
		return nil, fmt.Errorf("agent %q 的后端不支持列出远端会话", agent.Name)
	}
	result, err := s.request(ctx, "session/list", map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("远端 session/list 失败：%w", err)
	}
	var out struct {
		Sessions []struct {
			ID      string `json:"sessionId"`
			Title   string `json:"title"`
			Cwd     string `json:"cwd"`
			Updated string `json:"updatedAt"`
		} `json:"sessions"`
	}
	if json.Unmarshal(result, &out) != nil {
		return nil, fmt.Errorf("远端 session/list 返回无效")
	}
	var infos []SessionInfo
	for _, item := range out.Sessions {
		infos = append(infos, SessionInfo{ID: item.ID, Title: item.Title, Cwd: item.Cwd, Updated: item.Updated})
	}
	return infos, nil
}

// DeleteRemoteSession 删除一条远端会话（需 delete 能力）。
func DeleteRemoteSession(ctx context.Context, agent *Agent, remoteID string) error {
	if remoteID == "" {
		return fmt.Errorf("远端会话 ID 不能为空")
	}
	spec, err := DecodeAdapter(agent)
	if err != nil {
		return err
	}
	s, cleanup, err := openEphemeral(ctx, agent, spec)
	if err != nil {
		return err
	}
	defer cleanup()
	if !s.Capabilities().DeleteSession {
		return fmt.Errorf("agent %q 的后端不支持删除远端会话", agent.Name)
	}
	if _, err := s.request(ctx, "session/delete", map[string]any{"sessionId": remoteID}); err != nil {
		return fmt.Errorf("远端 session/delete 失败：%w", err)
	}
	return nil
}
