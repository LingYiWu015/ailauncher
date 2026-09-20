package tui

import (
	"fmt"
	"strings"
)

// prompt 单行输入（共享 lineEditor，与参数页/会话输入同一手感）。
// 确认返回去首尾空白的内容；ESC/Ctrl-C 取消返回 errInputCancel（与空提交区分）。
func (s *session) prompt(question, hint string) (string, error) {
	var ed lineEditor
	r := s.renderer()
	paint := func() {
		w, h := r.width, r.height
		if nw, nh, ok := getConsoleSize(); ok {
			w, h = nw, nh
		}
		r.resize(w, h)
		f := newFrame(h)
		f.add(fitClip(yellow+question+reset, w))
		if hint != "" {
			f.add(fitClip(ccFaint+hint+reset, w))
		} else {
			f.add("")
		}
		f.add("")
		lines, curRow, curCol := ed.view(maxInt(w-4, 8))
		start := len(f.lines) + 1
		row, col := start, 3
		for i, ln := range lines {
			prefix := "> "
			if i > 0 {
				prefix = "  "
			}
			if i == curRow {
				row = start + i
				col = minInt(strWidth(prefix)+curCol+1, w)
			}
			f.add(fitClip(prefix+ln, w))
		}
		f.add("")
		f.add(fitClip(ccFaint+"Enter 确认 · ESC 取消"+reset, w))
		r.draw(f)
		fmt.Fprintf(r.s.w, "%s%d;%dH%s", csi, row, col, showCursor)
	}
	paint()
	for {
		k, err := readKey(s.kr)
		if err != nil {
			fmt.Fprint(r.s.w, hideCursor)
			return "", err
		}
		if k.Name == "tab" || k.Name == "shift-tab" {
			continue
		}
		confirm, cancel, handled := ed.key(k)
		if confirm {
			fmt.Fprint(r.s.w, hideCursor)
			return strings.TrimSpace(ed.String()), nil
		}
		if cancel {
			fmt.Fprint(r.s.w, hideCursor)
			return "", errInputCancel
		}
		if handled {
			paint()
		}
	}
}
