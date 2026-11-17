package porcelain

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/OL486/minigit/internal/index"
	"github.com/OL486/minigit/internal/merge"
	"github.com/OL486/minigit/internal/object"
	"github.com/OL486/minigit/internal/refs"
)

// Status 是三方比较的结果：
//   - Staged*：索引 vs HEAD 的 tree（已暂存待提交）
//   - Modified / Deleted：工作区 vs 索引（改了但没暂存）
//   - Untracked：工作区有、索引里没有
type Status struct {
	Branch         string
	NoCommits      bool   // 还没有任何提交
	Merging        bool   // 正在合并（.git/MERGE_HEAD 存在）
	MergeBranch    string // 正在合并的分支名（MERGE_MSG 里解析，解析不到为空）
	StagedAdded    []string
	StagedModified []string
	StagedDeleted  []string
	Modified       []string // 工作区改了，索引没变
	Deleted        []string // 工作区删了，索引还在
	Untracked      []string
}

// IsClean 报告工作区是否干净（没有任何改动）。
func (s *Status) IsClean() bool {
	return len(s.StagedAdded) == 0 && len(s.StagedModified) == 0 &&
		len(s.StagedDeleted) == 0 && len(s.Modified) == 0 &&
		len(s.Deleted) == 0 && len(s.Untracked) == 0
}

// ComputeStatus 做三方比较。工作区文件先拿索引里的 stat（mtime/size）
// 快速判断，未变直接跳过；stat 变了再算内容 SHA-1 确认（和真 git
// 同样的两层策略）。
func ComputeStatus(gitDir, workDir string) (*Status, error) {
	branch, err := refs.CurrentBranch(gitDir)
	if err != nil {
		return nil, err
	}
	st := &Status{Branch: branch}
	// 正在合并（MERGE_HEAD 存在）：从 MERGE_MSG 的首行
	// "Merge branch '<name>'" 里解析分支名。
	if _, err := os.Stat(filepath.Join(gitDir, "MERGE_HEAD")); err == nil {
		st.Merging = true
		if msg, err := merge.MergeMessage(gitDir); err == nil {
			if rest, ok := strings.CutPrefix(msg, "Merge branch '"); ok {
				if i := strings.Index(rest, "'"); i >= 0 {
					st.MergeBranch = rest[:i]
				}
			}
		}
	}
	// HEAD 的 tree 拍平。
	head := map[string]object.Entry{}
	headSHA, err := refs.ResolveRef(gitDir, "HEAD")
	switch {
	case err == nil:
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
	case refs.IsNotFound(err):
		st.NoCommits = true
	default:
		return nil, err
	}
	// 索引。
	idx, err := index.Read(gitDir)
	if err != nil {
		return nil, err
	}
	indexed := map[string]index.Entry{}
	for _, e := range idx.Entries {
		indexed[e.Path] = e
	}
	// 工作区：扫一遍文件（跳过 .git）。
	work := map[string]os.FileInfo{}
	err = filepath.WalkDir(workDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(workDir, p)
		if err != nil {
			return err
		}
		if d.IsDir() {
			if rel == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		st, err := os.Stat(p)
		if err != nil {
			return err
		}
		work[filepath.ToSlash(rel)] = st
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("扫描工作区失败：%w", err)
	}
	// 暂存区 vs HEAD。
	for path, e := range indexed {
		h, ok := head[path]
		if !ok {
			st.StagedAdded = append(st.StagedAdded, path)
		} else if e.SHA != h.SHA {
			st.StagedModified = append(st.StagedModified, path)
		}
	}
	for path := range head {
		if _, ok := indexed[path]; !ok {
			st.StagedDeleted = append(st.StagedDeleted, path)
		}
	}
	// 工作区 vs 索引。
	for path, e := range indexed {
		fi, ok := work[path]
		if !ok {
			st.Deleted = append(st.Deleted, path)
			continue
		}
		// stat 快速判断：size 或 mtime 变了才算内容。
		if fi.Size() == e.Size && fi.ModTime().Unix() == e.MtimeSec &&
			int64(fi.ModTime().Nanosecond()) == e.MtimeNsec {
			continue
		}
		data, err := os.ReadFile(filepath.Join(workDir, filepath.FromSlash(path)))
		if err != nil {
			return nil, fmt.Errorf("读取工作区文件失败：%w", err)
		}
		if object.Hash(object.Blob, data) != e.SHA {
			st.Modified = append(st.Modified, path)
		}
	}
	// 未追踪：工作区有、索引里没有。
	for path := range work {
		if _, ok := indexed[path]; !ok {
			st.Untracked = append(st.Untracked, path)
		}
	}
	sort.Strings(st.StagedAdded)
	sort.Strings(st.StagedModified)
	sort.Strings(st.StagedDeleted)
	sort.Strings(st.Modified)
	sort.Strings(st.Deleted)
	sort.Strings(st.Untracked)
	return st, nil
}

// PrintStatus 按固定格式打印状态（格式见 README）：
//
//	On branch main
//	[No commits yet]
//	Changes to be committed:
//	  new file:   a.txt
//	  modified:   b.txt
//	  deleted:    c.txt
//
//	Changes not staged for commit:
//	  modified:   d.txt
//	  deleted:    e.txt
//
//	Untracked files:
//	  f.txt
//
// 全部干净时只打印 "On branch main" + "nothing to commit, working tree clean"。
func PrintStatus(out io.Writer, s *Status) {
	var b strings.Builder
	fmt.Fprintf(&b, "On branch %s\n", s.Branch)
	if s.NoCommits {
		b.WriteString("No commits yet\n")
	}
	if s.Merging {
		if s.MergeBranch != "" {
			fmt.Fprintf(&b, "正在合并 %s（有冲突未解决）\n", s.MergeBranch)
		} else {
			b.WriteString("正在合并（有冲突未解决）\n")
		}
	}
	if len(s.StagedAdded) > 0 || len(s.StagedModified) > 0 || len(s.StagedDeleted) > 0 {
		b.WriteString("\nChanges to be committed:\n")
		for _, p := range s.StagedAdded {
			fmt.Fprintf(&b, "  new file:   %s\n", p)
		}
		for _, p := range s.StagedModified {
			fmt.Fprintf(&b, "  modified:   %s\n", p)
		}
		for _, p := range s.StagedDeleted {
			fmt.Fprintf(&b, "  deleted:    %s\n", p)
		}
	}
	if len(s.Modified) > 0 || len(s.Deleted) > 0 {
		b.WriteString("\nChanges not staged for commit:\n")
		for _, p := range s.Modified {
			fmt.Fprintf(&b, "  modified:   %s\n", p)
		}
		for _, p := range s.Deleted {
			fmt.Fprintf(&b, "  deleted:    %s\n", p)
		}
	}
	if len(s.Untracked) > 0 {
		b.WriteString("\nUntracked files:\n")
		for _, p := range s.Untracked {
			fmt.Fprintf(&b, "  %s\n", p)
		}
	}
	if s.IsClean() {
		b.WriteString("nothing to commit, working tree clean\n")
	}
	fmt.Fprint(out, b.String())
}
