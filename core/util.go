package core

import "os"

// dirExists 报告路径是否存在且为目录。
func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}
