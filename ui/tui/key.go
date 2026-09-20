// Package tui 是 AILauncher v4 的 TUI 前端：交互与体验优化全在本层，
// 逻辑（config/state/run……）全部经 CLI 端口（见 cli_port.go）完成，本包不持有逻辑。
package tui

import (
	"time"
	"unicode"
)

// escapeTimeout 单独 ESC 与转义序列的判定窗口（与 Node readline ~100ms 一致）。
const escapeTimeout = 100 * time.Millisecond

// Key 表示一次按键。
type Key struct {
	Name string // "up","down","return","tab","escape","backspace","space","char",...
	Rune rune   // 可打印字符
}

// readKey 从 keyReader 读一个按键；转义序列（方向键等）会拼完整。
func readKey(kr *keyReader) (Key, error) {
	r, err := kr.readRune()
	if err != nil {
		return Key{}, err
	}
	switch r {
	case '\r', '\n':
		return Key{Name: "return"}, nil
	case '\t':
		return Key{Name: "tab"}, nil
	case 0x1b:
		return readEscape(kr)
	case 0x7f, 0x08:
		return Key{Name: "backspace"}, nil
	case ' ':
		return Key{Name: "space", Rune: ' '}, nil
	default:
		if unicode.IsControl(r) {
			return Key{Name: ctrlName(r)}, nil
		}
		return Key{Name: "char", Rune: r}, nil
	}
}

func ctrlName(r rune) string {
	switch r {
	case 0x03:
		return "ctrl-c"
	case 0x04:
		return "ctrl-d"
	case 0x1a:
		return "ctrl-z"
	}
	return "ctrl-" + string(r)
}

// readEscape 处理以 ESC 开头的输入：单独 ESC、CSI([...)、SS3(O...)、Alt+字符。
// 单独 ESC 用 escapeTimeout 判定：窗口内无后续字节则视为取消键。
func readEscape(kr *keyReader) (Key, error) {
	b, ok := kr.get(escapeTimeout)
	if !ok {
		return Key{Name: "escape"}, nil // 单独 ESC
	}
	switch b {
	case '[':
		var seq []byte
		for {
			c, ok := kr.get(0)
			if !ok {
				return Key{Name: "escape"}, nil
			}
			seq = append(seq, c)
			// CSI 终符：0x40–0x7e（字母 / ~ 等）
			if c >= 0x40 && c <= 0x7e {
				return csiKey(seq), nil
			}
		}
	case 'O':
		c, ok := kr.get(0)
		if !ok {
			return Key{Name: "escape"}, nil
		}
		return ss3Key(rune(c)), nil
	default:
		r := rune(b)
		switch r {
		case 'A', 'B', 'C', 'D':
			return Key{Name: "alt-" + arrowName(r)}, nil
		}
		return Key{Name: "alt-" + string(r)}, nil
	}
}

// csiKey 解析 CSI 序列，如 [A(up) [B(down) [C(right) [D(left) [H(home) [F(end)
// [Z(shift-tab) [3~(delete) [1;5C(带修饰的右箭头) 等。取终符（最后一个字节）为主键。
func csiKey(seq []byte) Key {
	last := seq[len(seq)-1]
	switch last {
	case 'A':
		return Key{Name: "up"}
	case 'B':
		return Key{Name: "down"}
	case 'C':
		return Key{Name: "right"}
	case 'D':
		return Key{Name: "left"}
	case 'H':
		return Key{Name: "home"}
	case 'F':
		return Key{Name: "end"}
	case 'Z':
		return Key{Name: "shift-tab"}
	case '~':
		switch seq[0] {
		case '1', '7':
			return Key{Name: "home"}
		case '4', '8':
			return Key{Name: "end"}
		case '3':
			return Key{Name: "delete"}
		case '5':
			return Key{Name: "pageup"}
		case '6':
			return Key{Name: "pagedown"}
		}
	}
	return Key{Name: "escape"}
}

func ss3Key(r rune) Key {
	switch r {
	case 'A':
		return Key{Name: "up"}
	case 'B':
		return Key{Name: "down"}
	case 'C':
		return Key{Name: "right"}
	case 'D':
		return Key{Name: "left"}
	case 'P':
		return Key{Name: "f1"}
	case 'Q':
		return Key{Name: "f2"}
	case 'R':
		return Key{Name: "f3"}
	case 'S':
		return Key{Name: "f4"}
	}
	return Key{Name: "escape"}
}

func arrowName(r rune) string {
	switch r {
	case 'A':
		return "up"
	case 'B':
		return "down"
	case 'C':
		return "right"
	case 'D':
		return "left"
	}
	return "unknown"
}
