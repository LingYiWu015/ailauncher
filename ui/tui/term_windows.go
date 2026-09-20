//go:build windows

package tui

import (
	"fmt"
	"syscall"
	"unsafe"
)

// Windows 控制台模式标志。
const (
	enableProcessedInput = 0x0001
	enableLineInput      = 0x0002
	enableEchoInput      = 0x0004

	enableVirtualTerminalInput  = 0x0200 // 输入：让方向键等以 ANSI 转义到达
	enableVirtualTerminalOutput = 0x0004 // 输出：启用 ANSI 颜色/光标控制
)

// kernel32 句柄（syscall.SetConsoleMode 不在 stdlib 暴露，走 LazyDLL；仍零依赖）。
var (
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	procGetStdHandle   = kernel32.NewProc("GetStdHandle")
	procGetConsoleMode = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode = kernel32.NewProc("SetConsoleMode")
)

// STD_INPUT_HANDLE / STD_OUTPUT_HANDLE（-10 / -11，转成 DWORD）。
const (
	stdInputHandle  = 0xFFFFFFF6
	stdOutputHandle = 0xFFFFFFF5
	invalidHandle   = ^uintptr(0)
)

func getConsoleMode(h uintptr) (uint32, error) {
	var mode uint32
	r, _, err := procGetConsoleMode.Call(h, uintptr(unsafe.Pointer(&mode)))
	if r == 0 {
		return 0, err
	}
	return mode, nil
}

func setConsoleMode(h uintptr, mode uint32) error {
	r, _, err := procSetConsoleMode.Call(h, uintptr(mode))
	if r == 0 {
		return err
	}
	return nil
}

// enterRaw 把 stdin 切到 raw 模式（关闭行缓冲/回显），并启用 ANSI 虚拟终端，
// 返回恢复函数。若 stdin 不是控制台（如管道），返回错误 = 非交互 TTY 检测。
func enterRaw() (func(), error) {
	inH, _, _ := procGetStdHandle.Call(stdInputHandle)
	if inH == 0 || inH == invalidHandle {
		return nil, fmt.Errorf("标准输入不是 Windows 控制台（请在 Windows Terminal、PowerShell 或 cmd 中运行）")
	}
	origIn, err := getConsoleMode(inH)
	if err != nil {
		return nil, fmt.Errorf("标准输入不是 Windows 控制台：%w", err)
	}

	outH, _, _ := procGetStdHandle.Call(stdOutputHandle)
	if outH == 0 || outH == invalidHandle {
		return nil, fmt.Errorf("标准输出不是 Windows 控制台（请在 Windows Terminal、PowerShell 或 cmd 中运行）")
	}
	origOut, err := getConsoleMode(outH)
	if err != nil {
		return nil, fmt.Errorf("标准输出不是 Windows 控制台：%w", err)
	}

	in := origIn &^ (enableProcessedInput | enableLineInput | enableEchoInput)
	in |= enableVirtualTerminalInput
	if err := setConsoleMode(inH, in); err != nil {
		return nil, err
	}
	out := origOut | enableVirtualTerminalOutput
	if err := setConsoleMode(outH, out); err != nil {
		setConsoleMode(inH, origIn)
		return nil, err
	}

	return func() {
		setConsoleMode(inH, origIn)
		setConsoleMode(outH, origOut)
	}, nil
}
