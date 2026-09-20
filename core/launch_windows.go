//go:build windows

package core

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// buildCmdline 按 Windows 命令行为每个 argv 转义后拼成一个串。
// 复用 syscall.EscapeArg（与 os/exec 拼命令行的规则同源），保证带空格路径、
// 引号等 token 在 CreateProcess / cmd / wscript 解析时与直接 exec.Command 一致。
func buildCmdline(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		parts[i] = syscall.EscapeArg(a)
	}
	return strings.Join(parts, " ")
}

// writeVBS 生成一个临时 .vbs：用 wscript 以隐藏窗口（样式 0）运行 cmdline。
// 隐藏窗口可避免打开 .cmd 时的控制台闪烁；GUI 程序（Alacritty）仍照常弹自己的窗口。
// cmdline 里的引号按 VBS 规则翻倍。
func writeVBS(cmdline string) (string, error) {
	body := fmt.Sprintf("CreateObject(\"WScript.Shell\").Run \"%s\", 0, False",
		strings.ReplaceAll(cmdline, `"`, `""`))
	path := filepath.Join(os.TempDir(), fmt.Sprintf("ailauncher-launch-%d.vbs", os.Getpid()))
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// writeWmiVBS 生成一个临时 .vbs：经 WMI Win32_Process.Create 由 WmiPrvSE 进程创建目标。
// WmiPrvSE 是系统服务、不在启动终端的 kill-on-close Job 里，因此它创建的进程确定
// 不随启动终端关闭而消亡（explorer 逃逸依赖 ShellExecute 委托行为、不保证，这是
// 确定性的替代）。脚本用 Create 的返回值作为退出码，调用方据此判断创建是否成功。
// 注意：VBS 中 Create 参数必须用括号括起，否则字符串会被当作子表达式拼接（见 devlog 05）。
func writeWmiVBS(cmdline, cwd string) (string, error) {
	body := fmt.Sprintf("Set o = GetObject(\"winmgmts:\\\\.\\root\\cimv2\")\r\n"+
		"rc = o.Get(\"Win32_Process\").Create(\"%s\", \"%s\", null, pid)\r\n"+
		"WScript.Quit rc\r\n",
		strings.ReplaceAll(cmdline, `"`, `""`),
		strings.ReplaceAll(cwd, `"`, `""`))
	path := filepath.Join(os.TempDir(), fmt.Sprintf("ailauncher-launch-%d.vbs", os.Getpid()))
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// launchWindowsFallback 是直接 detach 拉起失败（大概率是启动终端的 kill-on-close
// Job 拒绝了 CREATE_BREAKAWAY_FROM_JOB）时的兜底，两层递进：
//
//  1. WMI Win32_Process.Create：进程由 WmiPrvSE（系统服务）创建，确定在启动终端
//     的 Job/进程树之外；wscript 只负责发起 WMI 调用、随即退出，即使它短暂在 Job 里
//     也无所谓（目标进程不是它创建的）。用退出码判成功，失败继续往下。
//  2. explorer.exe 逃逸：explorer 是桌面 shell、不在 Job 里，但 ShellExecute 是否
//     委托给主 explorer 的行为不保证，仅作 WMI 不可用时的第二选择。
//
// 最后手段（不带 BREAKAWAY 直拉，窗口随启动终端关闭）由调用方负责。
func launchWindowsFallback(argv []string, wd string) error {
	cmdline := buildCmdline(argv)
	if script, err := writeWmiVBS(cmdline, wd); err == nil {
		go func() { time.Sleep(8 * time.Second); os.Remove(script) }()
		if err := exec.Command("wscript.exe", script).Run(); err == nil {
			return nil
		}
	}
	script, err := writeVBS(cmdline)
	if err != nil {
		return err
	}
	go func() { time.Sleep(8 * time.Second); os.Remove(script) }()
	return exec.Command("explorer.exe", script).Start()
}

// launchGUI 直接 detached 拉起 gui 型 agent；失败走 WMI/explorer 逃逸，最后不带 BREAKAWAY 兜底。
func launchGUI(agent *Agent, wd string, envs map[string]string, extraArgs []string) error {
	args := []string{}
	if agent.Args != "" {
		args = append(args, strings.Fields(agent.Args)...)
	}
	args = append(args, extraArgs...)
	argv := append([]string{agent.Exec}, args...)
	cmd := exec.Command(agent.Exec, args...)
	cmd.Dir = wd
	cmd.Env = append(os.Environ(), envPairs(envs)...)
	cmd.SysProcAttr = detachProcAttr()
	if err := cmd.Start(); err == nil {
		return nil
	}
	if cerr := launchWindowsFallback(argv, wd); cerr == nil {
		return nil
	}
	cmd2 := exec.Command(agent.Exec, args...)
	cmd2.Dir = wd
	cmd2.Env = cmd.Env
	cmd2.SysProcAttr = detachProcAttrNoBreakaway()
	return cmd2.Start()
}

// launchTUI 拼 set K=V && … 命令行，用 config.terminal 拉起；失败走逃逸链。
func launchTUI(agent *Agent, wd string, envs map[string]string, extraArgs []string, terminal string) error {
	cmdline := buildTUIShellCmdline(agent, envs, extraArgs)
	argv := terminalArgv(terminal, wd, cmdline)
	return startDetached(argv, wd)
}

// buildTUIShellCmdline 拼 Windows 版 tui 命令：set K=V && … <exec> <args> <extra>。
func buildTUIShellCmdline(agent *Agent, envs map[string]string, extraArgs []string) string {
	var b strings.Builder
	for k, v := range envs {
		fmt.Fprintf(&b, "set %s=%s && ", k, v)
	}
	b.WriteString(agent.Exec)
	if agent.Args != "" {
		b.WriteString(" " + agent.Args)
	}
	if len(extraArgs) > 0 {
		b.WriteString(" " + strings.Join(extraArgs, " "))
	}
	return b.String()
}

// startDetached 直拉（BREAKAWAY）→ WMI/explorer 逃逸 → 不带 BREAKAWAY 兜底。
func startDetached(argv []string, wd string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = wd
	cmd.SysProcAttr = detachProcAttr()
	if err := cmd.Start(); err == nil {
		return nil
	}
	if cerr := launchWindowsFallback(argv, wd); cerr == nil {
		return nil
	}
	cmd2 := exec.Command(argv[0], argv[1:]...)
	cmd2.Dir = wd
	cmd2.SysProcAttr = detachProcAttrNoBreakaway()
	return cmd2.Start()
}

// launchDetachedEnv 与 startDetached 同构，但显式注入进程环境。
// 供 dsh 等「需要 env 的后台服务」detached 拉起使用（tui 的 env 经命令行 set 内置，
// 不需要走这里；gui 走 launchGUI 的 cmd.Env 直拉，此处是其可注入复用的形态）。
func launchDetachedEnv(argv []string, wd string, env []string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = wd
	cmd.Env = env
	cmd.SysProcAttr = detachProcAttr()
	if err := cmd.Start(); err == nil {
		return nil
	}
	if cerr := launchWindowsFallback(argv, wd); cerr == nil {
		return nil
	}
	cmd2 := exec.Command(argv[0], argv[1:]...)
	cmd2.Dir = wd
	cmd2.Env = env
	cmd2.SysProcAttr = detachProcAttrNoBreakaway()
	return cmd2.Start()
}
