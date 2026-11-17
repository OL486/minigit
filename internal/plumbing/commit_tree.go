package plumbing

import (
	"fmt"
	"strings"
	"time"

	"github.com/OL486/minigit/internal/object"
)

// FormatIdentity 把名字、邮箱、时间格式化成 git 的身份行：
// "名字 <邮箱> 1794897000 +0800"。时区按给定的时间所在时区写。
func FormatIdentity(name, email string, when time.Time) string {
	return fmt.Sprintf("%s <%s> %d %s", name, email, when.Unix(), when.Format("-0700"))
}

// CommitTree 按 tree 创建 commit 对象，返回 commit SHA。
// parents 为空表示根提交。message 没有结尾换行时自动补一个。
// author / committer 是 FormatIdentity 产出的完整身份行。
func CommitTree(gitDir, treeSHA string, parents []string, author, committer, message string) (string, error) {
	if len(treeSHA) != 40 {
		return "", fmt.Errorf("无效的 tree SHA：%q", treeSHA)
	}
	if !strings.HasSuffix(message, "\n") {
		message += "\n"
	}
	c := &object.ParsedCommit{
		Tree:      treeSHA,
		Parents:   parents,
		Author:    author,
		Committer: committer,
		Message:   message,
	}
	return object.WriteObject(gitDir, object.Commit, c.Encode())
}
