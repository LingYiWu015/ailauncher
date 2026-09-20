package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrateConfigLegacyToolsRejected(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	old := `{"tools":[{"name":"a","exec":"a","type":"tui"}]}`
	if err := os.WriteFile(p, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := MigrateConfig(p)
	if err == nil {
		t.Fatalf("old tools[] should be rejected, got changed=%v", changed)
	}
}

func TestMigrateConfigV3NoChange(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	v3 := `{"terminal":"t","agents":[{"name":"a","exec":"a","type":"tui"}]}`
	if err := os.WriteFile(p, []byte(v3), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := MigrateConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("v4 config should not be rewritten")
	}
	b, _ := os.ReadFile(p)
	if string(b) != v3 {
		t.Fatal("v4 config file should be untouched byte-for-byte")
	}
}

func TestMigrateConfigMissingFileNoError(t *testing.T) {
	dir := t.TempDir()
	changed, err := MigrateConfig(filepath.Join(dir, "nope.json"))
	if err != nil || changed {
		t.Fatalf("missing file: changed=%v err=%v, want no-op no-error", changed, err)
	}
}

func TestMigrateStateNullCleanup(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "data.json")
	old := `{"claude":{"directories":["D:\\a"],"removedDirs":[],"commands":[],"removedCmds":null,"seeded":true}}`
	if err := os.WriteFile(p, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := MigrateState(p)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("null residue should report changed")
	}
	var s State
	b, _ := os.ReadFile(p)
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	st := s["claude"]
	if st.RemovedCmds == nil {
		t.Fatal("removedCmds should be [] not null")
	}
	if len(st.RemovedCmds) != 0 {
		t.Fatalf("removedCmds should be empty, got %v", st.RemovedCmds)
	}
	if len(st.Directories) != 1 || st.Directories[0] != `D:\a` {
		t.Fatalf("directories lost: %v", st.Directories)
	}
	if !st.Seeded {
		t.Fatal("seeded lost")
	}
	changed2, err := MigrateState(p)
	if err != nil || changed2 {
		t.Fatalf("second migrate should be no-op: changed=%v err=%v", changed2, err)
	}
}

func TestMigrateStateCleanNoChange(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "data.json")
	clean := `{"a":{"directories":[],"removedDirs":[],"commands":[],"removedCmds":[],"seeded":false}}`
	if err := os.WriteFile(p, []byte(clean), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := MigrateState(p)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("clean state should be untouched")
	}
}

func TestMigrateStateTopLevelNullSkipped(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "data.json")
	if err := os.WriteFile(p, []byte(`{"b":null}`), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := MigrateState(p)
	if err != nil {
		t.Fatalf("top-level null should not panic: %v", err)
	}
	if changed {
		t.Fatal("null-only entry should not be rewritten")
	}
}

func TestMigrateUnit(t *testing.T) {
	ctx := testCtx(t, `{"agents":[]}`, `{"a":{"removedCmds":null}}`)
	res, err := migrateUnit(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "data.json") {
		t.Fatalf("migrate should report data.json cleanup: %q", res.Text)
	}
}
