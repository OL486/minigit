package porcelain

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/OL486/minigit/internal/index"
	"github.com/OL486/minigit/internal/merge"
	"github.com/OL486/minigit/internal/object"
	"github.com/OL486/minigit/internal/plumbing"
	"github.com/OL486/minigit/internal/refs"
	"github.com/OL486/minigit/internal/repo"
)

// Commit 按暂存区创建一次提交，更新当前分支指针，返回 commit SHA。
// 暂存区为空、或暂存内容与 HEAD 的 tree 完全相同时报错
// "nothing to commit"，不产生空提交。message 为空也报错。
// 合并进行中（.git/MERGE_HEAD 存在）时，这次提交是合并提交：
// 第二 parent 是 MERGE_HEAD，提交成功后清除合并状态。
func Commit(gitDir, message string) (string, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return "", fmt.Errorf("提交信息不能为空（用 -m <message>）")
	}
	// 合并进行中：索引可能为空（合并删光了所有文件），也不要求
	// tree 与 HEAD 不同——下面两处检查都要跳过。
	merging := false
	mergeHead, err := merge.ReadMergeHead(gitDir)
	if err != nil {
		return "", err
	}
	if mergeHead != "" {
		merging = true
	}
	idx, err := index.Read(gitDir)
	if err != nil {
		return "", err
	}
	if !merging && len(idx.Entries) == 0 {
		return "", fmt.Errorf("nothing to commit（没有暂存内容）")
	}
	treeSHA, err := plumbing.WriteTree(gitDir, idx)
	if err != nil {
		return "", err
	}
	var parents []string
	headSHA, err := refs.ResolveRef(gitDir, "HEAD")
	switch {
	case err == nil:
		head, err := readCommit(gitDir, headSHA)
		if err != nil {
			return "", err
		}
		if !merging && head.Tree == treeSHA {
			return "", fmt.Errorf("nothing to commit, working tree clean")
		}
		parents = []string{headSHA}
	case refs.IsNotFound(err):
		if merging {
			return "", fmt.Errorf("无法完成合并：HEAD 还没有提交")
		}
		// 还没有提交：根提交，无 parent。
	default:
		return "", err
	}
	if merging {
		// 还有带冲突标记没解决的文件时拒绝提交。
		if err := checkMergeResolved(gitDir, idx); err != nil {
			return "", err
		}
		parents = append(parents, mergeHead)
	}
	author, err := repo.AuthorIdentity(gitDir)
	if err != nil {
		return "", err
	}
	committer, err := repo.CommitterIdentity(gitDir)
	if err != nil {
		return "", err
	}
	now := time.Now()
	sha, err := plumbing.CommitTree(gitDir, treeSHA, parents,
		plumbing.FormatIdentity(author.Name, author.Email, now),
		plumbing.FormatIdentity(committer.Name, committer.Email, now),
		message)
	if err != nil {
		return "", err
	}
	if err := refs.UpdateRef(gitDir, "HEAD", sha); err != nil {
		return "", err
	}
	if merging {
		merge.ClearMergeState(gitDir)
	}
	return sha, nil
}

// checkMergeResolved 检查合并冲突是否都解决了：MERGE_MSG 里列出的
// 冲突文件，工作区里还有冲突标记（"<<<<<<< " 开头的行）时拒绝提交。
// 文件不存在时：已从索引里暂存删除（接受删除那一边）才算解决，
// 否则也算未解决（提醒用户 add 该文件暂存删除）。
// 二进制冲突没有标记可查：只要用户 add 过（索引里是他选的版本）
// 就放行，见 docs/03-merge.md。
func checkMergeResolved(gitDir string, idx *index.Index) error {
	paths, err := merge.MergeConflicts(gitDir)
	if err != nil {
		return err
	}
	workDir := filepath.Dir(gitDir)
	var left []string
	for _, p := range paths {
		data, err := os.ReadFile(filepath.Join(workDir, filepath.FromSlash(p)))
		if err != nil {
			if os.IsNotExist(err) {
				if _, ok := idx.Get(p); ok {
					left = append(left, p)
				}
				continue
			}
			return fmt.Errorf("读取 %s 失败：%w", p, err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "<<<<<<< ") {
				left = append(left, p)
				break
			}
		}
	}
	if len(left) > 0 {
		return fmt.Errorf("还有未解决的冲突：%s（解决后 add 再提交）", strings.Join(left, "、"))
	}
	return nil
}

// readCommit 读出并解析一个 commit 对象。
func readCommit(gitDir, sha string) (*object.ParsedCommit, error) {
	typ, data, err := object.ReadObject(gitDir, sha)
	if err != nil {
		return nil, err
	}
	if typ != object.Commit {
		return nil, fmt.Errorf("对象 %s 不是 commit", sha)
	}
	return object.Parse(data)
}
