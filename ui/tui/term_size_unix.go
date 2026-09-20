//go:build linux || darwin || freebsd || netbsd || openbsd

package tui

import (
	"os"
	"syscall"
	"unsafe"
)

type winsize struct {
	Row    uint16
	Col    uint16
	Xpixel uint16
	Ypixel uint16
}

// getConsoleSize 经 TIOCGWINSZ 查询终端窗口宽高（列，行）。
func getConsoleSize() (int, int, bool) {
	var ws winsize
	for _, f := range []*os.File{os.Stdout, os.Stdin} {
		if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&ws))); errno == 0 {
			w, h := int(ws.Col), int(ws.Row)
			if w >= 20 && h >= 6 {
				return w, h, true
			}
			return 0, 0, false
		}
	}
	return 0, 0, false
}
