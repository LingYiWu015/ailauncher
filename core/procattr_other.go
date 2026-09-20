//go:build !windows

package core

import "syscall"

// detachProcAttr 在 Unix 下让子进程独立进程组运行。
func detachProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// detachProcAttrNoBreakaway：Unix 下 detach 即 Setpgid，无 Job/breakaway 概念，
// 兜底变体与 detachProcAttr 相同（占位，保证跨平台编译）。
func detachProcAttrNoBreakaway() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}
