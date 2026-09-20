package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"cli"
)

// session 承载一次 TUI 交互的共享状态。逻辑全部经 cli 端口（cli/port.go）完成，
// 本层只维护 UI 与内存态（当前 agent、目录列表、参数缓冲），不持有逻辑。
// r 是全会话共享的增量渲染器：newSession 进入备用屏，所有页面共用它绘制，
// 退出时一次性恢复主屏（避免每页 enter/exit 闪屏）。
type session struct {
	s   *Screen
	r   *renderer
	kr  *keyReader
	p   cli.Port
	cfg *cli.Config // 引导时经 cli config 获得（只读数据模型）
}

// renderer 返回会话共享渲染器（测试注入的 session 懒创建，默认 80x24）。
func (s *session) renderer() *renderer {
	if s.r == nil {
		w, h := consoleSizeOrDefault()
		s.r = newRenderer(s.s, w, h)
		s.r.enter()
	}
	return s.r
}

// newSession 建立一次 TUI 交互的基础：进入 raw 模式、经 cli 引导配置、
// 构造 session、进入备用屏并隐藏光标。返回 (session, 清理函数, error)。
func newSession(p cli.Port) (*session, func(), error) {
	restore, err := enterRaw()
	if err != nil {
		return nil, nil, fmt.Errorf("需要交互式终端: %w", err)
	}
	w, h := consoleSizeOrDefault()
	ss := &session{
		s:  NewScreen(os.Stdout),
		kr: newTTYKeyReader(),
		p:  p,
	}
	ss.r = newRenderer(ss.s, w, h)
	ss.r.enter()
	if err := ss.bootstrap(); err != nil {
		ss.r.exit()
		restore()
		return nil, nil, err
	}
	fmt.Fprint(ss.s.w, hideCursor)
	cleanup := func() { ss.r.exit(); fmt.Fprint(ss.s.w, showCursor); restore() }
	return ss, cleanup, nil
}

// bootstrap 经 cli config 单元引导配置。
func (s *session) bootstrap() error {
	cfg, err := fetchConfig(s.p)
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}
	s.cfg = cfg
	return nil
}

// saveAgentState 经 cli state put 落盘单个 agent 的运行时状态。
func (s *session) saveAgentState(name string, td *cli.AgentState) error {
	return saveState(s.p, name, td)
}

// buildRunArgv 组装 run 命令 argv。workdir 始终占住 path 槽位（空串也传），
// 避免 args 首个非 KEY=V token 被 run 单元误当路径。
func buildRunArgv(name, workdir, raw string) []string {
	argv := []string{"run", name, workdir}
	return append(argv, strings.Fields(raw)...)
}

// showResult 经共享渲染器展示 Dispatch 结果并短暂停留 0.8s。
// Text 为空时用兜底行；长输出截断到一屏（启动提示只看首屏即可）。
func (s *session) showResult(res *cli.Result, fallback string) {
	text := fallback
	if res != nil && res.Text != "" {
		text = res.Text
	}
	lines := strings.Split(text, "\n")
	r := s.renderer()
	w, h := r.width, r.height
	if nw, nh, ok := getConsoleSize(); ok {
		w, h = nw, nh
	}
	r.resize(w, h)
	f := newFrame(h)
	f.add(fitClip(ccTeal+bold+"✓ 已启动"+reset, w))
	f.add("")
	budget := h - 4
	if budget < 1 {
		budget = 1
	}
	if len(lines) > budget {
		lines = lines[:budget-1]
		lines = append(lines, ccFaint+fmt.Sprintf("… 还有 %d 行（终端回滚查看）", len(strings.Split(text, "\n"))-len(lines))+reset)
	}
	for _, ln := range lines {
		f.add(fitClip("  "+ln, w))
	}
	f.add("")
	f.add(fitClip(ccFaint+"0.8s 后自动继续"+reset, w))
	r.draw(f)
	fmt.Fprint(r.s.w, hideCursor)
	time.Sleep(800 * time.Millisecond)
}

// runTuiAgent 跑一个 tui 型 agent 的「选目录 → 输参数 → 启动」流程。
// preWorkdir 非空时跳过目录选择直接用它作工作目录（交互直达模式）。
// stay 为 true 时启动后循环回参数页（直达模式，直到取消）；false 时启动一次即返回
// （完整模式，由外层回到选工具）。目录/参数页取消均返回 nil（不算错误）。
func (s *session) runTuiAgent(agent *cli.Agent, preWorkdir string, stay bool) error {
	td, err := loadState(s.p, agent.Name)
	if err != nil {
		return err
	}
	save := func() error { return s.saveAgentState(agent.Name, td) }

	workdir := preWorkdir
	if workdir == "" {
		workdir, err = s.selectDir(agent, td, save)
		if err != nil {
			return err
		}
		if workdir == "" {
			return nil // 目录页取消
		}
	}

	for {
		argsRes, err := s.runArgs(td, agent.Display, save)
		if err != nil {
			return err
		}
		if argsRes.action == "cancel" {
			return nil
		}
		td.RemovedCmds = argsRes.removed
		if err := save(); err != nil {
			return err
		}

		res, err := s.p.Dispatch(buildRunArgv(agent.Name, workdir, argsRes.value), nil)
		if err != nil {
			return err
		}
		s.showResult(res, "已启动 "+agent.Display)
		if !stay {
			return nil
		}
	}
}

// runGui 拉起 gui 型 agent（不选目录/参数，直接短路），并展示 run 单元的结果
// 文案（dsh 等仓库服务型会带访问地址）。
func (s *session) runGui(agent *cli.Agent, workdir string) error {
	argv := []string{"run", agent.Name}
	if workdir != "" {
		argv = append(argv, workdir)
	}
	res, err := s.p.Dispatch(argv, nil)
	if err != nil {
		return err
	}
	s.showResultLong(res, "已启动 "+agent.Display)
	return nil
}

// showResultLong 是 gui/服务型的加长版结果展示（2s，便于读访问地址）。
func (s *session) showResultLong(res *cli.Result, fallback string) {
	text := fallback
	if res != nil && res.Text != "" {
		text = res.Text
	}
	lines := strings.Split(text, "\n")
	r := s.renderer()
	w, h := r.width, r.height
	if nw, nh, ok := getConsoleSize(); ok {
		w, h = nw, nh
	}
	r.resize(w, h)
	f := newFrame(h)
	f.add(fitClip(ccTeal+bold+"✓ 已启动"+reset, w))
	f.add("")
	budget := h - 4
	if budget < 1 {
		budget = 1
	}
	if len(lines) > budget {
		lines = lines[:budget]
	}
	for _, ln := range lines {
		f.add(fitClip("  "+ln, w))
	}
	f.add("")
	f.add(fitClip(ccFaint+"2s 后自动继续"+reset, w))
	r.draw(f)
	fmt.Fprint(r.s.w, hideCursor)
	time.Sleep(2000 * time.Millisecond)
}

// Run 启动交互式 TUI：显式主菜单（启动工具 / Agent 会话 / 历史会话）。
// p 参数是 cli 端口（生产传 cli.NewDefault()；测试注入桩）。
// 约定：按键流错误（EOF）直接退出；业务错误（无 adapter、暂无历史等）闪屏提示并留在菜单。
func Run(p cli.Port) error {
	s, cleanup, err := newSession(p)
	if err != nil {
		return err
	}
	defer cleanup()

	for {
		choice, err := s.runMainMenu()
		if err != nil {
			if isKeyStreamError(err) {
				return nil
			}
			s.flashMenu(err.Error())
			continue
		}
		var flowErr error
		switch choice {
		case "":
			return nil
		case "run":
			flowErr = s.runLauncher()
		case "chat":
			flowErr = s.runChatFlow()
		case "history":
			flowErr = s.runHistoryFlow()
		}
		if flowErr != nil {
			if isKeyStreamError(flowErr) {
				return nil
			}
			s.flashMenu(flowErr.Error())
		}
	}
}

// flashMenu 在主菜单上闪屏提示一条业务错误（2.5s，不退出 TUI）。
func (s *session) flashMenu(msg string) {
	r := s.renderer()
	w, h := r.width, r.height
	if nw, nh, ok := getConsoleSize(); ok {
		w, h = nw, nh
	}
	r.resize(w, h)
	f := newFrame(h)
	f.add(fitClip(red+"✕ "+truncateCells(msg, w-4)+reset, w))
	f.add("")
	f.add(fitClip(ccFaint+"2.5s 后返回主菜单"+reset, w))
	r.draw(f)
	fmt.Fprint(r.s.w, hideCursor)
	time.Sleep(2500 * time.Millisecond)
}

// runLauncher 是 legacy 流程（选工具 → 选目录 → 输参数 → 启动），从主菜单进入。
func (s *session) runLauncher() error {
	for {
		// ---- 选工具 ----
		items := make([]string, len(s.cfg.Agents))
		for i, a := range s.cfg.Agents {
			items[i] = fmt.Sprintf("%s  %s- %s%s", a.Display, dark, a.Desc, reset)
		}
		res, err := s.listPage(listOpts{
			title:        "AILauncher - 选择工具",
			mainTitle:    "工具列表",
			removedTitle: "",
			mainItems:    &items,
			removedItems: &[]string{},
			hint:         listHint("gui 型直接拉起"),
		})
		if err != nil {
			return err
		}
		if res.action == "cancel" {
			return nil
		}
		idx := indexOf(items, res.value)
		if idx < 0 || idx >= len(s.cfg.Agents) {
			continue
		}
		agent := s.cfg.Agents[idx]

		// ---- gui 短路：不选目录/参数，直接拉起 ----
		if agent.Type == "gui" {
			if err := s.runGui(agent, ""); err != nil {
				return err
			}
			continue
		}

		if err := s.runTuiAgent(agent, "", false); err != nil {
			return err
		}
	}
}

// runChatFlow 是主菜单的 Agent 会话流程：选 agent → 远端恢复（可选）→ 问目录 → 进会话。
// 无 adapter 的 agent 会在 Open 时明确报错并指引走 run（由 Core DecodeAdapter 保证）。
func (s *session) runChatFlow() error {
	sp, ok := s.p.(cli.SessionPort)
	if !ok {
		return fmt.Errorf("当前 CLI 端口不支持 live 会话")
	}
	agent, err := s.pickChatAgent()
	if err != nil {
		return err
	}
	if agent == nil {
		return nil
	}
	resumeID, err := s.pickRemoteResume(sp, agent)
	if err != nil {
		if errors.Is(err, errInputCancel) {
			return nil
		}
		return err
	}
	path, err := s.askChatPath(agent)
	if err != nil {
		if errors.Is(err, errInputCancel) {
			return nil
		}
		return err
	}
	live, err := s.openSessionSplash(sp, agent, path, resumeID)
	if err != nil {
		return err
	}
	defer live.Close()
	m := newChatModel(displayName(agent), path)
	m.sessionID = live.ID()
	m.status = "ready"
	m.live = live
	m.syncLive()
	return runChatOnSession(s, live, &m)
}

// openSessionSplash 在后台建连，前台转 spinner：握手/initialize/session-new 再慢也有反馈。
func (s *session) openSessionSplash(sp cli.SessionPort, agent *cli.Agent, path, resumeID string) (cli.Session, error) {
	type result struct {
		live cli.Session
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		live, err := sp.OpenSession(context.Background(), cli.SessionRequest{AgentName: agent.Name, Workdir: path, ResumeID: resumeID})
		ch <- result{live: live, err: err}
	}()
	r := s.renderer()
	ticker := time.NewTicker(120 * time.Millisecond)
	defer ticker.Stop()
	spin := 0
	frames := []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠿'}
	paint := func() {
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
		if resumeID != "" {
			f.add(fitClip("  "+ccFaint+"恢复远端会话…"+reset, w))
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
		case res := <-ch:
			return res.live, res.err
		case <-ticker.C:
			spin++
			paint()
		case kr, ok := <-keyCh:
			if !ok {
				continue
			}
			if kr.err != nil || isBackKey(kr.key) {
				go func() {
					if res := <-ch; res.live != nil {
						res.live.Close()
					}
				}()
				return nil, errInputCancel
			}
		}
	}
}

// pickRemoteResume 列出远端会话供恢复：空提交=新建；后端不支持则静默跳过。
// 远端 list 在后台跑，前台转 spinner（后端冷启动再慢也有反馈，可 ESC 跳过）。
func (s *session) pickRemoteResume(sp cli.SessionPort, agent *cli.Agent) (string, error) {
	type result struct {
		remotes []cli.SessionInfo
		err     error
	}
	ch := make(chan result, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		remotes, err := sp.ListRemoteSessions(ctx, agent.Name)
		ch <- result{remotes: remotes, err: err}
	}()
	r := s.renderer()
	ticker := time.NewTicker(120 * time.Millisecond)
	defer ticker.Stop()
	spin := 0
	frames := []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠿'}
	paint := func() {
		w, h := r.width, r.height
		if nw, nh, ok := getConsoleSize(); ok {
			w, h = nw, nh
		}
		r.resize(w, h)
		f := newFrame(h)
		f.add(fitClip(ccOrange+bold+"✦ "+displayName(agent)+reset, w))
		f.add(fitClip(ccLine+strings.Repeat("─", minInt(w, 60))+reset, w))
		f.add("")
		f.add(fitClip(fmt.Sprintf("  %s%s 正在读取远端会话…%s", ccTeal+bold, string(frames[spin%len(frames)]), reset), w))
		f.add("")
		f.add(fitClip(ccFaint+"ESC 跳过（直接新建）"+reset, w))
		r.draw(f)
		fmt.Fprint(r.s.w, hideCursor)
	}
	paint()
	keyCh := make(chan keyRead, 16)
	done := make(chan struct{})
	defer close(done)
	go readListKeys(s.kr, keyCh, done)
	var remotes []cli.SessionInfo
	for {
		select {
		case res := <-ch:
			if res.err != nil || len(res.remotes) == 0 {
				return "", nil
			}
			remotes = res.remotes
		case <-ticker.C:
			spin++
			paint()
			continue
		case kr, ok := <-keyCh:
			if !ok {
				continue
			}
			if kr.err == nil && isBackKey(kr.key) {
				go func() { <-ch }()
				return "", nil
			}
			continue
		}
		break
	}
	items := make([]string, len(remotes))
	ids := make([]string, len(remotes))
	for i, r := range remotes {
		title := r.Title
		if title == "" {
			title = r.ID
		}
		when := r.Updated
		if when == "" {
			when = "--"
		}
		cwd := r.Cwd
		if cwd == "" {
			cwd = "--"
		}
		items[i] = fmt.Sprintf("%s  %s%s · %s · %s%s", title, dark, truncateCells(r.ID, 16), when, truncateCells(cwd, 30), reset)
		ids[i] = r.ID
	}
	newItem := "＋ 新建会话"
	items = append(items, newItem)
	res, err := s.listPage(listOpts{
		title:        displayName(agent) + " - 远端会话",
		mainTitle:    "恢复远端上下文（空=新建）",
		removedTitle: "",
		mainItems:    &items,
		removedItems: &[]string{},
		hint:         listHint(),
	})
	if err != nil {
		return "", err
	}
	if res.action == "cancel" {
		return "", errInputCancel
	}
	if res.value == newItem {
		return "", nil
	}
	idx := indexOf(items, res.value)
	if idx < 0 || idx >= len(ids) {
		return "", nil
	}
	return ids[idx], nil
}

// runHistoryFlow 是主菜单的历史回放流程：选记录 → 只读回放。
func (s *session) runHistoryFlow() error {
	record, err := s.pickHistoryRecord()
	if err != nil {
		return err
	}
	if record == nil {
		return nil
	}
	return replayRecord(s, record)
}

// RunDirect 交互直达模式：跳过选工具页，直接进入指定 agent 的流程。
// path 非空且为已存在目录时跳过选目录页（直接用该目录）；之后进参数页，
// 启动后循环回参数页直到取消。gui 型 agent 直接拉起，不选目录/参数。
func RunDirect(p cli.Port, agentName, path string) error {
	s, cleanup, err := newSession(p)
	if err != nil {
		return err
	}
	defer cleanup()

	agent, err := resolveAgent(s.p, agentName)
	if err != nil {
		return err
	}
	if path != "" && !dirExists(path) {
		return fmt.Errorf("目录不存在: %s", path)
	}
	if agent.Type == "gui" {
		return s.runGui(agent, path)
	}
	return s.runTuiAgent(agent, path, true)
}
