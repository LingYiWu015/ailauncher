package tui

import (
	"strings"
)

// lineEditor 是全 TUI 共享的单模型行编辑器：prompt、参数页输入框、chat 输入框
// 共用同一套按键语义，保证三处输入体感完全一致。
type lineEditor struct {
	buf []rune
	pos int
}

func (e *lineEditor) String() string { return string(e.buf) }

func (e *lineEditor) Len() int { return len(e.buf) }

func (e *lineEditor) clamp() {
	if e.pos < 0 {
		e.pos = 0
	}
	if e.pos > len(e.buf) {
		e.pos = len(e.buf)
	}
}

func (e *lineEditor) insertRune(r rune) {
	e.clamp()
	e.buf = append(e.buf[:e.pos], append([]rune{r}, e.buf[e.pos:]...)...)
	e.pos++
}

func (e *lineEditor) insertText(s string) {
	for _, r := range s {
		e.insertRune(r)
	}
}

func (e *lineEditor) setText(s string) {
	e.buf = []rune(s)
	e.pos = len(e.buf)
}

func (e *lineEditor) clear() {
	e.buf = nil
	e.pos = 0
}

func (e *lineEditor) backspace() {
	e.clamp()
	if e.pos <= 0 {
		return
	}
	e.buf = append(e.buf[:e.pos-1], e.buf[e.pos:]...)
	e.pos--
}

func (e *lineEditor) deleteForward() {
	e.clamp()
	if e.pos >= len(e.buf) {
		return
	}
	e.buf = append(e.buf[:e.pos], e.buf[e.pos+1:]...)
}

func (e *lineEditor) moveLeft() {
	if e.pos > 0 {
		e.pos--
	}
}

func (e *lineEditor) moveRight() {
	if e.pos < len(e.buf) {
		e.pos++
	}
}

func (e *lineEditor) moveHome() { e.pos = 0 }

func (e *lineEditor) moveEnd() { e.pos = len(e.buf) }

func (e *lineEditor) killToEnd() {
	e.clamp()
	e.buf = e.buf[:e.pos]
}

func (e *lineEditor) deleteWord() {
	e.clamp()
	i := e.pos
	for i > 0 && e.buf[i-1] == ' ' {
		i--
	}
	for i > 0 && e.buf[i-1] != ' ' && e.buf[i-1] != '\n' {
		i--
	}
	e.buf = append(e.buf[:i], e.buf[e.pos:]...)
	e.pos = i
}

// key 处理通用编辑键；调用方先拦截页内专有键（Tab 切焦点、↑↓ 历史等），
// 再把剩余按键交给这里。返回 confirm/cancel/handled。
func (e *lineEditor) key(k Key) (confirm, cancel, handled bool) {
	switch k.Name {
	case "return":
		return true, false, true
	case "escape", "ctrl-c":
		return false, true, true
	case "backspace":
		e.backspace()
		return false, false, true
	case "delete":
		e.deleteForward()
		return false, false, true
	case "left":
		e.moveLeft()
		return false, false, true
	case "right":
		e.moveRight()
		return false, false, true
	case "home", "ctrl-a":
		e.moveHome()
		return false, false, true
	case "end", "ctrl-e":
		e.moveEnd()
		return false, false, true
	case "ctrl-k":
		e.killToEnd()
		return false, false, true
	case "ctrl-u":
		e.clear()
		return false, false, true
	case "ctrl-w":
		e.deleteWord()
		return false, false, true
	case "char", "space":
		e.insertRune(k.Rune)
		return false, false, true
	}
	return false, false, false
}

// view 把内容按 width 折行（\n 保留），返回行、光标行号、光标列（终端列数）。
func (e *lineEditor) view(width int) ([]string, int, int) {
	if width < 8 {
		width = 8
	}
	var lines []string
	var cur strings.Builder
	used := 0
	curRow, curCol := 0, 0
	placed := false
	flush := func() {
		lines = append(lines, cur.String())
		cur.Reset()
		used = 0
	}
	for i, r := range e.buf {
		if i == e.pos {
			curRow, curCol = len(lines), used
			placed = true
		}
		if r == '\n' {
			flush()
			continue
		}
		if used+runeWidth(r) > width && used > 0 {
			flush()
		}
		cur.WriteRune(r)
		used += runeWidth(r)
	}
	if !placed {
		curRow, curCol = len(lines), used
	}
	flush()
	if len(lines) == 0 {
		lines = []string{""}
	}
	return lines, curRow, curCol
}
