// Package refs 实现 git 的引用层：分支就是文件。
//
// .git/refs/heads/<name> 里存 40 位十六进制 commit SHA；HEAD 是文本文件，
// 内容 "ref: refs/heads/main"。packed-refs 只读（M2 不写）。
package refs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ErrRefNotFound 表示引用不存在（常见于还没有任何提交的分支）。
var ErrRefNotFound = errors.New("引用不存在")

// IsNotFound 判断 err 是否为引用不存在。
func IsNotFound(err error) bool {
	return errors.Is(err, ErrRefNotFound)
}

// refPath 返回引用对应的文件路径。name 可以是 "refs/heads/main"
// 这样的完整引用名。
func refPath(gitDir, name string) string {
	return filepath.Join(gitDir, name)
}

// ReadHead 解析 HEAD 文件，返回它指向的引用名。
// detached HEAD（HEAD 里直接是 SHA）时返回错误——M2 不做 detached HEAD。
func HeadRef(gitDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return "", fmt.Errorf("读取 HEAD 失败：%w", err)
	}
	line := strings.TrimSpace(string(data))
	if !strings.HasPrefix(line, "ref: ") {
		return "", fmt.Errorf("detached HEAD（%q）：M2 不支持", line)
	}
	return strings.TrimSpace(line[len("ref: "):]), nil
}

// ResolveRef 解析引用名，返回它指向的 commit SHA。
// name 可以是 "HEAD"、完整引用名（"refs/heads/main"）、
// 或分支短名（"main"）。找不到引用时返回 ErrRefNotFound。
func ResolveRef(gitDir, name string) (string, error) {
	if name == "HEAD" {
		ref, err := HeadRef(gitDir)
		if err != nil {
			return "", err
		}
		return ResolveRef(gitDir, ref)
	}
	if !strings.HasPrefix(name, "refs/") {
		name = "refs/heads/" + name
	}
	// 先看松散引用文件。
	if sha, err := readRefFile(refPath(gitDir, name)); err == nil {
		return sha, nil
	} else if !errors.Is(err, ErrRefNotFound) {
		return "", err
	}
	// 再看 packed-refs（只读）。
	packed, err := ReadPackedRefs(gitDir)
	if err != nil {
		return "", err
	}
	if sha, ok := packed[name]; ok {
		return sha, nil
	}
	return "", fmt.Errorf("%w：%s（还没有提交？）", ErrRefNotFound, name)
}

func readRefFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrRefNotFound
		}
		return "", fmt.Errorf("读取引用 %s 失败：%w", path, err)
	}
	sha := strings.TrimSpace(string(data))
	if len(sha) != 40 || !isHex(sha) {
		return "", fmt.Errorf("引用 %s 内容损坏：%q", path, sha)
	}
	return sha, nil
}

// UpdateRef 把引用指向新的 commit SHA。name 为 "HEAD" 时先解析
// 它指向的分支再写。写盘用临时文件 + 改名 + fsync。
func UpdateRef(gitDir, name, sha string) error {
	if len(sha) != 40 || !isHex(sha) {
		return fmt.Errorf("无效的 commit SHA：%q", sha)
	}
	if name == "HEAD" {
		ref, err := HeadRef(gitDir)
		if err != nil {
			return err
		}
		name = ref
	}
	if !strings.HasPrefix(name, "refs/") {
		name = "refs/heads/" + name
	}
	path := refPath(gitDir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建引用目录失败：%w", err)
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("写入引用失败：%w", err)
	}
	if _, err := f.WriteString(sha + "\n"); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("写入引用失败：%w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("同步引用失败：%w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("写入引用失败：%w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("写入引用失败：%w", err)
	}
	return nil
}

// SetHead 把 HEAD 指向给定的引用（完整名，如 "refs/heads/feature"）。
// 写盘用临时文件 + 改名 + fsync，和 UpdateRef 同样的策略。
func SetHead(gitDir, ref string) error {
	if !strings.HasPrefix(ref, "refs/") {
		return fmt.Errorf("HEAD 只能指向引用， got %q", ref)
	}
	path := filepath.Join(gitDir, "HEAD")
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("写入 HEAD 失败：%w", err)
	}
	if _, err := f.WriteString("ref: " + ref + "\n"); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("写入 HEAD 失败：%w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("同步 HEAD 失败：%w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("写入 HEAD 失败：%w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("写入 HEAD 失败：%w", err)
	}
	return nil
}

// ListBranches 列出所有本地分支名（按字母序）。
func ListBranches(gitDir string) ([]string, error) {
	dir := filepath.Join(gitDir, "refs", "heads")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("列出分支失败：%w", err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		out = append(out, e.Name())
	}
	packed, err := ReadPackedRefs(gitDir)
	if err != nil {
		return nil, err
	}
	for ref := range packed {
		if strings.HasPrefix(ref, "refs/heads/") {
			name := strings.TrimPrefix(ref, "refs/heads/")
			found := false
			for _, b := range out {
				if b == name {
					found = true
					break
				}
			}
			if !found {
				out = append(out, name)
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

// CurrentBranch 返回 HEAD 指向的分支短名（"main"）。
func CurrentBranch(gitDir string) (string, error) {
	ref, err := HeadRef(gitDir)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(ref, "refs/heads/") {
		return "", fmt.Errorf("HEAD 指向的不是分支：%s", ref)
	}
	return strings.TrimPrefix(ref, "refs/heads/"), nil
}

// ReadPackedRefs 解析 .git/packed-refs，返回引用名到 SHA 的映射。
// 文件不存在时返回空映射，不报错。
func ReadPackedRefs(gitDir string) (map[string]string, error) {
	out := map[string]string{}
	data, err := os.ReadFile(filepath.Join(gitDir, "packed-refs"))
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, fmt.Errorf("读取 packed-refs 失败：%w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "^") {
			continue
		}
		sha, ref, ok := strings.Cut(line, " ")
		if !ok || len(sha) != 40 {
			continue
		}
		out[strings.TrimSpace(ref)] = sha
	}
	return out, nil
}

func isHex(s string) bool {
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return len(s) > 0
}
