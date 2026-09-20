package core

import (
	"reflect"
	"sort"
	"testing"
)

func TestParseArgs(t *testing.T) {
	envs, args := ParseArgs([]string{"FOO=bar", "--model", "o3", "-t", "codex.exe"})
	if envs["FOO"] != "bar" {
		t.Fatalf("env FOO = %q, want bar", envs["FOO"])
	}
	want := []string{"--model", "o3", "-t", "codex.exe"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %v, want %v", args, want)
	}
}

func TestParseArgsEdge(t *testing.T) {
	envs, args := ParseArgs([]string{"FOO=", "=x", "1A=b", "plain"})
	if envs["FOO"] != "" {
		t.Fatalf("FOO= should be env with empty value, got %q", envs["FOO"])
	}
	want := []string{"=x", "1A=b", "plain"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("=x / 1A=b / plain should be args, got %v", args)
	}
	if _, ok := envs["1A"]; ok {
		t.Fatal("1A=b must not be treated as env (digit-first key)")
	}
}

func TestIsEnvKey(t *testing.T) {
	for _, ok := range []string{"FOO", "foo", "FOO_BAR", "A1", "_X"} {
		if !isEnvKey(ok) {
			t.Fatalf("isEnvKey(%q) should be true", ok)
		}
	}
	for _, no := range []string{"", "1A", "FOO-BAR", "FOO BAR", "FOO.BAR"} {
		if isEnvKey(no) {
			t.Fatalf("isEnvKey(%q) should be false", no)
		}
	}
}

func TestIsEnvToken(t *testing.T) {
	if !isEnvToken("FOO=bar") {
		t.Fatal("FOO=bar should be env token")
	}
	if isEnvToken("plain") || isEnvToken("=x") || isEnvToken("1A=b") {
		t.Fatal("plain/=x/1A=b should not be env tokens")
	}
}

func TestMergeEnvOverrides(t *testing.T) {
	merged := mergeEnv(map[string]string{"A": "1", "B": "2"}, map[string]string{"B": "3"})
	if merged["A"] != "1" || merged["B"] != "3" {
		t.Fatalf("merge should keep base and override overlap: %v", merged)
	}
}

func TestEnvPairs(t *testing.T) {
	pairs := envPairs(map[string]string{"A": "1", "B": "2"})
	sort.Strings(pairs)
	want := []string{"A=1", "B=2"}
	if !reflect.DeepEqual(pairs, want) {
		t.Fatalf("pairs = %v, want %v", pairs, want)
	}
}

func TestDirExists(t *testing.T) {
	if !dirExists(t.TempDir()) {
		t.Fatal("temp dir should exist")
	}
	if dirExists(t.TempDir() + "/nope") {
		t.Fatal("nonexistent path should be false")
	}
}
