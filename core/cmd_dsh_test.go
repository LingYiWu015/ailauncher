package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

// dshCfgJSON 造一个带 extension.dsh.repo 的配置 JSON（指向临时仓库目录）。
// 路径含反斜杠必须 JSON 转义，由 json.Marshal 序列化，不手工拼串。
func dshCfgJSON(repo string) string {
	raw, err := json.Marshal(struct {
		Repo string `json:"repo"`
	}{Repo: repo})
	if err != nil {
		panic(err)
	}
	b, err := json.Marshal(map[string]json.RawMessage{
		"extension": json.RawMessage(fmt.Sprintf(`{"dsh":%s}`, raw)),
	})
	if err != nil {
		panic(err)
	}
	return string(b)
}

// agentWithDshRepo 构造一个带 extension.dsh.repo 的 Agent。
func agentWithDshRepo(t *testing.T, repo string) *Agent {
	t.Helper()
	raw, err := json.Marshal(struct {
		Repo string `json:"repo"`
	}{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	return &Agent{Name: "dsh", Type: "gui", Extension: Extension{"dsh": raw}}
}

func TestDshUnitRegisters(t *testing.T) {
	if u := Lookup("dsh"); u == nil {
		t.Fatal("dsh 应已自注册")
	}
	if u := Lookup("dsh"); u.Kind != UnitCommand || !u.Stage.Visible() {
		t.Fatalf("dsh 声明 = %+v（应 stable command，默认可见）", u)
	}
}

func TestDshRepoDirFromExtension(t *testing.T) {
	repo := t.TempDir()
	ctx := testCtx(t, dshCfgJSON(repo), `{}`)
	got, err := dshRepoDir(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != repo {
		t.Fatalf("repo = %q, want %q", got, repo)
	}
}

func TestDshRepoDirFallbackEnv(t *testing.T) {
	repo := t.TempDir()
	ctx := testCtx(t, `{}`, `{}`)
	t.Setenv("DSH_REPO", repo)
	got, err := dshRepoDir(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != repo {
		t.Fatalf("repo = %q, want %q", got, repo)
	}
}

func TestDshRepoDirExtensionBeatsEnv(t *testing.T) {
	extRepo, envRepo := t.TempDir(), t.TempDir()
	ctx := testCtx(t, dshCfgJSON(extRepo), `{}`)
	t.Setenv("DSH_REPO", envRepo)
	got, err := dshRepoDir(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != extRepo {
		t.Fatalf("repo = %q, want extension 优先 %q", got, extRepo)
	}
}

func TestDshRepoDirMissing(t *testing.T) {
	ctx := testCtx(t, `{}`, `{}`)
	if _, err := dshRepoDir(ctx); err == nil {
		t.Fatal("未配置 repo 应报错")
	}
}

func TestDshRepoDirNotExist(t *testing.T) {
	ctx := testCtx(t, dshCfgJSON(t.TempDir()+"/nope"), `{}`)
	if _, err := dshRepoDir(ctx); err == nil {
		t.Fatal("仓库不存在应报错")
	}
}

func TestDshRepoFromAgent(t *testing.T) {
	repo := t.TempDir()
	if got := dshRepoFromAgent(agentWithDshRepo(t, repo)); got != repo {
		t.Fatalf("repo = %q, want %q", got, repo)
	}
	if got := dshRepoFromAgent(&Agent{}); got != "" {
		t.Fatalf("无 extension 应返回空，got %q", got)
	}
	if got := dshRepoFromAgent(nil); got != "" {
		t.Fatalf("nil agent 应返回空，got %q", got)
	}
	if got := dshRepoFromAgent(agentWithDshRepo(t, repo+"/nope")); got != "" {
		t.Fatalf("仓库不存在应返回空，got %q", got)
	}
}

func TestDshWebSpec(t *testing.T) {
	repo := t.TempDir()
	argv, wd, env := dshWebSpec(repo, map[string]string{"DEEPSEEK_API_KEY": "k"}, []string{"--port", "8080"})
	want := []string{"pnpm", "dsh", "web", "--port", "8080"}
	if !reflect.DeepEqual(argv, want) {
		t.Fatalf("argv = %v, want %v", argv, want)
	}
	if wd != repo {
		t.Fatalf("wd = %q, want %q", wd, repo)
	}
	found := false
	for _, e := range env {
		if e == "DEEPSEEK_API_KEY=k" {
			found = true
		}
	}
	if !found {
		t.Fatalf("env 应含 DEEPSEEK_API_KEY：%v", env)
	}
}

func TestDshWebPort(t *testing.T) {
	if got := dshWebPort(nil); got != "3080" {
		t.Fatalf("默认端口 = %q, want 3080", got)
	}
	if got := dshWebPort([]string{"--port", "8080"}); got != "8080" {
		t.Fatalf("--port 8080 = %q", got)
	}
	if got := dshWebPort([]string{"--port=9090"}); got != "9090" {
		t.Fatalf("--port=9090 = %q", got)
	}
	if got := dshWebPort([]string{"--port", "0"}); got != "0" {
		t.Fatalf("--port 0 = %q", got)
	}
}

func TestDshHeadlessCmd(t *testing.T) {
	repo := t.TempDir()
	cmd := dshHeadlessCmd(repo, []string{"写一个 hello world"}, []string{"A=B"})
	if cmd.Dir != repo {
		t.Fatalf("cmd.Dir = %q, want %q", cmd.Dir, repo)
	}
	want := []string{"pnpm", "dsh", "--profile", "headless", "写一个 hello world"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("cmd.Args = %v, want %v", cmd.Args, want)
	}
	if len(cmd.Env) != 1 || cmd.Env[0] != "A=B" {
		t.Fatalf("cmd.Env = %v", cmd.Env)
	}
	if cmd.Stdin != os.Stdin || cmd.Stdout != os.Stdout || cmd.Stderr != os.Stderr {
		t.Fatal("headless 应透传标准 IO")
	}
}

func TestDshHeadlessNoTask(t *testing.T) {
	repo := t.TempDir()
	ctx := testCtx(t, dshCfgJSON(repo), `{}`)
	if _, err := dshUnit(ctx, []string{"headless"}); !errors.Is(err, ErrUsage) {
		t.Fatalf("无任务应 usage：%v", err)
	}
}

func TestDshUnknownSubcommand(t *testing.T) {
	repo := t.TempDir()
	ctx := testCtx(t, dshCfgJSON(repo), `{}`)
	if _, err := dshUnit(ctx, []string{"bogus"}); !errors.Is(err, ErrUsage) {
		t.Fatalf("未知子命令应 usage：%v", err)
	}
}

func TestDshHelpDoesNotRequireRepo(t *testing.T) {
	ctx := testCtx(t, `{}`, `{}`)
	res, err := dshUnit(ctx, []string{"help"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"DeepSeek Harness", "dsh", "web", "headless", "DSH_REPO", "--port"} {
		if !strings.Contains(res.Text, want) {
			t.Fatalf("help 应含 %q：%q", want, res.Text)
		}
	}
}

// TestExpandWorkdirDshRepo 验证仓库服务型 agent 的 cwd 覆盖：extension.dsh.repo
// 优先于传入 workdir，无 repo 时沿用原回退链（USERPROFILE / 传入 workdir）。
func TestExpandWorkdirDshRepo(t *testing.T) {
	repo := t.TempDir()
	other := t.TempDir()

	if got := expandWorkdir(agentWithDshRepo(t, repo), ""); got != repo {
		t.Fatalf("wd = %q, want 仓库根 %q", got, repo)
	}
	if got := expandWorkdir(agentWithDshRepo(t, repo), other); got != repo {
		t.Fatalf("有 repo 应忽略传入 wd：got %q, want %q", got, repo)
	}
	if got := expandWorkdir(&Agent{}, other); got != other {
		t.Fatalf("无 repo 应保留传入 wd：got %q, want %q", got, other)
	}
	if want := os.Getenv("USERPROFILE"); want != "" {
		if got := expandWorkdir(&Agent{}, ""); got != want {
			t.Fatalf("无 repo 无 path 应回退 USERPROFILE：got %q, want %q", got, want)
		}
	}
}
