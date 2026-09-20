package tui

import (
	"testing"
)

func TestReadKey(t *testing.T) {
	cases := []struct {
		in   string
		name string
	}{
		{"\r", "return"},
		{"\n", "return"},
		{"\t", "tab"},
		{"\x1b", "escape"},
		{"a", "char"},
		{"你", "char"}, // 多字节 UTF-8
		{"\x1b[A", "up"},
		{"\x1b[B", "down"},
		{"\x1b[C", "right"},
		{"\x1b[D", "left"},
		{"\x1b[H", "home"},
		{"\x1b[F", "end"},
		{"\x1b[Z", "shift-tab"},
		{"\x1b[3~", "delete"},
		{"\x1b[5~", "pageup"},
		{"\x1b[6~", "pagedown"},
		{"\x1b[1;5C", "right"}, // 带修饰的右箭头
		{"\x1bOA", "up"},       // SS3
		{"\x1bq", "alt-q"},     // Alt+字符
		{"\x1b[A", "up"},
		{"\x7f", "backspace"},
	}
	for _, c := range cases {
		kr := newBytesKeyReader([]byte(c.in))
		k, err := readKey(kr)
		if err != nil {
			t.Fatalf("%q: %v", c.in, err)
		}
		if k.Name != c.name {
			t.Errorf("%q: got name %q, want %q", c.in, k.Name, c.name)
		}
	}
}

func TestReadKeyRune(t *testing.T) {
	kr := newBytesKeyReader([]byte("你"))
	k, err := readKey(kr)
	if err != nil {
		t.Fatal(err)
	}
	if k.Name != "char" || k.Rune != '你' {
		t.Fatalf("got %q rune %c, want char 你", k.Name, k.Rune)
	}
}

func TestReadKeyBareEscape(t *testing.T) {
	// 单独 ESC：窗口内无后续字节 → 判定为取消键（不挂起）
	kr := newBytesKeyReader([]byte("\x1b"))
	k, err := readKey(kr)
	if err != nil {
		t.Fatal(err)
	}
	if k.Name != "escape" {
		t.Fatalf("got %q, want escape", k.Name)
	}
}
