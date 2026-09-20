package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// 本文件是 Claude Code 风格 TUI 的高效渲染引擎：
// 脏行 diff + 帧节流 + 中文双宽感知 + 行内截断，替代旧整屏 Clear() 重绘。
// 设计约束：纯 stdlib、零依赖；所有宽度按终端列数算（中文/全角占 2 列）；
// 输出永不让裸 \n 滚屏破坏布局（行数封顶 + 超长截断 + … 标记）。

const (
	altScreenOn  = csi + "?1049h"
	altScreenOff = csi + "?1049l"
	hideScroll   = csi + "?7l"
	showScroll   = csi + "?7h"

	// Claude Code 式配色（粉/橙/青/灰四色 + 状态色）。
	ccPink   = csi + "38;5;211m"
	ccOrange = csi + "38;5;214m"
	ccTeal   = csi + "38;5;81m"
	ccMuted  = csi + "38;5;242m"
	ccFaint  = csi + "38;5;238m"
	ccLine   = csi + "38;5;238m"
)

// frame 是已计算好的一帧：每行是“可打印内容”（含行内 ANSI，不含换行）。
type frame struct {
	lines []string
}

func newFrame(capHint int) frame {
	if capHint < 8 {
		capHint = 8
	}
	return frame{lines: make([]string, 0, capHint)}
}

func (f *frame) add(line string) { f.lines = append(f.lines, line) }

// renderer 是增量渲染器：记住上一帧，只重写变化的行。
type renderer struct {
	s      *Screen
	width  int
	height int
	prev   []string
	alt    bool
}

func newRenderer(s *Screen, width, height int) *renderer {
	if width < 20 {
		width = 80
	}
	if height < 6 {
		height = 24
	}
	return &renderer{s: s, width: width, height: height}
}

// enter 切到备用屏 + 隐藏滚动 + 隐藏光标。非 TTY 测试时照常写转义码（调用方丢弃输出）。
func (r *renderer) enter() {
	fmt.Fprint(r.s.w, altScreenOn, hideScroll, hideCursor)
	r.alt = true
	r.prev = nil
}

func (r *renderer) exit() {
	if !r.alt {
		return
	}
	fmt.Fprint(r.s.w, showScroll, showCursor, altScreenOff)
	r.alt = false
}

// resize 更新尺寸并丢弃上一帧（强制全量重绘，避免残留）。
func (r *renderer) resize(width, height int) {
	if width < 20 {
		width = 80
	}
	if height < 6 {
		height = 24
	}
	if width == r.width && height == r.height {
		return
	}
	r.width, r.height = width, height
	r.prev = nil
}

// draw 做脏行 diff：首帧全量，后续只写变化行。行数封顶 height，多余帧行直接丢弃。
func (r *renderer) draw(f frame) {
	if !r.alt {
		fmt.Fprint(r.s.w, altScreenOn, hideScroll, hideCursor)
		r.alt = true
	}
	if len(r.prev) != r.height {
		r.prev = make([]string, r.height)
		fmt.Fprint(r.s.w, csi+"H", csi+"2J")
		for i := 0; i < r.height; i++ {
			r.prev[i] = ""
		}
		for i, line := range f.lines {
			if i >= r.height {
				break
			}
			r.writeRow(i+1, line)
			r.prev[i] = line
		}
		return
	}
	for row := 0; row < r.height; row++ {
		var line string
		if row < len(f.lines) {
			line = f.lines[row]
		}
		if line == r.prev[row] {
			continue
		}
		r.writeRow(row+1, line)
		r.prev[row] = line
	}
}

func (r *renderer) writeRow(row int, line string) {
	fmt.Fprintf(r.s.w, "%s%d;1H%s%s", csi, row, csi, "2K")
	if line != "" {
		fmt.Fprint(r.s.w, line)
	}
}

// runeWidth 返回 rune 占用的终端列数：ASCII 1 列，CJK/全角/emoji 按 2 列。
func runeWidth(r rune) int {
	switch {
	case r < 0x1100:
		return 1
	case r <= 0x115F || r == 0x2329 || r == 0x232A:
		return 2
	case r >= 0x2E80 && r <= 0x303E:
		return 2
	case r >= 0x3041 && r <= 0x33FF:
		return 2
	case r >= 0x3400 && r <= 0x4DBF:
		return 2
	case r >= 0x4E00 && r <= 0xA4CF:
		return 2
	case r >= 0xAC00 && r <= 0xD7A3:
		return 2
	case r >= 0xF900 && r <= 0xFAFF:
		return 2
	case r >= 0xFE30 && r <= 0xFE4F:
		return 2
	case r >= 0xFF00 && r <= 0xFF60:
		return 2
	case r >= 0xFFE0 && r <= 0xFFE6:
		return 2
	case r >= 0x1F000 && r <= 0x1FAFF:
		return 2
	case r >= 0x20000 && r <= 0x3FFFD:
		return 2
	}
	return 1
}

// strWidth 按终端列数计算可见宽度（跳过 ANSI 转义段）。
func strWidth(s string) int {
	w := 0
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && !isANSIFinal(s[j]) {
				j++
			}
			i = j + 1
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size <= 1 {
			i++
			continue
		}
		w += runeWidth(r)
		i += size
	}
	return w
}

func isANSIFinal(c byte) bool { return c >= 0x40 && c <= 0x7e }

// fitClip 把一行裁到 width 列以内：保留行内 ANSI，超长截断并补 …。
func fitClip(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if strWidth(s) <= width {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	used := 0
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && !isANSIFinal(s[j]) {
				j++
			}
			if j < len(s) {
				b.WriteString(s[i : j+1])
				i = j + 1
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size <= 1 {
			i++
			continue
		}
		w := runeWidth(r)
		if used+w > width-1 {
			b.WriteString("…")
			break
		}
		b.WriteString(s[i : i+size])
		used += w
		i += size
	}
	return b.String()
}

// wrapCells 按 width 列宽折行（中文双宽感知）；长词硬切，保留空行。
func wrapCells(text string, width int) []string {
	if width < 1 {
		width = 1
	}
	var out []string
	for _, para := range strings.Split(text, "\n") {
		if para == "" {
			out = append(out, "")
			continue
		}
		var cur strings.Builder
		used := 0
		flush := func() {
			out = append(out, cur.String())
			cur.Reset()
			used = 0
		}
		for _, r := range para {
			w := runeWidth(r)
			if used+w > width && used > 0 {
				flush()
			}
			cur.WriteRune(r)
			used += w
		}
		out = append(out, cur.String())
	}
	if len(out) == 0 {
		return []string{""}
	}
	return out
}

// padRight 按列宽右补空格（用于状态栏/选中行铺满）。
func padRight(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if w := strWidth(s); w < width {
		return s + strings.Repeat(" ", width-w)
	}
	return s
}
