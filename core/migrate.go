package core

import (
	"encoding/json"
	"errors"
	"os"
)

// MigrateConfig 对 config.json 做 v4 数据归一化（幂等）：
//   - v4 已整体移除旧 tools[] 兼容读取。若文件仍为旧 tools[] 格式（无 agents），
//     返回明确错误（开发期不背包袱，请手工按 config.sample.json 迁移到 agents[]）。
//   - 已是 agents[] 形态则不动文件，返回 false。
func MigrateConfig(path string) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	var raw struct {
		Agents json.RawMessage `json:"agents"`
		Tools  json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return false, err
	}
	if len(raw.Tools) > 0 && len(raw.Agents) == 0 {
		return false, errors.New("config.json 仍为旧 tools[] 格式，v4 已移除该兼容；请按 config.sample.json 手工迁移到 agents[]")
	}
	return false, nil
}

// MigrateState 清理 data.json 里旧版残留的 null 数组（如 removedCmds: null），
// 归一化为 []；其余字段原样保留。无变化不动文件。返回是否发生变更。
func MigrateState(path string) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		return false, err
	}
	changed := false
	for _, st := range s {
		if st == nil {
			continue // 顶层键为 null 的残留：不动
		}
		if st.normalize() {
			changed = true
		}
	}
	if changed {
		if err := s.Save(path); err != nil {
			return false, err
		}
	}
	return changed, nil
}
