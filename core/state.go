package core

import (
	"encoding/json"
	"errors"
	"os"
)

// AgentState 是单个 agent 的运行时状态（与 Config 分离，未来 daemon/GUI 复用）。
type AgentState struct {
	Directories []string  `json:"directories"`
	RemovedDirs []string  `json:"removedDirs"`
	Commands    []string  `json:"commands"`
	RemovedCmds []string  `json:"removedCmds"`
	Seeded      bool      `json:"seeded"`
	RootDir     string    `json:"rootDir,omitempty"`
	Recent      []string  `json:"recent,omitempty"`    // 预留：最近使用
	Favorites   []string  `json:"favorites,omitempty"` // 预留：收藏
	Extension   Extension `json:"extension,omitempty"`
}

// State 按 agent name 分 key 的运行时状态表（顶层 JSON 形状与旧版 data.json 一致）。
type State map[string]*AgentState

func newAgentState() *AgentState {
	return &AgentState{
		Directories: []string{},
		RemovedDirs: []string{},
		Commands:    []string{},
		RemovedCmds: []string{},
	}
}

// normalize 把 nil 切片归一化为空切片（避免 JSON 落盘成 null 数组），
// 返回是否发生变更。migrate 与 state put 共用此单点归一化。
func (st *AgentState) normalize() bool {
	changed := false
	for _, sl := range []*[]string{&st.Directories, &st.RemovedDirs, &st.Commands, &st.RemovedCmds} {
		if *sl == nil {
			*sl = []string{}
			changed = true
		}
	}
	return changed
}

// Get 返回指定 agent 的状态；缺失时补默认结构并登记。
// nil map 无法通过值接收器写回，遇到 nil 时返回独立的默认状态，避免公共 API panic。
func (s State) Get(name string) *AgentState {
	if s == nil {
		return newAgentState()
	}
	if s[name] == nil {
		s[name] = newAgentState()
	}
	return s[name]
}

// LoadState 读取状态文件；缺失返回空 State（非 nil）。
func LoadState(path string) (State, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return State{}, nil
		}
		return nil, err
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	if s == nil {
		s = State{}
	}
	return s, nil
}

// Save 序列化状态到 path。
func (s State) Save(path string) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
