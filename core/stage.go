package core

import "os"

// Stage 是逻辑单元的生命周期分级。自研逻辑直接进入 experimental；
// 从外部拉取的逻辑先过 checking；晋升/回退只改声明，不挪代码。
type Stage string

const (
	StageDraft        Stage = "draft"        // 草稿：仅存在，不暴露
	StageChecking     Stage = "checking"     // 验证中：仅存在，不暴露
	StageExperimental Stage = "experimental" // 实验：打包但默认不暴露（env 开启）
	StageStable       Stage = "stable"       // 稳定：默认暴露
	StageAbandoned    Stage = "abandoned"    // 废弃：不参与路由
)

// EnvExperimental 打开 experimental 暴露的开关（阶段 A 运行时分级）。
const EnvExperimental = "AILAUNCHER_EXPERIMENTAL"

// ExperimentalEnabled 报告 experimental 单元是否参与路由与能力清单。
func ExperimentalEnabled() bool { return os.Getenv(EnvExperimental) == "1" }

// Visible 报告该 stage 当前是否参与路由与能力清单。
func (s Stage) Visible() bool {
	switch s {
	case StageStable:
		return true
	case StageExperimental:
		return ExperimentalEnabled()
	default:
		return false
	}
}

// Rank 供能力清单按生命周期排序（稳定最前，其余按推进顺序）。
func (s Stage) Rank() int {
	switch s {
	case StageStable:
		return 0
	case StageExperimental:
		return 1
	case StageChecking:
		return 2
	case StageDraft:
		return 3
	default:
		return 4 // abandoned 及未知
	}
}
