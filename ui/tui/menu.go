package tui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"cli"
)

// 本文件是显式 TUI 主菜单：启动工具（legacy）/ Agent 会话（Claude Code 风格）/
// 历史会话（sessions.jsonl 回放）。菜单本身用增量渲染器绘制，避免整屏闪烁。
// 按键语义见 keymap.go（统一交互规范）；数字在此页是快选（直接进入）。

type menuEntry struct {
	key   string
	title string
	desc  string
}

func mainMenuEntries() []menuEntry {
	return []menuEntry{
		{key: "run", title: "启动工具", desc: "选工具 → 选目录 → 输参数 → 拉起（legacy）"},
		{key: "chat", title: "Agent 会话", desc: "Claude Code 风格流式会话（ACP 接入也有界面）"},
		{key: "history", title: "历史会话", desc: "回放 sessions.jsonl 本地 transcript"},
	}
}

// runMainMenu 用共享渲染器绘制显式主菜单，返回选中的 key（空串=退出）。
// 数字是快选（直接进入）；Ctrl-C 与 ESC 同为退出。
func (s *session) runMainMenu() (string, error) {
	entries := mainMenuEntries()
	sel := 0
	r := s.renderer()
	draw := func() {
		w, h := r.width, r.height
		if nw, nh, ok := getConsoleSize(); ok {
			w, h = nw, nh
		}
		r.resize(w, h)
		f := newFrame(h)
		f.add(fitClip(ccOrange+bold+"✦ AILauncher"+reset+ccMuted+"  ·  TUI"+reset, w))
		f.add(fitClip(ccLine+strings.Repeat("─", minInt(w, 60))+reset, w))
		f.add("")
		for i, e := range entries {
			marker := "  "
			style := ""
			if i == sel {
				marker = ccTeal + bold + "❯ " + reset
				style = invert
				line := fmt.Sprintf("%s %s  %s%s", e.title, ccFaint+"-"+reset, ccMuted, e.desc)
				f.add(fitClip(marker+style+" "+padRight(stripANSI(" "+line+" "), w-6)+reset, w))
				continue
			}
			line := fmt.Sprintf("%s%s  %s- %s", marker, e.title, ccFaint, e.desc)
			f.add(fitClip(line+reset, w))
		}
		f.add("")
		f.add(fitClip(ccFaint+menuHint+reset, w))
		r.draw(f)
		fmt.Fprint(r.s.w, hideCursor)
	}
	draw()
	for {
		k, err := readKey(s.kr)
		if err != nil {
			return "", err
		}
		if isBackKey(k) {
			return "", nil
		}
		switch k.Name {
		case "up", "left":
			if sel > 0 {
				sel--
				draw()
			}
		case "down", "right":
			if sel < len(entries)-1 {
				sel++
				draw()
			}
		case "home":
			sel = 0
			draw()
		case "end":
			sel = len(entries) - 1
			draw()
		case "return", "space":
			return entries[sel].key, nil
		case "char":
			switch k.Rune {
			case 'q', 'Q':
				return "", nil
			case 'j', 'J':
				if sel < len(entries)-1 {
					sel++
					draw()
				}
			case 'k', 'K':
				if sel > 0 {
					sel--
					draw()
				}
			case 'g', 'G':
				sel = 0
				draw()
			case '1', '2', '3':
				idx := int(k.Rune - '1')
				if idx < len(entries) {
					return entries[idx].key, nil
				}
			}
		}
	}
}

// pickChatAgent 选择要进入会话的 agent：列表即时出现，后台并行探测逐行刷新。
// 摘要来自 agent list（Core 算好，零 IO）；probe 结果进内存缓存，同一次 TUI 内复用。
func (s *session) pickChatAgent() (*cli.Agent, error) {
	agents := enabledAgents(s.cfg)
	if len(agents) == 0 {
		return nil, fmt.Errorf("全部 agent 已禁用（agent enable <name> 恢复）")
	}
	summaries := fetchAgentSummaries(s.p)
	byName := map[string]cli.AgentSummary{}
	for _, sm := range summaries {
		byName[sm.Name] = sm
	}
	items := make([]string, len(agents))
	probes := make([]cli.AgentProbe, len(agents))
	for i, a := range agents {
		sm, ok := byName[a.Name]
		if !ok || sm.Adapter == "" || sm.Adapter == "none" {
			probes[i] = cli.AgentProbe{Agent: a.Name, Display: displayName(a), Error: "暂不支持 ACP，走 run 原启动模式"}
			items[i] = fmt.Sprintf("%s  %s%s%s", a.Display, dark, "run 模式（暂不支持 ACP）", reset)
			continue
		}
		if !sm.Installed {
			probes[i] = cli.AgentProbe{Agent: a.Name, Display: displayName(a), Error: "未找到命令：先安装对应 CLI"}
			items[i] = fmt.Sprintf("%s  %s%s%s", a.Display, dark, "未安装命令（先安装对应 CLI）", reset)
			continue
		}
		if cached, ok := loadProbeCache(a.Name); ok {
			probes[i] = cached
		} else {
			probes[i] = cli.AgentProbe{Agent: a.Name, Display: displayName(a), Error: "探测中…"}
		}
		items[i] = fmt.Sprintf("%s  %s%s%s", a.Display, dark, probeStatusLine(probes[i]), reset)
		if sm.Source == "builtin" && probes[i].Reachable {
			items[i] = fmt.Sprintf("%s  %s%s%s", a.Display, dark, probeStatusLine(probes[i])+" · 一键接入", reset)
		}
	}
	updates, cancels := s.startProbes(agents, byName, items, probes)
	res, err := s.listPageLive(listOpts{
		title:        "Agent 会话 - 选择对象",
		mainTitle:    "Agent 列表",
		removedTitle: "",
		mainItems:    &items,
		removedItems: &[]string{},
		hint:         listHint("e 禁用", "不可达项回车看原因"),
		extraKeys: map[rune]func() (extraResult, error){
			'e': func() (extraResult, error) {
				return extraResult{exit: true}, nil
			},
		},
	}, updates)
	for _, cancel := range cancels {
		cancel()
	}
	if err != nil {
		return nil, err
	}
	if res.action == "cancel" {
		return nil, nil
	}
	if res.action == "exit" {
		idx := indexOf(items, res.value)
		if idx < 0 || idx >= len(agents) {
			return s.pickChatAgent()
		}
		if err := setAgentEnabled(s.p, agents[idx].Name, false); err != nil {
			return nil, err
		}
		if err := s.bootstrap(); err != nil {
			return nil, err
		}
		return s.pickChatAgent()
	}
	idx := indexOf(items, res.value)
	if idx < 0 || idx >= len(agents) {
		return nil, nil
	}
	if !probes[idx].Reachable {
		return nil, fmt.Errorf("%s：%s", displayName(agents[idx]), probeStatusLine(probes[idx]))
	}
	return agents[idx], nil
}

// enabledAgents 只返回启用的 agent（缺省全启用，保证老配置零迁移）。
func enabledAgents(cfg *cli.Config) []*cli.Agent {
	if cfg == nil {
		return nil
	}
	var out []*cli.Agent
	for _, a := range cfg.Agents {
		if a != nil && a.IsEnabled() {
			out = append(out, a)
		}
	}
	return out
}

func setAgentEnabled(p cli.Port, name string, enabled bool) error {
	action := "disable"
	if enabled {
		action = "enable"
	}
	_, err := dispatchNoIn(p, []string{"agent", action, name})
	return err
}

func displayName(agent *cli.Agent) string {
	if agent.Display != "" {
		return agent.Display
	}
	return agent.Name
}

// probeCache 是同一次 TUI 内的探测缓存：选单进出不再重复握手。
var (
	probeCacheMu sync.RWMutex
	probeCache   = map[string]cli.AgentProbe{}
)

func loadProbeCache(name string) (cli.AgentProbe, bool) {
	probeCacheMu.RLock()
	defer probeCacheMu.RUnlock()
	p, ok := probeCache[name]
	return p, ok
}

func storeProbeCache(name string, p cli.AgentProbe) {
	probeCacheMu.Lock()
	defer probeCacheMu.Unlock()
	probeCache[name] = p
}

// startProbes 对 pending 项起后台并行探测：每完成一个发一行刷新；返回更新通道与取消函数。
func (s *session) startProbes(agents []*cli.Agent, byName map[string]cli.AgentSummary, items []string, probes []cli.AgentProbe) (<-chan listUpdate, []context.CancelFunc) {
	var pending []int
	for i, a := range agents {
		sm, ok := byName[a.Name]
		if !ok || sm.Adapter == "" || sm.Adapter == "none" || !sm.Installed {
			continue
		}
		if _, ok := loadProbeCache(a.Name); ok {
			continue
		}
		pending = append(pending, i)
	}
	if len(pending) == 0 {
		return nil, nil
	}
	updates := make(chan listUpdate, len(pending))
	var cancels []context.CancelFunc
	var wg sync.WaitGroup
	for _, i := range pending {
		a := agents[i]
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		cancels = append(cancels, cancel)
		wg.Add(1)
		go func(idx int, agent *cli.Agent, ctx context.Context) {
			defer wg.Done()
			probe := s.probeChatAgentWith(ctx, agent)
			storeProbeCache(agent.Name, probe)
			probes[idx] = probe
			sm := byName[agent.Name]
			status := probeStatusLine(probe)
			if sm.Source == "builtin" && probe.Reachable {
				status += " · 一键接入"
			}
			select {
			case updates <- listUpdate{index: idx, text: fmt.Sprintf("%s  %s%s%s", agent.Display, dark, status, reset)}:
			case <-ctx.Done():
			}
		}(i, a, ctx)
	}
	go func() {
		wg.Wait()
		close(updates)
	}()
	return updates, cancels
}

// probeChatAgent 同步探测一项（仅兜底用；选单主体走后台 startProbes）。
func (s *session) probeChatAgent(agent *cli.Agent) cli.AgentProbe {
	if cached, ok := loadProbeCache(agent.Name); ok {
		return cached
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	return s.probeChatAgentWith(ctx, agent)
}

// probeChatAgentWith 用指定 ctx 探测（后台并行用，可取消）。
func (s *session) probeChatAgentWith(ctx context.Context, agent *cli.Agent) cli.AgentProbe {
	sp, ok := s.p.(cli.SessionPort)
	if !ok {
		return cli.AgentProbe{Agent: agent.Name, Display: displayName(agent), Error: "当前 CLI 端口不支持 live 会话"}
	}
	probe, err := sp.ProbeAgent(ctx, agent.Name)
	if err != nil {
		return cli.AgentProbe{Agent: agent.Name, Display: displayName(agent), Error: err.Error()}
	}
	return probe
}

func probeStatusLine(probe cli.AgentProbe) string {
	if probe.Reachable {
		extra := ""
		if probe.AgentName != "" {
			extra = " · " + probe.AgentName
			if probe.AgentVersion != "" {
				extra += " " + probe.AgentVersion
			}
		}
		return "就绪" + extra
	}
	if probe.Error == "" {
		return "不可用"
	}
	return "不可用：" + probe.Error
}

// askChatPath 询问会话工作目录（空提交=后端默认；ESC/Ctrl-C 取消返回 errInputCancel）。
func (s *session) askChatPath(agent *cli.Agent) (string, error) {
	hint := "空行使用后端默认目录；ESC 返回"
	if agent != nil && agent.RootDir != "" {
		hint = "空行默认: " + agent.RootDir + "；ESC 返回"
	}
	got, err := s.prompt("工作目录（Agent 会话）", hint)
	if err != nil {
		return "", err
	}
	got = strings.TrimSpace(got)
	if got == "" && agent != nil {
		return agent.RootDir, nil
	}
	if got != "" && !dirExists(got) {
		return "", fmt.Errorf("目录不存在: %s", got)
	}
	return got, nil
}

func fetchAgentSummaries(p cli.Port) []cli.AgentSummary {
	res, err := dispatchNoIn(p, []string{"agent", "list"})
	if err != nil {
		return nil
	}
	summaries, ok := res.Data.([]cli.AgentSummary)
	if !ok {
		return nil
	}
	return summaries
}

func fetchSessionList(p cli.Port) ([]cli.SessionRecord, error) {
	res, err := dispatchNoIn(p, []string{"session", "list"})
	if err != nil {
		return nil, err
	}
	records, ok := res.Data.([]cli.SessionRecord)
	if !ok {
		return nil, errPortType("session list", res.Data)
	}
	return records, nil
}

func fetchSessionRecord(p cli.Port, id string) (*cli.SessionRecord, error) {
	res, err := dispatchNoIn(p, []string{"session", "get", id})
	if err != nil {
		return nil, err
	}
	record, ok := res.Data.(*cli.SessionRecord)
	if !ok {
		return nil, errPortType("session get", res.Data)
	}
	return record, nil
}

// pickHistoryRecord 选择一条历史会话：回车回放，d 删除本地 transcript（删空自动返回）。
func (s *session) pickHistoryRecord() (*cli.SessionRecord, error) {
	for {
		records, err := fetchSessionList(s.p)
		if err != nil {
			return nil, err
		}
		if len(records) == 0 {
			return nil, fmt.Errorf("暂无历史会话（先跑一次 Agent 会话）")
		}
		items := make([]string, len(records))
		ids := make([]string, len(records))
		for i, r := range records {
			when := r.UpdatedAt.Format("01-02 15:04")
			if r.UpdatedAt.IsZero() {
				when = "--"
			}
			title := string(r.ID)
			if r.Title != "" {
				title = r.Title
			}
			items[i] = fmt.Sprintf("%s  %s%s · %s · %s · %d 条%s", title, dark, r.Agent, when, r.Status, len(r.Events), reset)
			ids[i] = string(r.ID)
		}
		var delErr error
		res, err := s.listPage(listOpts{
			title:        "历史会话 - 选择回放",
			mainTitle:    "本地 transcript（sessions.jsonl）",
			removedTitle: "",
			mainItems:    &items,
			removedItems: &[]string{},
			hint:         listHint("d 删除本地记录"),
			onDeleteSelected: func(index int, _ string) error {
				if index < 0 || index >= len(ids) {
					return nil
				}
				if err := deleteSessionRecord(s.p, ids[index]); err != nil {
					delErr = err
					return err
				}
				ids = append(ids[:index], ids[index+1:]...)
				return nil
			},
			extraKeys: map[rune]func() (extraResult, error){
				'd': func() (extraResult, error) {
					return extraResult{del: true}, nil
				},
			},
		})
		if delErr != nil {
			return nil, delErr
		}
		if err != nil {
			return nil, err
		}
		if res.action == "cancel" {
			// 删空后 listPage 自动 cancel：回到循环顶部，records 为空则报暂无
			if len(ids) == 0 {
				continue
			}
			return nil, nil
		}
		idx := indexOf(items, res.value)
		if idx < 0 || idx >= len(ids) {
			return nil, nil
		}
		return fetchSessionRecord(s.p, ids[idx])
	}
}

func deleteSessionRecord(p cli.Port, id string) error {
	_, err := dispatchNoIn(p, []string{"session", "delete", id})
	return err
}

// RunHistoryReplay 以只读方式回放一条本地 transcript：同一 chatModel + 同一布局，
// 按键只滚动不发送。ESC/Ctrl-C/q 退出。
func RunHistoryReplay(p cli.Port, id string) error {
	s, cleanup, err := newSession(p)
	if err != nil {
		return err
	}
	defer cleanup()
	record, err := fetchSessionRecord(p, id)
	if err != nil {
		return err
	}
	if record == nil {
		return fmt.Errorf("未找到本地 session %q", id)
	}
	return replayRecord(s, record)
}

// replayViewport 是回放页的视口高度（2 顶栏 + 1 提示 + 2 状态区 + 1 底栏）。
func replayViewport(h int) int {
	v := h - 6
	if v < 3 {
		v = 3
	}
	return v
}

func replayRecord(s *session, record *cli.SessionRecord) error {
	m := newChatModel(record.Agent, record.Workdir)
	m.sessionID = record.ID
	m.status = string(record.Status)
	for _, e := range record.Events {
		m.applyStored(e)
	}
	m.follow = true
	m.scroll = 0
	m.flash(fmt.Sprintf("回放 %d 条 · 只读", len(record.Events)))

	r := s.renderer()
	paint := func() {
		w, h := r.width, r.height
		if nw, nh, ok := getConsoleSize(); ok {
			w, h = nw, nh
		}
		r.resize(w, h)
		f, _, _ := layoutChat(&m, w, replayViewport(h), time.Now())
		fmt.Fprint(r.s.w, hideCursor)
		r.draw(f)
	}
	scrollUp := func(step int) {
		m.follow = false
		m.scroll += step
		m.clampScroll()
		m.touch()
		paint()
	}
	scrollDown := func(step int) {
		m.scroll -= step
		if m.scroll <= 0 {
			m.scroll = 0
			m.follow = true
		}
		m.touch()
		paint()
	}
	paint()
	for {
		k, err := readKey(s.kr)
		if err != nil {
			return err
		}
		if isBackKey(k) || isBackRune(k) {
			return nil
		}
		_, h := r.width, r.height
		if _, nh, ok := getConsoleSize(); ok {
			h = nh
		}
		viewport := replayViewport(h)
		switch k.Name {
		case "up":
			scrollUp(1)
		case "down":
			scrollDown(1)
		case "pageup":
			scrollUp(viewport)
		case "pagedown":
			scrollDown(viewport)
		case "home", "ctrl-a", "g":
			m.follow = false
			m.scroll = len(m.blocks) + 20
			m.clampScroll()
			m.touch()
			paint()
		case "end", "ctrl-e":
			m.follow = true
			m.scroll = 0
			m.touch()
			paint()
		case "space":
			scrollDown(viewport)
		case "char":
			switch k.Rune {
			case 'j', 'J':
				scrollUp(1)
			case 'k', 'K':
				scrollDown(1)
			case 'g', 'G':
				m.follow = false
				m.scroll = len(m.blocks) + 20
				m.clampScroll()
				m.touch()
				paint()
			}
		}
	}
}
