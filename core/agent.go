package core

// Agent 定义一个 AI 工具的启动配置。
// 接口差异（原 adapter）走 Extension 约定，不入顶层字段。
// Enabled=false 的 agent 在 TUI 选单与 probe 中隐藏（CLI list 照常可见，显式点名仍可用）。
type Agent struct {
	Name        string            `json:"name"`
	Display     string            `json:"display,omitempty"`
	Desc        string            `json:"desc,omitempty"`
	Exec        string            `json:"exec"`
	Type        string            `json:"type"` // "tui" | "gui"
	Args        string            `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	RootDir     string            `json:"rootDir,omitempty"`
	Directories []string          `json:"directories,omitempty"`
	Enabled     *bool             `json:"enabled,omitempty"`
	Extension   Extension         `json:"extension,omitempty"`
}

// IsEnabled 报告 agent 是否启用（缺省启用，保证老配置零迁移）。
func (a *Agent) IsEnabled() bool {
	if a == nil || a.Enabled == nil {
		return true
	}
	return *a.Enabled
}
