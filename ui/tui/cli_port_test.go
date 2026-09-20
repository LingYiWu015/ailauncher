package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"testing"

	"cli"
)

// stubCLI 是 CLI 端口的测试桩：内存维护 config/state，记录 run 调用。
// 供 cli_port 接线测试与 runTuiAgent 流程测试共用。
type stubCLI struct {
	cfg      *cli.Config
	states   map[string]*cli.AgentState
	launches [][]string // 每次 run 调用的 argv
}

func newStubCLI(cfg *cli.Config) *stubCLI {
	if cfg == nil {
		cfg = &cli.Config{}
	}
	return &stubCLI{cfg: cfg, states: map[string]*cli.AgentState{}}
}

func (st *stubCLI) Dispatch(argv []string, in io.Reader) (*cli.Result, error) {
	switch argv[0] {
	case "config":
		return cli.JSON(st.cfg), nil
	case "resolve":
		a := st.cfg.ResolveAgent(argv[1])
		if a == nil {
			return nil, fmt.Errorf("未知 agent %s", argv[1])
		}
		return cli.JSON(a), nil
	case "state":
		switch argv[1] {
		case "get":
			td, ok := st.states[argv[2]]
			if !ok {
				td = &cli.AgentState{}
				st.states[argv[2]] = td
			}
			return cli.JSON(td), nil
		case "put":
			var td cli.AgentState
			if err := json.NewDecoder(in).Decode(&td); err != nil {
				return nil, err
			}
			st.states[argv[2]] = &td
			return cli.Text("saved"), nil
		}
		return nil, fmt.Errorf("unknown state subcommand %v", argv[1:])
	case "run":
		st.launches = append(st.launches, sliceCopy(argv))
		return cli.Text("已启动"), nil
	case "agent":
		return st.agent(argv[1:])
	case "session":
		return st.session(argv[1:])
	}
	return nil, fmt.Errorf("unknown command %v", argv)
}

func (st *stubCLI) agent(args []string) (*cli.Result, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("agent 需要子命令")
	}
	switch args[0] {
	case "list":
		var out []cli.AgentSummary
		for _, a := range st.cfg.Agents {
			if a == nil {
				continue
			}
			adapter := "none"
			if a.Extension.Has("adapter") {
				adapter = "acp"
			}
			out = append(out, cli.AgentSummary{Name: a.Name, Display: a.Display, Enabled: true, Adapter: adapter, Source: "config", Installed: true})
		}
		return cli.JSON(out), nil
	case "enable", "disable":
		return cli.Text("ok"), nil
	}
	return nil, fmt.Errorf("unknown agent subcommand %v", args)
}

func (st *stubCLI) session(args []string) (*cli.Result, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("session 需要子命令")
	}
	switch args[0] {
	case "list":
		return cli.JSON([]cli.SessionRecord{}), nil
	case "delete":
		return cli.Text("deleted"), nil
	}
	return nil, fmt.Errorf("unknown session subcommand %v", args)
}

func (st *stubCLI) OpenSession(context.Context, cli.SessionRequest) (cli.Session, error) {
	return nil, fmt.Errorf("stub 不支持 live 会话")
}

func (st *stubCLI) ProbeAgent(_ context.Context, name string) (cli.AgentProbe, error) {
	return cli.AgentProbe{Agent: name, Reachable: true, AgentName: "Stub", AgentVersion: "0"}, nil
}

func (st *stubCLI) ListSessions() ([]cli.SessionRecord, error) { return nil, nil }

func (st *stubCLI) GetSession(cli.SessionID) (*cli.SessionRecord, error) { return nil, nil }

func (st *stubCLI) DeleteSession(cli.SessionID) error { return nil }

func (st *stubCLI) ListRemoteSessions(context.Context, string) ([]cli.SessionInfo, error) {
	return nil, nil
}

func (st *stubCLI) DeleteRemoteSession(context.Context, string, string) error { return nil }

func TestFetchConfig(t *testing.T) {
	st := newStubCLI(&cli.Config{Terminal: "t", Agents: []*cli.Agent{{Name: "a"}}})
	cfg, err := fetchConfig(st)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Terminal != "t" || len(cfg.Agents) != 1 {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestResolveAgent(t *testing.T) {
	st := newStubCLI(&cli.Config{Agents: []*cli.Agent{{Name: "claude", Display: "Claude"}}})
	a, err := resolveAgent(st, "claude")
	if err != nil || a.Name != "claude" {
		t.Fatalf("resolve name = %v err=%v", a, err)
	}
	a, err = resolveAgent(st, "Claude")
	if err != nil || a.Name != "claude" {
		t.Fatalf("resolve display = %v err=%v", a, err)
	}
	if _, err := resolveAgent(st, "nope"); err == nil {
		t.Fatal("unknown agent should error")
	}
}

func TestLoadSaveStateRoundTrip(t *testing.T) {
	st := newStubCLI(nil)
	td, err := loadState(st, "x")
	if err != nil {
		t.Fatal(err)
	}
	if td == nil {
		t.Fatal("loadState should seed non-nil state")
	}
	td.Directories = []string{"D:/a"}
	td.Commands = []string{"--model sonnet"}
	if err := saveState(st, "x", td); err != nil {
		t.Fatal(err)
	}
	got, err := loadState(st, "x")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Directories, []string{"D:/a"}) || !reflect.DeepEqual(got.Commands, []string{"--model sonnet"}) {
		t.Fatalf("round trip mismatch: %+v", got)
	}
}

func TestBuildRunArgv(t *testing.T) {
	// workdir 空也占住 path 槽位，避免 args 首个非 KEY=V token 被当路径
	got := buildRunArgv("x", "", "--model o3")
	want := []string{"run", "x", "", "--model", "o3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildRunArgv = %v, want %v", got, want)
	}
	got = buildRunArgv("x", "D:/proj", "")
	want = []string{"run", "x", "D:/proj"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildRunArgv = %v, want %v", got, want)
	}
}
