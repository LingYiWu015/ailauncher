//go:build windows

package tui

import (
	"unsafe"
)

// procGetConsoleScreenBufferInfo 查询控制台窗口尺寸（与 term_windows.go 共享 kernel32 句柄）。
var procGetConsoleScreenBufferInfo = kernel32.NewProc("GetConsoleScreenBufferInfo")

type smallRect struct {
	left, top, right, bottom int16
}

type consoleScreenBufferInfo struct {
	size              [2]int16
	cursor            [2]int16
	attributes        uint16
	window            smallRect
	maximumWindowSize [2]int16
}

// getConsoleSize 返回当前控制台窗口的宽高（列，行）。
func getConsoleSize() (int, int, bool) {
	outH, _, _ := procGetStdHandle.Call(stdOutputHandle)
	if outH == 0 || outH == invalidHandle {
		return 0, 0, false
	}
	var info consoleScreenBufferInfo
	r, _, _ := procGetConsoleScreenBufferInfo.Call(outH, uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		return 0, 0, false
	}
	w := int(info.window.right-info.window.left) + 1
	h := int(info.window.bottom-info.window.top) + 1
	if w < 20 || h < 6 {
		return 0, 0, false
	}
	return w, h, true
}
