package cli

import "core"

// protoLab 是 cli 本地的实验位（A 阶段留空占位）。
// 未来：core 还没有、仍在快速变化、不适合立即进 core 的逻辑，先放这里试跑；
// 稳定后作为 core 单元注册（改 stage / 挪文件），再从这里移除。
func protoLab() []*core.Unit {
	return nil
}
