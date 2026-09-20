package tui

import (
	"fmt"
	"strings"
	"time"
)

// 本文件把 chatModel 排成一帧：顶栏 / 消息视口 / 错误行 / 输入框 / 状态栏。
// 布局按 height 分批：视口只渲染可见块（分页窗口），输入框多行折行，行数封顶不滚屏。

const (
	chatMaxBlocksPerFrame = 400
	spinnerFrames         = "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠿"
)

func statusLabel(status string) string {
	switch status {
	case "connecting":
		return "连接中"
	case "ready":
		return "就绪"
	case "running":
		return "生成中"
	case "idle":
		return "空闲"
	case "cancelled":
		return "已取消"
	case "failed":
		return "失败"
	case "closed":
		return "已关闭"
	default:
		if status == "" {
			return "就绪"
		}
		return status
	}
}

func spinnerFor(block *chatBlock, now time.Time) string {
	if block.finished {
		return "✓"
	}
	runes := []rune(spinnerFrames)
	return string(runes[int(now.UnixMilli()/120)%len(runes)])
}

// layoutChat 把模型排成一帧。viewportH 由调用方按 height 推导；返回帧与光标屏坐标。
func layoutChat(m *chatModel, width, viewportH int, now time.Time) (frame, int, int) {
	if width < 20 {
		width = 80
	}
	if viewportH < 3 {
		viewportH = 3
	}
	f := newFrame(viewportH + 8)

	agent := m.agent
	if agent == "" {
		agent = "agent"
	}
	head := fmt.Sprintf("%s✦ %s%s  %s%s", ccOrange+bold, reset+ccPink+bold, agent, reset+ccMuted, statusLabel(m.status))
	if m.title != "" {
		head += "  " + ccTeal + truncateCells(m.title, width/4) + reset
	}
	if m.mode != "" {
		head += "  " + ccMuted + "[" + m.mode + "]" + reset
	}
	if m.workdir != "" {
		head += "  " + ccFaint + truncateCells(m.workdir, width/4) + reset
	}
	if m.sessionID != "" {
		head += "  " + ccFaint + string(m.sessionID) + reset
	}
	f.add(fitClip(head+reset, width))
	f.add(fitClip(ccLine+strings.Repeat("─", minInt(width, 120))+reset, width))

	body := renderBlocks(m, width, now)
	if len(body) > chatMaxBlocksPerFrame {
		body = body[len(body)-chatMaxBlocksPerFrame:]
	}
	start := 0
	if m.follow || m.scroll <= 0 {
		if len(body) > viewportH {
			start = len(body) - viewportH
		}
	} else {
		start = len(body) - viewportH - m.scroll
		if start < 0 {
			start = 0
			m.scroll = len(body) - viewportH
			if m.scroll < 0 {
				m.scroll = 0
			}
		}
		if start+viewportH > len(body) {
			start = len(body) - viewportH
			if start < 0 {
				start = 0
			}
		}
	}
	for i := start; i < len(body) && i < start+viewportH; i++ {
		f.add(body[i])
	}
	for len(f.lines) < 2+viewportH {
		f.add("")
	}

	if m.err != "" {
		f.add(fitClip(red+"✕ "+truncateCells(m.err, width-2)+reset, width))
	} else if m.notice != "" && now.Sub(m.noticeAt) < 3*time.Second {
		f.add(fitClip(yellow+m.notice+reset, width))
	} else {
		f.add("")
	}

	cursorRow, cursorCol := -1, -1
	inputLines, curRow, curCol := m.input.view(maxInt(width-4, 8))
	if len(inputLines) == 0 {
		inputLines = []string{""}
		curRow, curCol = 0, 0
	}
	prompt := ccTeal + bold + "❯ " + reset
	for i, ln := range inputLines {
		prefix := prompt
		if i > 0 {
			prefix = "  "
		}
		if i == curRow {
			cursorRow = len(f.lines) + 1
			cursorCol = minInt(strWidth(prefix)+curCol+1, width)
		}
		f.add(fitClip(prefix+ln+reset, width))
	}

	bar := statusBar(m, width, now)
	f.add(fitClip(bar, width))
	return f, cursorRow, cursorCol
}

func statusBar(m *chatModel, width int, now time.Time) string {
	busy := m.status == "running"
	state := ccTeal + "● 空闲" + reset
	if busy {
		state = ccOrange + "◐ 生成中" + reset
	}
	extra := ""
	if m.usage.Size > 0 {
		pct := m.usage.Used * 100 / m.usage.Size
		extra = ccMuted + fmt.Sprintf(" ctx %d%%", pct) + reset
	} else if m.usage.Cost != "" {
		extra = ccMuted + " " + m.usage.Cost + reset
	}
	elapsed := now.Sub(m.lastActive).Truncate(time.Second)
	idle := ""
	if !busy && elapsed >= 2*time.Second {
		idle = ccFaint + fmt.Sprintf(" idle %s", elapsed) + reset
	}
	pos := ""
	if !m.follow && m.scroll > 0 {
		pos = ccMuted + fmt.Sprintf(" ▲%d", m.scroll) + reset
	}
	keys := ccFaint + " Enter 发送 · / 命令 · t 折叠思考 · ↑↓ 历史 · PgUp/Dn 滚动 · Ctrl-C 取消 · Esc 退出" + reset
	_ = now
	line := state + extra + idle + pos + "  " + keys
	if strWidth(line) > width {
		line = state + extra + idle + pos + ccFaint + " Enter发送 /命令 t折叠 ↑↓历史 PgDn滚动 ^C取消 Esc退出" + reset
	}
	if strWidth(line) > width {
		line = state + extra + idle + pos
	}
	return fitClip(padRight(line, width), width)
}

func renderBlocks(m *chatModel, width int, now time.Time) []string {
	var out []string
	push := func(lines ...string) {
		for _, ln := range lines {
			out = append(out, fitClip(ln, width))
		}
	}
	for i := range m.blocks {
		b := &m.blocks[i]
		if b.finished && b.cachedW == width && len(b.cached) > 0 {
			out = append(out, b.cached...)
			continue
		}
		before := len(out)
		renderOneBlock(m, b, width, now, push)
		out = append(out, "")
		if b.finished {
			b.cached = append([]string(nil), out[before:]...)
			b.cachedW = width
		}
	}
	if len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	if len(out) == 0 {
		out = []string{ccFaint + "  暂无消息，输入问题后 Enter 发送。" + reset}
	}
	return out
}

func renderOneBlock(m *chatModel, b *chatBlock, width int, now time.Time, push func(...string)) {
	switch b.role {
	case roleUser:
		push(ccTeal + bold + "● 你" + reset)
		for _, ln := range wrapCells(b.text, width-2) {
			push("  " + ln)
		}
	case roleAssistant:
		mark := "●"
		if !b.finished {
			mark = spinnerFor(b, now)
		}
		push(ccPink + mark + " Agent" + reset)
		for _, ln := range wrapCells(b.text, width-2) {
			push("  " + ln)
		}
	case roleThinking:
		push(renderThinkingHead(m, b, width))
		if !m.thinkingCollapsed {
			for _, ln := range wrapCells(truncateCells(b.text, 600), width-4) {
				push("    " + ccFaint + ln + reset)
			}
		}
	case roleTool:
		icon := "⚙"
		style := ccOrange
		if b.finished {
			icon = "✓"
			style = ccTeal
		}
		title := b.title
		if title == "" {
			title = "tool"
		}
		line := fmt.Sprintf("%s%s %s%s", style, icon, title, reset)
		if !b.finished {
			line += " " + ccMuted + spinnerFor(b, now) + reset
		} else if b.detail != "" {
			line += " " + ccFaint + "· " + truncateCells(singleLine(b.detail), width/2) + reset
		}
		push(line)
		if b.detail != "" && (!b.finished || strWidth(b.detail) > width/2) {
			for _, ln := range wrapCells(truncateCells(b.detail, 400), width-6) {
				push("      " + ccFaint + ln + reset)
			}
		}
	case rolePermission:
		style := yellow
		if b.finished {
			style = ccFaint
		}
		mark := "⚠"
		if b.status == "elicitation" {
			mark = "✎"
		}
		push(style + mark + " " + b.title + reset)
		if b.text != "" {
			push("    " + b.text)
		}
		if b.detail != "" {
			for _, ln := range wrapCells(b.detail, width-6) {
				push("    " + ccMuted + ln + reset)
			}
		}
		if !b.finished && len(b.options) > 0 {
			for i, o := range b.options {
				push(fmt.Sprintf("    %s%d. %s%s", ccTeal+bold, i+1, reset, o.Name))
			}
			push("    " + ccFaint + "数字选择 · y 允许一次 · n 拒绝 · ESC 稍后" + reset)
		} else if !b.finished {
			push("    " + ccFaint + "y 允许 · n 拒绝 · ESC 稍后" + reset)
		}
	case roleUsage:
		for _, ln := range wrapCells(b.text, width-4) {
			push(ccMuted + "  ※ " + ln + reset)
		}
	case roleStatus:
		for _, ln := range wrapCells(b.text, width-4) {
			push(ccFaint + "  · " + ln + reset)
		}
	case roleError:
		push(red + "✕ " + b.text + reset)
	}
}

// renderThinkingHead 渲染思考块首行：折叠时只显示摘要行（t 切换）。
func renderThinkingHead(m *chatModel, b *chatBlock, width int) string {
	n := 0
	for _, r := range b.text {
		if r == '\n' {
			n++
		}
	}
	summary := truncateCells(singleLine(b.text), width-20)
	if m.thinkingCollapsed {
		if summary == "" {
			summary = "思考中…"
		}
		return ccFaint + "… 思考 (" + itoa(n+1) + " 行，已折叠按 t 展开)" + reset + " " + ccFaint + summary + reset
	}
	return ccFaint + "… 思考中" + reset
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func singleLine(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	return s
}

func truncateCells(s string, n int) string {
	if n <= 0 {
		return ""
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		w := runeWidth(r)
		if used+w > n {
			b.WriteString("…")
			break
		}
		b.WriteRune(r)
		used += w
	}
	return b.String()
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
