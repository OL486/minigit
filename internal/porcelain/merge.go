package porcelain

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/OL486/minigit/internal/index"
	"github.com/OL486/minigit/internal/merge"
	"github.com/OL486/minigit/internal/object"
	"github.com/OL486/minigit/internal/plumbing"
	"github.com/OL486/minigit/internal/refs"
	"github.com/OL486/minigit/internal/repo"
)

// Merge 把 branch 合并进当前分支（fast-forward 或三路合并）。
// message 为空时用默认信息 "Merge branch '<name>'"。
// 有冲突时返回错误，工作区留冲突标记文件，.git/MERGE_HEAD 与
// .git/MERGE_MSG 已写好，等用户手动解决后 add + commit。
func Merge(gitDir, workDir, branch, message string, out io.Writer) error {
	targetSHA, err := refs.ResolveRef(gitDir, branch)
	if err != nil {
		if refs.IsNotFound(err) {
			return fmt.Errorf("分支不存在：%s", branch)
		}
		return err
	}
	headSHA, err := refs.ResolveRef(gitDir, "HEAD")
	if err != nil {
		if refs.IsNotFound(err) {
			return fmt.Errorf("还没有提交，无法合并")
		}
		return err
	}
	curBranch, err := refs.CurrentBranch(gitDir)
	if err != nil {
		return err
	}
	// 目标已是 HEAD（或 HEAD 的祖先）：已是最新。
	if targetSHA == headSHA {
		fmt.Fprintln(out, "Already up to date.")
		return nil
	}
	isAnc, err := merge.IsAncestor(gitDir, targetSHA, headSHA)
	if err != nil {
		return err
	}
	if isAnc {
		fmt.Fprintln(out, "Already up to date.")
		return nil
	}
	// 脏工作区保护：有已暂存或未暂存的修改时拒绝合并
	//（和 checkout 同样的保护；未追踪文件不在此列，写文件前
	// 再单独检查会不会被覆盖）。
	st, err := ComputeStatus(gitDir, workDir)
	if err != nil {
		return err
	}
	if len(st.StagedAdded) > 0 || len(st.StagedModified) > 0 || len(st.StagedDeleted) > 0 ||
		len(st.Modified) > 0 || len(st.Deleted) > 0 {
		var files []string
		files = append(files, st.StagedAdded...)
		files = append(files, st.StagedModified...)
		files = append(files, st.StagedDeleted...)
		files = append(files, st.Modified...)
		files = append(files, st.Deleted...)
		sort.Strings(files)
		return fmt.Errorf("无法合并：工作区有未提交的修改（%s），请先提交或撤销", strings.Join(files, "、"))
	}
	// HEAD 是目标的祖先：fast-forward。
	ff, err := merge.IsAncestor(gitDir, headSHA, targetSHA)
	if err != nil {
		return err
	}
	if ff {
		return fastForward(gitDir, workDir, headSHA, targetSHA, st.Untracked, out)
	}
	return threeWayMerge(gitDir, workDir, branch, targetSHA, headSHA, curBranch, message, st.Untracked, out)
}

// fastForward 把当前分支指针直接移到目标 commit，
// 并按目标 tree 更新工作区与索引（复用 checkout 的更新逻辑）。
func fastForward(gitDir, workDir, headSHA, targetSHA string, untracked []string, out io.Writer) error {
	headCommit, err := readCommit(gitDir, headSHA)
	if err != nil {
		return err
	}
	targetCommit, err := readCommit(gitDir, targetSHA)
	if err != nil {
		return err
	}
	headEntries, err := object.FlattenTree(gitDir, headCommit.Tree)
	if err != nil {
		return err
	}
	targetEntries, err := object.FlattenTree(gitDir, targetCommit.Tree)
	if err != nil {
		return err
	}
	ours := treeMap(headEntries)
	tgt := treeMap(targetEntries)

	// 未追踪文件预检：目标 tree 里新增的路径，工作区里如果有个
	// 内容不同的同名文件，合并会覆盖它，先挡住。
	untrackedSet := map[string]bool{}
	for _, p := range untracked {
		untrackedSet[p] = true
	}
	for p, e := range tgt {
		if _, ok := ours[p]; ok {
			continue
		}
		_, data, err := object.ReadObject(gitDir, e.SHA)
		if err != nil {
			return err
		}
		if blocked, err := untrackedConflict(workDir, untrackedSet, p, data); err != nil {
			return err
		} else if blocked {
			return fmt.Errorf("未追踪文件会被合并覆盖：%s", p)
		}
	}
	// 写前预检：目标文件写不进去的情况（目录挡路、非法 mode）。
	actions := make([]checkoutAction, 0, len(tgt)+len(ours))
	for p, e := range tgt {
		actions = append(actions, checkoutAction{path: p, kind: actWrite, entry: e})
	}
	for p := range ours {
		if _, ok := tgt[p]; !ok {
			actions = append(actions, checkoutAction{path: p, kind: actRemove})
		}
	}
	if err := preflightWrites(workDir, actions); err != nil {
		return err
	}

	// 应用：先删后写，再重建索引，最后移动分支指针。
	idx, err := index.Read(gitDir)
	if err != nil {
		return err
	}
	kept := map[string]index.Entry{}
	for _, e := range idx.Entries {
		kept[e.Path] = e
	}
	for p := range ours {
		if _, ok := tgt[p]; !ok {
			abs := filepath.Join(workDir, filepath.FromSlash(p))
			if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("删除 %s 失败：%w", p, err)
			}
			pruneEmptyDirs(workDir, p)
			delete(kept, p)
		}
	}
	for p, e := range tgt {
		if err := writeWorkdirFile(gitDir, workDir, p, e); err != nil {
			return err
		}
		fi, err := os.Stat(filepath.Join(workDir, filepath.FromSlash(p)))
		if err != nil {
			return fmt.Errorf("读取合并后文件状态失败：%w", err)
		}
		kept[p] = index.EntryFromStat(p, e.SHA, e.Mode, fi)
	}
	newIdx := &index.Index{Version: 2}
	for _, e := range kept {
		newIdx.Entries = append(newIdx.Entries, e)
	}
	if err := newIdx.Write(gitDir); err != nil {
		return err
	}
	if err := refs.UpdateRef(gitDir, "HEAD", targetSHA); err != nil {
		return err
	}
	fmt.Fprintf(out, "Updating %s..%s\nFast-forward\n", headSHA[:7], targetSHA[:7])
	return nil
}

// mergeActionKind 是三路合并对一个文件算出的决策。
type mergeActionKind int

const (
	mWrite    mergeActionKind = iota // 写文件（内容取自 blob sha），索引跟进
	mDelete                          // 从工作区与索引删掉
	mChmod                           // 内容不动，只改文件 mode 与索引 mode
	mConflict                        // 写带冲突标记的内容到工作区，索引不动
)

// mergeAction 是三路合并对一个文件的决策。
type mergeAction struct {
	kind mergeActionKind
	sha  string // mWrite：要写进工作区的 blob SHA（已在对象库里）
	mode string // mWrite/mChmod/mConflict：写文件时用的 mode
	data []byte // mConflict：带冲突标记的文件内容
}

// threeWayMerge 做真正的三路合并：base 取 merge-base 的 tree，
// ours 取 HEAD 的 tree，theirs 取目标的 tree。
func threeWayMerge(gitDir, workDir, branch, targetSHA, headSHA, curBranch, message string, untracked []string, out io.Writer) error {
	baseSHA, err := merge.FindMergeBase(gitDir, headSHA, targetSHA)
	if err != nil {
		return err
	}
	baseCommit, err := readCommit(gitDir, baseSHA)
	if err != nil {
		return err
	}
	headCommit, err := readCommit(gitDir, headSHA)
	if err != nil {
		return err
	}
	targetCommit, err := readCommit(gitDir, targetSHA)
	if err != nil {
		return err
	}
	baseEntries, err := object.FlattenTree(gitDir, baseCommit.Tree)
	if err != nil {
		return err
	}
	headEntries, err := object.FlattenTree(gitDir, headCommit.Tree)
	if err != nil {
		return err
	}
	targetEntries, err := object.FlattenTree(gitDir, targetCommit.Tree)
	if err != nil {
		return err
	}
	baseMap := treeMap(baseEntries)
	oursMap := treeMap(headEntries)
	theirsMap := treeMap(targetEntries)

	// 第一阶段：逐文件算决策（不动文件）。
	actions := map[string]mergeAction{}
	var conflicts []string
	paths := make([]string, 0, len(baseMap)+len(oursMap)+len(theirsMap))
	seen := map[string]bool{}
	for p := range baseMap {
		seen[p] = true
		paths = append(paths, p)
	}
	for p := range oursMap {
		if !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}
	for p := range theirsMap {
		if !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	for _, p := range paths {
		b, bOK := baseMap[p]
		o, oOK := oursMap[p]
		t, tOK := theirsMap[p]
		oursChanged := treeChanged(b, o, bOK, oOK)
		theirsChanged := treeChanged(b, t, bOK, tOK)
		switch {
		case !oursChanged && !theirsChanged:
			// 两边都没变：不动。
		case !oursChanged && theirsChanged:
			// 只有 theirs 变了：取 theirs。
			if tOK {
				actions[p] = mergeAction{kind: mWrite, sha: t.SHA, mode: t.Mode}
			} else {
				actions[p] = mergeAction{kind: mDelete}
			}
		case oursChanged && !theirsChanged:
			// 只有 ours 变了：工作区已经是 ours 的版本，不动。
		default:
			// 两边都变了。
			if oOK && tOK && o.SHA == t.SHA {
				// 内容相同：只处理 mode（只有 theirs 改了 mode 时
				// 才需要动；都改成不同 mode 时保留 ours 的）。
				// 例外：双方都新增（base 里没有）时，工作区里还
				// 没有这个文件，必须写进去。
				if !bOK {
					actions[p] = mergeAction{kind: mWrite, sha: o.SHA, mode: mergedMode(b, o, t, bOK)}
					continue
				}
				if mode := mergedMode(b, o, t, bOK); mode != o.Mode {
					actions[p] = mergeAction{kind: mChmod, mode: mode}
				}
				continue
			}
			if !oOK && !tOK {
				// 两边都删了：删掉。
				actions[p] = mergeAction{kind: mDelete}
				continue
			}
			if !oOK || !tOK {
				// 一方删除一方修改：冲突，保留修改那边的内
				// 容、另一边留空。
				var data []byte
				var mode string
				if oOK {
					oursData, err := readBlob(gitDir, o.SHA)
					if err != nil {
						return err
					}
					data = conflictMarkers(oursData, nil)
					mode = o.Mode
				} else {
					theirsData, err := readBlob(gitDir, t.SHA)
					if err != nil {
						return err
					}
					data = conflictMarkers(nil, theirsData)
					mode = t.Mode
				}
				actions[p] = mergeAction{kind: mConflict, data: data, mode: mode}
				conflicts = append(conflicts, p)
				continue
			}
			// 两边都在：行级合并。base 里没有这个文件（双方都新增）
			// 时，base 内容为空。
			var baseData []byte
			if bOK {
				baseData, err = readBlob(gitDir, b.SHA)
				if err != nil {
					return err
				}
			}
			oursData, err := readBlob(gitDir, o.SHA)
			if err != nil {
				return err
			}
			theirsData, err := readBlob(gitDir, t.SHA)
			if err != nil {
				return err
			}
			if isBinaryFile(baseData) || isBinaryFile(oursData) || isBinaryFile(theirsData) {
				// 二进制文件不做行级合并：冲突，工作区留 ours
				// 的版本（没有标记可放，用户 add 选中的版本解决）。
				actions[p] = mergeAction{kind: mConflict, data: oursData, mode: o.Mode}
				conflicts = append(conflicts, p)
				continue
			}
			mergedBytes, mconflicts := merge.MergeBytes(baseData, oursData, theirsData)
			mode := mergedMode(b, o, t, bOK)
			if len(mconflicts) > 0 {
				actions[p] = mergeAction{kind: mConflict, data: mergedBytes, mode: mode}
				conflicts = append(conflicts, p)
				continue
			}
			sha, err := object.WriteObject(gitDir, object.Blob, mergedBytes)
			if err != nil {
				return err
			}
			actions[p] = mergeAction{kind: mWrite, sha: sha, mode: mode}
		}
	}

	// 第二阶段：写前预检（还没动任何文件）。
	// 未追踪文件：合并要写的路径上如果有个内容不同的同名文件，先挡住。
	untrackedSet := map[string]bool{}
	for _, p := range untracked {
		untrackedSet[p] = true
	}
	for p, a := range actions {
		if a.kind != mWrite && a.kind != mConflict {
			continue
		}
		var want []byte
		if a.kind == mWrite {
			data, err := readBlob(gitDir, a.sha)
			if err != nil {
				return err
			}
			want = data
		} else {
			want = a.data
		}
		if blocked, err := untrackedConflict(workDir, untrackedSet, p, want); err != nil {
			return err
		} else if blocked {
			return fmt.Errorf("未追踪文件会被合并覆盖：%s", p)
		}
	}
	// 文件写不进去的情况（目录挡路、非法 mode），复用 checkout 的预检。
	chkActions := make([]checkoutAction, 0, len(actions))
	for p, a := range actions {
		switch a.kind {
		case mWrite, mConflict:
			chkActions = append(chkActions, checkoutAction{path: p, kind: actWrite, entry: object.Entry{Mode: a.mode, Name: p}})
		case mDelete:
			chkActions = append(chkActions, checkoutAction{path: p, kind: actRemove})
		}
	}
	if err := preflightWrites(workDir, chkActions); err != nil {
		return err
	}

	// 第三阶段：应用。先删后写，索引在内存里跟进。
	idx, err := index.Read(gitDir)
	if err != nil {
		return err
	}
	kept := map[string]index.Entry{}
	for _, e := range idx.Entries {
		kept[e.Path] = e
	}
	for p, a := range actions {
		if a.kind != mDelete {
			continue
		}
		abs := filepath.Join(workDir, filepath.FromSlash(p))
		if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("删除 %s 失败：%w", p, err)
		}
		pruneEmptyDirs(workDir, p)
		delete(kept, p)
	}
	for p, a := range actions {
		switch a.kind {
		case mWrite:
			entry := object.Entry{Mode: a.mode, Name: p, SHA: a.sha}
			if err := writeWorkdirFile(gitDir, workDir, p, entry); err != nil {
				return err
			}
			fi, err := os.Stat(filepath.Join(workDir, filepath.FromSlash(p)))
			if err != nil {
				return fmt.Errorf("读取合并后文件状态失败：%w", err)
			}
			kept[p] = index.EntryFromStat(p, a.sha, a.mode, fi)
		case mConflict:
			abs := filepath.Join(workDir, filepath.FromSlash(p))
			if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
				return fmt.Errorf("写冲突文件 %s 失败：%w", p, err)
			}
			if err := os.WriteFile(abs, a.data, 0o644); err != nil {
				return fmt.Errorf("写冲突文件 %s 失败：%w", p, err)
			}
			if err := ensureMode(abs, a.mode); err != nil {
				return err
			}
			// 索引不动：冲突文件保持 HEAD 的条目（新增文件本来
			// 就没有条目），等用户解决后 add。
		case mChmod:
			abs := filepath.Join(workDir, filepath.FromSlash(p))
			if err := ensureMode(abs, a.mode); err != nil {
				return err
			}
			if e, ok := kept[p]; ok {
				e.Mode = a.mode
				kept[p] = e
			}
		}
	}
	newIdx := &index.Index{Version: 2}
	for _, e := range kept {
		newIdx.Entries = append(newIdx.Entries, e)
	}

	if len(conflicts) > 0 {
		// 有冲突：写索引与合并状态，不创建提交。
		if err := newIdx.Write(gitDir); err != nil {
			return err
		}
		if message == "" {
			message = fmt.Sprintf("Merge branch '%s'", branch)
		}
		sort.Strings(conflicts)
		if err := merge.WriteMergeState(gitDir, targetSHA, message, conflicts); err != nil {
			return err
		}
		fmt.Fprintln(out, "自动合并失败；请解决冲突后提交结果。")
		fmt.Fprintln(out, "冲突文件：")
		for _, p := range conflicts {
			fmt.Fprintf(out, "\t%s\n", p)
		}
		return fmt.Errorf("自动合并失败：%d 个文件存在冲突", len(conflicts))
	}

	// 干净合并：创建合并提交（两个 parent）。
	treeSHA, err := plumbing.WriteTree(gitDir, newIdx)
	if err != nil {
		return err
	}
	if message == "" {
		message = fmt.Sprintf("Merge branch '%s'", branch)
	}
	author, err := repo.AuthorIdentity(gitDir)
	if err != nil {
		return err
	}
	committer, err := repo.CommitterIdentity(gitDir)
	if err != nil {
		return err
	}
	now := time.Now()
	sha, err := plumbing.CommitTree(gitDir, treeSHA, []string{headSHA, targetSHA},
		plumbing.FormatIdentity(author.Name, author.Email, now),
		plumbing.FormatIdentity(committer.Name, committer.Email, now),
		message)
	if err != nil {
		return err
	}
	if err := refs.UpdateRef(gitDir, "HEAD", sha); err != nil {
		return err
	}
	if err := newIdx.Write(gitDir); err != nil {
		return err
	}
	fmt.Fprintln(out, "Merge made by minigit.")
	fmt.Fprintf(out, "[%s %s] %s\n", curBranch, sha[:7], firstLine(message))
	return nil
}

// treeChanged 判断文件在 base 与 other 之间是否变了
// （只看存在性与内容 SHA，mode 不算——mode 另外处理）。
func treeChanged(b, o object.Entry, bOK, oOK bool) bool {
	if bOK != oOK {
		return true
	}
	if !bOK {
		return false
	}
	return b.SHA != o.SHA
}

// mergedMode 决定合并后文件的 mode：
//   - 只有一边改了 mode → 取改了的那边；
//   - 两边都没改、或改成同一个 → 那一个；
//   - 两边改成不同 mode → 保留 ours 的（简化，见 docs/03-merge.md）。
//
// bOK 为 false 表示双方都新增（base 里没有这个文件）。
func mergedMode(b, o, t object.Entry, bOK bool) string {
	if !bOK {
		return o.Mode
	}
	oChanged := o.Mode != b.Mode
	tChanged := t.Mode != b.Mode
	switch {
	case oChanged && !tChanged:
		return o.Mode
	case tChanged && !oChanged:
		return t.Mode
	default:
		return o.Mode
	}
}

// conflictMarkers 把 ours/theirs 两份内容包进冲突标记。
// 一边为空表示那一边删除了这个文件。
// 内容没有结尾换行符时补一个，否则标记行会粘在内容后面
// （git 补的是 "\ No newline at end of file"，本轮简化成补换行符）。
func conflictMarkers(ours, theirs []byte) []byte {
	var b bytes.Buffer
	b.WriteString("<<<<<<< ours\n")
	b.Write(ours)
	if len(ours) > 0 && !bytes.HasSuffix(ours, []byte("\n")) {
		b.WriteString("\n")
	}
	b.WriteString("=======\n")
	b.Write(theirs)
	if len(theirs) > 0 && !bytes.HasSuffix(theirs, []byte("\n")) {
		b.WriteString("\n")
	}
	b.WriteString(">>>>>>> theirs\n")
	return b.Bytes()
}

// isBinaryFile 按 git 的规则判断内容是不是二进制：
// 前 8000 字节里出现 NUL 字节。
func isBinaryFile(data []byte) bool {
	limit := len(data)
	if limit > 8000 {
		limit = 8000
	}
	return bytes.IndexByte(data[:limit], 0) >= 0
}

// readBlob 按 SHA 读出一个 blob 的内容。
func readBlob(gitDir, sha string) ([]byte, error) {
	typ, data, err := object.ReadObject(gitDir, sha)
	if err != nil {
		return nil, err
	}
	if typ != object.Blob {
		return nil, fmt.Errorf("对象 %s 不是 blob", sha)
	}
	return data, nil
}

// untrackedConflict 检查 path 在工作区里是不是有个内容不同的
// 未追踪文件：是则返回 true（调用方报错）。文件不存在、
// 或内容与要写入的相同，返回 false。
func untrackedConflict(workDir string, untracked map[string]bool, path string, want []byte) (bool, error) {
	if !untracked[path] {
		return false, nil
	}
	data, err := os.ReadFile(filepath.Join(workDir, filepath.FromSlash(path)))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("读取工作区文件失败：%w", err)
	}
	return !bytes.Equal(data, want), nil
}
