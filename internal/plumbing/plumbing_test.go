package plumbing

import (
	"bytes"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/OL486/minigit/internal/object"
	"github.com/OL486/minigit/internal/repo"
)

func requireGit(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("git")
	if err != nil {
		t.Skip("环境里没有 git，跳过互读测试")
	}
	return p
}

// runGit 在 dir 里跑 git 命令，返回标准输出（失败则 t.Fatal）。
func runGit(t *testing.T, git, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command(git, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v 失败：%v，stderr：%s", args, err, errBuf.String())
	}
	return strings.TrimRight(out.String(), "\n")
}

func TestHashObjectMatchesGit(t *testing.T) {
	git := requireGit(t)
	dir := t.TempDir()
	rng := rand.New(rand.NewSource(1234))
	// 100 个随机文件：空文件、二进制、1MB 大文件都在内。
	for i := 0; i < 100; i++ {
		var size int
		switch i % 5 {
		case 0:
			size = 0
		case 1:
			size = 1
		case 2:
			size = 17
		case 3:
			size = 4096
		case 4:
			size = 1 << 20
		}
		data := make([]byte, size)
		rng.Read(data)
		// 混一点文本行，覆盖常见情况。
		if size >= 10 {
			copy(data, "hello minigit\n")
		}
		path := filepath.Join(dir, "f")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		want := runGit(t, git, dir, nil, "hash-object", path)
		got, err := HashObject("", path, false)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("第 %d 个文件（%d 字节）SHA 不一致：minigit %s vs git %s", i, size, got, want)
		}
	}
}

func TestInteropMinigitToGit(t *testing.T) {
	git := requireGit(t)
	dir := t.TempDir()
	work := filepath.Join(dir, "proj")
	gitDir, _, err := repo.Init(work)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("hello minigit\n第二行\n")
	path := filepath.Join(work, "hello.txt")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	sha, err := HashObject(gitDir, path, true)
	if err != nil {
		t.Fatal(err)
	}
	// 真 git 读 minigit 写出的对象。
	if got := runGit(t, git, work, nil, "cat-file", "-p", sha); got != "hello minigit\n第二行" {
		t.Fatalf("git cat-file -p 读 minigit 对象不对：%q", got)
	}
	if got := runGit(t, git, work, nil, "cat-file", "-t", sha); got != "blob" {
		t.Fatalf("git cat-file -t 不对：%q", got)
	}
	// 对象文件确实落在 .git/objects/xx/ 下，且是 zlib 格式。
	if _, err := os.Stat(filepath.Join(gitDir, "objects", sha[:2], sha[2:])); err != nil {
		t.Fatalf("对象文件不在预期位置：%v", err)
	}
}

func TestInteropGitToMinigit(t *testing.T) {
	git := requireGit(t)
	dir := t.TempDir()
	work := filepath.Join(dir, "proj")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, git, work, nil, "init", "-b", "main", "-q", ".")
	env := []string{
		"GIT_AUTHOR_NAME=Test User", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test User", "GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_AUTHOR_DATE=1794897000 +0800", "GIT_COMMITTER_DATE=1794897000 +0800",
	}
	// git 写 blob。
	content := "git wrote this\n"
	if err := os.WriteFile(filepath.Join(work, "a.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	blobSHA := runGit(t, git, work, env, "hash-object", "-w", "a.txt")
	// git 写 tree：mktree 读标准输入。
	treeSHA := func() string {
		cmd := exec.Command(git, "mktree")
		cmd.Dir = work
		cmd.Env = append(os.Environ(), env...)
		cmd.Stdin = strings.NewReader("100644 blob " + blobSHA + "\ta.txt\n")
		var out, errBuf bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &errBuf
		if err := cmd.Run(); err != nil {
			t.Fatalf("git mktree 失败：%v %s", err, errBuf.String())
		}
		return strings.TrimSpace(out.String())
	}()
	// git 写 commit。
	commitSHA := runGit(t, git, work, env, "commit-tree", treeSHA, "-m", "test commit")

	gitDir, _, err := repo.FindRepo(work)
	if err != nil {
		t.Fatal(err)
	}
	// minigit -t 读三类对象。
	for _, tc := range []struct{ sha, typ string }{
		{blobSHA, "blob"}, {treeSHA, "tree"}, {commitSHA, "commit"},
	} {
		var buf bytes.Buffer
		if err := CatFile(&buf, gitDir, tc.sha, true, false); err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(buf.String()) != tc.typ {
			t.Fatalf("CatFile -t %s：期望 %s，实际 %q", tc.sha, tc.typ, buf.String())
		}
	}
	// minigit -p 读 blob 原文。
	var buf bytes.Buffer
	if err := CatFile(&buf, gitDir, blobSHA, false, true); err != nil {
		t.Fatal(err)
	}
	if buf.String() != content {
		t.Fatalf("CatFile -p blob 不对：%q", buf.String())
	}
	// minigit -p 读 tree：git 的显示格式。
	buf.Reset()
	if err := CatFile(&buf, gitDir, treeSHA, false, true); err != nil {
		t.Fatal(err)
	}
	wantTree := "100644 blob " + blobSHA + "\ta.txt\n"
	if buf.String() != wantTree {
		t.Fatalf("CatFile -p tree 不对：%q，期望 %q", buf.String(), wantTree)
	}
	// minigit -p 读 commit：含 tree 行与提交信息。
	buf.Reset()
	if err := CatFile(&buf, gitDir, commitSHA, false, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "tree "+treeSHA+"\n") ||
		!strings.Contains(buf.String(), "author Test User <test@example.com> 1794897000 +0800") ||
		!strings.HasSuffix(buf.String(), "test commit\n") {
		t.Fatalf("CatFile -p commit 不对：%q", buf.String())
	}
	// 短前缀也能读。
	buf.Reset()
	if err := CatFile(&buf, gitDir, blobSHA[:7], false, true); err != nil {
		t.Fatal(err)
	}
	if buf.String() != content {
		t.Fatalf("短前缀 cat-file 不对：%q", buf.String())
	}
	// minigit 的对象层直接读 git 的 commit 并解析。
	typ, data, err := object.ReadObject(gitDir, commitSHA)
	if err != nil || typ != object.Commit {
		t.Fatalf("ReadObject 读 git commit 失败：%v %s", err, typ)
	}
	c, err := object.Parse(data)
	if err != nil || c.Tree != treeSHA || c.Message != "test commit\n" {
		t.Fatalf("解析 git 的 commit 失败：%v %+v", err, c)
	}
}

func TestCatFileErrors(t *testing.T) {
	dir := t.TempDir()
	gitDir, _, err := repo.Init(filepath.Join(dir, "proj"))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	// 不存在的对象。
	if err := CatFile(&buf, gitDir, "0000000000000000000000000000000000000000", false, true); err == nil {
		t.Fatal("不存在的对象应报错")
	}
	// 前缀太短。
	if err := CatFile(&buf, gitDir, "ab", false, true); err == nil {
		t.Fatal("太短的前缀应报错")
	}
	// -p 与 -t 都不给（由 main.go 校验，这里只测 CatFile 本身不 panic）。
	if err := CatFile(&buf, gitDir, "0000000000000000000000000000000000000000", false, false); err == nil {
		t.Fatal("不存在的对象应报错（无模式也一样）")
	}
}
