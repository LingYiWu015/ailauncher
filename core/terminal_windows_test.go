//go:build windows

package core

import (
	"reflect"
	"testing"
)

func TestTerminalArgvWindows(t *testing.T) {
	cmdline := "set K=V && claude"
	cases := []struct {
		name string
		term string
		wd   string
		want []string
	}{
		{
			"Alacritty（带空格路径，与历史行为字节一致）",
			`C:\Program Files\Alacritty\alacritty.exe`,
			`D:\foo bar`,
			[]string{`C:\Program Files\Alacritty\alacritty.exe`, `--working-directory`, `D:\foo bar`, `-e`, `cmd`, `/k`, cmdline},
		},
		{
			"Windows Terminal",
			`wt.exe`,
			`D:\foo`,
			[]string{`wt.exe`, `-d`, `D:\foo`, `cmd`, `/k`, cmdline},
		},
		{
			"WezTerm（大小写不敏感）",
			`C:\Program Files\WezTerm\wezterm.exe`,
			`D:\foo`,
			[]string{`C:\Program Files\WezTerm\wezterm.exe`, `start`, `--cwd`, `D:\foo`, `--`, `cmd`, `/k`, cmdline},
		},
		{
			"未知终端 → Alacritty 兜底",
			`mintty.exe`,
			`D:\foo`,
			[]string{`mintty.exe`, `--working-directory`, `D:\foo`, `-e`, `cmd`, `/k`, cmdline},
		},
	}
	for _, c := range cases {
		got := terminalArgv(c.term, c.wd, cmdline)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: argv = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestBuildTUIShellCmdlineWindows(t *testing.T) {
	a := &Agent{Name: "claude", Exec: `C:\x\claude.exe`, Args: "--verbose"}
	got := buildTUIShellCmdline(a, map[string]string{"K": "V"}, []string{"--foo", "bar"})
	want := "set K=V && C:\\x\\claude.exe --verbose --foo bar"
	if got != want {
		t.Fatalf("cmdline = %q, want %q", got, want)
	}
}
