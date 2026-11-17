package porcelain

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/OL486/minigit/internal/refs"
)

// commitFile 写文件、add、提交，返回 commit SHA（测试辅助）。
func commitFile(t *testing.T, work, gitDir, path, content, msg string) string {
	t.Helper()
	writeFile(t, filepath.Join(work, filepath.FromSlash(path)), content)
	if err := Add(gitDir, work, []string{filepath.Join(work, filepath.FromSlash(path))}); err != nil {
		t.Fatal(err)
	}
	sha, err := Commit(gitDir, msg)
	if err != nil {
		t.Fatal(err)
	}
	return sha
}

func TestBranchCreateAndList(t *testing.T) {
	work, gitDir := setupRepo(t)
	sha1 := commitFile(t, work, gitDir, "a.txt", "v1\n", "c1")
	// 还没有任何分支之外的操作前，branch 列表只有 main。
	var buf bytes.Buffer
	if err := ListBranches(gitDir, &buf); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "* main\n" {
		t.Fatalf("branch 列表不对：%q", buf.String())
	}
	// 在 HEAD 建 feature。
	if err := CreateBranch(gitDir, "feature", ""); err != nil {
		t.Fatal(err)
	}
	sha, err := refs.ResolveRef(gitDir, "feature")
	if err != nil || sha != sha1 {
		t.Fatalf("feature 应指向 %s，实际 %s %v", sha1, sha, err)
	}
	buf.Reset()
	if err := ListBranches(gitDir, &buf); err != nil {
		t.Fatal(err)
	}
	// 分支按字母序排（和 git branch 一致），不是按创建时间。
	if buf.String() != "  feature\n* main\n" {
		t.Fatalf("branch 列表不对：%q", buf.String())
	}
	// 从指定 start-point（第一个提交）建分支。
	commitFile(t, work, gitDir, "a.txt", "v2\n", "c2")
	if err := CreateBranch(gitDir, "old", sha1); err != nil {
		t.Fatal(err)
	}
	sha, err = refs.ResolveRef(gitDir, "old")
	if err != nil || sha != sha1 {
		t.Fatalf("old 应指向 %s，实际 %s %v", sha1, sha, err)
	}
	// 从分支名作 start-point。
	if err := CreateBranch(gitDir, "from-feature", "feature"); err != nil {
		t.Fatal(err)
	}
	sha, err = refs.ResolveRef(gitDir, "from-feature")
	if err != nil || sha != sha1 {
		t.Fatalf("from-feature 应指向 %s，实际 %s %v", sha1, sha, err)
	}
}

func TestBranchErrors(t *testing.T) {
	work, gitDir := setupRepo(t)
	// 还没有任何提交：建分支应报错。
	if err := CreateBranch(gitDir, "x", ""); err == nil {
		t.Fatal("没有提交时建分支应报错")
	}
	commitFile(t, work, gitDir, "a.txt", "v1\n", "c1")
	// 重名。
	if err := CreateBranch(gitDir, "main", ""); err == nil {
		t.Fatal("分支已存在时应报错")
	}
	if err := CreateBranch(gitDir, "feature", ""); err != nil {
		t.Fatal(err)
	}
	if err := CreateBranch(gitDir, "feature", ""); err == nil {
		t.Fatal("重复建同一分支应报错")
	}
	// start-point 不存在。
	for _, bad := range []string{"nope", "deadbeef", "zzzz"} {
		if err := CreateBranch(gitDir, "b-"+bad, bad); err == nil {
			t.Fatalf("start-point %q 不存在时应报错", bad)
		}
	}
	// start-point 不是合法引用时也报错。
	if err := CreateBranch(gitDir, "from-bad", "HEAD~0"); err == nil {
		t.Fatal("start-point 不是合法引用时应报错")
	}
	// 非法名字。
	for _, bad := range []string{"", "a..b", "a b", "a/b", "-x", "HEAD", ".x", "x.", "x.lock", "a~b", "a^b", "a:b", "a?b", "a*b", "a[b", "a\\b", "a@{b"} {
		if err := CreateBranch(gitDir, bad, ""); err == nil {
			t.Fatalf("分支名 %q 应报错", bad)
		}
	}
}

func TestBranchListNoCommits(t *testing.T) {
	_, gitDir := setupRepo(t)
	var buf bytes.Buffer
	if err := ListBranches(gitDir, &buf); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "" {
		t.Fatalf("没有任何分支时列表应为空：%q", buf.String())
	}
}

func TestGitSeesBranches(t *testing.T) {
	git := requireGit(t)
	work, gitDir := setupRepo(t)
	commitFile(t, work, gitDir, "a.txt", "v1\n", "c1")
	if err := CreateBranch(gitDir, "feature", ""); err != nil {
		t.Fatal(err)
	}
	// git branch 列出 minigit 建的分支，* 在当前分支上（按字母序）。
	out := runGit(t, git, work, nil, "branch")
	if out != "  feature\n* main" {
		t.Fatalf("git branch 不对：%q", out)
	}
	// git log --all 看到两个分支（这里两个分支指向同一提交）。
	all := runGit(t, git, work, nil, "log", "--all", "--oneline")
	if !strings.Contains(all, "c1") {
		t.Fatalf("git log --all 不对：%q", all)
	}
}
