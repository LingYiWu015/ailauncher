package core

import (
	"errors"
	"fmt"
	"io"
	"sort"
)

// UnitKind 区分命令类（有副作用）与查询/自省类（只读）。
type UnitKind string

const (
	UnitCommand UnitKind = "command" // 命令类：启动/写/变更，有副作用
	UnitQuery   UnitKind = "query"   // 查询/自省类：只读
)

// ResultKind 决定 cli 的格式化方向。
type ResultKind string

const (
	ResultText ResultKind = "text" // 人类可读
	ResultJSON ResultKind = "json" // 脚本可读
)

// Result 是逻辑单元的输出。Kind=JSON 时 Data 由 cli 序列化输出；
// Kind=Text 时 cli 原样打印 Text。Exit 覆盖进程退出码（0/1/2）。
type Result struct {
	Kind ResultKind
	Text string
	Data any
	Exit int
}

// Text 构造文本结果。
func Text(s string) *Result { return &Result{Kind: ResultText, Text: s} }

// JSON 构造 JSON 结果（cli 负责序列化）。
func JSON(data any) *Result { return &Result{Kind: ResultJSON, Data: data} }

// Ctx 由路由注入逻辑单元：数据文件路径、已装载的配置/状态、IO 输入源。
// 单元不自己读文件，避免重复装载与路径漂移。
type Ctx struct {
	BaseDir    string
	ConfigPath string
	StatePath  string
	Cfg        *Config
	St         State
	Sessions   SessionStore
	In         io.Reader // state put 的 JSON 输入源（TUI 注入 reader，脚本为 os.Stdin）
}

// Unit 是逻辑处理层的一个自注册单元：声明（stage+declared）与逻辑同文件。
// 既有命令类也有查询/自省类。
type Unit struct {
	Name     string // 路由名（单 token；子命令由单元内部处理，如 state get/put）
	Stage    Stage
	Kind     UnitKind
	Declared string // 一句话声明（help / cap / 详细报错用）
	Run      func(ctx *Ctx, args []string) (*Result, error)
}

var units []*Unit

// Register 登记一个逻辑单元（在各自源文件的 init() 中调用）。
func Register(u *Unit) { units = append(units, u) }

// Snapshot 返回全部单元（含未暴露的）的拷贝，供 cli 构建路由表。
func Snapshot() []*Unit {
	out := make([]*Unit, len(units))
	copy(out, units)
	return out
}

// Lookup 按名字精确查单元（含未暴露的）。
func Lookup(name string) *Unit {
	for _, u := range units {
		if u.Name == name {
			return u
		}
	}
	return nil
}

// Capability 是机器可读的能力条目（cap 输出 / cli 详细报错的信息源）。
type Capability struct {
	Name     string   `json:"name"`
	Stage    Stage    `json:"stage"`
	Kind     UnitKind `json:"kind"`
	Declared string   `json:"declared"`
}

// Capabilities 返回当前可见（stage 过滤后）单元的能力清单，按 stage 排序。
func Capabilities() []Capability {
	var out []Capability
	for _, u := range units {
		if !u.Stage.Visible() {
			continue
		}
		out = append(out, Capability{Name: u.Name, Stage: u.Stage, Kind: u.Kind, Declared: u.Declared})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Stage.Rank() < out[j].Stage.Rank() })
	return out
}

// ErrUsage 表示命令用法错误（cli 映射为退出码 2）。
var ErrUsage = errors.New("usage")

// Usagef 构造用法错误（可被 errors.Is(err, ErrUsage) 判定）。
func Usagef(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrUsage, fmt.Sprintf(format, a...))
}
