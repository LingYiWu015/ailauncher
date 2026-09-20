package core

import (
	"errors"
	"testing"
)

func TestStageVisibility(t *testing.T) {
	if !StageStable.Visible() {
		t.Fatal("stable should always be visible")
	}
	if StageDraft.Visible() || StageChecking.Visible() || StageAbandoned.Visible() {
		t.Fatal("draft/checking/abandoned should never be visible")
	}
	if StageExperimental.Visible() {
		t.Fatal("experimental should be hidden by default")
	}
	t.Setenv(EnvExperimental, "1")
	if !ExperimentalEnabled() || !StageExperimental.Visible() {
		t.Fatal("experimental should be visible when AILAUNCHER_EXPERIMENTAL=1")
	}
}

func TestStageRankOrder(t *testing.T) {
	if StageStable.Rank() >= StageExperimental.Rank() {
		t.Fatal("stable should sort before experimental")
	}
	if StageExperimental.Rank() >= StageChecking.Rank() {
		t.Fatal("experimental should sort before checking")
	}
	if StageChecking.Rank() >= StageDraft.Rank() {
		t.Fatal("checking should sort before draft")
	}
}

func TestUsageErrorWrapping(t *testing.T) {
	err := Usagef("run 需要 <agent>")
	if !errors.Is(err, ErrUsage) {
		t.Fatalf("Usagef error should wrap ErrUsage: %v", err)
	}
	if errors.Is(errors.New("x"), ErrUsage) {
		t.Fatal("plain error should not match ErrUsage")
	}
}

func TestCapabilitiesStableOnly(t *testing.T) {
	// 默认（experimental 关）下，所有已注册单元应为 stable 且可见。
	names := map[string]bool{}
	for _, c := range Capabilities() {
		names[c.Name] = true
		if c.Stage != StageStable {
			t.Fatalf("capability %s stage = %s, want stable", c.Name, c.Stage)
		}
	}
	for _, want := range []string{"run", "list", "version", "help", "migrate", "resolve", "cap", "config", "state"} {
		if !names[want] {
			t.Fatalf("capabilities missing %s (got %v)", want, names)
		}
	}
}

func TestLookup(t *testing.T) {
	if Lookup("run") == nil || Lookup("state") == nil {
		t.Fatal("known units should be findable")
	}
	if Lookup("nope") != nil {
		t.Fatal("unknown unit should be nil")
	}
}

func TestSnapshotCopy(t *testing.T) {
	snap := Snapshot()
	if len(snap) == 0 {
		t.Fatal("registry should not be empty")
	}
	name := snap[0].Name
	// 修改快照不影响内部注册表
	snap[0] = nil
	if Lookup(name) == nil {
		t.Fatalf("snapshot mutation leaked into registry (lost %s)", name)
	}
}
