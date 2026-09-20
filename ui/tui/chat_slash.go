package tui

import (
	"context"
	"fmt"
)

// 本文件是 /mode /model /commands 的选择页：用统一 listPage 展示后端下发的
// 选项，用户确认后透传 SetMode/SetConfig。CLI 只给开发者用，用户面全走这里。

func (l *chatLoop) slashMode(ctx context.Context, args string, consume func() (bool, error)) (bool, error) {
	m := l.model
	modes := l.live.Modes()
	if len(modes) == 0 {
		modes = m.modes
	}
	if args != "" {
		if err := l.live.SetMode(ctx, args); err != nil {
			m.err = err.Error()
			m.touch()
		} else {
			m.mode = args
			m.touch()
		}
		return consume()
	}
	if len(modes) == 0 {
		m.blocks = append(m.blocks, chatBlock{role: roleStatus, text: "后端未下发可用模式", finished: true})
		m.touch()
		return consume()
	}
	items := make([]string, len(modes))
	ids := make([]string, len(modes))
	cur := l.live.CurrentMode()
	if cur == "" {
		cur = m.mode
	}
	for i, md := range modes {
		mark := ""
		if md.ID == cur {
			mark = "  " + ccFaint + "(当前)" + reset
		}
		desc := ""
		if md.Desc != "" {
			desc = " - " + md.Desc
		}
		items[i] = fmt.Sprintf("%s%s%s%s", md.Name, dark, desc, reset) + mark
		ids[i] = md.ID
	}
	res, err := l.s.listPage(listOpts{
		title:        "切换模式",
		mainTitle:    "后端下发的可用模式",
		removedTitle: "",
		mainItems:    &items,
		removedItems: &[]string{},
		hint:         listHint(),
	})
	if err != nil {
		return consume()
	}
	if res.action != "select" {
		return consume()
	}
	idx := indexOf(items, res.value)
	if idx < 0 || idx >= len(ids) {
		return consume()
	}
	if err := l.live.SetMode(ctx, ids[idx]); err != nil {
		m.err = err.Error()
		m.touch()
		return consume()
	}
	m.mode = ids[idx]
	m.flash("已切换模式: " + ids[idx])
	return consume()
}

func (l *chatLoop) slashModel(ctx context.Context, args string, consume func() (bool, error)) (bool, error) {
	m := l.model
	opts := l.live.ConfigOptions()
	if len(opts) == 0 {
		opts = m.configs
	}
	var modelOpt *struct {
		id      string
		options []string
		current string
	}
	for _, o := range opts {
		if o.ID == "model" {
			oo := o
			modelOpt = &struct {
				id      string
				options []string
				current string
			}{id: oo.ID, options: oo.Options, current: oo.Value}
			break
		}
	}
	if modelOpt == nil {
		m.blocks = append(m.blocks, chatBlock{role: roleStatus, text: "后端未下发模型选项（/usage 看看也行）", finished: true})
		m.touch()
		return consume()
	}
	if args != "" {
		if err := l.live.SetConfig(ctx, modelOpt.id, args); err != nil {
			m.err = err.Error()
			m.touch()
		} else {
			m.flash("已切换模型: " + args)
		}
		return consume()
	}
	items := make([]string, len(modelOpt.options))
	ids := make([]string, len(modelOpt.options))
	for i, raw := range modelOpt.options {
		value, label := splitOption(raw)
		mark := ""
		if value == modelOpt.current {
			mark = "  " + ccFaint + "(当前)" + reset
		}
		items[i] = fmt.Sprintf("%s%s", label, mark)
		ids[i] = value
	}
	res, err := l.s.listPage(listOpts{
		title:        "切换模型",
		mainTitle:    "后端下发的可用模型",
		removedTitle: "",
		mainItems:    &items,
		removedItems: &[]string{},
		hint:         listHint(),
	})
	if err != nil {
		return consume()
	}
	if res.action != "select" {
		return consume()
	}
	idx := indexOf(items, res.value)
	if idx < 0 || idx >= len(ids) {
		return consume()
	}
	if err := l.live.SetConfig(ctx, modelOpt.id, ids[idx]); err != nil {
		m.err = err.Error()
		m.touch()
		return consume()
	}
	m.flash("已切换模型: " + ids[idx])
	return consume()
}

func splitOption(raw string) (value, label string) {
	for i := 0; i < len(raw); i++ {
		if raw[i] == '|' {
			return raw[:i], raw[i+1:]
		}
	}
	return raw, raw
}

func (l *chatLoop) slashCommands(consume func() (bool, error)) (bool, error) {
	m := l.model
	if cmds := l.live.AvailableCommands(); len(cmds) > 0 {
		m.commands = cmds
	}
	cmds := m.commands
	if len(cmds) == 0 {
		m.blocks = append(m.blocks, chatBlock{role: roleStatus, text: "后端尚未下发可用命令", finished: true})
		m.touch()
		return consume()
	}
	items := make([]string, len(cmds))
	for i, c := range cmds {
		desc := ""
		if c.Desc != "" {
			desc = " - " + c.Desc
		}
		hint := ""
		if c.Input != "" {
			hint = "  " + ccFaint + "(" + c.Input + ")" + reset
		}
		items[i] = fmt.Sprintf("/%s%s%s%s", c.Name, dark, desc, reset) + hint
	}
	res, err := l.s.listPage(listOpts{
		title:        "可用命令",
		mainTitle:    "后端下发的命令（选中填入输入框）",
		removedTitle: "",
		mainItems:    &items,
		removedItems: &[]string{},
		hint:         listHint(),
	})
	if err != nil {
		return consume()
	}
	if res.action != "select" {
		return consume()
	}
	idx := indexOf(items, res.value)
	if idx < 0 || idx >= len(cmds) {
		return consume()
	}
	m.input.setText("/" + cmds[idx].Name + " ")
	m.touch()
	return consume()
}
