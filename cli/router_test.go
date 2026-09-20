package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"core"
)

// fakeUnits 合成测试单元：隔离 core 注册表，专注路由行为。
func fakeUnits() []*core.Unit {
	return []*core.Unit{
		{Name: "list", Stage: core.StageStable, Kind: core.UnitQuery, Declared: "列出",
			Run: func(ctx *core.Ctx, args []string) (*core.Result, error) {
				return core.JSON(ctx.Cfg.Agents), nil
			}},
		{Name: "boom", Stage: core.StageStable, Kind: core.UnitCommand, Declared: "运行时爆炸",
			Run: func(ctx *core.Ctx, args []string) (*core.Result, error) {
				return nil, errors.New("runtime boom")
			}},
		{Name: "usage", Stage: core.StageStable, Kind: core.UnitCommand, Declared: "用法错",
			Run: func(ctx *core.Ctx, args []string) (*core.Result, error) {
				return nil, core.Usagef("usage 示例错误")
			}},
		{Name: "echo", Stage: core.StageStable, Kind: core.UnitCommand, Declared: "回显",
			Run: func(ctx *core.Ctx, args []string) (*core.Result, error) {
				return core.Text(strings.Join(args, " ")), nil
			}},
	}
}

// setupBase 造一个带 config.json/data.json 的临时数据目录。
func setupBase(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"),
		[]byte(`{"terminal":"t","agents":[{"name":"a","exec":"a","type":"tui"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "data.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestDispatchKnownRoute(t *testing.T) {
	r := New(fakeUnits(), nil)
	r.SetBaseDir(setupBase(t))
	res, err := r.Dispatch([]string{"list"}, strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	agents, ok := res.Data.([]*core.Agent)
	if !ok || len(agents) != 1 || agents[0].Name != "a" {
		t.Fatalf("dispatch(list) data = %v", res.Data)
	}
}

func TestDispatchArgsPassed(t *testing.T) {
	r := New(fakeUnits(), nil)
	r.SetBaseDir(setupBase(t))
	res, err := r.Dispatch([]string{"echo", "a", "b"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "a b" {
		t.Fatalf("echo = %q, want %q", res.Text, "a b")
	}
}

func TestDispatchUnknownDetailed(t *testing.T) {
	r := New(fakeUnits(), nil)
	r.SetBaseDir(setupBase(t))
	_, err := r.Dispatch([]string{"nope"}, nil)
	if err == nil {
		t.Fatal("unknown command should error")
	}
	if !IsUnknown(err) {
		t.Fatalf("should be unknown error: %v", err)
	}
	msg := err.Error()
	for _, want := range []string{"未知命令或 agent：nope", "list", "[stable/query]", "cap"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("unknown error should mention %q: %s", want, msg)
		}
	}
	if ExitCode(err) != 2 {
		t.Fatalf("unknown command exit = %d, want 2", ExitCode(err))
	}
}

func TestDispatchEmptyArgv(t *testing.T) {
	r := New(fakeUnits(), nil)
	_, err := r.Dispatch(nil, nil)
	if !errors.Is(err, core.ErrUsage) || ExitCode(err) != 2 {
		t.Fatalf("empty argv should be usage (exit 2): %v / %d", err, ExitCode(err))
	}
}

func TestDispatchRuntimeError(t *testing.T) {
	r := New(fakeUnits(), nil)
	r.SetBaseDir(setupBase(t))
	_, err := r.Dispatch([]string{"boom"}, nil)
	if err == nil || ExitCode(err) != 1 {
		t.Fatalf("runtime error exit = %d, want 1", ExitCode(err))
	}
}

func TestDispatchUsageError(t *testing.T) {
	r := New(fakeUnits(), nil)
	r.SetBaseDir(setupBase(t))
	_, err := r.Dispatch([]string{"usage"}, nil)
	if !errors.Is(err, core.ErrUsage) || ExitCode(err) != 2 {
		t.Fatalf("usage error exit = %d, want 2", ExitCode(err))
	}
}

func TestDispatchExperimentalHiddenThenEnabled(t *testing.T) {
	exp := &core.Unit{Name: "exp", Stage: core.StageExperimental, Kind: core.UnitQuery, Declared: "实验",
		Run: func(ctx *core.Ctx, args []string) (*core.Result, error) {
			return core.Text("exp ran"), nil
		}}
	r := New([]*core.Unit{exp}, nil)
	r.SetBaseDir(setupBase(t))

	if _, err := r.Dispatch([]string{"exp"}, nil); !IsUnknown(err) {
		t.Fatalf("experimental should be hidden by default: %v", err)
	}
	t.Setenv(core.EnvExperimental, "1")
	res, err := r.Dispatch([]string{"exp"}, nil)
	if err != nil || res.Text != "exp ran" {
		t.Fatalf("experimental should route when enabled: res=%v err=%v", res, err)
	}
}

func TestDispatchProtoLabRouted(t *testing.T) {
	proto := []*core.Unit{{Name: "proto1", Stage: core.StageStable, Kind: core.UnitCommand, Declared: "proto",
		Run: func(ctx *core.Ctx, args []string) (*core.Result, error) {
			return core.Text("proto ran"), nil
		}}}
	r := New(nil, proto)
	r.SetBaseDir(setupBase(t))
	res, err := r.Dispatch([]string{"proto1"}, nil)
	if err != nil || res.Text != "proto ran" {
		t.Fatalf("proto-lab should route: res=%v err=%v", res, err)
	}
}

func TestCoreWinsOverProtoSameName(t *testing.T) {
	coreUnit := &core.Unit{Name: "x", Stage: core.StageStable, Kind: core.UnitCommand, Declared: "core",
		Run: func(ctx *core.Ctx, args []string) (*core.Result, error) { return core.Text("core x"), nil }}
	protoUnit := &core.Unit{Name: "x", Stage: core.StageStable, Kind: core.UnitCommand, Declared: "proto",
		Run: func(ctx *core.Ctx, args []string) (*core.Result, error) { return core.Text("proto x"), nil }}
	r := New([]*core.Unit{coreUnit}, []*core.Unit{protoUnit})
	r.SetBaseDir(setupBase(t))
	res, err := r.Dispatch([]string{"x"}, nil)
	if err != nil || res.Text != "core x" {
		t.Fatalf("core should win over proto: res=%v err=%v", res, err)
	}
}

func TestCapabilitiesReflectStageAndEnv(t *testing.T) {
	units := append(fakeUnits(),
		&core.Unit{Name: "exp", Stage: core.StageExperimental, Kind: core.UnitQuery, Declared: "实验",
			Run: func(ctx *core.Ctx, args []string) (*core.Result, error) { return nil, nil }})
	r := New(units, nil)

	caps := r.capabilities()
	for _, c := range caps {
		if c.Name == "exp" {
			t.Fatal("experimental should not appear in capabilities by default")
		}
	}
	t.Setenv(core.EnvExperimental, "1")
	found := false
	for _, c := range r.capabilities() {
		if c.Name == "exp" {
			found = true
		}
	}
	if !found {
		t.Fatal("experimental should appear in capabilities when enabled")
	}
}

func TestDispatchConfigStateInjection(t *testing.T) {
	unit := &core.Unit{Name: "cfgcheck", Stage: core.StageStable, Kind: core.UnitQuery, Declared: "cfg",
		Run: func(ctx *core.Ctx, args []string) (*core.Result, error) {
			return core.JSON(map[string]any{"agents": len(ctx.Cfg.Agents), "base": ctx.BaseDir, "state": ctx.StatePath != ""}), nil
		}}
	r := New([]*core.Unit{unit}, nil)
	r.SetBaseDir(setupBase(t))
	res, err := r.Dispatch([]string{"cfgcheck"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	m := res.Data.(map[string]any)
	if m["agents"] != 1 || m["state"] != true {
		t.Fatalf("ctx injection wrong: %v", m)
	}
}

func TestFormat(t *testing.T) {
	b, err := Format(core.Text("hi"))
	if err != nil || string(b) != "hi\n" {
		t.Fatalf("text format = %q err=%v", b, err)
	}
	b, err = Format(core.JSON(map[string]string{"a": "1"}))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("json format not parseable: %v", err)
	}
	if m["a"] != "1" {
		t.Fatalf("json format = %s", b)
	}
}

func TestExitCode(t *testing.T) {
	if ExitCode(nil) != 0 {
		t.Fatal("nil → 0")
	}
	if ExitCode(errors.New("x")) != 1 {
		t.Fatal("runtime error → 1")
	}
	if ExitCode(core.Usagef("x")) != 2 {
		t.Fatal("usage → 2")
	}
}

func TestNewDefault(t *testing.T) {
	r := NewDefault()
	if r == nil {
		t.Fatal("NewDefault should build a router")
	}
}
