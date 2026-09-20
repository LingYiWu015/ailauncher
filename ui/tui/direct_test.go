package tui

import (
	"os"
	"path/filepath"
	"testing"

	"cli"
)

// newDirectSession 构造 runTuiAgent 测试用 session：注入字节输入流 + stub CLI，
// 避免真实拉起进程。返回 (session, 记录 run 调用的 stub)。
func newDirectSession(t *testing.T, input string) (*session, *stubCLI) {
	t.Helper()
	s := newTestSession(input)
	s.cfg = &cli.Config{}
	s.p = newStubCLI(nil)
	return s, s.p.(*stubCLI)
}

// newTempProj 建一个临时目录下的单项目结构，返回 (base, proj)。
func newTempProj(t *testing.T) (string, string) {
	t.Helper()
	base := t.TempDir()
	proj := filepath.Join(base, "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	return base, proj
}

func TestRunTuiAgentDirectSkipsDirPage(t *testing.T) {
	s, st := newDirectSession(t, "\r") // 参数页回车（空命令）→ 启动
	agent := &cli.Agent{Name: "x", Display: "X", Type: "tui"}
	base := t.TempDir()

	if err := s.runTuiAgent(agent, base, false); err != nil {
		t.Fatal(err)
	}
	if len(st.launches) != 1 || st.launches[0][2] != base {
		t.Fatalf("launched = %v, want workdir=%s（应跳过目录页直接用 preWorkdir）", st.launches, base)
	}
}

func TestRunTuiAgentNoPreDirSelectsDir(t *testing.T) {
	_, proj := newTempProj(t)
	s, st := newDirectSession(t, "\r\r\r") // 选根回车 + 项目列表回车 + 参数页回车
	agent := &cli.Agent{Name: "x", Display: "X", Type: "tui", Directories: []string{proj}}

	if err := s.runTuiAgent(agent, "", false); err != nil {
		t.Fatal(err)
	}
	if len(st.launches) != 1 || st.launches[0][2] != proj {
		t.Fatalf("launched = %v, want workdir=%s（无 preWorkdir 应走目录选择）", st.launches, proj)
	}
	if !st.states["x"].Seeded {
		t.Fatal("应完成播种")
	}
}

func TestRunTuiAgentStayLoopsArgsPage(t *testing.T) {
	s, st := newDirectSession(t, "a\rb\r\x1b") // 两次输入回车启动，ESC 取消退出
	agent := &cli.Agent{Name: "x", Display: "X", Type: "tui"}
	base := t.TempDir()

	if err := s.runTuiAgent(agent, base, true); err != nil {
		t.Fatal(err)
	}
	if len(st.launches) != 2 {
		t.Fatalf("launched = %v, want 2 次启动（stay 模式启动后应循环回参数页）", st.launches)
	}
	// 历史命令应被落盘（state put）
	if cmds := st.states["x"].Commands; len(cmds) != 2 || cmds[0] != "b" || cmds[1] != "a" {
		t.Fatalf("commands = %v, want [b a]（去重置顶）", cmds)
	}
}

func TestRunTuiAgentArgsCancelNoLaunch(t *testing.T) {
	s, st := newDirectSession(t, "\x1b") // 参数页 ESC 取消
	agent := &cli.Agent{Name: "x", Display: "X", Type: "tui"}
	base := t.TempDir()

	if err := s.runTuiAgent(agent, base, true); err != nil {
		t.Fatal(err)
	}
	if len(st.launches) != 0 {
		t.Fatalf("launched = %v, want 0（参数页取消不应启动）", st.launches)
	}
}

func TestRunTuiAgentDirCancelNoLaunch(t *testing.T) {
	s, st := newDirectSession(t, "qq") // 选根页 q 跳过 → 手动模式目录列表 q 取消
	agent := &cli.Agent{Name: "x", Display: "X", Type: "tui"}

	if err := s.runTuiAgent(agent, "", false); err != nil {
		t.Fatal(err)
	}
	if len(st.launches) != 0 {
		t.Fatalf("launched = %v, want 0（目录页取消不应启动）", st.launches)
	}
}

func TestRunTuiAgentUnknownDirPrePassed(t *testing.T) {
	s, st := newDirectSession(t, "\r")
	agent := &cli.Agent{Name: "x", Display: "X", Type: "tui"}

	// preWorkdir 非空且不存在：runTuiAgent 不校验（校验在 RunDirect），直接用它启动。
	if err := s.runTuiAgent(agent, filepath.Join("Z:", "no", "such"), false); err != nil {
		t.Fatal(err)
	}
	if len(st.launches) != 1 {
		t.Fatalf("launched = %v, want 1", st.launches)
	}
}

func TestRunTuiAgentArgsPassedToRun(t *testing.T) {
	s, st := newDirectSession(t, "--model opus\r") // 参数页输入非空命令
	agent := &cli.Agent{Name: "x", Display: "X", Type: "tui"}
	base := t.TempDir()

	if err := s.runTuiAgent(agent, base, false); err != nil {
		t.Fatal(err)
	}
	if len(st.launches) != 1 {
		t.Fatalf("launched = %v, want 1", st.launches)
	}
	got := st.launches[0]
	want := []string{"run", "x", base, "--model", "opus"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("launched argv = %v, want %v", got, want)
		}
	}
}

func TestRunGuiShortCircuits(t *testing.T) {
	s, st := newDirectSession(t, "")
	agent := &cli.Agent{Name: "g", Display: "G", Type: "gui"}

	if err := s.runGui(agent, ""); err != nil {
		t.Fatal(err)
	}
	if len(st.launches) != 1 {
		t.Fatalf("launched = %v, want 1", st.launches)
	}
	// gui 无 workdir：argv 只有 [run, g]，不占 path 槽位
	if len(st.launches[0]) != 2 || st.launches[0][0] != "run" || st.launches[0][1] != "g" {
		t.Fatalf("launched argv = %v, want [run g]", st.launches[0])
	}
}
