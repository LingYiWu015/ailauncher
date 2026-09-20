package core

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const testCfgJSON = `{"terminal":"alacritty","agents":[{"name":"claude","display":"claude (Claude Code)","exec":"claude","type":"tui"}]}`

// testCtx 构造一个带临时 config/data 文件的 Ctx（单元测试用）。
func testCtx(t *testing.T, cfgJSON, stateJSON string) *Ctx {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	statePath := filepath.Join(dir, "data.json")
	if err := os.WriteFile(cfgPath, []byte(cfgJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, []byte(stateJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	st, err := LoadState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	return &Ctx{BaseDir: dir, ConfigPath: cfgPath, StatePath: statePath, Cfg: cfg, St: st, In: strings.NewReader("")}
}

// launchCall 捕获 launchFn 桩的调用参数。
type launchCall struct {
	agent *Agent
	wd    string
	envs  map[string]string
	extra []string
	term  string
}

// stubLaunch 临时替换 launchFn，捕获调用参数；返回还原函数。
func stubLaunch(t *testing.T, capture *launchCall) func() {
	t.Helper()
	orig := launchFn
	launchFn = func(agent *Agent, wd string, envs map[string]string, extra []string, term string) error {
		capture.agent, capture.wd, capture.envs, capture.extra, capture.term = agent, wd, envs, extra, term
		return nil
	}
	return func() { launchFn = orig }
}

func TestRunUnitLaunch(t *testing.T) {
	ctx := testCtx(t, testCfgJSON, `{}`)
	var got launchCall
	restore := stubLaunch(t, &got)
	defer restore()

	res, err := runUnit(ctx, []string{"claude"})
	if err != nil {
		t.Fatal(err)
	}
	if got.agent.Name != "claude" || got.wd != "" || got.term != "alacritty" {
		t.Fatalf("launch args = %v %v %v", got.agent.Name, got.wd, got.term)
	}
	if !strings.Contains(res.Text, "claude (Claude Code)") {
		t.Fatalf("result should mention display name: %q", res.Text)
	}
}

func TestRunUnitPathEnvArgs(t *testing.T) {
	ctx := testCtx(t, testCfgJSON, `{}`)
	wd := t.TempDir()
	var got launchCall
	restore := stubLaunch(t, &got)
	defer restore()

	if _, err := runUnit(ctx, []string{"claude", wd, "MODEL=o3", "--foo"}); err != nil {
		t.Fatal(err)
	}
	if got.wd != wd || got.envs["MODEL"] != "o3" || !reflect.DeepEqual(got.extra, []string{"--foo"}) {
		t.Fatalf("parsed = wd:%q env:%v extra:%v", got.wd, got.envs, got.extra)
	}
}

func TestRunUnitEnvFirstNoPathSlot(t *testing.T) {
	ctx := testCtx(t, testCfgJSON, `{}`)
	dir := t.TempDir()
	var got launchCall
	restore := stubLaunch(t, &got)
	defer restore()

	// path 必须是首个位置参数：首 token 是 KEY=V 时 path 槽位被跳过，
	// 后续非 KEY=V token 都归入附加参数。
	if _, err := runUnit(ctx, []string{"claude", "MODEL=o3", dir, "--foo"}); err != nil {
		t.Fatal(err)
	}
	if got.wd != "" {
		t.Fatalf("wd should stay empty when first token is env, got %q", got.wd)
	}
	if got.envs["MODEL"] != "o3" || !reflect.DeepEqual(got.extra, []string{dir, "--foo"}) {
		t.Fatalf("env=%v extra=%v", got.envs, got.extra)
	}
}

func TestRunUnitErrors(t *testing.T) {
	ctx := testCtx(t, testCfgJSON, `{}`)
	var got launchCall
	restore := stubLaunch(t, &got)
	defer restore()

	if _, err := runUnit(ctx, nil); !errors.Is(err, ErrUsage) {
		t.Fatalf("no-arg run should be usage error: %v", err)
	}
	if _, err := runUnit(ctx, []string{"nope"}); err == nil {
		t.Fatal("unknown agent should error")
	}
	if _, err := runUnit(ctx, []string{"claude", filepath.Join(t.TempDir(), "nope")}); err == nil {
		t.Fatal("nonexistent workdir should error")
	}
	// 启动失败 → 报错带 agent 名
	orig := launchFn
	launchFn = func(*Agent, string, map[string]string, []string, string) error { return errors.New("boom") }
	defer func() { launchFn = orig }()
	if _, err := runUnit(ctx, []string{"claude"}); err == nil || !strings.Contains(err.Error(), "claude") {
		t.Fatalf("launch error should mention agent: %v", err)
	}
}

func TestListUnit(t *testing.T) {
	ctx := testCtx(t, testCfgJSON, `{}`)
	res, err := listUnit(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Kind != ResultJSON {
		t.Fatalf("list should be JSON, got %s", res.Kind)
	}
	agents, ok := res.Data.([]*Agent)
	if !ok || len(agents) != 1 || agents[0].Name != "claude" {
		t.Fatalf("list data = %v", res.Data)
	}
}

func TestResolveUnit(t *testing.T) {
	ctx := testCtx(t, testCfgJSON, `{}`)
	res, err := resolveUnit(ctx, []string{"claude (Claude Code)"})
	if err != nil {
		t.Fatal(err)
	}
	a, ok := res.Data.(*Agent)
	if !ok || a.Name != "claude" {
		t.Fatalf("resolve data = %v", res.Data)
	}
	if _, err := resolveUnit(ctx, []string{"nope"}); err == nil {
		t.Fatal("unknown resolve should error")
	}
	if _, err := resolveUnit(ctx, nil); !errors.Is(err, ErrUsage) {
		t.Fatal("resolve without name should be usage error")
	}
}

func TestCapUnit(t *testing.T) {
	ctx := testCtx(t, testCfgJSON, `{}`)
	res, err := capUnit(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	caps, ok := res.Data.([]Capability)
	if !ok {
		t.Fatalf("cap data = %T", res.Data)
	}
	found := false
	for _, c := range caps {
		if c.Name == "run" {
			found = true
		}
	}
	if !found {
		t.Fatal("capabilities should include run")
	}
}

func TestConfigUnit(t *testing.T) {
	ctx := testCtx(t, testCfgJSON, `{}`)
	res, err := configUnit(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg, ok := res.Data.(*Config)
	if !ok || cfg.Terminal != "alacritty" {
		t.Fatalf("config data = %v", res.Data)
	}
}

func TestVersionUnit(t *testing.T) {
	ctx := testCtx(t, `{}`, `{}`)
	res, err := versionUnit(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != Version {
		t.Fatalf("version = %q, want %q", res.Text, Version)
	}
}

func TestHelpUnit(t *testing.T) {
	ctx := testCtx(t, `{}`, `{}`)
	res, err := helpUnit(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "run") || !strings.Contains(res.Text, "cap") {
		t.Fatalf("help should list commands: %q", res.Text)
	}
	res, err = helpUnit(ctx, []string{"run"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "run <agent>") {
		t.Fatalf("help run should show run's declared: %q", res.Text)
	}
	if _, err := helpUnit(ctx, []string{"nope"}); err == nil {
		t.Fatal("help for unknown command should error")
	}
	if _, err := helpUnit(ctx, []string{"a", "b"}); !errors.Is(err, ErrUsage) {
		t.Fatal("help with 2 args should be usage error")
	}
}

func TestStateGetUnitAutoSeeds(t *testing.T) {
	ctx := testCtx(t, testCfgJSON, `{}`)
	res, err := stateGet(ctx, []string{"claude"})
	if err != nil {
		t.Fatal(err)
	}
	st, ok := res.Data.(*AgentState)
	if !ok || st.Directories == nil {
		t.Fatalf("state get data = %v", res.Data)
	}
}

func TestStatePutUnitRoundTrip(t *testing.T) {
	ctx := testCtx(t, testCfgJSON, `{}`)
	ctx.In = strings.NewReader(`{"directories":["D:\\x"],"seeded":true}`)
	res, err := statePut(ctx, []string{"opencode"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "opencode") {
		t.Fatalf("put result should mention agent: %q", res.Text)
	}
	// 重新装载：状态已落盘
	st, err := LoadState(ctx.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if !st["opencode"].Seeded || st["opencode"].Directories[0] != `D:\x` {
		t.Fatalf("persisted state = %+v", st["opencode"])
	}
	// get 读回一致
	g, err := stateGet(ctx, []string{"opencode"})
	if err != nil {
		t.Fatal(err)
	}
	got := g.Data.(*AgentState)
	b, _ := json.Marshal(got)
	if !strings.Contains(string(b), `D:\\x`) {
		t.Fatalf("get data = %s", b)
	}
}

func TestStatePutUnitBadJSON(t *testing.T) {
	ctx := testCtx(t, testCfgJSON, `{}`)
	ctx.In = strings.NewReader(`{bad json`)
	if _, err := statePut(ctx, []string{"opencode"}); err == nil {
		t.Fatal("bad JSON should error")
	}
}

func TestStatePutUnitNormalizesNullArrays(t *testing.T) {
	ctx := testCtx(t, testCfgJSON, `{}`)
	// 只给 directories，其余数组字段缺省 → 归一化为 [] 而非 null
	ctx.In = strings.NewReader(`{"directories":["D:\\x"],"seeded":true}`)
	if _, err := statePut(ctx, []string{"opencode"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(ctx.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, null := range []string{`"removedDirs": null`, `"commands": null`, `"removedCmds": null`} {
		if strings.Contains(string(raw), null) {
			t.Fatalf("persisted state 含 null 数组 %s: %s", null, raw)
		}
	}
}

func TestStateUnitSubcommandErrors(t *testing.T) {
	ctx := testCtx(t, `{}`, `{}`)
	if _, err := stateUnit(ctx, nil); !errors.Is(err, ErrUsage) {
		t.Fatal("state without subcommand should be usage error")
	}
	if _, err := stateUnit(ctx, []string{"bogus"}); !errors.Is(err, ErrUsage) {
		t.Fatal("unknown state subcommand should be usage error")
	}
}
