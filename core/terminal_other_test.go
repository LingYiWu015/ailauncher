//go:build !windows

package core

import (
	"reflect"
	"testing"
)

func TestTerminalArgvUnix(t *testing.T) {
	cmdline := "K=V claude"
	got := terminalArgv("/usr/bin/alacritty", "/home/x", cmdline)
	want := []string{"/usr/bin/alacritty", "--working-directory", "/home/x", "-e", "sh", "-lc", cmdline}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %v, want %v", got, want)
	}
}

func TestBuildTUIShellCmdlineUnix(t *testing.T) {
	a := &Agent{Name: "claude", Exec: "claude", Args: "--verbose"}
	got := buildTUIShellCmdline(a, map[string]string{"K": "V"}, []string{"--foo", "bar"})
	want := "K=V claude --verbose --foo bar"
	if got != want {
		t.Fatalf("cmdline = %q, want %q", got, want)
	}
}
