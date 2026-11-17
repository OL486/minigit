// Package repo 负责仓库发现与 .git 布局。
//
// 布局与真 git 一致：objects/、refs/heads/、HEAD、config、description、
// hooks/ 空目录。M1 不做索引、不做引用解析（那是 M2/M3 的事）。
package repo

import (
	"fmt"
	"os"
	"path/filepath"
)

// FindRepo 从 start 目录向上找 .git，返回 .git 目录与工作区根目录。
func FindRepo(start string) (gitDir, workDir string, err error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", "", fmt.Errorf("解析路径失败：%w", err)
	}
	for {
		cand := filepath.Join(dir, ".git")
		if st, err := os.Stat(cand); err == nil && st.IsDir() {
			return cand, dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "", fmt.Errorf("不在 minigit 仓库内（从 %s 向上找不到 .git）", start)
		}
		dir = parent
	}
}

// Init 在 path（工作区目录，不存在则创建）里建仓库，返回 .git 目录。
// 已存在 .git 时幂等：只补缺失的目录和文件，不覆盖已有的 config。
// 返回 usedPlaceholder 表示 config 里写的是占位 user.name/user.email，
// 调用方应提示用户去改。
func Init(path string) (gitDir string, usedPlaceholder bool, err error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false, fmt.Errorf("解析路径失败：%w", err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", false, fmt.Errorf("创建目录失败：%w", err)
	}
	gitDir = filepath.Join(abs, ".git")
	for _, d := range []string{
		"objects", "objects/info", "objects/pack",
		"refs", "refs/heads", "refs/tags",
		"branches", "hooks", "info",
	} {
		if err := os.MkdirAll(filepath.Join(gitDir, d), 0o755); err != nil {
			return "", false, fmt.Errorf("创建 .git 布局失败：%w", err)
		}
	}
	if err := writeIfMissing(filepath.Join(gitDir, "HEAD"), "ref: refs/heads/main\n"); err != nil {
		return "", false, err
	}
	name, email, placeholder := defaultIdentity()
	config := "[core]\n" +
		"\trepositoryformatversion = 0\n" +
		"\tfilemode = true\n" +
		"\tbare = false\n" +
		"\tlogallrefupdates = true\n" +
		"[user]\n" +
		"\tname = " + name + "\n" +
		"\temail = " + email + "\n"
	if err := writeIfMissing(filepath.Join(gitDir, "config"), config); err != nil {
		return "", false, err
	}
	// config 已存在时不覆盖，usedPlaceholder 只反映“本次是否写了占位值”。
	if err := writeIfMissing(filepath.Join(gitDir, "description"), "Unnamed repository; edit this file 'description' to name the repository.\n"); err != nil {
		return "", false, err
	}
	if err := writeIfMissing(filepath.Join(gitDir, "info", "exclude"), defaultExclude()); err != nil {
		return "", false, err
	}
	return gitDir, placeholder, nil
}

func writeIfMissing(path, content string) error {
	if _, err := os.Stat(path); err == nil {
		return nil // 已存在，不覆盖
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("写入 %s 失败：%w", path, err)
	}
	return nil
}

// defaultIdentity 先看 GIT_AUTHOR_NAME / GIT_AUTHOR_EMAIL 环境变量，
// 没有就用占位值（调用方负责提示用户改）。
func defaultIdentity() (name, email string, placeholder bool) {
	name = os.Getenv("GIT_AUTHOR_NAME")
	email = os.Getenv("GIT_AUTHOR_EMAIL")
	if name == "" || email == "" {
		return "Your Name", "you@example.com", true
	}
	return name, email, false
}

func defaultExclude() string {
	return "# git ls-files --others --exclude-from=.git/info/exclude\n" +
		"# Lines that start with '#' are comments.\n" +
		"# For a project-specific .gitignore file, see <https://git-scm.com/docs/gitignore>.\n" +
		".DS_Store\n"
}
