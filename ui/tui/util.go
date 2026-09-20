package tui

import (
	"os"
	"strings"
)

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func indexOf(list []string, s string) int {
	for i, x := range list {
		if x == s {
			return i
		}
	}
	return -1
}

func sliceCopy(s []string) []string {
	return append([]string(nil), s...)
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// filterDirs 只保留仍然存在的目录。
func filterDirs(list []string) []string {
	out := list[:0]
	for _, d := range list {
		if dirExists(d) {
			out = append(out, d)
		}
	}
	return out
}

// filterNotIn 返回 list 中不在 exclude 里的元素。
func filterNotIn(list, exclude []string) []string {
	out := list[:0]
	for _, x := range list {
		if !contains(exclude, x) {
			out = append(out, x)
		}
	}
	return out
}

// stripANSI 去掉 ANSI 转义码（用于从带样式的选中项反查真实路径）。
func stripANSI(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
