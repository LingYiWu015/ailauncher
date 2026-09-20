package tui

import (
	"fmt"
	"strings"
	"unicode"
)

// extraResult 额外按键（N 新建 / R 切根 等）的返回。
type extraResult struct {
	action string // "select" 立即选中并返回
	value  string
	add    string // 追加到主列表并选中
	exit   bool   // 返回 exit 动作（外层决定是否重跑）
	del    bool   // 删除当前选中项并留在页内（外层经 onDeleteSelected 落盘）
}

// listOpts 通用双栏列表页参数（对应旧 Node dualListPage）。
type listOpts struct {
	title        string
	mainTitle    string
	removedTitle string // 空则单栏
	mainItems    *[]string
	removedItems *[]string
	focus        int
	hint         string
	onManual     func() (string, error) // 返回新条目；空串表示取消
	markSelected func(string) bool
	onChange     func() error // 列表变更后回调（持久化）
	// onDeleteSelected 删除主栏当前选中项（如下游 transcript 删除）；返回 error 则停留报错。
	onDeleteSelected func(index int, value string) error
	extraKeys        map[rune]func() (extraResult, error)
}

// listResult 列表页结果。
type listResult struct {
	action  string // "select" | "cancel" | "exit"
	value   string
	removed []string
}

// windowSlice 把 n 项按选中 sel 切出至多 maxRows 行的可见窗口，保证选中行可见。
func windowSlice(n, sel, maxRows int) (int, int) {
	if maxRows < 1 {
		maxRows = 1
	}
	if sel < 0 {
		sel = 0
	}
	if n <= maxRows {
		return 0, n
	}
	start := sel - maxRows + 1
	if start < 0 {
		start = 0
	}
	if start+maxRows > n {
		start = n - maxRows
	}
	return start, start + maxRows
}

func clampSel(sel, n int) int {
	if n <= 0 {
		return 0
	}
	if sel < 0 {
		return 0
	}
	if sel >= n {
		return n - 1
	}
	return sel
}

// listHead 渲染栏目标题（激活栏带 ❯ 标记）。
func listHead(title string, active bool) string {
	if active {
		return ccTeal + bold + "❯ " + reset + title
	}
	return "  " + ccMuted + title + reset
}

// listRow 渲染一行（选中行反显铺满终端宽）。
func listRow(w int, text string, selected, marked bool) string {
	if selected {
		return fitClip(invert+" "+padRight(" "+text+" ", w-2)+reset, w)
	}
	line := "  " + text
	if marked {
		line += "  " + ccFaint + "(现用)" + reset
	}
	return fitClip(line+reset, w)
}

// listPage 运行通用双栏列表页（共享增量渲染器 + 可见窗口；按键语义见 keymap.go）。
// mainItems/removedItems 以指针传入，页面内的增删/排序会写回调用方。
func (s *session) listPage(o listOpts) (listResult, error) {
	return s.listPageLive(o, nil)
}

// listUpdate 是外部刷新通道的一条更新：改某行文本并重绘（后台探测逐行刷新用）。
type listUpdate struct {
	index int
	text  string
}

// listPageLive 在 listPage 基础上多路复用 updates 通道：后台任务完成后发更新，
// 页面即时刷新该行，用户无需等待所有探测完成。updates 为 nil 时等价 listPage。
func (s *session) listPageLive(o listOpts, updates <-chan listUpdate) (listResult, error) {
	main := *o.mainItems
	rem := *o.removedItems
	defer func() {
		*o.mainItems = main
		*o.removedItems = rem
	}()

	dual := o.removedTitle != ""
	curFocus := 0
	if o.focus == 1 && dual {
		curFocus = 1
	}
	mainSel, remSel := 0, 0

	r := s.renderer()
	budget := func() (int, int) {
		h := r.height
		if _, nh, ok := getConsoleSize(); ok {
			h = nh
		}
		b := h - 8
		if !dual {
			b = h - 6
		}
		if b < 4 {
			b = 4
		}
		if !dual {
			return b, 0
		}
		return (b + 1) / 2, b / 2
	}

	draw := func() {
		w, h := r.width, r.height
		if nw, nh, ok := getConsoleSize(); ok {
			w, h = nw, nh
		}
		r.resize(w, h)
		mainMax, remMax := budget()
		f := newFrame(h)
		f.add(fitClip(ccOrange+bold+o.title+reset, w))
		f.add(fitClip(ccLine+strings.Repeat("─", minInt(w, 60))+reset, w))
		f.add("")
		f.add(fitClip(listHead(o.mainTitle, curFocus == 0), w))
		ms, me := windowSlice(len(main), mainSel, mainMax)
		if ms > 0 {
			f.add(fitClip(ccFaint+fmt.Sprintf("  ↑ 上面还有 %d 项", ms)+reset, w))
		}
		if len(main) == 0 {
			f.add(fitClip("  "+ccFaint+"<空>"+reset, w))
		}
		for i := ms; i < me; i++ {
			f.add(listRow(w, main[i], curFocus == 0 && i == mainSel, o.markSelected != nil && o.markSelected(main[i])))
		}
		if me < len(main) {
			f.add(fitClip(ccFaint+fmt.Sprintf("  ↓ 下面还有 %d 项", len(main)-me)+reset, w))
		}
		if dual {
			f.add("")
			f.add(fitClip(listHead(o.removedTitle, curFocus == 1), w))
			rs, re := windowSlice(len(rem), remSel, remMax)
			if rs > 0 {
				f.add(fitClip(ccFaint+fmt.Sprintf("  ↑ 上面还有 %d 项", rs)+reset, w))
			}
			if len(rem) == 0 {
				f.add(fitClip("  "+ccFaint+"<空>"+reset, w))
			}
			for i := rs; i < re; i++ {
				f.add(listRow(w, rem[i], curFocus == 1 && i == remSel, false))
			}
			if re < len(rem) {
				f.add(fitClip(ccFaint+fmt.Sprintf("  ↓ 下面还有 %d 项", len(rem)-re)+reset, w))
			}
		}
		f.add("")
		f.add(fitClip(ccFaint+o.hint+reset, w))
		r.draw(f)
		fmt.Fprint(r.s.w, hideCursor)
	}

	move := func(dir int) {
		if curFocus == 0 {
			mainSel = clampSel(mainSel+dir, len(main))
		} else {
			remSel = clampSel(remSel+dir, len(rem))
		}
	}

	swap := func(dir int) error {
		if curFocus == 0 {
			i := mainSel
			j := clampSel(i+dir, len(main))
			if i != j {
				main[i], main[j] = main[j], main[i]
				mainSel = j
			}
		} else {
			i := remSel
			j := clampSel(i+dir, len(rem))
			if i != j {
				rem[i], rem[j] = rem[j], rem[i]
				remSel = j
			}
		}
		if o.onChange != nil {
			return o.onChange()
		}
		return nil
	}

	draw()
	if updates == nil {
		return s.listKeys(o, &main, &rem, &mainSel, &remSel, curFocus, dual, draw, move, swap, budget)
	}
	keyCh := make(chan keyRead, 16)
	done := make(chan struct{})
	defer close(done)
	go readListKeys(s.kr, keyCh, done)
	for {
		select {
		case up, ok := <-updates:
			if !ok {
				updates = nil
				continue
			}
			if up.index >= 0 && up.index < len(main) {
				main[up.index] = up.text
				draw()
			}
		case kr, ok := <-keyCh:
			if !ok {
				return listResult{}, kr.err
			}
			if kr.err != nil {
				return listResult{}, kr.err
			}
			res, exit, err := s.listKey(o, &main, &rem, &mainSel, &remSel, curFocus, dual, draw, move, swap, budget, kr.key)
			if err != nil {
				return listResult{}, err
			}
			if exit {
				return res, nil
			}
			curFocus = resFocus(res, curFocus)
		}
	}
}

type keyRead struct {
	key Key
	err error
}

func readListKeys(kr *keyReader, out chan<- keyRead, done <-chan struct{}) {
	defer close(out)
	for {
		key, err := readKey(kr)
		select {
		case out <- keyRead{key: key, err: err}:
		case <-done:
			return
		}
		if err != nil {
			return
		}
	}
}

// resFocus 从 listKey 的中间结果恢复焦点（0/1 正常，2 表示已返回）。
func resFocus(res listResult, cur int) int {
	if res.action == "focus" {
		if res.value == "1" {
			return 1
		}
		return 0
	}
	return cur
}

// listKeys 是原同步读键循环（updates==nil 时用，保持单测字节流语义）。
func (s *session) listKeys(o listOpts, main, rem *[]string, mainSel, remSel *int, curFocus int, dual bool, draw func(), move func(int), swap func(int) error, budget func() (int, int)) (listResult, error) {
	for {
		k, err := readKey(s.kr)
		if err != nil {
			return listResult{}, err
		}
		res, exit, err := s.listKey(o, main, rem, mainSel, remSel, curFocus, dual, draw, move, swap, budget, k)
		if err != nil {
			return listResult{}, err
		}
		if exit {
			return res, nil
		}
		curFocus = resFocus(res, curFocus)
	}
}

// listKey 处理单个按键：返回 (结果, 是否退出, 错误)。
// 焦点切换时返回 action=focus 的中间结果，调用方据此更新 curFocus。
func (s *session) listKey(o listOpts, main, rem *[]string, mainSel, remSel *int, curFocus int, dual bool, draw func(), move func(int), swap func(int) error, budget func() (int, int), k Key) (listResult, bool, error) {
	if isBackKey(k) {
		return listResult{action: "cancel", removed: sliceCopy(*rem)}, true, nil
	}

	if k.Name == "char" {
		if k.Rune >= '1' && k.Rune <= '9' {
			idx := int(k.Rune - '1')
			if curFocus == 0 && idx < len(*main) {
				*mainSel = idx
				draw()
			} else if curFocus == 1 && idx < len(*rem) {
				*remSel = idx
				draw()
			}
			return listResult{}, false, nil
		}
		if fn, ok := o.extraKeys[unicode.ToLower(k.Rune)]; ok {
			rr, err := fn()
			if err != nil {
				return listResult{}, true, err
			}
			if rr.action == "select" {
				return listResult{action: "select", value: rr.value, removed: sliceCopy(*rem)}, true, nil
			}
			if rr.exit {
				return listResult{action: "exit", removed: sliceCopy(*rem)}, true, nil
			}
			if rr.del {
				if curFocus == 0 && *mainSel >= 0 && *mainSel < len(*main) && o.onDeleteSelected != nil {
					if err := o.onDeleteSelected(*mainSel, (*main)[*mainSel]); err != nil {
						return listResult{}, true, err
					}
					*main = append((*main)[:*mainSel], (*main)[*mainSel+1:]...)
					*mainSel = clampSel(*mainSel, len(*main))
					if len(*main) == 0 {
						return listResult{action: "cancel", removed: sliceCopy(*rem)}, true, nil
					}
				}
				draw()
				return listResult{}, false, nil
			}
			if rr.add != "" && !contains(*main, rr.add) {
				*main = append(*main, rr.add)
				*mainSel = len(*main) - 1
			}
			if o.onChange != nil {
				if err := o.onChange(); err != nil {
					return listResult{}, true, err
				}
			}
			draw()
			return listResult{}, false, nil
		}
	}

	switch k.Name {
	case "tab", "shift-tab":
		if dual {
			if curFocus == 0 {
				curFocus = 1
			} else {
				curFocus = 0
			}
			draw()
			if curFocus == 1 {
				return listResult{action: "focus", value: "1"}, false, nil
			}
			return listResult{action: "focus", value: "0"}, false, nil
		}
		return listResult{}, false, nil
	case "up":
		move(-1)
		draw()
		return listResult{}, false, nil
	case "down":
		move(1)
		draw()
		return listResult{}, false, nil
	case "home":
		if curFocus == 0 {
			*mainSel = 0
		} else {
			*remSel = 0
		}
		draw()
		return listResult{}, false, nil
	case "end":
		if curFocus == 0 {
			*mainSel = clampSel(len(*main)-1, len(*main))
		} else {
			*remSel = clampSel(len(*rem)-1, len(*rem))
		}
		draw()
		return listResult{}, false, nil
	case "return", "space":
		if curFocus == 0 && *mainSel >= 0 && *mainSel < len(*main) {
			return listResult{action: "select", value: (*main)[*mainSel], removed: sliceCopy(*rem)}, true, nil
		}
		if curFocus == 0 && len(*main) == 0 {
			return listResult{action: "cancel", removed: sliceCopy(*rem)}, true, nil
		}
		return listResult{}, false, nil
	}

	if k.Name != "char" {
		if k.Name == "pageup" || k.Name == "pagedown" {
			mainMax, remMax := budget()
			step := mainMax
			if curFocus == 1 {
				step = remMax
			}
			if k.Name == "pageup" {
				move(-step)
			} else {
				move(step)
			}
			draw()
		}
		return listResult{}, false, nil
	}
	switch unicode.ToLower(k.Rune) {
	case 'k':
		move(-1)
		draw()
	case 'j':
		move(1)
		draw()
	case 'h':
		if err := swap(-1); err != nil {
			return listResult{}, true, err
		}
		draw()
	case 'l':
		if err := swap(1); err != nil {
			return listResult{}, true, err
		}
		draw()
	case 'd':
		if curFocus == 0 && len(*main) > 0 {
			item := (*main)[*mainSel]
			*main = append((*main)[:*mainSel], (*main)[*mainSel+1:]...)
			*rem = append(*rem, item)
			*mainSel = clampSel(*mainSel, len(*main))
			if o.onChange != nil {
				if err := o.onChange(); err != nil {
					return listResult{}, true, err
				}
			}
			draw()
		}
	case 'u':
		if curFocus == 1 && len(*rem) > 0 {
			item := (*rem)[*remSel]
			*rem = append((*rem)[:*remSel], (*rem)[*remSel+1:]...)
			*main = append(*main, item)
			*remSel = clampSel(*remSel, len(*rem))
			if o.onChange != nil {
				if err := o.onChange(); err != nil {
					return listResult{}, true, err
				}
			}
			draw()
		}
	case 'm':
		if o.onManual != nil {
			item, err := o.onManual()
			if err != nil {
				return listResult{}, true, err
			}
			if item != "" && !contains(*main, item) {
				*main = append(*main, item)
			}
			if len(*main) > 0 {
				*mainSel = len(*main) - 1
			}
			if o.onChange != nil {
				if err := o.onChange(); err != nil {
					return listResult{}, true, err
				}
			}
			draw()
		}
	case 'q':
		return listResult{action: "cancel", removed: sliceCopy(*rem)}, true, nil
	}
	return listResult{}, false, nil
}
