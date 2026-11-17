package porcelain

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/OL486/minigit/internal/index"
	"github.com/OL486/minigit/internal/object"
	"github.com/OL486/minigit/internal/refs"
)

// Checkout 切换到分支 target，返回 switched（是否真的切了分支）。
//
// 规则（按 git 的做法）：
//   - 目标分支不存在时报错；只接受分支名，checkout commit 会报
//     detached HEAD 不支持；切回当前分支时什么都不做，返回 switched=false。
//   - 先算决策、再动文件：两边 tree 相同的路径不动（带着走的未提交
//     修改原样保留）；tree 不同的路径，已暂存的修改、已暂存的删除、
//     工作区里与索引不同的修改、会被覆盖的未追踪文件，都会挡住切换
//     并报错（报错里写清是哪个文件）；唯一的例外是工作区文件内容
//     恰好就是目标版本——那直接允许，文件变成已追踪。
//   - 应用时先删后写：目标 tree 里没有的文件从工作区删掉（空目录
//     顺手清理），有的按 blob 内容写回并更新索引；最后移动 HEAD。
//   - 索引里有、两边 tree 里都没有的路径（已暂存的新文件）原样保留，
//     跟着走。
func Checkout(gitDir, workDir, target string) (bool, error) {
	target = strings.TrimPrefix(target, "refs/heads/")
	cur, err := refs.CurrentBranch(gitDir)
	if err != nil {
		return false, err
	}
	if cur == target {
		return false, nil
	}
	targetSHA, err := resolveCheckoutTarget(gitDir, target)
	if err != nil {
		return false, err
	}
	targetCommit, err := readCommit(gitDir, targetSHA)
	if err != nil {
		return false, err
	}
	targetEntries, err := object.FlattenTree(gitDir, targetCommit.Tree)
	if err != nil {
		return false, err
	}
	tgt := treeMap(targetEntries)

	curTree := map[string]object.Entry{}
	if headSHA, err := refs.ResolveRef(gitDir, "HEAD"); err == nil {
		headCommit, err := readCommit(gitDir, headSHA)
		if err != nil {
			return false, err
		}
		entries, err := object.FlattenTree(gitDir, headCommit.Tree)
		if err != nil {
			return false, err
		}
		curTree = treeMap(entries)
	} else if !refs.IsNotFound(err) {
		return false, err
	}

	idx, err := index.Read(gitDir)
	if err != nil {
		return false, err
	}
	indexed := map[string]index.Entry{}
	for _, e := range idx.Entries {
		indexed[e.Path] = e
	}
	st, err := ComputeStatus(gitDir, workDir)
	if err != nil {
		return false, err
	}
	dirty := map[string]bool{}
	for _, p := range st.Modified {
		dirty[p] = true
	}
	for _, p := range st.Deleted {
		dirty[p] = true
	}
	untracked := map[string]bool{}
	for _, p := range st.Untracked {
		untracked[p] = true
	}

	// 第一遍：决策，不动任何文件。
	var actions []checkoutAction
	for p := range unionKeys(curTree, tgt, indexed) {
		c, cOK := curTree[p]
		te, tOK := tgt[p]
		i, iOK := indexed[p]
		sameTree := cOK == tOK && (!cOK || (c.SHA == te.SHA && c.Mode == te.Mode))
		if sameTree {
			if !iOK && tOK {
				// 两边 tree 都有、索引里没有（防御性处理，正常流程到不了）。
				sha, exists, err := workdirBlobSHA(workDir, p)
				if err != nil {
					return false, err
				}
				if !exists {
					actions = append(actions, checkoutAction{path: p, kind: actWrite, entry: te})
				} else if sha != te.SHA {
					return false, fmt.Errorf("本地修改会被切换覆盖：%s", p)
				} else {
					actions = append(actions, checkoutAction{path: p, kind: actTrack, entry: te})
				}
			}
			continue
		}
		// 两边 tree 不同：先判暂存区。
		if iOK && i.SHA != te.SHA && (!cOK || i.SHA != c.SHA) {
			return false, fmt.Errorf("已暂存的修改会被切换覆盖：%s", p)
		}
		if !iOK && cOK {
			return false, fmt.Errorf("已暂存的删除会被切换覆盖：%s", p)
		}
		// 再判工作区。
		if dirty[p] {
			if tOK {
				if sha, exists, err := workdirBlobSHA(workDir, p); err == nil && exists && sha == te.SHA {
					// 工作区内容已经是目标版本：允许，索引跟进即可。
					actions = append(actions, checkoutAction{path: p, kind: actTrack, entry: te})
					continue
				} else if err != nil {
					return false, err
				}
			}
			return false, fmt.Errorf("本地修改会被切换覆盖：%s", p)
		}
		if !iOK && !cOK && untracked[p] && tOK {
			sha, exists, err := workdirBlobSHA(workDir, p)
			if err != nil {
				return false, err
			}
			if !exists {
				actions = append(actions, checkoutAction{path: p, kind: actWrite, entry: te})
			} else if sha == te.SHA {
				actions = append(actions, checkoutAction{path: p, kind: actTrack, entry: te})
			} else {
				return false, fmt.Errorf("未追踪文件会被切换覆盖：%s", p)
			}
			continue
		}
		// 应用。
		if tOK {
			actions = append(actions, checkoutAction{path: p, kind: actWrite, entry: te})
		} else {
			actions = append(actions, checkoutAction{path: p, kind: actRemove})
		}
	}
	// 预检：写文件前确认没有文件/目录冲突或不支持的模式挡路。
	if err := preflightWrites(workDir, actions); err != nil {
		return false, err
	}
	// 第二遍：先删后写（删在前，避免文件/目录冲突）。
	for _, a := range actions {
		if a.kind != actRemove {
			continue
		}
		abs := filepath.Join(workDir, filepath.FromSlash(a.path))
		if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
			return false, fmt.Errorf("删除工作区文件失败：%w", err)
		}
		pruneEmptyDirs(workDir, a.path)
	}
	for _, a := range actions {
		switch a.kind {
		case actWrite:
			if err := writeWorkdirFile(gitDir, workDir, a.path, a.entry); err != nil {
				return false, err
			}
		case actTrack:
			if err := ensureMode(filepath.Join(workDir, filepath.FromSlash(a.path)), a.entry.Mode); err != nil {
				return false, err
			}
		}
	}
	// 索引：没动过的条目原样保留（stat 信息不动，stat 快速路径才准）；
	// 写过/跟进过的条目按目标 tree + 文件现况重建；删掉的移除。
	kept := map[string]index.Entry{}
	for _, e := range idx.Entries {
		kept[e.Path] = e
	}
	for _, a := range actions {
		switch a.kind {
		case actRemove:
			delete(kept, a.path)
		case actWrite, actTrack:
			fi, err := os.Stat(filepath.Join(workDir, filepath.FromSlash(a.path)))
			if err != nil {
				return false, fmt.Errorf("读取切换后文件状态失败：%w", err)
			}
			kept[a.path] = index.Entry{
				Path:      a.path,
				SHA:       a.entry.SHA,
				Mode:      a.entry.Mode,
				Size:      fi.Size(),
				MtimeSec:  fi.ModTime().Unix(),
				MtimeNsec: int64(fi.ModTime().Nanosecond()),
			}
		}
	}
	newIdx := &index.Index{Version: 1}
	for _, e := range kept {
		newIdx.Entries = append(newIdx.Entries, e)
	}
	if err := newIdx.Write(gitDir); err != nil {
		return false, err
	}
	if err := refs.SetHead(gitDir, "refs/heads/"+target); err != nil {
		return false, err
	}
	return true, nil
}

// checkoutAction 是 checkout 决策出的一个动作。
type checkoutAction struct {
	path  string
	kind  actionKind
	entry object.Entry // actWrite / actTrack 时的目标 tree 条目
}

type actionKind int

const (
	actWrite  actionKind = iota // 按目标 tree 写回工作区文件
	actRemove                   // 从工作区删掉（目标 tree 里没有）
	actTrack                    // 文件不动，只把索引跟进到目标版本
)

// resolveCheckoutTarget 把 checkout 的目标解析成 commit SHA。
// 只接受分支名；目标是 commit SHA 时报 detached HEAD 不支持，
// 目标不存在时报分支不存在。
func resolveCheckoutTarget(gitDir, target string) (string, error) {
	sha, err := refs.ResolveRef(gitDir, target)
	if err == nil {
		return sha, nil
	}
	if !refs.IsNotFound(err) {
		return "", err
	}
	if full, rerr := object.ResolveSHA(gitDir, target); rerr == nil {
		if typ, _, rerr2 := object.ReadObject(gitDir, full); rerr2 == nil && typ == object.Commit {
			return "", fmt.Errorf("不支持 detached HEAD：%s 是 commit，checkout 只接受分支名", target)
		}
	}
	return "", fmt.Errorf("分支不存在：%s", target)
}

// treeMap 把 FlattenTree 的结果转成“路径 → 条目”。
func treeMap(entries []object.Entry) map[string]object.Entry {
	m := make(map[string]object.Entry, len(entries))
	for _, e := range entries {
		m[e.Name] = e
	}
	return m
}

// unionKeys 取三个映射的并集路径。
func unionKeys(cur, tgt map[string]object.Entry, idx map[string]index.Entry) map[string]bool {
	out := map[string]bool{}
	for p := range cur {
		out[p] = true
	}
	for p := range tgt {
		out[p] = true
	}
	for p := range idx {
		out[p] = true
	}
	return out
}

// workdirBlobSHA 算工作区文件的 blob SHA。文件不存在时 exists=false。
func workdirBlobSHA(workDir, path string) (sha string, exists bool, err error) {
	data, err := os.ReadFile(filepath.Join(workDir, filepath.FromSlash(path)))
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("读取工作区文件失败：%w", err)
	}
	return object.Hash(object.Blob, data), true, nil
}

// preflightWrites 在动文件之前检查写操作是否都可行：目标路径本身
// 不能是目录、祖先不能是文件（除非该祖先本次会被删掉）、模式
// 必须是 100644 或 100755。任何一项不过就直接报错，
// 保证 checkout 要么全做完、要么一个文件都不动。
func preflightWrites(workDir string, actions []checkoutAction) error {
	removing := map[string]bool{}
	for _, a := range actions {
		if a.kind == actRemove {
			removing[a.path] = true
		}
	}
	for _, a := range actions {
		if a.kind != actWrite {
			continue
		}
		if a.entry.Mode != "100644" && a.entry.Mode != "100755" {
			return fmt.Errorf("不支持的文件模式 %s：%s（M3 只处理普通文件）", a.entry.Mode, a.path)
		}
		abs := filepath.Join(workDir, filepath.FromSlash(a.path))
		if st, err := os.Stat(abs); err == nil && st.IsDir() {
			return fmt.Errorf("无法写入 %s：工作区里有个同名目录挡着", a.path)
		}
		parts := strings.Split(a.path, "/")
		for i := 1; i < len(parts); i++ {
			anc := strings.Join(parts[:i], "/")
			ancAbs := filepath.Join(workDir, filepath.FromSlash(anc))
			st, err := os.Stat(ancAbs)
			if err != nil {
				continue // 不存在，后面 MkdirAll 会建
			}
			if !st.IsDir() && !removing[anc] {
				return fmt.Errorf("无法写入 %s：%s 是个文件挡着", a.path, anc)
			}
		}
	}
	return nil
}

// writeWorkdirFile 按 tree 条目把 blob 内容写回工作区，并按 mode 改权限。
func writeWorkdirFile(gitDir, workDir, path string, entry object.Entry) error {
	typ, data, err := object.ReadObject(gitDir, entry.SHA)
	if err != nil {
		return err
	}
	if typ != object.Blob {
		return fmt.Errorf("tree 条目 %s 不是 blob：%s", path, typ)
	}
	abs := filepath.Join(workDir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return fmt.Errorf("创建目录失败：%w", err)
	}
	if err := os.WriteFile(abs, data, 0o644); err != nil {
		return fmt.Errorf("写入工作区文件失败：%w", err)
	}
	return ensureMode(abs, entry.Mode)
}

// ensureMode 按 tree 里的 mode 改文件权限（只支持普通文件两种模式）。
func ensureMode(abs, mode string) error {
	var perm os.FileMode
	switch mode {
	case "100644":
		perm = 0o644
	case "100755":
		perm = 0o755
	default:
		return fmt.Errorf("不支持的文件模式：%s", mode)
	}
	if err := os.Chmod(abs, perm); err != nil {
		return fmt.Errorf("改文件权限失败：%w", err)
	}
	return nil
}

// pruneEmptyDirs 删掉 path 的各级空父目录（到工作区根为止）。
// 遇到非空目录就停——只清理本次删文件留下的空壳。
func pruneEmptyDirs(workDir, path string) {
	dir := filepath.Dir(filepath.Join(workDir, filepath.FromSlash(path)))
	for {
		rel, err := filepath.Rel(workDir, dir)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			return
		}
		if err := os.Remove(dir); err != nil {
			return // 非空或删不掉就停
		}
		dir = filepath.Dir(dir)
	}
}
