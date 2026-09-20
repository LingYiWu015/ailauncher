package tui

import (
	"errors"
	"io"
)

// 本文件是全 TUI 统一交互规范（唯一事实来源）。
// 目标是零思想阻力：任意页面看到同一提示，就有同一手感。
//
// 全局（列表导航语境；文本输入框内可打印键一律落字）：
//   ↑↓/jk 选择移动 · 数字跳转 · Enter 确认 · ESC/Ctrl-C 返回上一级 · q 返回
// 文本输入框（prompt / 参数输入 / chat 输入）统一 emacs 子集：
//   ←→/Home/End/Ctrl-A/E 移动光标 · Backspace/Delete 删除 · Ctrl-K 删到行尾 ·
//   Ctrl-U 清空 · Ctrl-W 删词 · Enter 确认/发送 · ESC/Ctrl-C 取消
//   （输入框内 q 与数字照常落字，不再有“按 q 取消”怪癖）
// chat 会话：焦点恒在输入框，↑↓ 回忆历史，PgUp/Dn 滚动；
// 回放页无输入框，↑↓/jk 直接滚动。
// 主菜单数字是快选（直接进入），列表页数字是跳转（移动选中，需 Enter 确认）。

// errInputCancel 表示用户在输入框按 ESC/Ctrl-C 取消（与空输入提交区分）。
var errInputCancel = errors.New("输入已取消")

// isKeyStreamError 报告是否为按键流本身的致命错误（EOF 等）；这类错误应直接
// 退出 TUI，其他业务错误（如无 adapter、暂无历史）只闪屏提示并留在菜单。
func isKeyStreamError(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}

// isBackKey 报告按键是否为统一返回键（ESC / Ctrl-C）。
func isBackKey(k Key) bool { return k.Name == "escape" || k.Name == "ctrl-c" }

// isBackRune 报告列表导航语境下 q 是否视为返回（输入框内不调用）。
func isBackRune(k Key) bool {
	return k.Name == "char" && (k.Rune == 'q' || k.Rune == 'Q')
}

// joinHint 拼接统一风格的底部提示（· 分隔，ccFaint 灰）。
func joinHint(parts ...string) string {
	out := "  "
	for i, p := range parts {
		if i > 0 {
			out += "  ·  "
		}
		out += p
	}
	return out
}

// listHint 单栏列表的标准提示；extra 追加页内专有键（如 N 新建）。
func listHint(extra ...string) string {
	parts := []string{"↑↓/jk 选择", "1-9 跳转", "Enter 确认", "ESC/q 返回"}
	return joinHint(append(parts, extra...)...)
}

// dualHint 双栏列表的标准提示（含切栏与软删除全套）。
func dualHint(extra ...string) string {
	parts := []string{"↑↓/jk 选择", "Tab 切栏", "d 移除 u 移回", "H/L 排序", "M 手动", "Enter 确认", "ESC/q 返回"}
	return joinHint(append(parts, extra...)...)
}

const menuHint = "  ↑↓/jk 选择  ·  1-3 快选  ·  Enter 进入  ·  ESC/q 退出"

const argsHint = "  输入框直接输入  ·  Tab 切换焦点  ·  d/u 移除移回  ·  H/L 排序  ·  Enter 运行  ·  ESC 返回"
