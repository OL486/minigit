package porcelain

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/OL486/minigit/internal/diff"
	"github.com/OL486/minigit/internal/index"
	"github.com/OL486/minigit/internal/object"
	"github.com/OL486/minigit/internal/refs"
)

// Diff 输出 unified diff。
// cached=false：工作区相对索引（未暂存的修改）；
// cached=true：索引相对 HEAD（已暂存的修改）。
// 未追踪的文件不显示（和 git 一致）。`diff <commit>` 本轮不做。
func Diff(gitDir, workDir string, cached bool, out io.Writer) error {
	var files []fileDiff
	var err error
	if cached {
		files, err = cachedDiffs(gitDir)
	} else {
		files, err = workdirDiffs(gitDir, workDir)
	}
	if err != nil {
		return err
	}
	for _, f := range files {
		if err := printFileDiff(out, f); err != nil {
			return err
		}
	}
	return nil
}

// fileDiff 是一对要比较的文件：一边可能不存在（新增/删除）。
type fileDiff struct {
	path    string
	oldMode string // "" 表示旧边没有这个文件
	newMode string // "" 表示新边没有这个文件
	oldSHA  string // "" 表示旧边没有
	newSHA  string // "" 表示新边没有
	oldData []byte
	newData []byte
}

// workdirDiffs 算工作区相对索引的差异（未暂存）。
func workdirDiffs(gitDir, workDir string) ([]fileDiff, error) {
	idx, err := index.Read(gitDir)
	if err != nil {
		return nil, err
	}
	var out []fileDiff
	for _, e := range idx.Entries {
		abs := filepath.Join(workDir, filepath.FromSlash(e.Path))
		fi, err := os.Stat(abs)
		if err != nil {
			if os.IsNotExist(err) {
				// 工作区删了：旧边是索引里的 blob，新边没有。
				_, oldData, err := object.ReadObject(gitDir, e.SHA)
				if err != nil {
					return nil, err
				}
				out = append(out, fileDiff{path: e.Path, oldMode: e.Mode, oldSHA: e.SHA, oldData: oldData})
				continue
			}
			return nil, fmt.Errorf("读取工作区文件失败：%w", err)
		}
		if fi.IsDir() {
			return nil, fmt.Errorf("%s 在工作区里是个目录，无法比较", e.Path)
		}
		newData, err := os.ReadFile(abs)
		if err != nil {
			return nil, fmt.Errorf("读取工作区文件失败：%w", err)
		}
		newSHA := object.Hash(object.Blob, newData)
		newMode := "100644"
		if fi.Mode().Perm()&0o111 != 0 {
			newMode = "100755"
		}
		if newSHA == e.SHA && newMode == e.Mode {
			continue // 内容和 mode 都没变
		}
		_, oldData, err := object.ReadObject(gitDir, e.SHA)
		if err != nil {
			return nil, err
		}
		out = append(out, fileDiff{
			path: e.Path, oldMode: e.Mode, newMode: newMode,
			oldSHA: e.SHA, newSHA: newSHA, oldData: oldData, newData: newData,
		})
	}
	return out, nil
}

// cachedDiffs 算索引相对 HEAD 的差异（已暂存）。
func cachedDiffs(gitDir string) ([]fileDiff, error) {
	head := map[string]object.Entry{}
	if headSHA, err := refs.ResolveRef(gitDir, "HEAD"); err == nil {
		headCommit, err := readCommit(gitDir, headSHA)
		if err != nil {
			return nil, err
		}
		entries, err := object.FlattenTree(gitDir, headCommit.Tree)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			head[e.Name] = e
		}
	} else if !refs.IsNotFound(err) {
		return nil, err
	}
	idx, err := index.Read(gitDir)
	if err != nil {
		return nil, err
	}
	indexed := map[string]index.Entry{}
	for _, e := range idx.Entries {
		indexed[e.Path] = e
	}
	paths := map[string]bool{}
	for p := range head {
		paths[p] = true
	}
	for p := range indexed {
		paths[p] = true
	}
	sorted := make([]string, 0, len(paths))
	for p := range paths {
		sorted = append(sorted, p)
	}
	sort.Strings(sorted)
	var out []fileDiff
	for _, p := range sorted {
		h, hOK := head[p]
		e, eOK := indexed[p]
		switch {
		case eOK && !hOK:
			// 新增：旧边没有。
			_, newData, err := object.ReadObject(gitDir, e.SHA)
			if err != nil {
				return nil, err
			}
			out = append(out, fileDiff{path: p, newMode: e.Mode, newSHA: e.SHA, newData: newData})
		case hOK && !eOK:
			// 删除：新边没有。
			_, oldData, err := object.ReadObject(gitDir, h.SHA)
			if err != nil {
				return nil, err
			}
			out = append(out, fileDiff{path: p, oldMode: h.Mode, oldSHA: h.SHA, oldData: oldData})
		default:
			if h.SHA == e.SHA && h.Mode == e.Mode {
				continue
			}
			_, oldData, err := object.ReadObject(gitDir, h.SHA)
			if err != nil {
				return nil, err
			}
			_, newData, err := object.ReadObject(gitDir, e.SHA)
			if err != nil {
				return nil, err
			}
			out = append(out, fileDiff{
				path: p, oldMode: h.Mode, newMode: e.Mode,
				oldSHA: h.SHA, newSHA: e.SHA, oldData: oldData, newData: newData,
			})
		}
	}
	return out, nil
}

// shortSHA 取 SHA 的前 7 位（和 git 默认缩写长度一致）；
// 空 SHA（新增/删除的那一边）显示为 0000000。
func shortSHA(sha string) string {
	if sha == "" {
		return "0000000"
	}
	return sha[:7]
}

// isBinary 判断内容是不是二进制（git 的规则：前 8000 字节里有 NUL）。
func isBinary(data []byte) bool {
	if len(data) > 8000 {
		data = data[:8000]
	}
	return bytes.IndexByte(data, 0) >= 0
}

// printFileDiff 输出一对文件的 diff，格式对齐 git diff 的默认输出。
func printFileDiff(out io.Writer, f fileDiff) error {
	var b bytes.Buffer
	fmt.Fprintf(&b, "diff --git a/%s b/%s\n", f.path, f.path)
	modeChanged := f.oldMode != "" && f.newMode != "" && f.oldMode != f.newMode
	switch {
	case f.oldMode == "":
		fmt.Fprintf(&b, "new file mode %s\n", f.newMode)
	case f.newMode == "":
		fmt.Fprintf(&b, "deleted file mode %s\n", f.oldMode)
	case modeChanged:
		fmt.Fprintf(&b, "old mode %s\nnew mode %s\n", f.oldMode, f.newMode)
	}
	contentSame := f.oldMode != "" && f.newMode != "" && bytes.Equal(f.oldData, f.newData)
	if !contentSame {
		// 内容变了才有 index 行；mode 没变时带 mode，变了（或新/删文件）不带。
		if f.oldMode == "" || f.newMode == "" || modeChanged {
			fmt.Fprintf(&b, "index %s..%s\n", shortSHA(f.oldSHA), shortSHA(f.newSHA))
		} else {
			fmt.Fprintf(&b, "index %s..%s %s\n", shortSHA(f.oldSHA), shortSHA(f.newSHA), f.newMode)
		}
		if isBinary(f.oldData) || isBinary(f.newData) {
			fmt.Fprintf(&b, "Binary files a/%s and b/%s differ\n", f.path, f.path)
			_, err := b.WriteTo(out)
			return err
		}
		edits, oldLines, newLines, oldNL, newNL := diff.DiffBytes(f.oldData, f.newData)
		oldLabel, newLabel := "a/"+f.path, "b/"+f.path
		if f.oldMode == "" {
			oldLabel = "/dev/null"
		}
		if f.newMode == "" {
			newLabel = "/dev/null"
		}
		b.WriteString(diff.FormatUnified(oldLabel, newLabel, oldLines, newLines, oldNL, newNL, edits))
	}
	// 内容没变、只有 mode 变了：上面只打印了 old/new mode 两行，
	// 和 git 一致（没有 index 行、没有 hunk）。
	_, err := b.WriteTo(out)
	return err
}
