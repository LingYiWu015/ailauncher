//go:build windows

package core

import (
	"os"
	"testing"
)

func TestBuildCmdline(t *testing.T) {
	got := buildCmdline([]string{`C:\Program Files\Alacritty\alacritty.exe`, `--working-directory`, `D:\foo bar`})
	want := `"C:\Program Files\Alacritty\alacritty.exe" --working-directory "D:\foo bar"`
	if got != want {
		t.Fatalf("buildCmdline = %q, want %q", got, want)
	}
	if got := buildCmdline([]string{`C:\x\app.exe`}); got != `C:\x\app.exe` {
		t.Fatalf("no-space argv should stay unquoted: %q", got)
	}
	if got := buildCmdline([]string{""}); got != `""` {
		t.Fatalf("empty argv should be %q, got %q", `""`, got)
	}
}

func TestWriteVBS(t *testing.T) {
	cmdline := `"C:\Program Files\Alacritty\alacritty.exe" -e cmd /k "set K=V && claude"`
	path, err := writeVBS(cmdline)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := `CreateObject("WScript.Shell").Run """C:\Program Files\Alacritty\alacritty.exe"" -e cmd /k ""set K=V && claude""", 0, False`
	if string(b) != want {
		t.Fatalf("vbs body = %q, want %q", string(b), want)
	}
}

func TestWriteWmiVBS(t *testing.T) {
	cmdline := `"C:\Program Files\Alacritty\alacritty.exe" -e cmd /k "set K=V && claude"`
	path, err := writeWmiVBS(cmdline, `D:\foo bar`)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "Set o = GetObject(\"winmgmts:\\\\.\\root\\cimv2\")\r\n" +
		"rc = o.Get(\"Win32_Process\").Create(\"\"\"C:\\Program Files\\Alacritty\\alacritty.exe\"\" -e cmd /k \"\"set K=V && claude\"\"\", \"D:\\foo bar\", null, pid)\r\n" +
		"WScript.Quit rc\r\n"
	if string(b) != want {
		t.Fatalf("vbs body = %q, want %q", string(b), want)
	}
}
