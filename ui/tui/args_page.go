package tui

import (
	"fmt"
	"strings"
	"unicode"

	"cli"
)

// argsResult 参数页结果。
type argsResult struct {
	action  string // "run" | "cancel"
	value   string
	removed []string
}

// runArgs 参数页：输入框 + 历史 + 已移除三区（按键语义见 keymap.go）。
// 输入框由共享 lineEditor 驱动：q 与数字照常落字；Tab 在三区循环；
// 输入框内 ↑↓ 按 shell 习惯召回历史命令。
func (s *session) runArgs(td *cli.AgentState, display string, save func() error) (argsResult, error) {
	var ed lineEditor
	focus := 0 // 0=输入框 1=历史 2=已移除
	histSel, remSel := 0, 0
	histRecall := -1
	var typed []rune

	r := s.renderer()
	listsBudget := func(inputLines int) (int, int) {
		h := r.height
		if _, nh, ok := getConsoleSize(); ok {
			h = nh
		}
		b := h - 10 - inputLines
		if b < 4 {
			b = 4
		}
		return (b + 1) / 2, b / 2
	}

	draw := func() {
		w, h := r.width, r.height
		if nw, nh, ok := getConsoleSize(); ok {
			w, h = nw, nh
		}
		r.resize(w, h)
		f := newFrame(h)
		f.add(fitClip(ccOrange+bold+"高级参数 - "+display+reset, w))
		f.add(fitClip(ccLine+strings.Repeat("─", minInt(w, 60))+reset, w))
		f.add("")
		f.add(fitClip(listHead("输入框（直接输入，回车运行）", focus == 0), w))
		lines, curRow, curCol := ed.view(maxInt(w-4, 8))
		inputStart := len(f.lines) + 1
		curOutRow, curOutCol := -1, 3
		for i, ln := range lines {
			prefix := "> "
			if i > 0 {
				prefix = "  "
			}
			if i == curRow {
				curOutRow = inputStart + i
				curOutCol = minInt(strWidth(prefix)+curCol+1, w)
			}
			f.add(fitClip(prefix+ln, w))
		}
		f.add("")
		f.add(fitClip(listHead("历史参数", focus == 1), w))
		histMax, remMax := listsBudget(len(lines))
		hs, he := windowSlice(len(td.Commands), histSel, histMax)
		if hs > 0 {
			f.add(fitClip(ccFaint+fmt.Sprintf("  ↑ 上面还有 %d 项", hs)+reset, w))
		}
		if len(td.Commands) == 0 {
			f.add(fitClip("  "+ccFaint+"<空>"+reset, w))
		}
		for i := hs; i < he; i++ {
			f.add(listRow(w, td.Commands[i], focus == 1 && i == histSel, false))
		}
		if he < len(td.Commands) {
			f.add(fitClip(ccFaint+fmt.Sprintf("  ↓ 下面还有 %d 项", len(td.Commands)-he)+reset, w))
		}
		f.add("")
		f.add(fitClip(listHead("已移除", focus == 2), w))
		rs, re := windowSlice(len(td.RemovedCmds), remSel, remMax)
		if rs > 0 {
			f.add(fitClip(ccFaint+fmt.Sprintf("  ↑ 上面还有 %d 项", rs)+reset, w))
		}
		if len(td.RemovedCmds) == 0 {
			f.add(fitClip("  "+ccFaint+"<空>"+reset, w))
		}
		for i := rs; i < re; i++ {
			f.add(listRow(w, td.RemovedCmds[i], focus == 2 && i == remSel, false))
		}
		if re < len(td.RemovedCmds) {
			f.add(fitClip(ccFaint+fmt.Sprintf("  ↓ 下面还有 %d 项", len(td.RemovedCmds)-re)+reset, w))
		}
		f.add("")
		f.add(fitClip(ccFaint+argsHint+reset, w))
		r.draw(f)
		if focus == 0 && curOutRow >= 1 && curOutRow <= h {
			fmt.Fprintf(r.s.w, "%s%d;%dH%s", csi, curOutRow, curOutCol, showCursor)
		} else {
			fmt.Fprint(r.s.w, hideCursor)
		}
	}

	cycle := func(dir int) {
		focus = (focus + dir + 3) % 3
		histSel = clampSel(histSel, len(td.Commands))
		remSel = clampSel(remSel, len(td.RemovedCmds))
	}

	recall := func(dir int) {
		if len(td.Commands) == 0 {
			return
		}
		if histRecall < 0 && dir > 0 {
			typed = append([]rune(nil), ed.buf...)
		}
		histRecall += dir
		if histRecall < 0 {
			histRecall = -1
			ed.buf = typed
			ed.pos = len(ed.buf)
			return
		}
		if histRecall >= len(td.Commands) {
			histRecall = len(td.Commands) - 1
		}
		ed.setText(td.Commands[histRecall])
	}

	move := func(dir int) {
		if focus == 1 {
			histSel = clampSel(histSel+dir, len(td.Commands))
		} else {
			remSel = clampSel(remSel+dir, len(td.RemovedCmds))
		}
	}

	mutate := func(fn func() error) error {
		if save != nil {
			if err := save(); err != nil {
				return err
			}
		}
		if fn != nil {
			fn()
		}
		return nil
	}

	confirmRun := func(value string) argsResult {
		return argsResult{action: "run", value: value, removed: sliceCopy(td.RemovedCmds)}
	}

	draw()
	for {
		k, err := readKey(s.kr)
		if err != nil {
			return argsResult{}, err
		}
		if isBackKey(k) {
			return argsResult{action: "cancel", removed: sliceCopy(td.RemovedCmds)}, nil
		}

		if focus == 0 {
			switch k.Name {
			case "up":
				recall(1)
				draw()
				continue
			case "down":
				recall(-1)
				draw()
				continue
			case "tab":
				cycle(1)
				draw()
				continue
			case "shift-tab":
				cycle(-1)
				draw()
				continue
			}
			confirm, _, handled := ed.key(k)
			if confirm {
				cmd := strings.TrimSpace(ed.String())
				if cmd != "" {
					if i := indexOf(td.Commands, cmd); i >= 0 {
						td.Commands = append(td.Commands[:i], td.Commands[i+1:]...)
					}
					td.Commands = append([]string{cmd}, td.Commands...)
				}
				return confirmRun(cmd), nil
			}
			if handled {
				histRecall = -1
				typed = nil
				draw()
			}
			continue
		}

		if k.Name == "tab" {
			cycle(1)
			draw()
			continue
		}
		if k.Name == "shift-tab" {
			cycle(-1)
			draw()
			continue
		}
		if k.Name == "char" && k.Rune >= '1' && k.Rune <= '9' {
			idx := int(k.Rune - '1')
			if focus == 1 && idx < len(td.Commands) {
				histSel = idx
				draw()
			} else if focus == 2 && idx < len(td.RemovedCmds) {
				remSel = idx
				draw()
			}
			continue
		}
		switch k.Name {
		case "up":
			move(-1)
			draw()
			continue
		case "down":
			move(1)
			draw()
			continue
		case "home":
			if focus == 1 {
				histSel = 0
			} else {
				remSel = 0
			}
			draw()
			continue
		case "end":
			if focus == 1 {
				histSel = clampSel(len(td.Commands)-1, len(td.Commands))
			} else {
				remSel = clampSel(len(td.RemovedCmds)-1, len(td.RemovedCmds))
			}
			draw()
			continue
		case "pageup", "pagedown":
			histMax, remMax := listsBudget(1)
			step := remMax
			if focus == 1 {
				step = histMax
			}
			if k.Name == "pageup" {
				move(-step)
			} else {
				move(step)
			}
			draw()
			continue
		case "return", "space":
			if focus == 1 && histSel >= 0 && histSel < len(td.Commands) {
				return confirmRun(td.Commands[histSel]), nil
			}
			continue
		}
		if k.Name != "char" {
			continue
		}
		switch unicode.ToLower(k.Rune) {
		case 'k':
			move(-1)
			draw()
		case 'j':
			move(1)
			draw()
		case 'd':
			if focus == 1 && len(td.Commands) > 0 {
				item := td.Commands[histSel]
				td.Commands = append(td.Commands[:histSel], td.Commands[histSel+1:]...)
				td.RemovedCmds = append(td.RemovedCmds, item)
				histSel = clampSel(histSel, len(td.Commands))
				if err := mutate(nil); err != nil {
					return argsResult{}, err
				}
				draw()
			}
		case 'u':
			if focus == 2 && len(td.RemovedCmds) > 0 {
				item := td.RemovedCmds[remSel]
				td.RemovedCmds = append(td.RemovedCmds[:remSel], td.RemovedCmds[remSel+1:]...)
				td.Commands = append(td.Commands, item)
				remSel = clampSel(remSel, len(td.RemovedCmds))
				if err := mutate(nil); err != nil {
					return argsResult{}, err
				}
				draw()
			}
		case 'h', 'l':
			dir := 1
			if unicode.ToLower(k.Rune) == 'h' {
				dir = -1
			}
			if focus == 1 && len(td.Commands) > 1 {
				i := histSel
				j := clampSel(i+dir, len(td.Commands))
				td.Commands[i], td.Commands[j] = td.Commands[j], td.Commands[i]
				histSel = j
				if err := mutate(nil); err != nil {
					return argsResult{}, err
				}
				draw()
			} else if focus == 2 && len(td.RemovedCmds) > 1 {
				i := remSel
				j := clampSel(i+dir, len(td.RemovedCmds))
				td.RemovedCmds[i], td.RemovedCmds[j] = td.RemovedCmds[j], td.RemovedCmds[i]
				remSel = j
				if err := mutate(nil); err != nil {
					return argsResult{}, err
				}
				draw()
			}
		case 'q':
			return argsResult{action: "cancel", removed: sliceCopy(td.RemovedCmds)}, nil
		}
	}
}
