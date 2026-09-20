package core

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func init() {
	Register(&Unit{
		Name:     "dsh",
		Stage:    StageStable,
		Kind:     UnitCommand,
		Declared: "DeepSeek Harness：dsh [web|headless] [KEY=V|arg...]（web 起后台服务，headless 跑一次性任务）",
		Run:      dshUnit,
	})
}

// dshRepoDir 解析 deepseek-harness 仓库根：config extension.dsh.repo 优先，
// 回退环境变量 DSH_REPO。都无或路径不存在时报错。pnpm dsh 必须在仓库根执行。
func dshRepoDir(ctx *Ctx) (string, error) {
	repo := ""
	var ec struct {
		Repo string `json:"repo"`
	}
	if ctx.Cfg != nil {
		_ = ctx.Cfg.Extension.Decode("dsh", &ec)
		repo = ec.Repo
	}
	if repo == "" {
		repo = os.Getenv("DSH_REPO")
	}
	if repo == "" {
		return "", fmt.Errorf("未配置 deepseek-harness 仓库路径：config.json 的 extension.dsh.repo 或环境变量 DSH_REPO")
	}
	if !dirExists(repo) {
		return "", fmt.Errorf("deepseek-harness 仓库不存在：%s", repo)
	}
	return repo, nil
}

// dshRepoFromAgent 解析 agent 级 extension.dsh.repo（gui 型 dsh agent 启动时用作
// 固定工作目录）；未配置或目录不存在返回空串。Launch 经 expandWorkdir 覆盖 wd。
func dshRepoFromAgent(a *Agent) string {
	if a == nil || len(a.Extension) == 0 {
		return ""
	}
	var ec struct {
		Repo string `json:"repo"`
	}
	if err := a.Extension.Decode("dsh", &ec); err != nil || ec.Repo == "" {
		return ""
	}
	if !dirExists(ec.Repo) {
		return ""
	}
	return ec.Repo
}

// dshAgentHint 生成「仓库服务型 agent」经 run 启动后的提示行：仓库服务即 dsh web
// 后台服务，追加访问地址。非 dsh 仓库 agent 返回空串。extra 里的 --port 同步进提示。
func dshAgentHint(agent *Agent, extra []string) string {
	if dshRepoFromAgent(agent) == "" {
		return ""
	}
	port := dshWebPort(extra)
	if port == "0" {
		return "\n端口由系统分配（--port 0），实际地址见 dsh 侧日志；默认 3080。"
	}
	return fmt.Sprintf("\n浏览器打开 http://127.0.0.1:%s 使用 DeepSeek Harness", port)
}

// dshWebSpec 构造 dsh web 的后台启动三元组：argv / 固定工作目录（仓库根）/ 环境。
func dshWebSpec(repo string, envs map[string]string, extra []string) ([]string, string, []string) {
	argv := append([]string{"pnpm", "dsh", "web"}, extra...)
	return argv, repo, append(os.Environ(), envPairs(envs)...)
}

// dshWebPort 解析透传参数里的 --port 值；未指定返回默认 "3080"；--port 0 返回 "0"（由 OS 分配）。
func dshWebPort(extra []string) string {
	for i, a := range extra {
		if a == "--port" && i+1 < len(extra) {
			return extra[i+1]
		}
		if v, ok := strings.CutPrefix(a, "--port="); ok {
			return v
		}
	}
	return "3080"
}

// dshHeadlessCmd 构造 dsh headless 的前台命令：阻塞透传输出，用户拿到最终答案。
func dshHeadlessCmd(repo string, args []string, env []string) *exec.Cmd {
	argv := append([]string{"dsh", "--profile", "headless"}, args...)
	cmd := exec.Command("pnpm", argv...)
	cmd.Dir = repo
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}

// dshUnit 分发 dsh 子命令：
//
//	dsh [web] [KEY=V|arg...]     起 web UI（detached 后台服务），提示实际地址
//	dsh headless "任务" [KEY=V]  前台跑一次性任务，阻塞透传输出直到出答案
func dshUnit(ctx *Ctx, args []string) (*Result, error) {
	mode := "web"
	if len(args) > 0 {
		switch args[0] {
		case "web":
		case "headless":
			mode = "headless"
		case "help":
			return Text("DeepSeek Harness 用法：\n" +
				"  ailauncher dsh [web] [KEY=V|arg...]     起后台 web 服务（浏览器打开 http://127.0.0.1:3080）\n" +
				"  ailauncher dsh headless \"任务\" [KEY=V]  前台跑一次性任务\n" +
				"  KEY=V 注入环境变量（如 DEEPSEEK_API_KEY=xxx）；--port N 改 web 端口\n" +
				"仓库路径：config.json extension.dsh.repo 或环境变量 DSH_REPO"), nil
		default:
			return nil, Usagef("未知 dsh 子命令 %q：dsh [web|headless] [KEY=V|arg...]", args[0])
		}
		args = args[1:]
	}

	repo, err := dshRepoDir(ctx)
	if err != nil {
		return nil, err
	}

	envs, extra := ParseArgs(args)
	env := append(os.Environ(), envPairs(envs)...)

	if mode == "headless" {
		if len(extra) == 0 {
			return nil, Usagef("dsh headless 需要任务描述：dsh headless \"任务\" [KEY=V]")
		}
		if err := dshHeadlessCmd(repo, extra, env).Run(); err != nil {
			return nil, fmt.Errorf("deepseek-harness headless 执行失败：%v", err)
		}
		return Text(fmt.Sprintf("deepseek-harness 任务完成（%d 个任务参数）", len(extra))), nil
	}

	argv, wd, webEnv := dshWebSpec(repo, envs, extra)
	if err := launchDetachedEnv(argv, wd, webEnv); err != nil {
		return nil, fmt.Errorf("启动 DeepSeek Harness 失败：%v", err)
	}
	port := dshWebPort(extra)
	if port == "0" {
		return Text("已启动 DeepSeek Harness 后台服务（--port 0 端口由系统分配，实际地址见 dsh 侧日志；默认 3080）。"), nil
	}
	return Text(fmt.Sprintf("已启动 DeepSeek Harness 后台服务。浏览器打开 http://127.0.0.1:%s\n"+
		"提示：首次使用需在 Web UI 的「设置→模型」配置密钥/模型；无 key 可先跑 mock server（见 deepseek-harness 启动指南第 3A 节）。", port)), nil
}
