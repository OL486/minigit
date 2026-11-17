// Package porcelain 是日常命令（add / commit / status / log …），
// 对应 git 的 porcelain 层。每个子命令一个文件。
package porcelain

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/OL486/minigit/internal/index"
	"github.com/OL486/minigit/internal/object"
)

// Add 把文件加入暂存区：文件内容写成 blob，索引条目更新 stat 信息。
// paths 是相对当前工作目录的路径（也可以是绝对路径），支持一次多个、
// 支持目录（递归加，跳过 .git）。不存在的文件、仓库外的路径、
// 符号链接都报错，不 panic。索引只在全部成功后写盘一次。
func Add(gitDir, workDir string, paths []string) error {
	if len(paths) == 0 {
		return fmt.Errorf("add 需要至少一个文件参数")
	}
	idx, err := index.Read(gitDir)
	if err != nil {
		return err
	}
	for _, p := range paths {
		if err := addOne(gitDir, workDir, idx, p); err != nil {
			return err
		}
	}
	return idx.Write(gitDir)
}

func addOne(gitDir, workDir string, idx *index.Index, path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("解析路径失败：%w", err)
	}
	rel, err := filepath.Rel(workDir, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("文件在仓库外：%s", path)
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return fmt.Errorf("add 失败：%s 不存在或无法读取：%w", path, err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("add 失败：不支持符号链接：%s", path)
	}
	if info.IsDir() {
		return filepath.WalkDir(abs, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(workDir, p)
			if err != nil {
				return err
			}
			if d.IsDir() {
				// 跳过 .git 整棵树。
				if rel == ".git" {
					return filepath.SkipDir
				}
				return nil
			}
			if d.Type()&fs.ModeSymlink != 0 {
				return fmt.Errorf("add 失败：不支持符号链接：%s", p)
			}
			return stageFile(gitDir, workDir, idx, p)
		})
	}
	return stageFile(gitDir, workDir, idx, abs)
}

// stageFile 把单个文件写成 blob 并更新索引条目。
func stageFile(gitDir, workDir string, idx *index.Index, abs string) error {
	rel, err := filepath.Rel(workDir, abs)
	if err != nil {
		return fmt.Errorf("解析路径失败：%w", err)
	}
	rel = filepath.ToSlash(rel)
	data, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Errorf("读取文件失败：%w", err)
	}
	sha, err := object.WriteObject(gitDir, object.Blob, data)
	if err != nil {
		return err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("读取文件状态失败：%w", err)
	}
	mode := "100644"
	if st.Mode().Perm()&0o111 != 0 {
		mode = "100755"
	}
	idx.Upsert(index.Entry{
		Path:      rel,
		SHA:       sha,
		Mode:      mode,
		Size:      st.Size(),
		MtimeSec:  st.ModTime().Unix(),
		MtimeNsec: int64(st.ModTime().Nanosecond()),
	})
	return nil
}
