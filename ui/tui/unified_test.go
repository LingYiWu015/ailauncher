package tui

import (
	"errors"
	"strings"
	"testing"

	"cli"
)

func TestLineEditorKeySemantics(t *testing.T) {
	var ed lineEditor
	ed.insertText("ab")
	if _, _, handled := ed.key(Key{Name: "char", Rune: 'q'}); !handled || ed.String() != "abq" {
		t.Fatalf("input q should insert, got %q", ed.String())
	}
	ed.key(Key{Name: "left"})
	ed.key(Key{Name: "backspace"})
	if ed.String() != "aq" {
		t.Fatalf("left+backspace = %q, want aq", ed.String())
	}
	ed.key(Key{Name: "ctrl-k"})
	if ed.String() != "a" {
		t.Fatalf("ctrl-k = %q, want a", ed.String())
	}
	ed.key(Key{Name: "ctrl-u"})
	if ed.String() != "" {
		t.Fatalf("ctrl-u = %q, want empty", ed.String())
	}
	ed.insertText("foo bar")
	ed.key(Key{Name: "ctrl-w"})
	if ed.String() != "foo " {
		t.Fatalf("ctrl-w = %q, want 'foo '", ed.String())
	}
	if confirm, cancel, _ := ed.key(Key{Name: "return"}); !confirm || cancel {
		t.Fatal("return should confirm")
	}
	if confirm, cancel, _ := ed.key(Key{Name: "escape"}); confirm || !cancel {
		t.Fatal("escape should cancel")
	}
	if confirm, cancel, _ := ed.key(Key{Name: "ctrl-c"}); confirm || !cancel {
		t.Fatal("ctrl-c should cancel")
	}
}

func TestLineEditorViewCursor(t *testing.T) {
	var ed lineEditor
	ed.insertText("你好ab")
	ed.moveLeft()
	lines, row, col := ed.view(8)
	if len(lines) != 1 || row != 0 || col != 5 {
		t.Fatalf("view = %v row=%d col=%d", lines, row, col)
	}
}

func TestWindowSliceKeepsSelectionVisible(t *testing.T) {
	s, e := windowSlice(100, 90, 10)
	if e-s != 10 || s > 90 || e <= 90 {
		t.Fatalf("window = %d,%d", s, e)
	}
	s, e = windowSlice(3, 0, 10)
	if s != 0 || e != 3 {
		t.Fatalf("window = %d,%d", s, e)
	}
}

func TestBackKeysUnified(t *testing.T) {
	if !isBackKey(Key{Name: "escape"}) || !isBackKey(Key{Name: "ctrl-c"}) {
		t.Fatal("escape and ctrl-c should both be back keys")
	}
	if !isBackRune(Key{Name: "char", Rune: 'q'}) {
		t.Fatal("q should be back in list context")
	}
	strip := stripANSI(red + "你好" + reset)
	if strip != "你好" {
		t.Fatalf("strip = %q", strip)
	}
}

func TestPromptCancelVsEmpty(t *testing.T) {
	s := newTestSession("\x1b")
	if _, err := s.prompt("q?", ""); !errors.Is(err, errInputCancel) {
		t.Fatalf("escape should cancel, err=%v", err)
	}
	s = newTestSession("\r")
	got, err := s.prompt("q?", "")
	if err != nil || got != "" {
		t.Fatalf("empty submit = %q err=%v", got, err)
	}
	s = newTestSession("q\r")
	got, err = s.prompt("q?", "")
	if err != nil || got != "q" {
		t.Fatalf("q should insert, got %q err=%v", got, err)
	}
}

func TestHintsUnified(t *testing.T) {
	for _, h := range []string{listHint(), dualHint(), argsHint, menuHint} {
		if !strings.Contains(h, "ESC") || !strings.Contains(h, "Enter") {
			t.Fatalf("hint missing ESC/Enter: %q", h)
		}
	}
}

func TestReplayScrollClamped(t *testing.T) {
	m := newChatModel("a", "")
	m.scroll = 9999
	m.clampScroll()
	if m.scroll > len(m.blocks)+20 {
		t.Fatalf("scroll = %d", m.scroll)
	}
}

func TestFetchAgentSummaries(t *testing.T) {
	st := newStubCLI(&cli.Config{Agents: []*cli.Agent{{Name: "a", Display: "A"}}})
	got := fetchAgentSummaries(st)
	if len(got) != 1 || got[0].Name != "a" || got[0].Adapter != "none" {
		t.Fatalf("summaries = %+v", got)
	}
}

func TestEnabledAgentsFiltersDisabled(t *testing.T) {
	off := false
	cfg := &cli.Config{Agents: []*cli.Agent{{Name: "a"}, {Name: "b", Enabled: &off}}}
	got := enabledAgents(cfg)
	if len(got) != 1 || got[0].Name != "a" {
		t.Fatalf("enabled = %+v", got)
	}
}
