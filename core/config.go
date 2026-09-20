package core

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// 默认数据文件名（与旧 Node 版同名，schema 为 v4）。
const (
	DefaultConfigFile = "config.json"
	DefaultStateFile  = "data.json"
)

// Config 是静态配置（只读种子），与运行时 State 分离。
type Config struct {
	Terminal           string    `json:"terminal,omitempty"` // 拉起 TUI 工具的终端（如 Alacritty）
	DefaultDirectories []string  `json:"defaultDirectories,omitempty"`
	Agents             []*Agent  `json:"agents"`
	Extension          Extension `json:"extension,omitempty"`
}

// LoadConfig 读取 JSON 配置；文件缺失返回空配置。
func LoadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Config{}, nil
		}
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	if c.Agents == nil {
		c.Agents = []*Agent{}
	}
	return &c, nil
}

// Save 序列化配置到 path。
func (c *Config) Save(path string) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// ResolveAgent 按 name 或 display 精确匹配。
func (c *Config) ResolveAgent(name string) *Agent {
	if name == "" {
		return nil
	}
	for _, a := range c.Agents {
		if a != nil && (a.Name == name || a.Display == name) {
			return a
		}
	}
	return nil
}

// BaseDir 解析配置/状态的基准目录：优先当前工作目录（与旧 Node 版一致）；
// 若 cwd 无 config.json 则回退到可执行文件所在目录，便于从任意目录调用二进制。
func BaseDir() string {
	if _, err := os.Stat(DefaultConfigFile); err == nil {
		if wd, werr := os.Getwd(); werr == nil {
			return wd
		}
	}
	if exe, eerr := os.Executable(); eerr == nil {
		return filepath.Dir(exe)
	}
	return "."
}

// Paths 把 baseDir 解析成 config/state 的完整路径。
func Paths(baseDir string) (configPath, statePath string) {
	return filepath.Join(baseDir, DefaultConfigFile), filepath.Join(baseDir, DefaultStateFile)
}
