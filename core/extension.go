package core

import (
	"encoding/json"
	"fmt"
)

// Extension 是数据模型统一的向前兼容槽（Agent/Config/AgentState 各留一份）：
// 任何未来的 schema 差异、接口适配（含原 adapter 字段）都落入这里。
// 旧字段永不删除，新字段通过 extension 渐进加入，保证读旧文件平滑。
type Extension map[string]json.RawMessage

// Has 报告 key 是否存在。
func (x Extension) Has(key string) bool {
	_, ok := x[key]
	return ok
}

// Get 返回 key 的原始字节（不存在返回 nil）。
func (x Extension) Get(key string) json.RawMessage { return x[key] }

// Decode 把 key 解码到 dst；key 不存在时不做任何事（返回 nil）。
func (x Extension) Decode(key string, dst any) error {
	if !x.Has(key) {
		return nil
	}
	return json.Unmarshal(x[key], dst)
}

// Set 写入 key；map 为 nil 时返回错误（调用方须先初始化 Extension{}）。
func (x Extension) Set(key string, v any) error {
	if x == nil {
		return fmt.Errorf("extension: nil map，调用方须先初始化 Extension{}")
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	x[key] = b
	return nil
}
