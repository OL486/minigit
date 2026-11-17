package porcelain

import (
	"bytes"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// normalizeDiff 归一化 diff 输出再比对：
//   - index 行里的 SHA 前缀（`index <sha>..<sha>`，后面可能跟 mode）
//     换成占位符——git 的缩写长度可能因仓库不同而变化，
//     minigit 固定取 7 位；SHA 本身是否正确由 hash-object 的
//     对照测试保证，这里只比格式与内容。
//
// 其他行（包括文件路径前缀、hunk 头、内容行）逐字比对，不归一化。
var indexLineRe = regexp.MustCompile(`index [0-9a-f]+\.\.[0-9a-f]+`)

func normalizeDiff(s string) string {
	return indexLineRe.ReplaceAllString(s, "index X..X")
}

// runGitOutput 在 dir 里跑 git 命令，返回输出与 exit 码
// （不像 runGit 那样失败就 Fatal，因为这里要断言 git 的行为）。
func runGitOutput(t *testing.T, git, dir string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(git, args...)
	cmd.Dir = dir
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("git %v 执行失败：%v", args, err)
		}
	}
	return out.String(), code
}

// minigitDiffString 调 Diff 函数取输出。
func minigitDiffString(t *testing.T, gitDir, work string, cached bool) string {
	t.Helper()
	var buf bytes.Buffer
	if err := Diff(gitDir, work, cached, &buf); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// commitAll 暂存全部改动并提交一次。
func commitAll(t *testing.T, gitDir, work, msg string) {
	t.Helper()
	// 注意：Add 的路径按调用者当前目录解析，"." 在测试进程
	// 里不在仓库中，所以传 work 的绝对路径。
	if err := Add(gitDir, work, []string{work}); err != nil {
		t.Fatal(err)
	}
	if _, err := Commit(gitDir, msg); err != nil {
		t.Fatal(err)
	}
}

func TestDiffMatchesGit(t *testing.T) {
	git := requireGit(t)
	work, gitDir := setupRepo(t)
	// 基础提交：三个文件，一个不带末尾换行。
	writeFile(t, filepath.Join(work, "a.txt"), "one\ntwo\nthree\n")
	writeFile(t, filepath.Join(work, "b.txt"), "alpha\nbeta\n")
	writeFile(t, filepath.Join(work, "nonl.txt"), "no newline at end")
	commitAll(t, gitDir, work, "base")

	// 改 a.txt（改一行、加一行），删 b.txt，加新文件 c.txt，
	// nonl.txt 只补一个末尾换行，a.txt 再改个权限。
	writeFile(t, filepath.Join(work, "a.txt"), "one\nTWO\nthree\nfour\n")
	if err := os.Remove(filepath.Join(work, "b.txt")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(work, "c.txt"), "fresh\nfile\n")
	writeFile(t, filepath.Join(work, "nonl.txt"), "no newline at end\n")
	if err := os.Chmod(filepath.Join(work, "a.txt"), 0o755); err != nil {
		t.Fatal(err)
	}

	// 未暂存：工作区相对索引。
	got := minigitDiffString(t, gitDir, work, false)
	want, code := runGitOutput(t, git, work, "diff")
	if code != 0 {
		t.Fatalf("git diff exit %d", code)
	}
	if normalizeDiff(got) != normalizeDiff(want) {
		t.Fatalf("minigit diff 与 git diff 不一致：\n得到：\n%s\n期望：\n%s", got, want)
	}

	// 暂存后：索引相对 HEAD。
	commitAll(t, gitDir, work, "second")
	// 再改一次，只改 a.txt 的一行（未暂存），另加一个空文件暂存。
	writeFile(t, filepath.Join(work, "a.txt"), "one\nTWO2\nthree\nfour\n")
	writeFile(t, filepath.Join(work, "empty.txt"), "")
	if err := Add(gitDir, work, []string{filepath.Join(work, "empty.txt")}); err != nil {
		t.Fatal(err)
	}
	got = minigitDiffString(t, gitDir, work, false)
	want, _ = runGitOutput(t, git, work, "diff")
	if normalizeDiff(got) != normalizeDiff(want) {
		t.Fatalf("第二轮 minigit diff 与 git diff 不一致：\n得到：\n%s\n期望：\n%s", got, want)
	}
	got = minigitDiffString(t, gitDir, work, true)
	want, _ = runGitOutput(t, git, work, "diff", "--cached")
	if normalizeDiff(got) != normalizeDiff(want) {
		t.Fatalf("minigit diff --cached 与 git diff --cached 不一致：\n得到：\n%s\n期望：\n%s", got, want)
	}
}

func TestDiffModeOnly(t *testing.T) {
	git := requireGit(t)
	work, gitDir := setupRepo(t)
	writeFile(t, filepath.Join(work, "a.txt"), "x\n")
	commitAll(t, gitDir, work, "base")
	if err := os.Chmod(filepath.Join(work, "a.txt"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := minigitDiffString(t, gitDir, work, false)
	want, _ := runGitOutput(t, git, work, "diff")
	if normalizeDiff(got) != normalizeDiff(want) {
		t.Fatalf("纯改 mode 的 diff 不一致：\n得到：\n%s\n期望：\n%s", got, want)
	}
	// 二进制文件。
	writeFile(t, filepath.Join(work, "bin.dat"), "bin\x00ary\n")
	commitAll(t, gitDir, work, "bin")
	writeFile(t, filepath.Join(work, "bin.dat"), "bin\x00ary2\n")
	got = minigitDiffString(t, gitDir, work, false)
	want, _ = runGitOutput(t, git, work, "diff")
	if normalizeDiff(got) != normalizeDiff(want) {
		t.Fatalf("二进制文件的 diff 不一致：\n得到：\n%s\n期望：\n%s", got, want)
	}
}

func TestGitStatusWorks(t *testing.T) {
	git := requireGit(t)
	work, gitDir := setupRepo(t)
	writeFile(t, filepath.Join(work, "a.txt"), "v1\n")
	commitAll(t, gitDir, work, "base")
	writeFile(t, filepath.Join(work, "a.txt"), "v2\n")
	writeFile(t, filepath.Join(work, "new.txt"), "untracked\n")
	// 真 git 的 git status 在 minigit 仓库里直接跑：exit 0，
	// 且输出里有已改文件和未追踪文件。
	out, code := runGitOutput(t, git, work, "status")
	if code != 0 {
		t.Fatalf("git status exit %d，输出：%s", code, out)
	}
	if !strings.Contains(out, "a.txt") || !strings.Contains(out, "new.txt") {
		t.Fatalf("git status 输出里没有已改/未追踪文件：\n%s", out)
	}
	// git ls-files --stage 能读 minigit 写的 v2 索引。
	ls, code := runGitOutput(t, git, work, "ls-files", "--stage")
	if code != 0 {
		t.Fatalf("git ls-files exit %d", code)
	}
	if !strings.Contains(ls, "a.txt") {
		t.Fatalf("git ls-files 读不到索引里的 a.txt：%s", ls)
	}
}

func TestGitFsckDirect(t *testing.T) {
	git := requireGit(t)
	work, gitDir := setupRepo(t)
	writeFile(t, filepath.Join(work, "a.txt"), "v1\n")
	commitAll(t, gitDir, work, "base")
	writeFile(t, filepath.Join(work, "a.txt"), "v2\n")
	if err := Add(gitDir, work, []string{filepath.Join(work, "a.txt")}); err != nil {
		t.Fatal(err)
	}
	if _, err := Commit(gitDir, "second"); err != nil {
		t.Fatal(err)
	}
	// M2 的已知限制本轮消除：git fsck 直接跑，不许再挪走索引。
	out, code := runGitOutput(t, git, work, "fsck")
	if code != 0 {
		t.Fatalf("git fsck 直接跑失败 exit %d：%s", code, out)
	}
}

// TestDiffRandomTrees：随机文件树场景，minigit diff 与 git diff
// 归一化后一致（未暂存和已暂存各比一次）。种子固定，失败可复现。
func TestDiffRandomTrees(t *testing.T) {
	git := requireGit(t)
	for _, seed := range []int64{1, 7, 42, 99, 12345} {
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			r := rand.New(rand.NewSource(seed))
			work, gitDir := setupRepo(t)
			files := []string{"a.txt", "b.txt", "sub/c.txt", "sub/d.txt", "e.txt"}
			for _, f := range files {
				lines := make([]string, 3+r.Intn(8))
				for i := range lines {
					lines[i] = fmt.Sprintf("line-%d-%c", i, 'a'+r.Intn(4))
				}
				writeFile(t, filepath.Join(work, f), strings.Join(lines, "\n")+"\n")
			}
			commitAll(t, gitDir, work, "base")
			// 随机改：改几行、删一个文件、加一个文件。
			for _, f := range files {
				if r.Intn(2) == 0 {
					continue
				}
				data, err := os.ReadFile(filepath.Join(work, f))
				if err != nil {
					t.Fatal(err)
				}
				lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
				for i := range lines {
					if r.Intn(3) == 0 {
						lines[i] = fmt.Sprintf("changed-%d", r.Intn(100))
					}
				}
				if r.Intn(4) == 0 {
					lines = append(lines, "appended")
				}
				writeFile(t, filepath.Join(work, f), strings.Join(lines, "\n")+"\n")
			}
			if r.Intn(2) == 0 {
				if err := os.Remove(filepath.Join(work, "b.txt")); err != nil {
					t.Fatal(err)
				}
			}
			writeFile(t, filepath.Join(work, "new.txt"), "brand new\n")
			got := minigitDiffString(t, gitDir, work, false)
			want, _ := runGitOutput(t, git, work, "diff")
			if normalizeDiff(got) != normalizeDiff(want) {
				t.Fatalf("seed %d 未暂存 diff 不一致：\n得到：\n%s\n期望：\n%s", seed, got, want)
			}
			// 全部暂存后再比 --cached。注意：被删的 b.txt 在两边
			// 都还留在索引里（add . 不暂存删除，git 也一样），
			// 所以两边对它都没有已暂存差异，照常比对即可。
			if err := Add(gitDir, work, []string{work}); err != nil {
				t.Fatal(err)
			}
			got = minigitDiffString(t, gitDir, work, true)
			want, _ = runGitOutput(t, git, work, "diff", "--cached")
			if normalizeDiff(got) != normalizeDiff(want) {
				t.Fatalf("seed %d 已暂存 diff 不一致：\n得到：\n%s\n期望：\n%s", seed, got, want)
			}
		})
	}
}
