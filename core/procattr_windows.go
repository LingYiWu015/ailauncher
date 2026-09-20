//go:build windows

package core

import "syscall"

const (
	detachedProcess        = 0x00000008
	createNewProcessGroup  = 0x00000200
	createBreakawayFromJob = 0x01000000
)

// detachProcAttr 让子进程脱离父进程独立运行（gui/tui 都直接拉起后返回）。
// 附带 CREATE_BREAKAWAY_FROM_JOB：逃出启动终端的 kill-on-close Job（如 WezTerm
// 关标签会杀整棵进程树），使拉起的窗口不随启动终端关闭而消亡。
// 注意：若 Job 禁止 breakaway，CreateProcess 会返回 ERROR_ACCESS_DENIED，
// 此时由 launch 层走 WMI/explorer 逃逸兜底。
func detachProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		CreationFlags: detachedProcess | createNewProcessGroup | createBreakawayFromJob,
	}
}

// detachProcAttrNoBreakaway 兜底：不带 BREAKAWAY（Job 拒绝时退化为旧行为，
// 保证至少能启动；但关启动终端会带走窗口）。
func detachProcAttrNoBreakaway() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		CreationFlags: detachedProcess | createNewProcessGroup,
	}
}
