package tui

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cli"
)

// newTestSession 用注入的输入流 + 丢弃输出的 Screen 构造 session，无需真实 TTY。
func newTestSession(input string) *session {
	return &session{
		s:  NewScreen(&bytes.Buffer{}),
		kr: newBytesKeyReader([]byte(input)),
	}
}

func TestListPageDigitJumpAndSelect(t *testing.T) {
	s := newTestSession("2\r") // 数字跳转到第 2 项，回车选中
	items := []string{"a", "b", "c"}
	res, err := s.listPage(listOpts{
		title:        "t",
		mainTitle:    "main",
		removedTitle: "",
		mainItems:    &items,
		removedItems: &[]string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.action != "select" || res.value != "b" {
		t.Fatalf("got %s %q, want select b", res.action, res.value)
	}
	if len(items) != 3 {
		t.Fatalf("main items mutated unexpectedly: %v", items)
	}
}

func TestListPageUpDownKeys(t *testing.T) {
	s := newTestSession("\x1b[B\x1b[B\r") // down down enter → 第 3 项
	items := []string{"a", "b", "c"}
	res, err := s.listPage(listOpts{
		title:        "t",
		mainTitle:    "main",
		removedTitle: "",
		mainItems:    &items,
		removedItems: &[]string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.value != "c" {
		t.Fatalf("got %q, want c", res.value)
	}
}

func TestListPageRemoveAndRestore(t *testing.T) {
	s := newTestSession("d\ttu\t\r") // d 移除首项 → tab 到已移除 → u 移回 → tab 回主列表 → 回车选中
	main := []string{"a", "b", "c"}
	rem := []string{}
	res, err := s.listPage(listOpts{
		title:        "t",
		mainTitle:    "main",
		removedTitle: "removed",
		mainItems:    &main,
		removedItems: &rem,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.action != "select" {
		t.Fatalf("got action %s", res.action)
	}
	// d 后 main=[b,c] rem=[a]；u 移回 → main=[b,c,a]
	if len(main) != 3 || main[0] != "b" || main[2] != "a" {
		t.Fatalf("main = %v, want [b c a]", main)
	}
	if len(rem) != 0 {
		t.Fatalf("rem = %v, want empty", rem)
	}
}

func TestListPageCancel(t *testing.T) {
	s := newTestSession("q")
	items := []string{"a", "b"}
	res, err := s.listPage(listOpts{
		title:        "t",
		mainTitle:    "main",
		removedTitle: "",
		mainItems:    &items,
		removedItems: &[]string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.action != "cancel" {
		t.Fatalf("got %s, want cancel", res.action)
	}
}

func TestListPageManualAdd(t *testing.T) {
	s := newTestSession("m\r") // m 手动输入 → 追加并选中 → 回车
	items := []string{"a"}
	res, err := s.listPage(listOpts{
		title:        "t",
		mainTitle:    "main",
		removedTitle: "",
		mainItems:    &items,
		removedItems: &[]string{},
		onManual: func() (string, error) {
			return "X", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.value != "X" {
		t.Fatalf("got %q, want X", res.value)
	}
	if len(items) != 2 || items[1] != "X" {
		t.Fatalf("items = %v, want [a X]", items)
	}
}

func TestListPageExtraKey(t *testing.T) {
	s := newTestSession("n\r") // n 额外键追加 → 回车选中追加项
	items := []string{"a"}
	res, err := s.listPage(listOpts{
		title:        "t",
		mainTitle:    "main",
		removedTitle: "",
		mainItems:    &items,
		removedItems: &[]string{},
		extraKeys: map[rune]func() (extraResult, error){
			'n': func() (extraResult, error) {
				return extraResult{add: "new"}, nil
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.value != "new" {
		t.Fatalf("got %q, want new", res.value)
	}
}

func TestListPageOnChangeErrorStopsMutation(t *testing.T) {
	s := newTestSession("d")
	items := []string{"a", "b"}
	wantErr := errors.New("save failed")
	_, err := s.listPage(listOpts{
		title:        "t",
		mainTitle:    "main",
		removedTitle: "",
		mainItems:    &items,
		removedItems: &[]string{},
		onChange: func() error {
			return wantErr
		},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("listPage error = %v, want %v", err, wantErr)
	}
}
func TestArgsPageInputRun(t *testing.T) {
	s := newTestSession("--model opus\r")
	td := &cli.AgentState{}
	res, err := s.runArgs(td, "claude", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.action != "run" || res.value != "--model opus" {
		t.Fatalf("got %s %q, want run --model opus", res.action, res.value)
	}
	// 输入的命令应进入历史（去重置顶）
	if len(td.Commands) != 1 || td.Commands[0] != "--model opus" {
		t.Fatalf("commands = %v, want [--model opus]", td.Commands)
	}
}

func TestArgsPageSaveErrorStops(t *testing.T) {
	s := newTestSession("\td")
	td := &cli.AgentState{Commands: []string{"old"}}
	wantErr := errors.New("save failed")
	_, err := s.runArgs(td, "claude", func() error { return wantErr })
	if !errors.Is(err, wantErr) {
		t.Fatalf("runArgs error = %v, want %v", err, wantErr)
	}
}
func TestArgsPageHistorySelect(t *testing.T) {
	s := newTestSession("\t1\r") // tab 到历史 → 数字 1 跳第 0 项 → 回车运行
	td := &cli.AgentState{Commands: []string{"-q", "--verbose"}}
	res, err := s.runArgs(td, "claude", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.value != "-q" {
		t.Fatalf("got %q, want -q", res.value)
	}
}

func TestArgsPageCancel(t *testing.T) {
	s := newTestSession("\x1b") // 输入框按 ESC 取消
	td := &cli.AgentState{}
	res, err := s.runArgs(td, "claude", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.action != "cancel" {
		t.Fatalf("got %s, want cancel", res.action)
	}
}

func TestSelectDirWithRoot(t *testing.T) {
	base := t.TempDir()
	proj := filepath.Join(base, "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}

	s := newTestSession("\r") // 项目列表只有一项，直接回车
	s.cfg = &cli.Config{}
	agent := &cli.Agent{Name: "x", Display: "X", Type: "tui"}
	td := &cli.AgentState{RootDir: base}

	got, err := s.selectDir(agent, td, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != proj {
		t.Fatalf("got %q, want %q", got, proj)
	}
	if !td.Seeded {
		t.Fatal("td.Seeded 应为 true")
	}
}

func TestSelectDirSeedingAndRootChoose(t *testing.T) {
	base := t.TempDir()
	proj := filepath.Join(base, "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}

	// 无 rootDir：先选根（唯一候选 = proj 的父目录），再选项目
	s := newTestSession("\r\r")
	s.cfg = &cli.Config{}
	agent := &cli.Agent{Name: "x", Display: "X", Type: "tui", Directories: []string{proj}}
	td := &cli.AgentState{}

	got, err := s.selectDir(agent, td, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != proj {
		t.Fatalf("got %q, want %q", got, proj)
	}
	if td.RootDir != base {
		t.Fatalf("td.RootDir = %q, want %q", td.RootDir, base)
	}
	if !td.Seeded {
		t.Fatal("td.Seeded 应为 true")
	}
}

// TestSelectDirManualRootChoose 回归：根目录页按 M 手动输入后回车，
// listPage 会把原始路径追加进 items（与 candidates 不再一一对应），
// 旧代码 candidates[idx] 越界 panic，此处应正确选中手动根并进入项目列表。
func TestSelectDirManualRootChoose(t *testing.T) {
	base := t.TempDir()
	root2 := t.TempDir()
	proj := filepath.Join(root2, "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}

	// m → 输入 root2 → 回车选中追加项 → 项目列表回车选中 proj
	s := newTestSession("m" + root2 + "\r\r\r")
	s.cfg = &cli.Config{DefaultDirectories: []string{base}}
	agent := &cli.Agent{Name: "x", Display: "X", Type: "tui"}
	td := &cli.AgentState{}

	got, err := s.selectDir(agent, td, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != proj {
		t.Fatalf("got %q, want %q", got, proj)
	}
	if td.RootDir != root2 {
		t.Fatalf("td.RootDir = %q, want %q", td.RootDir, root2)
	}
}
func TestListPageRenderContent(t *testing.T) {
	var buf bytes.Buffer
	s := &session{s: NewScreen(&buf), kr: newBytesKeyReader([]byte("q"))}
	items := []string{"alpha", "beta"}
	if _, err := s.listPage(listOpts{
		title:        "标题",
		mainTitle:    "主列表",
		removedTitle: "已移除",
		mainItems:    &items,
		removedItems: &[]string{"gone"},
		hint:         "底部提示",
	}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"\x1b[2J", "标题", "主列表", "已移除", "alpha", "beta", "gone", "底部提示"} {
		if !strings.Contains(out, want) {
			t.Errorf("渲染输出缺少 %q", want)
		}
	}
}
