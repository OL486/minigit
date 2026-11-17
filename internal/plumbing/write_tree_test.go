package plumbing

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/OL486/minigit/internal/index"
	"github.com/OL486/minigit/internal/object"
	"github.com/OL486/minigit/internal/repo"
)

func TestWriteTreeFlatAndNested(t *testing.T) {
	git := requireGit(t)
	dir := t.TempDir()
	work := filepath.Join(dir, "proj")
	gitDir, _, err := repo.Init(work)
	if err != nil {
		t.Fatal(err)
	}
	// 索引里放根文件 + 嵌套目录文件。
	idx := &index.Index{Version: 1}
	blobA, err := object.WriteObject(gitDir, object.Blob, []byte("aaa\n"))
	if err != nil {
		t.Fatal(err)
	}
	blobB, err := object.WriteObject(gitDir, object.Blob, []byte("bbb\n"))
	if err != nil {
		t.Fatal(err)
	}
	idx.Upsert(index.Entry{Path: "a.txt", SHA: blobA, Mode: "100644"})
	idx.Upsert(index.Entry{Path: "src/b.go", SHA: blobB, Mode: "100644"})
	idx.Upsert(index.Entry{Path: "src/deep/c.txt", SHA: blobA, Mode: "100644"})
	treeSHA, err := WriteTree(gitDir, idx)
	if err != nil {
		t.Fatal(err)
	}
	// git ls-tree -r 读我们写的 tree。
	out := runGit(t, git, work, nil, "ls-tree", "-r", treeSHA)
	want := "100644 blob " + blobA + "\ta.txt\n" +
		"100644 blob " + blobB + "\tsrc/b.go\n" +
		"100644 blob " + blobA + "\tsrc/deep/c.txt"
	if out != want {
		t.Fatalf("git ls-tree -r 不对：\n%s\n期望：\n%s", out, want)
	}
	// 空索引生成 git 的空 tree SHA。
	emptySHA, err := WriteTree(gitDir, &index.Index{Version: 1})
	if err != nil {
		t.Fatal(err)
	}
	if emptySHA != "4b825dc642cb6eb9a060e54bf8d69288fbee4904" {
		t.Fatalf("空 tree SHA 不对：%s", emptySHA)
	}
}

func TestCommitTreeGitReads(t *testing.T) {
	git := requireGit(t)
	dir := t.TempDir()
	work := filepath.Join(dir, "proj")
	gitDir, _, err := repo.Init(work)
	if err != nil {
		t.Fatal(err)
	}
	blobSHA, err := object.WriteObject(gitDir, object.Blob, []byte("x\n"))
	if err != nil {
		t.Fatal(err)
	}
	idx := &index.Index{Version: 1}
	idx.Upsert(index.Entry{Path: "x.txt", SHA: blobSHA, Mode: "100644"})
	treeSHA, err := WriteTree(gitDir, idx)
	if err != nil {
		t.Fatal(err)
	}
	when := time.Unix(1794897000, 0).In(time.FixedZone("", 8*3600))
	author := FormatIdentity("Test User", "test@example.com", when)
	commitSHA, err := CommitTree(gitDir, treeSHA, nil, author, author, "first commit")
	if err != nil {
		t.Fatal(err)
	}
	// git cat-file -p 能读，内容完整。
	out := runGit(t, git, work, nil, "cat-file", "-p", commitSHA)
	if !strings.Contains(out, "tree "+treeSHA+"\n") ||
		!strings.Contains(out, "author Test User <test@example.com> 1794897000 +0800") ||
		!strings.Contains(out, "committer Test User <test@example.com> 1794897000 +0800") ||
		!strings.HasSuffix(out, "first commit") {
		t.Fatalf("git cat-file -p 读 commit 不对：\n%s", out)
	}
	// 第二个提交带 parent，git log 能沿着链走。
	commit2, err := CommitTree(gitDir, treeSHA, []string{commitSHA}, author, author, "second")
	if err != nil {
		t.Fatal(err)
	}
	// 直接以 commit SHA 为起点——CommitTree 只写对象不更新分支，
	// HEAD 还是未出生状态，git log 不跟参数会报“还没有提交”。
	log := runGit(t, git, work, nil, "log", "--format=%H %P", commit2)
	lines := strings.Split(log, "\n")
	if len(lines) != 2 || lines[0] != commit2+" "+commitSHA || lines[1] != commitSHA+" " {
		t.Fatalf("git log parent 链不对：%q", log)
	}
}

func TestFormatIdentity(t *testing.T) {
	when := time.Unix(1794897000, 0).In(time.FixedZone("", -5*3600))
	got := FormatIdentity("A U Thor", "a@x.y", when)
	want := "A U Thor <a@x.y> 1794897000 -0500"
	if got != want {
		t.Fatalf("FormatIdentity 不对：%q，期望 %q", got, want)
	}
}

func TestCommitTreeRejectsBadTree(t *testing.T) {
	dir := t.TempDir()
	gitDir, _, err := repo.Init(filepath.Join(dir, "proj"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CommitTree(gitDir, "abc", nil, "A <a@x> 1 +0000", "A <a@x> 1 +0000", "x"); err == nil {
		t.Fatal("无效 tree SHA 应报错")
	}
}
