package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestResolveAgentByNameAndDisplay(t *testing.T) {
	cfg := &Config{Agents: []*Agent{
		{Name: "claude", Display: "claude (Claude Code)"},
		{Name: "codex", Display: "codex"},
	}}
	if cfg.ResolveAgent("claude") == nil {
		t.Fatal("resolve by name failed")
	}
	if cfg.ResolveAgent("codex") == nil {
		t.Fatal("resolve by display failed")
	}
	if cfg.ResolveAgent("") != nil || cfg.ResolveAgent("nope") != nil {
		t.Fatal("should be nil for empty/unknown")
	}
}

func TestLoadConfigMissingReturnsEmpty(t *testing.T) {
	cfg, err := LoadConfig(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Agents) != 0 {
		t.Fatalf("missing config should yield empty agents, got %v", cfg.Agents)
	}
}

func TestConfigRoundTripWithExtension(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	content := `{"terminal":"t","defaultDirectories":["D:\\a"],"agents":[{"name":"a","exec":"a","type":"tui","extension":{"adapter":{"kind":"x"}}}]}`
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Terminal != "t" || len(cfg.DefaultDirectories) != 1 {
		t.Fatalf("config fields lost: %+v", cfg)
	}
	ext := cfg.Agents[0].Extension
	if !ext.Has("adapter") {
		t.Fatal("extension.adapter should be preserved")
	}
	var adapter struct{ Kind string }
	if err := ext.Decode("adapter", &adapter); err != nil {
		t.Fatal(err)
	}
	if adapter.Kind != "x" {
		t.Fatalf("adapter kind = %q, want x", adapter.Kind)
	}
	// Save 后 extension 仍在 JSON 里
	if err := cfg.Save(p); err != nil {
		t.Fatal(err)
	}
	again, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if !again.Agents[0].Extension.Has("adapter") {
		t.Fatal("extension lost after save/reload")
	}
}

func TestExtensionSetAndDecode(t *testing.T) {
	var x Extension = Extension{}
	if err := x.Set("kind", "tui"); err != nil {
		t.Fatal(err)
	}
	var kind string
	if err := x.Decode("kind", &kind); err != nil {
		t.Fatal(err)
	}
	if kind != "tui" {
		t.Fatalf("kind = %q, want tui", kind)
	}
	if err := x.Decode("missing", &kind); err != nil {
		t.Fatalf("missing key decode should be no-op: %v", err)
	}
	var nilExt Extension
	if err := nilExt.Set("k", 1); err == nil {
		t.Fatal("Set on nil Extension should error")
	}
}

func TestStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "data.json")
	s := State{}
	st := s.Get("claude")
	st.Directories = []string{"D:/a", "D:/b"}
	st.Commands = []string{"--model sonnet"}
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	got, err := LoadState(p)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got["claude"].Directories, st.Directories) {
		t.Fatalf("dirs = %v, want %v", got["claude"].Directories, st.Directories)
	}
	// JSON 顶层形状：按 agent name 分 key
	var raw map[string]json.RawMessage
	b, _ := os.ReadFile(p)
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["claude"]; !ok {
		t.Fatal("top-level key should be agent name")
	}
}

func TestStateGetNilMapDoesNotPanic(t *testing.T) {
	var s State
	st := s.Get("new-agent")
	if st == nil || st.Directories == nil || st.Commands == nil {
		t.Fatalf("nil State.Get should return initialized state: %+v", st)
	}
	if _, ok := s["new-agent"]; ok {
		t.Fatal("nil map cannot be populated through value receiver")
	}
}

func TestAgentStateNormalize(t *testing.T) {
	st := &AgentState{} // 四个数组全 nil
	if !st.normalize() {
		t.Fatal("normalize on all-nil should report change")
	}
	for _, sl := range [][]string{st.Directories, st.RemovedDirs, st.Commands, st.RemovedCmds} {
		if sl == nil {
			t.Fatal("nil slice should be normalized to empty")
		}
	}
	if st.normalize() {
		t.Fatal("normalize on already-normalized should be a no-op")
	}
}

func TestLoadStateMissingReturnsEmpty(t *testing.T) {
	s, err := LoadState(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatal(err)
	}
	if s == nil {
		t.Fatal("missing state should be non-nil empty map")
	}
}
