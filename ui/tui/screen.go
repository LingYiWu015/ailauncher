package tui

import (
	"fmt"
	"io"
)

// ANSI 常量（集中管理）。
const (
	esc        = "\x1b"
	csi        = esc + "["
	clearAll   = csi + "2J" + csi + "H"
	hideCursor = csi + "?25l"
	showCursor = csi + "?25h"
	reset      = csi + "0m"
	bold       = csi + "1m"
	cyan       = csi + "36m"
	green      = csi + "32m"
	yellow     = csi + "33m"
	red        = csi + "31m"
	dark       = csi + "2m"
	invert     = csi + "7m"
)

// Screen 封装 ANSI 全屏重绘原语（整屏重绘模型）。
type Screen struct {
	w io.Writer
}

func NewScreen(w io.Writer) *Screen { return &Screen{w: w} }

func (s *Screen) Clear()      { fmt.Fprint(s.w, clearAll) }
func (s *Screen) HideCursor() { fmt.Fprint(s.w, hideCursor) }
func (s *Screen) ShowCursor() { fmt.Fprint(s.w, showCursor) }

func (s *Screen) moveTo(row int)    { fmt.Fprintf(s.w, "%s%d;1H", csi, row) }
func (s *Screen) clearLine(row int) { s.moveTo(row); fmt.Fprint(s.w, csi, "2K") }

// Line 清空并写入一行（style 为空则原样）。
func (s *Screen) Line(row int, text, style string) {
	s.clearLine(row)
	fmt.Fprint(s.w, style, text, reset, "\n")
}

// Invert 把整行反显（选中态）。
func (s *Screen) Invert(row int, text string) {
	s.clearLine(row)
	fmt.Fprint(s.w, "  ", invert, " ", text, " ", reset, "\n")
}

// Center 居中包装文本（仅加样式，不做真实居中）。
func (s *Screen) Center(text, style string) string { return style + text + reset }

// Raw 用指定样式包一段文本（不换行）。
func (s *Screen) Raw(text, style string) string { return style + text + reset }
