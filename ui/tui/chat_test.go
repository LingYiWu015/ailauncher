package tui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"cli"
)

func TestRuneWidthCJK(t *testing.T) {
	if runeWidth('a') != 1 {
		t.Fatal("ascii should be 1")
	}
	if runeWidth('你') != 2 {
		t.Fatal("CJK should be 2")
	}
	if runeWidth('Ａ') != 2 {
		t.Fatal("fullwidth should be 2")
	}
}

func TestStrWidthSkipsANSI(t *testing.T) {
	if got := strWidth(red + "ab" + reset); got != 2 {
		t.Fatalf("strWidth = %d, want 2", got)
	}
	if got := strWidth("你好"); got != 4 {
		t.Fatalf("strWidth = %d, want 4", got)
	}
}

func TestFitClipKeepsWidth(t *testing.T) {
	got := fitClip("abcdef", 4)
	if strWidth(got) > 4 || !strings.HasSuffix(got, "…") {
		t.Fatalf("fitClip = %q", got)
	}
	if got := fitClip("ab", 4); got != "ab" {
		t.Fatalf("short line should pass through, got %q", got)
	}
	if got := fitClip(red+"abcdef"+reset, 4); strWidth(got) > 4 {
		t.Fatalf("styled clip too wide: %q", got)
	}
}

func TestWrapCellsCJK(t *testing.T) {
	got := wrapCells("你好世界", 4)
	if len(got) != 2 || got[0] != "你好" || got[1] != "世界" {
		t.Fatalf("wrap = %q", got)
	}
}

func TestRendererDiffOnlyWritesChangedRows(t *testing.T) {
	var buf bytes.Buffer
	r := newRenderer(NewScreen(&buf), 20, 6)
	f := newFrame(6)
	f.add("a")
	f.add("b")
	r.enter()
	r.draw(f)
	first := buf.String()
	if !strings.Contains(first, "a") || !strings.Contains(first, "b") {
		t.Fatalf("first draw missing rows: %q", first)
	}
	buf.Reset()
	same := newFrame(6)
	same.add("a")
	same.add("b")
	r.draw(same)
	if buf.Len() != 0 {
		t.Fatalf("identical frame should write nothing, got %q", buf.String())
	}
	buf.Reset()
	changed := newFrame(6)
	changed.add("a")
	changed.add("c")
	r.draw(changed)
	if !strings.Contains(buf.String(), "c") || strings.Contains(buf.String(), "\x1b[2J") {
		t.Fatalf("diff should rewrite only changed row: %q", buf.String())
	}
}

func TestChatModelStreamAppends(t *testing.T) {
	m := newChatModel("Agent", "")
	m.apply(cli.SessionEvent{Kind: cli.EventAssistantText, Text: "hello"})
	m.apply(cli.SessionEvent{Kind: cli.EventAssistantText, Text: " world"})
	if len(m.blocks) != 1 || m.blocks[0].text != "hello world" {
		t.Fatalf("blocks = %+v", m.blocks)
	}
	m.apply(cli.SessionEvent{Kind: cli.EventAssistantThinking, Text: "想"})
	m.apply(cli.SessionEvent{Kind: cli.EventToolCall, Title: "Read", ToolID: "t1"})
	m.apply(cli.SessionEvent{Kind: cli.EventToolResult, Text: "ok", ToolID: "t1"})
	if len(m.blocks) != 3 {
		t.Fatalf("blocks = %+v", m.blocks)
	}
	if m.blocks[2].role != roleTool || !m.blocks[2].finished || m.blocks[2].detail != "ok" {
		t.Fatalf("tool block = %+v", m.blocks[2])
	}
	m.applyStored(cli.StoredEvent{Kind: cli.EventUserText, Text: "hi"})
	if m.blocks[len(m.blocks)-1].text != "hi" {
		t.Fatalf("replay = %+v", m.blocks)
	}
}

func TestChatModelNewEvents(t *testing.T) {
	m := newChatModel("Agent", "")
	m.apply(cli.SessionEvent{Kind: cli.EventPermission, Title: "删文件", PermID: "p1", Options: []cli.PermissionOption{{ID: "y", Name: "允许", Kind: "allow_once"}}})
	m.apply(cli.SessionEvent{Kind: cli.EventPlan, Text: "○ 第一步"})
	m.apply(cli.SessionEvent{Kind: cli.EventMode, Text: "plan"})
	m.apply(cli.SessionEvent{Kind: cli.EventTitle, Text: "我的会话"})
	m.apply(cli.SessionEvent{Kind: cli.EventCommand, Text: "review"})
	if m.pendingBlock() == nil || m.pendingBlock().permID != "p1" {
		t.Fatalf("pending = %+v", m.blocks)
	}
	if m.mode != "plan" || m.title != "我的会话" {
		t.Fatalf("mode=%q title=%q", m.mode, m.title)
	}
	kinds := map[string]bool{}
	for _, b := range m.blocks {
		kinds[string(b.role)] = true
	}
	if !kinds["permission"] {
		t.Fatalf("blocks = %+v", m.blocks)
	}
}

func TestSlashHelpListsCommands(t *testing.T) {
	m := newChatModel("Agent", "")
	m.commands = []cli.AvailableCommand{{Name: "review", Desc: "看代码"}}
	help := slashHelp(&m)
	if !strings.Contains(help, "/mode") || !strings.Contains(help, "review") {
		t.Fatalf("help = %q", help)
	}
}

func TestRenderBlocksCachesFinished(t *testing.T) {
	m := newChatModel("Agent", "")
	m.apply(cli.SessionEvent{Kind: cli.EventUserText, Text: "hi"})
	m.apply(cli.SessionEvent{Kind: cli.EventAssistantText, Text: "hello"})
	m.apply(cli.SessionEvent{Kind: cli.EventStatus, Status: cli.SessionIdle})
	first := renderBlocks(&m, 40, time.Now())
	second := renderBlocks(&m, 40, time.Now())
	if len(first) != len(second) {
		t.Fatalf("cached render differs: %d vs %d", len(first), len(second))
	}
	cached := 0
	for i := range m.blocks {
		if len(m.blocks[i].cached) > 0 {
			cached++
		}
	}
	if cached != len(m.blocks) {
		t.Fatalf("finished blocks should be cached: %+v", m.blocks)
	}
}

func TestThinkingCollapse(t *testing.T) {
	m := newChatModel("Agent", "")
	m.apply(cli.SessionEvent{Kind: cli.EventAssistantThinking, Text: "a\nb\nc"})
	m.apply(cli.SessionEvent{Kind: cli.EventStatus, Status: cli.SessionIdle})
	full := renderBlocks(&m, 60, time.Now())
	m.thinkingCollapsed = true
	for i := range m.blocks {
		m.blocks[i].cached = nil
	}
	collapsed := renderBlocks(&m, 60, time.Now())
	if len(collapsed) >= len(full) {
		t.Fatalf("collapsed=%d should be shorter than full=%d", len(collapsed), len(full))
	}
}

func TestChatSubmitHistory(t *testing.T) {
	m := newChatModel("Agent", "")
	m.input.insertText("hello")
	if got := m.submit(); got != "hello" {
		t.Fatalf("submit = %q", got)
	}
	if len(m.history) != 1 || m.input.Len() != 0 {
		t.Fatalf("history=%v draft=%q", m.history, m.input.String())
	}
	m.input.insertText("hello")
	m.submit()
	if len(m.history) != 1 {
		t.Fatalf("duplicate history should dedupe: %v", m.history)
	}
}

func TestLayoutChatCursorAndCap(t *testing.T) {
	m := newChatModel("Agent", "")
	m.input.insertText("hi")
	f, row, col := layoutChat(&m, 40, 10, time.Now())
	if row < 1 || col < 1 {
		t.Fatalf("cursor = %d,%d", row, col)
	}
	for _, ln := range f.lines {
		if strWidth(ln) > 40 {
			t.Fatalf("line exceeds width: %q", ln)
		}
	}
}

func TestStatusBarNeverExceeds(t *testing.T) {
	m := newChatModel("Agent", "")
	m.status = "running"
	if w := strWidth(statusBar(&m, 30, time.Now())); w > 30 {
		t.Fatalf("status bar width = %d", w)
	}
}

func TestMainMenuEntries(t *testing.T) {
	entries := mainMenuEntries()
	if len(entries) != 3 || entries[0].key != "run" || entries[1].key != "chat" || entries[2].key != "history" {
		t.Fatalf("entries = %+v", entries)
	}
}
