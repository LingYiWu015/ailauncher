// Package core 是 AILauncher v4 的逻辑处理层：每条逻辑是一个自注册单元，
// 声明（stage + declared）与实现同文件，不手工维护集中注册表。
// cli 是纯路由，ui 只经 cli 完成逻辑处理；依赖方向 ui → cli → core。
package core
