package tui

import (
	"strings"
	"time"

	"cli"
)

// 本文件是 Claude Code 风格的会话模型：块流（block stream）+ 增量文本追加。
// 块类型：user / assistant / thinking / tool / permission / usage / status / error。
// 历史回放（sessions.jsonl）与 live 事件走同一 apply 入口，保证渲染一致、无渲染错误。

type chatRole string

const (
	roleUser       chatRole = "user"
	roleAssistant  chatRole = "assistant"
	roleThinking   chatRole = "thinking"
	roleTool       chatRole = "tool"
	rolePermission chatRole = "permission"
	roleUsage      chatRole = "usage"
	roleStatus     chatRole = "status"
	roleError      chatRole = "error"
)

type chatBlock struct {
	role     chatRole
	title    string
	text     string
	detail   string
	toolID   string
	permID   string
	options  []cli.PermissionOption
	status   string
	spinner  int
	finished bool
	// cached 是已完成块的行缓存：finish 后渲染一次常驻，layout 只拼装不重算。
	cached  []string
	cachedW int
}

type chatModel struct {
	agent             string
	sessionID         cli.SessionID
	workdir           string
	status            string
	title             string
	mode              string
	modes             []cli.SessionMode
	configs           []cli.SessionConfigOption
	commands          []cli.AvailableCommand
	usage             cli.SessionUsage
	caps              cli.SessionCapabilities
	live              cli.Session
	blocks            []chatBlock
	input             lineEditor
	history           []string
	histPos           int
	scroll            int
	follow            bool
	thinkingCollapsed bool
	err               string
	notice            string
	noticeAt          time.Time
	lastActive        time.Time
	dirty             bool
}

func newChatModel(agent, workdir string) chatModel {
	return chatModel{agent: agent, workdir: workdir, status: "connecting", follow: true, histPos: -1, lastActive: time.Now()}
}

func (m *chatModel) touch() {
	m.lastActive = time.Now()
	m.dirty = true
}

// syncLive 把 live 会话的能力快照同步到模型（模式/配置/命令/用量/标题）。
// 由 chatLoop 在 open 与每次事件后调用；回放页无 live 时 live==nil 直接跳过。
func (m *chatModel) syncLive() {
	if m.live == nil {
		return
	}
	m.caps = m.live.Capabilities()
	m.modes = m.live.Modes()
	if cur := m.live.CurrentMode(); cur != "" {
		m.mode = cur
	}
	m.configs = m.live.ConfigOptions()
	if cmds := m.live.AvailableCommands(); len(cmds) > 0 {
		m.commands = cmds
	}
	m.usage = m.live.Usage()
	if title := m.live.SessionTitle(); title != "" {
		m.title = title
	}
}

// lastAssistant 返回最后一个可追加的 assistant 文本块（流式 chunk 拼接到同一块）。
func (m *chatModel) lastAssistant() *chatBlock {
	for i := len(m.blocks) - 1; i >= 0; i-- {
		b := &m.blocks[i]
		if b.role == roleAssistant && !b.finished {
			return b
		}
		if b.role == roleAssistant || b.role == roleUser {
			break
		}
	}
	return nil
}

func (m *chatModel) lastThinking() *chatBlock {
	if n := len(m.blocks); n > 0 && m.blocks[n-1].role == roleThinking && !m.blocks[n-1].finished {
		return &m.blocks[n-1]
	}
	return nil
}

func (m *chatModel) lastTool(toolID string) *chatBlock {
	for i := len(m.blocks) - 1; i >= 0; i-- {
		b := &m.blocks[i]
		if b.role != roleTool {
			continue
		}
		if toolID == "" || b.toolID == toolID || b.toolID == "" {
			return b
		}
	}
	return nil
}

// apply 是唯一的事件入口：live 与回放共用。未知 kind 忽略（不炸屏）。
func (m *chatModel) apply(event cli.SessionEvent) {
	if event.SessionID != "" && m.sessionID == "" {
		m.sessionID = event.SessionID
	}
	if event.Status != "" {
		m.status = string(event.Status)
	}
	m.syncLive()
	m.finishOpenBlocks(event)
	switch event.Kind {
	case cli.EventUserText:
		m.blocks = append(m.blocks, chatBlock{role: roleUser, title: "你", text: event.Text, finished: true})
	case cli.EventAssistantText:
		if b := m.lastAssistant(); b != nil {
			b.text += event.Text
		} else {
			m.blocks = append(m.blocks, chatBlock{role: roleAssistant, text: event.Text})
		}
	case cli.EventAssistantThinking:
		if b := m.lastThinking(); b != nil {
			b.text += event.Text
		} else {
			m.blocks = append(m.blocks, chatBlock{role: roleThinking, text: event.Text})
		}
	case cli.EventToolCall:
		title := event.Title
		if title == "" {
			title = event.Name
		}
		if title == "" {
			title = "tool"
		}
		if b := m.lastTool(event.ToolID); b != nil && !b.finished {
			if title != "" {
				b.title = title
			}
			if event.Detail != "" {
				b.detail = event.Detail
			}
			if event.ToolID != "" {
				b.toolID = event.ToolID
			}
			b.spinner++
		} else {
			m.blocks = append(m.blocks, chatBlock{role: roleTool, title: title, detail: event.Detail, toolID: event.ToolID})
		}
	case cli.EventToolResult:
		if b := m.lastTool(event.ToolID); b != nil {
			if event.Text != "" {
				b.detail = event.Text
			} else if event.Detail != "" {
				b.detail = event.Detail
			}
			if event.Title != "" {
				b.title = event.Title
			}
			b.finished = true
		} else {
			m.blocks = append(m.blocks, chatBlock{role: roleTool, title: event.Title, detail: firstNonEmpty(event.Text, event.Detail), toolID: event.ToolID, finished: true})
		}
	case cli.EventPermission:
		title := event.Title
		if title == "" {
			title = "权限请求"
		}
		policy := "ask"
		if event.Metadata != nil && event.Metadata["policy"] != "" {
			policy = event.Metadata["policy"]
		}
		auto := event.Metadata != nil && event.Metadata["auto"] == "1"
		m.blocks = append(m.blocks, chatBlock{role: rolePermission, title: title, text: "策略: " + policy, detail: firstNonEmpty(event.Detail, event.Text), permID: event.PermID, options: event.Options, status: policy, finished: auto})
	case cli.EventUsage:
		if event.Text != "" {
			m.blocks = append(m.blocks, chatBlock{role: roleUsage, text: firstNonEmpty(event.Text, event.Detail), finished: true})
		}
	case cli.EventCommand:
		if event.Text != "" {
			m.blocks = append(m.blocks, chatBlock{role: roleUsage, title: "可用命令", text: event.Text, finished: true})
		}
	case cli.EventMode:
		if event.Text != "" {
			m.mode = event.Text
			m.blocks = append(m.blocks, chatBlock{role: roleStatus, text: "模式: " + event.Text, finished: true})
		}
	case cli.EventTitle:
		m.title = event.Text
		if event.Text != "" {
			m.blocks = append(m.blocks, chatBlock{role: roleStatus, text: "标题: " + event.Text, finished: true})
		}
	case cli.EventPlan:
		m.blocks = append(m.blocks, chatBlock{role: roleAssistant, title: "计划", text: event.Text, finished: true})
	case cli.EventDiff:
		m.blocks = append(m.blocks, chatBlock{role: roleTool, title: firstNonEmpty(event.Title, "改动"), detail: firstNonEmpty(event.Text, event.Detail), toolID: event.ToolID, finished: true})
	case cli.EventElicitation:
		m.blocks = append(m.blocks, chatBlock{role: rolePermission, title: firstNonEmpty(event.Title, "输入请求"), text: "等待输入", detail: firstNonEmpty(event.Detail, event.Text), permID: event.PermID, status: "elicitation", finished: false})
	case cli.EventStatus:
		text := event.Text
		if text == "" && event.Metadata != nil {
			text = event.Metadata["stderr"]
		}
		if text != "" {
			m.blocks = append(m.blocks, chatBlock{role: roleStatus, text: text, finished: true})
		}
	case cli.EventError:
		msg := event.Error
		if msg == "" {
			msg = event.Text
		}
		m.err = msg
		m.blocks = append(m.blocks, chatBlock{role: roleError, text: msg, finished: true})
	}
	if m.follow {
		m.scroll = 0
	}
	m.touch()
}

// applyStored 把 sessions.jsonl 的回放行喂给同一 apply 入口。
func (m *chatModel) applyStored(e cli.StoredEvent) {
	m.apply(cli.SessionEvent{SessionID: m.sessionID, Kind: e.Kind, Text: e.Text, Status: e.Status, Error: e.Error, Title: e.Title, Name: e.Name, Detail: e.Detail, ToolID: e.ToolID})
}

// finishOpenBlocks 在状态落定（idle/cancelled/failed/closed）时封口流式块。
func (m *chatModel) finishOpenBlocks(event cli.SessionEvent) {
	switch event.Status {
	case cli.SessionIdle, cli.SessionCancelled, cli.SessionFailed, cli.SessionClosed:
		for i := range m.blocks {
			if m.blocks[i].role == roleAssistant || m.blocks[i].role == roleThinking || m.blocks[i].role == roleTool {
				m.blocks[i].finished = true
			}
		}
	}
}

// submit 把草稿提交为 user 块并压入历史（去重置顶，上限 50）。
func (m *chatModel) submit() string {
	text := strings.TrimSpace(m.input.String())
	if text == "" {
		return ""
	}
	m.blocks = append(m.blocks, chatBlock{role: roleUser, title: "你", text: text, finished: true})
	m.input.clear()
	m.histPos = -1
	for i, h := range m.history {
		if h == text {
			m.history = append(m.history[:i], m.history[i+1:]...)
			break
		}
	}
	m.history = append([]string{text}, m.history...)
	if len(m.history) > 50 {
		m.history = m.history[:50]
	}
	m.status = "running"
	m.follow = true
	m.scroll = 0
	m.touch()
	return text
}

func (m *chatModel) historyPrev() {
	if len(m.history) == 0 {
		return
	}
	if m.histPos+1 < len(m.history) {
		m.histPos++
		m.input.setText(m.history[m.histPos])
		m.touch()
	}
}

func (m *chatModel) historyNext() {
	if m.histPos < 0 {
		return
	}
	m.histPos--
	if m.histPos < 0 {
		m.input.clear()
	} else {
		m.input.setText(m.history[m.histPos])
	}
	m.touch()
}

func (m *chatModel) flash(msg string) {
	m.notice = msg
	m.noticeAt = time.Now()
	m.touch()
}

func firstNonEmpty(items ...string) string {
	for _, s := range items {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
