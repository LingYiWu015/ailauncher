//go:build !windows

package core

import (
	"errors"
	"os"
	"os/exec"
	"strings"
)

// launchGUI 直接 detached 拉起（Setpgid）；Unix 无 kill-on-close Job 概念，无逃逸链。
func launchGUI(agent *Agent, wd string, envs map[string]string, extraArgs []string) error {
	args := []string{}
	if agent.Args != "" {
		args = append(args, strings.Fields(agent.Args)...)
	}
	args = append(args, extraArgs...)
	cmd := exec.Command(agent.Exec, args...)
	cmd.Dir = wd
	cmd.Env = append(os.Environ(), envPairs(envs)...)
	cmd.SysProcAttr = detachProcAttr()
	return cmd.Start()
}

// launchTUI 拼 K=V … 命令行，用 config.terminal 拉起。
func launchTUI(agent *Agent, wd string, envs map[string]string, extraArgs []string, terminal string) error {
	cmdline := buildTUIShellCmdline(agent, envs, extraArgs)
	argv := terminalArgv(terminal, wd, cmdline)
	return startDetached(argv, wd)
}

// buildTUIShellCmdline 拼 Unix 版 tui 命令：K=V … <exec> <args> <extra>。
func buildTUIShellCmdline(agent *Agent, envs map[string]string, extraArgs []string) string {
	var b strings.Builder
	for k, v := range envs {
		b.WriteString(k + "=" + v + " ")
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

// startDetached Unix 下 detach（Setpgid）即够，无 Job/breakaway 逃逸链。
func startDetached(argv []string, wd string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = wd
	cmd.SysProcAttr = detachProcAttr()
	return cmd.Start()
}

// launchDetachedEnv 与 startDetached 同构，显式注入进程环境（dsh 等后台服务用）。
func launchDetachedEnv(argv []string, wd string, env []string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = wd
	cmd.Env = env
	cmd.SysProcAttr = detachProcAttr()
	return cmd.Start()
}

// launchWindowsFallback 仅 Windows 需要（WMI/explorer 逃逸 kill-on-close Job）。
// 非 Windows 下 detachProcAttr 走 Setpgid 脱离进程组，直拉基本不会失败，
// 此函数实际不可达；仅占位保证跨平台编译。
func launchWindowsFallback(argv []string, wd string) error {
	return errors.New("launchWindowsFallback: not supported on this platform")
}
