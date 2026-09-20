package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"cli"
)

// 本文件是会话内的用户面交互：交互式权限确认 + 斜杠命令。
// 权限请求挂起时数字/y/n/ESC 直接回应，不污染输入框；斜杠命令走本地分发，
// 只有普通文本才发往后端。CLI 只给开发者用，用户面全在这里。

// pendingBlock 返回最近一个未完成的权限/输入请求块。
func (m *chatModel) pendingBlock() *chatBlock {
	for i := len(m.blocks) - 1; i >= 0; i-- {
		b := &m.blocks[i]
		if (b.role == rolePermission) && !b.finished && b.permID != "" {
			return b
		}
	}
	return nil
}

// handlePendingKey 在有挂起请求时拦截按键：数字选选项，y 允许一次，n 拒绝，
// ESC 稍后（按 cancelled 回应并标记完成）。返回 handled=true 表示已消费。
func (l *chatLoop) handlePendingKey(ctx context.Context, key Key) (bool, bool, error) {
	m := l.model
	b := m.pendingBlock()
	if b == nil {
		return false, false, nil
	}
	decide := func(optionID string) (bool, bool, error) {
		if err := l.live.DecidePermission(ctx, b.permID, optionID); err != nil {
			// elicitation 走另一条回应通道
			if b.status == "elicitation" {
				content := map[string]string{}
				if optionID != "" {
					content["value"] = optionID
				}
				if err2 := l.live.DecideElicitation(ctx, b.permID, content); err2 != nil {
					m.err = err2.Error()
					m.touch()
					return true, false, nil
				}
			} else {
				m.err = err.Error()
				m.touch()
				return true, false, nil
			}
		}
		b.finished = true
		if optionID == "" {
			b.text = "已拒绝"
		} else {
			b.text = "已确认"
		}
		m.touch()
		return true, false, nil
	}
	if isBackKey(key) {
		return decide("")
	}
	if key.Name != "char" {
		return false, false, nil
	}
	r := key.Rune
	if r >= '1' && r <= '9' {
		idx := int(r - '1')
		if idx < len(b.options) {
			return decide(b.options[idx].ID)
		}
		m.flash(fmt.Sprintf("只有 %d 个选项", len(b.options)))
		return true, false, nil
	}
	switch r {
	case 'y', 'Y':
		for _, o := range b.options {
			if strings.HasPrefix(o.Kind, "allow") {
				return decide(o.ID)
			}
		}
		if len(b.options) > 0 {
			return decide(b.options[0].ID)
		}
		return decide("allow")
	case 'n', 'N':
		return decide("")
	}
	return false, false, nil
}

// handleSlash 分发斜杠命令：/help /mode /model /commands /usage /title /quit。
// 返回 handled=true 表示已消费（不再发往后端）。
func (l *chatLoop) handleSlash(ctx context.Context, text string) (bool, error) {
	m := l.model
	if !strings.HasPrefix(text, "/") {
		return false, nil
	}
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return false, nil
	}
	cmd := strings.ToLower(strings.TrimPrefix(fields[0], "/"))
	args := ""
	if len(fields) > 1 {
		args = strings.Join(fields[1:], " ")
	}
	consume := func() (bool, error) {
		m.input.clear()
		m.histPos = -1
		m.touch()
		return true, nil
	}
	switch cmd {
	case "help", "h", "?":
		m.blocks = append(m.blocks, chatBlock{role: roleUsage, title: "命令", text: slashHelp(m), finished: true})
		m.touch()
		return consume()
	case "quit", "q", "exit":
		return true, errChatQuit
	case "mode":
		return l.slashMode(ctx, args, consume)
	case "model":
		return l.slashModel(ctx, args, consume)
	case "commands", "cmds":
		return l.slashCommands(consume)
	case "usage", "ctx":
		u := l.live.Usage()
		text := usageLine(u)
		if text == "" {
			text = "暂无用量数据（后端尚未下发 usage_update）"
		}
		m.blocks = append(m.blocks, chatBlock{role: roleUsage, title: "用量", text: text, finished: true})
		m.touch()
		return consume()
	case "title":
		title := l.live.SessionTitle()
		if title == "" {
			title = m.title
		}
		if title == "" {
			title = "（后端尚未下发标题）"
		}
		m.blocks = append(m.blocks, chatBlock{role: roleStatus, text: "标题: " + title, finished: true})
		m.touch()
		return consume()
	}
	// 未知斜杠：若后端下发过同名命令，提示用 /commands 查看；否则当普通文本发送。
	m.flash("未知命令 /" + cmd + "（/help 查看）")
	return consume()
}

// errChatQuit 表示用户用 /quit 主动退出会话（非错误）。
var errChatQuit = errors.New("quit")

func slashHelp(m *chatModel) string {
	lines := []string{
		"/help · 显示本帮助",
		"/mode [id] · 查看/切换 agent 模式",
		"/model [id] · 查看/切换模型",
		"/commands · 查看后端下发的可用命令",
		"/usage · 查看上下文与费用水位",
		"/title · 查看会话标题",
		"/quit · 退出会话",
	}
	if len(m.commands) > 0 {
		lines = append(lines, "后端命令："+strings.Join(commandNames(m.commands), ", "))
	}
	return strings.Join(lines, "\n")
}

func commandNames(cmds []cli.AvailableCommand) []string {
	out := make([]string, 0, len(cmds))
	for _, c := range cmds {
		out = append(out, c.Name)
	}
	return out
}

func usageLine(u cli.SessionUsage) string {
	if u.Size == 0 && u.Cost == "" {
		return ""
	}
	if u.Size == 0 {
		return "累计 " + u.Cost
	}
	pct := u.Used * 100 / u.Size
	text := fmt.Sprintf("上下文 %d/%d (%d%%)", u.Used, u.Size, pct)
	if u.Cost != "" {
		text += " · 累计 " + u.Cost
	}
	return text
}
