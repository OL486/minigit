package porcelain

import (
	"fmt"
	"strings"
	"time"

	"github.com/OL486/minigit/internal/index"
	"github.com/OL486/minigit/internal/object"
	"github.com/OL486/minigit/internal/plumbing"
	"github.com/OL486/minigit/internal/refs"
	"github.com/OL486/minigit/internal/repo"
)

// Commit 按暂存区创建一次提交，更新当前分支指针，返回 commit SHA。
// 暂存区为空、或暂存内容与 HEAD 的 tree 完全相同时报错
// "nothing to commit"，不产生空提交。message 为空也报错。
func Commit(gitDir, message string) (string, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return "", fmt.Errorf("提交信息不能为空（用 -m <message>）")
	}
	idx, err := index.Read(gitDir)
	if err != nil {
		return "", err
	}
	if len(idx.Entries) == 0 {
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
		if head.Tree == treeSHA {
			return "", fmt.Errorf("nothing to commit, working tree clean")
		}
		parents = []string{headSHA}
	case refs.IsNotFound(err):
		// 还没有提交：根提交，无 parent。
	default:
		return "", err
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
	return sha, nil
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
