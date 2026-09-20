package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"cli"
)

// selectDir 目录选择页：
// 首次进入播种 → 有根直接进项目列表 → 无根选根 → 跳过退化手动模式。
// 返回选中的工作目录；取消返回空串。
func (s *session) selectDir(agent *cli.Agent, td *cli.AgentState, save func() error) (string, error) {
	cfg := s.cfg

	// save 可能为空（如测试注入）：换成 no-op，保证内部调用安全。
	if save == nil {
		save = func() error { return nil }
	}

	// 播种（保留历史手动目录）
	if !td.Seeded {
		var seed []string
		for _, d := range agent.Directories {
			if !contains(seed, d) && dirExists(d) {
				seed = append(seed, d)
			}
		}
		for _, d := range cfg.DefaultDirectories {
			if !contains(seed, d) && dirExists(d) {
				seed = append(seed, d)
			}
		}
		td.Directories = seed
		td.Seeded = true
		if err := save(); err != nil {
			return "", err
		}
	}
	td.Directories = filterDirs(td.Directories)

	// 扫描根目录下所有子目录
	scanProjects := func(root string) []string {
		ents, err := os.ReadDir(root)
		if err != nil {
			return nil
		}
		out := make([]string, 0, len(ents))
		for _, e := range ents {
			if !e.IsDir() {
				continue
			}
			p := filepath.Join(root, e.Name())
			if dirExists(p) {
				out = append(out, p)
			}
		}
		return out
	}

	// 目录不存在提示（共享渲染器闪屏 1.2s，不整屏 Clear）。
	notFound := func(p string) {
		r := s.renderer()
		w, h := r.width, r.height
		if nw, nh, ok := getConsoleSize(); ok {
			w, h = nw, nh
		}
		r.resize(w, h)
		f := newFrame(h)
		f.add(fitClip(red+"目录不存在: "+truncateCells(p, w-10)+reset, w))
		r.draw(f)
		fmt.Fprint(r.s.w, hideCursor)
		time.Sleep(1200 * time.Millisecond)
	}

	// promptDir 统一目录输入：取消（errInputCancel）与校验失败都视为“本次不填”，
	// 只有读键流错误才向上传；空提交也视为不填。
	promptDir := func(question string) (string, error) {
		p, err := s.prompt(question, "ESC 返回（不填）")
		if err != nil {
			if errors.Is(err, errInputCancel) {
				return "", nil
			}
			return "", err
		}
		if p == "" {
			return "", nil
		}
		if !dirExists(p) {
			notFound(p)
			return "", nil
		}
		return p, nil
	}

	// ---- 根目录选择 ----
	// 返回 (skip, root)；skip=true 表示用户跳过（进入手动模式）。
	chooseRoot := func() (bool, string, error) {
		var candidates []string
		if agent.RootDir != "" && dirExists(agent.RootDir) && !contains(candidates, agent.RootDir) {
			candidates = append(candidates, agent.RootDir)
		}
		for _, d := range td.Directories {
			parent := filepath.Dir(d)
			if parent != d && dirExists(parent) && !contains(candidates, parent) {
				candidates = append(candidates, parent)
			}
		}
		for _, d := range cfg.DefaultDirectories {
			if !contains(candidates, d) && dirExists(d) {
				candidates = append(candidates, d)
			}
		}

		items := make([]string, len(candidates))
		for i, c := range candidates {
			n := len(scanProjects(c))
			items[i] = fmt.Sprintf("%s  %s(含 %d 个项目)%s", c, dark, n, reset)
		}

		res, err := s.listPage(listOpts{
			title:        fmt.Sprintf("%s - 选择 Agent 根目录", agent.Display),
			mainTitle:    "候选根目录 (选一个作为该项目根)",
			removedTitle: "",
			mainItems:    &items,
			removedItems: &[]string{},
			hint:         listHint("M 手动输入", "Q 跳过进手动模式"),
			onManual: func() (string, error) {
				return promptDir("输入根目录完整路径:")
			},
		})
		if err != nil {
			return false, "", err
		}
		if res.action == "cancel" {
			return true, "", nil // 跳过
		}
		// 从 items 反查真实路径（items 带 ANSI 样式）。
		// 注意：listPage 的 M 手动输入会把原始路径追加进 items（与 candidates
		// 不再一一对应），必须判界，否则选中追加项时 candidates[idx] 越界 panic。
		if idx := indexOf(items, res.value); idx >= 0 && idx < len(candidates) {
			return false, candidates[idx], nil
		}
		// 手动输入情况：清掉 ANSI 后校验
		clean := stripANSI(res.value)
		if clean != "" && dirExists(clean) {
			return false, clean, nil
		}
		return true, "", nil
	}

	// ---- 项目列表 ----
	projectList := func(root string) (string, error) {
		for {
			mainItems := scanProjects(root)
			removedItems := td.RemovedDirs

			res, err := s.listPage(listOpts{
				title:        fmt.Sprintf("%s - 选择项目 (根: %s)", agent.Display, root),
				mainTitle:    "项目目录 (根下所有子目录)",
				removedTitle: "已移除 (removed directories)",
				mainItems:    &mainItems,
				removedItems: &removedItems,
				hint:         dualHint("N 新建", "R 切根"),
				onChange:     func() error { return save() },
				onManual: func() (string, error) {
					return promptDir("输入完整工作目录路径:")
				},
				extraKeys: map[rune]func() (extraResult, error){
					'n': func() (extraResult, error) {
						name, err := s.prompt("新项目名 (将在根目录下创建):", "ESC 返回（不建）")
						if err != nil {
							if errors.Is(err, errInputCancel) {
								return extraResult{}, nil
							}
							return extraResult{}, err
						}
						if name == "" {
							return extraResult{}, nil
						}
						target := filepath.Join(root, name)
						if !dirExists(target) {
							_ = os.MkdirAll(target, 0o755)
						}
						if dirExists(target) {
							return extraResult{add: target}, nil
						}
						return extraResult{}, nil
					},
					'r': func() (extraResult, error) {
						skip, nr, err := chooseRoot()
						if err != nil {
							return extraResult{}, err
						}
						if !skip && nr != "" {
							td.RootDir = nr
							if err := save(); err != nil {
								return extraResult{}, err
							}
						}
						return extraResult{exit: true}, nil
					},
				},
			})
			if err != nil {
				return "", err
			}

			// 手动目录同步回状态；过滤掉已移除项
			td.RemovedDirs = removedItems
			td.Directories = filterNotIn(td.Directories, removedItems)
			if err := save(); err != nil {
				return "", err
			}

			switch res.action {
			case "select":
				return res.value, nil
			case "cancel":
				return "", nil
			case "exit":
				// 切根后：若新根仍有效则继续循环
				if td.RootDir != "" && dirExists(td.RootDir) {
					root = td.RootDir
					continue
				}
				return "", nil
			}
		}
	}

	// ---- 主流程 ----
	// 已有根：直接进项目列表；无根：选根；跳过：手动模式
	if td.RootDir != "" && dirExists(td.RootDir) {
		return projectList(td.RootDir)
	}

	skip, root, err := chooseRoot()
	if err != nil {
		return "", err
	}
	if !skip && root != "" {
		td.RootDir = root
		if err := save(); err != nil {
			return "", err
		}
		return projectList(root)
	}

	// 跳过根：退化为纯手动目录列表
	mainItems := td.Directories
	removedItems := td.RemovedDirs
	res, err := s.listPage(listOpts{
		title:        fmt.Sprintf("%s - 选择工作目录 (手动模式)", agent.Display),
		mainTitle:    "工作目录",
		removedTitle: "已移除 (removed directories)",
		mainItems:    &mainItems,
		removedItems: &removedItems,
		hint:         dualHint(),
		onChange:     func() error { return save() },
		onManual: func() (string, error) {
			return promptDir("输入工作目录完整路径 (将加入该工具列表):")
		},
	})
	if err != nil {
		return "", err
	}
	td.Directories = mainItems
	td.RemovedDirs = removedItems
	if err := save(); err != nil {
		return "", err
	}
	if res.action == "select" {
		return res.value, nil
	}
	return "", nil
}
