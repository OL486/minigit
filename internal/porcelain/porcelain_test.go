package porcelain

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/OL486/minigit/internal/refs"
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

func setIdentity(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_AUTHOR_NAME", "Test User")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test User")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
}

// setupRepo 建一个 minigit 仓库，返回工作区与 .git 目录。
func setupRepo(t *testing.T) (work, gitDir string) {
	t.Helper()
	setIdentity(t)
	dir := t.TempDir()
	work = filepath.Join(dir, "proj")
	var err error
	gitDir, _, err = repo.Init(work)
	if err != nil {
		t.Fatal(err)
	}
	return work, gitDir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAddCommitLogFlow(t *testing.T) {
	work, gitDir := setupRepo(t)
	writeFile(t, filepath.Join(work, "a.txt"), "hello\n")
	writeFile(t, filepath.Join(work, "src", "b.go"), "package main\n")
	if err := Add(gitDir, work, []string{filepath.Join(work, "a.txt"), filepath.Join(work, "src", "b.go")}); err != nil {
		t.Fatal(err)
	}
	sha1, err := Commit(gitDir, "first commit")
	if err != nil {
		t.Fatal(err)
	}
	// log 默认格式含 SHA、作者、信息。
	var buf bytes.Buffer
	if err := Log(gitDir, false, &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "commit "+sha1+"\n") ||
		!strings.Contains(buf.String(), "Author: Test User <test@example.com>") ||
		!strings.Contains(buf.String(), "    first commit\n") {
		t.Fatalf("log 默认格式不对：\n%s", buf.String())
	}
	// 第二次提交：改文件、再提交，parent 链正确。
	writeFile(t, filepath.Join(work, "a.txt"), "hello v2\n")
	if err := Add(gitDir, work, []string{filepath.Join(work, "a.txt")}); err != nil {
		t.Fatal(err)
	}
	sha2, err := Commit(gitDir, "second commit")
	if err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	if err := Log(gitDir, true, &buf); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 ||
		lines[0] != sha2[:7]+" second commit" ||
		lines[1] != sha1[:7]+" first commit" {
		t.Fatalf("log --oneline 不对：%q", buf.String())
	}
	// parent 链：第二个提交的 parent 是第一个。
	c2, err := readCommit(gitDir, sha2)
	if err != nil {
		t.Fatal(err)
	}
	if len(c2.Parents) != 1 || c2.Parents[0] != sha1 {
		t.Fatalf("parent 链不对：%+v", c2)
	}
	// 分支指针指向最新提交。
	head, err := refs.ResolveRef(gitDir, "HEAD")
	if err != nil || head != sha2 {
		t.Fatalf("HEAD 应指向 %s，实际 %s %v", sha2, head, err)
	}
}

func TestStatusThreeStates(t *testing.T) {
	work, gitDir := setupRepo(t)
	a := filepath.Join(work, "a.txt")
	writeFile(t, a, "v1\n")
	// 未提交：add 后是“已暂存新文件”，NoCommits。
	if err := Add(gitDir, work, []string{a}); err != nil {
		t.Fatal(err)
	}
	st, err := ComputeStatus(gitDir, work)
	if err != nil {
		t.Fatal(err)
	}
	if !st.NoCommits || len(st.StagedAdded) != 1 || st.StagedAdded[0] != "a.txt" {
		t.Fatalf("未提交时的 status 不对：%+v", st)
	}
	// 提交后干净。
	if _, err := Commit(gitDir, "c1"); err != nil {
		t.Fatal(err)
	}
	st, err = ComputeStatus(gitDir, work)
	if err != nil {
		t.Fatal(err)
	}
	if !st.IsClean() {
		t.Fatalf("提交后应干净：%+v", st)
	}
	// 改文件未 add：unstaged modified。
	writeFile(t, a, "v2\n")
	st, err = ComputeStatus(gitDir, work)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Modified) != 1 || st.Modified[0] != "a.txt" {
		t.Fatalf("改文件后 status 不对：%+v", st)
	}
	if len(st.StagedModified) != 0 {
		t.Fatalf("没 add 不应出现在 staged 里：%+v", st)
	}
	// add 后：staged modified。
	if err := Add(gitDir, work, []string{a}); err != nil {
		t.Fatal(err)
	}
	st, err = ComputeStatus(gitDir, work)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.StagedModified) != 1 || len(st.Modified) != 0 {
		t.Fatalf("add 后 status 不对：%+v", st)
	}
	// 新文件未追踪 + 删文件。
	writeFile(t, filepath.Join(work, "new.txt"), "new\n")
	if err := os.Remove(a); err != nil {
		t.Fatal(err)
	}
	st, err = ComputeStatus(gitDir, work)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Untracked) != 1 || st.Untracked[0] != "new.txt" {
		t.Fatalf("未追踪文件不对：%+v", st)
	}
	if len(st.Deleted) != 1 || st.Deleted[0] != "a.txt" {
		t.Fatalf("删除未暂存不对：%+v", st)
	}
	// PrintStatus 格式：含分支头与各区标题。
	var buf bytes.Buffer
	PrintStatus(&buf, st)
	out := buf.String()
	if !strings.HasPrefix(out, "On branch main\n") ||
		!strings.Contains(out, "Changes to be committed:\n  modified:   a.txt") ||
		!strings.Contains(out, "Changes not staged for commit:\n  deleted:    a.txt") ||
		!strings.Contains(out, "Untracked files:\n  new.txt") {
		t.Fatalf("PrintStatus 格式不对：\n%s", out)
	}
}

func TestStatusStatFastPath(t *testing.T) {
	work, gitDir := setupRepo(t)
	a := filepath.Join(work, "a.txt")
	writeFile(t, a, "same content\n")
	if err := Add(gitDir, work, []string{a}); err != nil {
		t.Fatal(err)
	}
	if _, err := Commit(gitDir, "c1"); err != nil {
		t.Fatal(err)
	}
	// 只摸一下 mtime（内容不变）：stat 变了但内容哈希一致，仍算干净。
	now := fileModTime(t, a)
	if err := os.Chtimes(a, now, now.AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	st, err := ComputeStatus(gitDir, work)
	if err != nil {
		t.Fatal(err)
	}
	if !st.IsClean() {
		t.Fatalf("只改 mtime、内容不变时应算干净：%+v", st)
	}
}

func fileModTime(t *testing.T, path string) time.Time {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.ModTime()
}

func TestCommitErrors(t *testing.T) {
	work, gitDir := setupRepo(t)
	// 空信息。
	if _, err := Commit(gitDir, ""); err == nil {
		t.Fatal("空提交信息应报错")
	}
	if _, err := Commit(gitDir, "   "); err == nil {
		t.Fatal("空白提交信息应报错")
	}
	// 暂存区为空。
	if _, err := Commit(gitDir, "x"); err == nil {
		t.Fatal("空暂存区提交应报错")
	}
	// add 不存在的文件。
	if err := Add(gitDir, work, []string{filepath.Join(work, "nope.txt")}); err == nil {
		t.Fatal("add 不存在的文件应报错")
	}
	// 提交一次后，无改动再提交应报 nothing to commit。
	writeFile(t, filepath.Join(work, "a.txt"), "v1\n")
	if err := Add(gitDir, work, []string{filepath.Join(work, "a.txt")}); err != nil {
		t.Fatal(err)
	}
	if _, err := Commit(gitDir, "c1"); err != nil {
		t.Fatal(err)
	}
	if _, err := Commit(gitDir, "c2"); err == nil {
		t.Fatal("无改动再次提交应报 nothing to commit")
	}
	// log 还没有提交时报错。
	work2, gitDir2 := setupRepo(t)
	_ = work2
	var buf bytes.Buffer
	if err := Log(gitDir2, false, &buf); err == nil {
		t.Fatal("没有提交时 log 应报错")
	}
}

func TestAddDirectory(t *testing.T) {
	work, gitDir := setupRepo(t)
	writeFile(t, filepath.Join(work, "a.txt"), "a\n")
	writeFile(t, filepath.Join(work, "sub", "b.txt"), "b\n")
	writeFile(t, filepath.Join(work, "sub", "deep", "c.txt"), "c\n")
	// add . 是相对当前目录的路径，测试里先 chdir 进仓库
	// （t.Chdir 要 Go 1.24，这里用 os.Chdir + Cleanup 恢复）。
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(work); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })
	// add . 递归加全部文件（跳过 .git）。
	if err := Add(gitDir, work, []string{"."}); err != nil {
		t.Fatal(err)
	}
	st, err := ComputeStatus(gitDir, work)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.StagedAdded) != 3 {
		t.Fatalf("add . 应暂存 3 个文件：%+v", st)
	}
	if _, err := Commit(gitDir, "all"); err != nil {
		t.Fatal(err)
	}
}

func TestGitReadsHistoryAndFsck(t *testing.T) {
	git := requireGit(t)
	work, gitDir := setupRepo(t)
	writeFile(t, filepath.Join(work, "a.txt"), "v1\n")
	if err := Add(gitDir, work, []string{filepath.Join(work, "a.txt")}); err != nil {
		t.Fatal(err)
	}
	sha1, err := Commit(gitDir, "first")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(work, "a.txt"), "v2\n")
	writeFile(t, filepath.Join(work, "b.txt"), "b\n")
	if err := Add(gitDir, work, []string{filepath.Join(work, "a.txt"), filepath.Join(work, "b.txt")}); err != nil {
		t.Fatal(err)
	}
	sha2, err := Commit(gitDir, "second")
	if err != nil {
		t.Fatal(err)
	}
	// git log 读 minigit 的提交历史。
	log := runGit(t, git, work, nil, "log", "--format=%H")
	if log != sha2+"\n"+sha1 {
		t.Fatalf("git log 历史不对：%q", log)
	}
	msg := runGit(t, git, work, nil, "log", "--format=%s")
	if msg != "second\nfirst" {
		t.Fatalf("git log 信息不对：%q", msg)
	}
	// git ls-tree -r 读嵌套 tree。
	tree := runGit(t, git, work, nil, "ls-tree", "-r", "--format=%(path)", "HEAD")
	if tree != "a.txt\nb.txt" {
		t.Fatalf("git ls-tree -r 不对：%q", tree)
	}
	// git fsck 直接跑通过。M2 时索引还是临时 JSON 格式，fsck 会
	// 报 "index file corrupt"，只能先把索引挪走再跑；M4 索引换
	// 成 v2 格式后不再需要这一步。
	cmd := exec.Command(git, "fsck")
	cmd.Dir = work
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		t.Fatalf("git fsck 失败：%v\nstdout：%s\nstderr：%s", err, out.String(), errBuf.String())
	}
}
