package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"cli"
)

// 本文件是 Claude Code 风格会话的事件循环：
// 分批渲染（事件按 50ms 窗口合并，只在 dirty 时重绘 + 脏行 diff），
// 按键即时回显，spinner 按 150ms 心跳推进，窗口尺寸轮询自适应。

const (
	chatFlushInterval   = 50 * time.Millisecond
	chatSpinnerInterval = 150 * time.Millisecond
	chatTickInterval    = 30 * time.Millisecond
	chatMaxInputRows    = 5
)

type chatLoop struct {
	s     *session
	r     *renderer
	live  cli.Session
	model *chatModel
	w     int
	h     int
}

func openChatSession(p cli.Port, agentName, path string) (*session, func(), cli.Session, *chatModel, error) {
	sp, ok := p.(cli.SessionPort)
	if !ok {
		return nil, nil, nil, nil, fmt.Errorf("当前 CLI 端口不支持 live 会话")
	}
	s, cleanup, err := newSession(p)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	agent, err := resolveAgent(p, agentName)
	if err != nil {
		cleanup()
		return nil, nil, nil, nil, err
	}
	if path != "" && !dirExists(path) {
		cleanup()
		return nil, nil, nil, nil, fmt.Errorf("目录不存在: %s", path)
	}
	liveCh := make(chan openResult, 1)
	go func() {
		live, err := sp.OpenSession(context.Background(), cli.SessionRequest{AgentName: agent.Name, Workdir: path})
		liveCh <- openResult{live: live, err: err}
	}()
	ticker := time.NewTicker(120 * time.Millisecond)
	defer ticker.Stop()
	spin := 0
	frames := []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠿'}
	paint := func() {
		r := s.renderer()
		w, h := r.width, r.height
		if nw, nh, ok := getConsoleSize(); ok {
			w, h = nw, nh
		}
		r.resize(w, h)
		f := newFrame(h)
		f.add(fitClip(ccOrange+bold+"✦ "+displayName(agent)+reset, w))
		f.add(fitClip(ccLine+strings.Repeat("─", minInt(w, 60))+reset, w))
		f.add("")
		f.add(fitClip(fmt.Sprintf("  %s%s 正在连接…%s", ccTeal+bold, string(frames[spin%len(frames)]), reset), w))
		if path != "" {
			f.add(fitClip("  "+ccFaint+truncateCells(path, w-4)+reset, w))
		}
		f.add("")
		f.add(fitClip(ccFaint+"ESC 取消"+reset, w))
		r.draw(f)
		fmt.Fprint(r.s.w, hideCursor)
	}
	paint()
	keyCh := make(chan keyRead, 16)
	done := make(chan struct{})
	defer close(done)
	go readListKeys(s.kr, keyCh, done)
	for {
		select {
		case res := <-liveCh:
			if res.err != nil {
				cleanup()
				return nil, nil, nil, nil, res.err
			}
			m := newChatModel(displayName(agent), path)
			m.sessionID = res.live.ID()
			m.status = "ready"
			m.live = res.live
			m.syncLive()
			return s, cleanup, res.live, &m, nil
		case <-ticker.C:
			spin++
			paint()
		case kr, ok := <-keyCh:
			if !ok {
				continue
			}
			if kr.err == nil && isBackKey(kr.key) {
				go func() {
					if res := <-liveCh; res.live != nil {
						res.live.Close()
					}
				}()
				cleanup()
				return nil, nil, nil, nil, errInputCancel
			}
		}
	}
}

type openResult struct {
	live cli.Session
	err  error
}

// RunChat 打开一个 Claude Code 风格的 live 会话：备用屏 + 脏行 diff + 分批渲染。
// 返回后恢复光标与主屏；legacy launcher 不受影响。
func RunChat(p cli.Port, agentName, path string) error {
	s, cleanup, live, m, err := openChatSession(p, agentName, path)
	if err != nil {
		return err
	}
	defer cleanup()
	defer live.Close()
	defer s.s.ShowCursor()
	return runChatOnSession(s, live, m)
}

// runChatOnSession 复用已有 session（主菜单已进 raw + 备用屏）跑会话循环。
func runChatOnSession(s *session, live cli.Session, m *chatModel) error {
	r := s.renderer()
	w, h := r.width, r.height
	if nw, nh, ok := getConsoleSize(); ok {
		w, h = nw, nh
	}
	r.resize(w, h)
	l := &chatLoop{s: s, r: r, live: live, model: m, w: w, h: h}
	return l.run()
}

func consoleSizeOrDefault() (int, int) {
	if w, h, ok := getConsoleSize(); ok {
		return w, h
	}
	return 80, 24
}

func (l *chatLoop) run() error {
	keyCh := make(chan keyResult, 16)
	done := make(chan struct{})
	defer close(done)
	go readChatKeys(l.s.kr, keyCh, done)

	events := l.live.Events()
	ticker := time.NewTicker(chatTickInterval)
	defer ticker.Stop()
	lastFlush := time.Now()
	lastSpin := time.Now()
	l.model.dirty = true

	render := func() {
		l.paint()
		lastFlush = time.Now()
		l.model.dirty = false
		lastSpin = lastFlush
	}

	for {
		select {
		case event, ok := <-events:
			if !ok {
				l.model.err = "连接已关闭，按任意键返回"
				l.model.touch()
				l.paint()
				<-keyCh
				return nil
			}
			l.model.apply(event)
			if time.Since(lastFlush) >= chatFlushInterval {
				render()
			}
		case result, ok := <-keyCh:
			if !ok {
				return nil
			}
			if result.err != nil {
				return result.err
			}
			if exit, err := l.handleKey(result.key); err != nil {
				if errors.Is(err, errChatQuit) {
					return nil
				}
				return err
			} else if exit {
				return nil
			}
			render()
		case now := <-ticker.C:
			if w, h, ok := getConsoleSize(); ok && (w != l.w || h != l.h) {
				l.w, l.h = w, h
				l.r.resize(w, h)
				l.model.dirty = true
			}
			if l.model.status == "running" && now.Sub(lastSpin) >= chatSpinnerInterval {
				l.model.dirty = true
			}
			if l.model.dirty && now.Sub(lastFlush) >= chatFlushInterval {
				render()
			} else if l.model.dirty {
				l.paint()
				lastFlush = now
				l.model.dirty = false
				lastSpin = now
			}
		}
	}
}

func (l *chatLoop) paint() {
	now := time.Now()
	rows, _, _ := l.model.input.view(maxInt(l.w-4, 8))
	inputRows := len(rows)
	if inputRows < 1 {
		inputRows = 1
	}
	if inputRows > chatMaxInputRows {
		inputRows = chatMaxInputRows
	}
	// 2 顶栏 + 1 错误/提示 + N 输入 + 1 状态栏
	viewport := l.h - 2 - 1 - inputRows - 1
	if viewport < 3 {
		viewport = 3
	}
	f, cursorRow, cursorCol := layoutChat(l.model, l.w, viewport, now)
	fmt.Fprint(l.r.s.w, hideCursor)
	l.r.draw(f)
	if cursorRow >= 1 && cursorRow <= l.h && cursorCol >= 1 {
		fmt.Fprintf(l.r.s.w, "%s%d;%dH%s", csi, cursorRow, cursorCol, showCursor)
	} else {
		fmt.Fprint(l.r.s.w, showCursor)
	}
}

func (l *chatLoop) handleKey(key Key) (bool, error) {
	m := l.model
	ctx := context.Background()
	// 交互式权限/输入请求优先：数字/y/n/ESC 直接回应，不落输入框。
	if handled, exit, err := l.handlePendingKey(ctx, key); handled {
		return exit, err
	}
	switch key.Name {
	case "return":
		text := strings.TrimSpace(m.input.String())
		if text == "" {
			return false, nil
		}
		if handled, err := l.handleSlash(ctx, text); handled {
			return false, err
		}
		if m.status == "running" {
			m.flash("生成中… Ctrl-C 取消")
			return false, nil
		}
		if err := l.live.Send(ctx, text); err != nil {
			m.err = err.Error()
			m.touch()
			return false, nil
		}
		m.err = ""
		m.submit()
		return false, nil
	case "ctrl-c", "escape":
		if m.status == "running" {
			if err := l.live.Cancel(ctx); err != nil {
				m.err = err.Error()
				m.touch()
			} else {
				m.flash("已发送取消")
			}
			return false, nil
		}
		return true, nil
	case "ctrl-d":
		if m.input.Len() == 0 {
			return true, nil
		}
		m.input.deleteForward()
		m.touch()
		return false, nil
	case "up":
		m.historyPrev()
		return false, nil
	case "down":
		m.historyNext()
		return false, nil
	case "pageup":
		m.pageUp(l.viewportH())
		return false, nil
	case "pagedown":
		m.pageDown(l.viewportH())
		return false, nil
	}
	if _, cancel, handled := m.input.key(key); handled {
		if cancel {
			return l.handleKey(Key{Name: "escape"})
		}
		m.histPos = -1
		m.touch()
		return false, nil
	}
	if key.Name == "char" && (key.Rune == 't' || key.Rune == 'T') && m.input.Len() == 0 {
		m.thinkingCollapsed = !m.thinkingCollapsed
		for i := range m.blocks {
			if m.blocks[i].role == roleThinking {
				m.blocks[i].cached = nil
			}
		}
		m.touch()
		return false, nil
	}
	if strings.HasPrefix(key.Name, "alt-") && (strings.HasSuffix(key.Name, "\r") || strings.HasSuffix(key.Name, "\n")) {
		m.input.insertRune('\n')
		m.histPos = -1
		m.touch()
		return false, nil
	}
	return false, nil
}

func (l *chatLoop) viewportH() int {
	rows, _, _ := l.model.input.view(maxInt(l.w-4, 8))
	inputRows := len(rows)
	if inputRows < 1 {
		inputRows = 1
	}
	if inputRows > chatMaxInputRows {
		inputRows = chatMaxInputRows
	}
	v := l.h - 2 - 1 - inputRows - 1
	if v < 3 {
		v = 3
	}
	return v
}

func (m *chatModel) pageUp(viewport int) {
	if viewport < 1 {
		viewport = 1
	}
	m.follow = false
	m.scroll += viewport
	m.clampScroll()
	m.touch()
}

func (m *chatModel) pageDown(viewport int) {
	if viewport < 1 {
		viewport = 1
	}
	m.scroll -= viewport
	if m.scroll <= 0 {
		m.scroll = 0
		m.follow = true
	}
	m.touch()
}

func (m *chatModel) clampScroll() {
	maxScroll := len(m.blocks) + 20
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.scroll > maxScroll {
		m.scroll = maxScroll
	}
	if m.scroll < 0 {
		m.scroll = 0
		m.follow = true
	}
}

type keyResult struct {
	key Key
	err error
}

func readChatKeys(kr *keyReader, out chan<- keyResult, done <-chan struct{}) {
	defer close(out)
	for {
		key, err := readKey(kr)
		select {
		case out <- keyResult{key: key, err: err}:
		case <-done:
			return
		}
		if err != nil {
			return
		}
	}
}
