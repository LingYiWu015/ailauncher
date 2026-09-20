package core

import (
	"os"
	"strings"
)

// ParseArgs 把命令行 token 拆成「环境变量 + 附加参数」。
// KEY=VALUE 且 key 为合法环境变量名的 token 视为环境变量，其余为附加参数。
func ParseArgs(tokens []string) (envs map[string]string, args []string) {
	envs = map[string]string{}
	for _, tok := range tokens {
		if k, v, ok := strings.Cut(tok, "="); ok && isEnvKey(k) {
			envs[k] = v
		} else {
			args = append(args, tok)
		}
	}
	return envs, args
}

func isEnvKey(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

// launchFn 是启动可注入点：单测替换为桩避免真实拉起进程，生产指向 Launch。
var launchFn = Launch

// expandWorkdir 决定启动 cwd：extension.dsh.repo（仓库服务型）优先，其次传入 workdir，
// 再回退 USERPROFILE / 当前目录。纯函数，便于单测。
func expandWorkdir(agent *Agent, workdir string) string {
	wd := workdir
	if r := dshRepoFromAgent(agent); r != "" {
		wd = r
	}
	if wd == "" {
		wd = os.Getenv("USERPROFILE")
	}
	if wd == "" {
		wd, _ = os.Getwd()
	}
	return wd
}

// Launch 按 agent 类型拉起：
//   - gui: 直接 detached 启动，不套终端
//   - tui: 拼 [set K=V && | K=V ]… <exec> <args> <extra>，用 terminal 拉起
//
// 平台差异（逃逸链 / 命令行拼法）在 *_windows / *_other 里按 build tag 隔离。
func Launch(agent *Agent, workdir string, envs map[string]string, extraArgs []string, terminal string) error {
	wd := expandWorkdir(agent, workdir)
	merged := mergeEnv(agent.Env, envs)
	if agent.Type == "gui" {
		return launchGUI(agent, wd, merged, extraArgs)
	}
	return launchTUI(agent, wd, merged, extraArgs, terminal)
}

func mergeEnv(base, over map[string]string) map[string]string {
	merged := make(map[string]string, len(base)+len(over))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range over {
		merged[k] = v
	}
	return merged
}

func envPairs(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}
