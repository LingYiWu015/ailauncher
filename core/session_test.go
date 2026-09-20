package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeAdapter(t *testing.T) {
	raw, err := json.Marshal(AdapterSpec{Kind: "acp", Command: "agent", Permission: "allow"})
	if err != nil {
		t.Fatal(err)
	}
	agent := &Agent{Name: "a", Extension: Extension{"adapter": raw}}
	spec, err := DecodeAdapter(agent)
	if err != nil || spec.Kind != "acp" || spec.Permission != "allow" {
		t.Fatalf("DecodeAdapter = %+v, %v", spec, err)
	}
	if _, err := DecodeAdapter(&Agent{Name: "legacy"}); err == nil || !strings.Contains(err.Error(), "run legacy") {
		t.Fatalf("legacy adapter error = %v", err)
	}
	bad := &Agent{Name: "bad", Extension: Extension{"adapter": json.RawMessage(`{"kind":"acp"}`)}}
	if _, err := DecodeAdapter(bad); err == nil {
		t.Fatal("missing command should fail")
	}
}

func TestFileSessionStoreRoundTrip(t *testing.T) {
	base := t.TempDir()
	store := NewFileSessionStore(base)
	if err := store.Create(SessionRecord{ID: "one", Agent: "a", Adapter: "acp", Workdir: base}); err != nil {
		t.Fatal(err)
	}
	if err := store.Append("one", SessionEvent{Kind: EventUserText, Text: "hello", Status: SessionRunning}); err != nil {
		t.Fatal(err)
	}
	if err := store.Append("one", SessionEvent{Kind: EventAssistantText, Text: "hi", Status: SessionIdle}); err != nil {
		t.Fatal(err)
	}
	record, err := store.Get("one")
	if err != nil {
		t.Fatal(err)
	}
	if record == nil || record.Status != SessionIdle || len(record.Events) != 2 || record.Events[1].Seq != 2 {
		t.Fatalf("record = %+v", record)
	}
	if _, err := os.Stat(filepath.Join(base, DefaultSessionsFile)); err != nil {
		t.Fatal(err)
	}
}

func TestFileSessionStoreRejectsDuplicateID(t *testing.T) {
	store := NewFileSessionStore(t.TempDir())
	record := SessionRecord{ID: "one", Agent: "a", Adapter: "acp"}
	if err := store.Create(record); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(record); err == nil {
		t.Fatal("duplicate session id should fail instead of hiding transcript")
	}
}

func TestSessionUnit(t *testing.T) {
	store := NewFileSessionStore(t.TempDir())
	if err := store.Create(SessionRecord{ID: "one", Agent: "a", Adapter: "acp"}); err != nil {
		t.Fatal(err)
	}
	ctx := &Ctx{Sessions: store}
	res, err := sessionUnit(ctx, []string{"list"})
	if err != nil || len(res.Data.([]SessionRecord)) != 1 {
		t.Fatalf("list = %#v err=%v", res, err)
	}
	res, err = sessionUnit(ctx, []string{"get", "one"})
	if err != nil || res.Data.(*SessionRecord).ID != "one" {
		t.Fatalf("get = %#v err=%v", res, err)
	}
}

func TestSessionStoreUpdateAndDelete(t *testing.T) {
	store := NewFileSessionStore(t.TempDir())
	if err := store.Create(SessionRecord{ID: "one", Agent: "a", Adapter: "acp"}); err != nil {
		t.Fatal(err)
	}
	title := "我的会话"
	mode := "plan"
	if err := store.Update("one", SessionPatch{Title: &title, Mode: &mode}); err != nil {
		t.Fatal(err)
	}
	record, err := store.Get("one")
	if err != nil || record.Title != title || record.Mode != mode {
		t.Fatalf("updated = %+v err=%v", record, err)
	}
	if err := store.Append("one", SessionEvent{Kind: EventTitle, Text: "新标题"}); err != nil {
		t.Fatal(err)
	}
	record, err = store.Get("one")
	if err != nil || record.Title != "新标题" {
		t.Fatalf("title event = %+v err=%v", record, err)
	}
	if err := store.Delete("one"); err != nil {
		t.Fatal(err)
	}
	record, err = store.Get("one")
	if err != nil || record != nil {
		t.Fatalf("deleted = %+v err=%v", record, err)
	}
}

func TestSessionUnitDeleteAndProbe(t *testing.T) {
	store := NewFileSessionStore(t.TempDir())
	if err := store.Create(SessionRecord{ID: "one", Agent: "a", Adapter: "acp"}); err != nil {
		t.Fatal(err)
	}
	ctx := &Ctx{Sessions: store}
	if _, err := sessionUnit(ctx, []string{"delete", "one"}); err != nil {
		t.Fatal(err)
	}
	if _, err := sessionUnit(ctx, []string{"delete", "one"}); err == nil {
		t.Fatal("delete missing should fail")
	}
	probeCtx := &Ctx{Sessions: store, Cfg: &Config{Agents: []*Agent{{Name: "legacy"}}}}
	res, err := sessionUnit(probeCtx, []string{"probe", "legacy"})
	if err != nil {
		t.Fatal(err)
	}
	probe, ok := res.Data.(AgentProbe)
	if !ok || probe.Reachable || probe.Error == "" {
		t.Fatalf("probe = %#v", res.Data)
	}
}

func TestEffectiveAdapterBuiltin(t *testing.T) {
	spec, source, err := EffectiveAdapter(&Agent{Name: "opencode", Exec: "opencode"})
	if err != nil || source != "builtin" || spec.Kind != "acp" || spec.Command != "opencode" {
		t.Fatalf("builtin = %+v %q %v", spec, source, err)
	}
	if _, _, err := EffectiveAdapter(&Agent{Name: "claude", Exec: "claude"}); err == nil {
		t.Fatal("claude should have no builtin adapter")
	}
}

func TestAgentEnableDisable(t *testing.T) {
	dir := t.TempDir()
	configPath := dir + "/config.json"
	cfg := &Config{Agents: []*Agent{{Name: "a", Exec: "a", Type: "tui"}}}
	if err := cfg.Save(configPath); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx := &Ctx{Cfg: loaded, ConfigPath: configPath, Sessions: NewFileSessionStore(dir)}
	if _, err := agentUnit(ctx, []string{"disable", "a"}); err != nil {
		t.Fatal(err)
	}
	if loaded.ResolveAgent("a").IsEnabled() {
		t.Fatal("should be disabled")
	}
	again, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if again.ResolveAgent("a").IsEnabled() {
		t.Fatal("disabled should persist")
	}
}

func TestSessionStoreMemIndex(t *testing.T) {
	store := NewFileSessionStore(t.TempDir())
	if err := store.Create(SessionRecord{ID: "one", Agent: "a", Adapter: "acp"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		if err := store.Append("one", SessionEvent{Kind: EventAssistantText, Text: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	record, err := store.Get("one")
	if err != nil || len(record.Events) != 50 || record.Events[49].Seq != 50 {
		t.Fatalf("indexed = %+v err=%v", record, err)
	}
	list, err := store.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %+v err=%v", list, err)
	}
}
