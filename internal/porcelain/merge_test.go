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
	"time"

	"github.com/OL486/minigit/internal/merge"
	"github.com/OL486/minigit/internal/plumbing"
	"github.com/OL486/minigit/internal/refs"
)

// commitFile 写文件、暂存、提交，返回 commit SHA（branch_test.go 的辅助函数）。
// 这里复用它。

// treeSHA 读 HEAD 的 tree SHA（用真 git 读，minigit 仓库 git 能直接读）。
func treeSHA(t *testing.T, git, work string) string {
	t.Helper()
	return runGit(t, git, work, nil, "rev-parse", "HEAD^{tree}")
}

// parentCount 读 HEAD 的 parent 个数。
func parentCount(t *testing.T, gitDir, sha string) int {
	t.Helper()
	c, err := readCommit(gitDir, sha)
	if err != nil {
		t.Fatal(err)
	}
	return len(c.Parents)
}

// mergeString 调 Merge 函数，返回输出与错误。
func mergeString(t *testing.T, gitDir, work, branch string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	err := Merge(gitDir, work, branch, "", &buf)
	return buf.String(), err
}

// baseScenario 建一个分叉仓库：base 提交后，feature 改 a.txt，
// main 改 a.txt。返回时停在 main 分支。
func baseScenario(t *testing.T, base, ours, theirs string) (work, gitDir string) {
	t.Helper()
	work, gitDir = setupRepo(t)
	writeFile(t, filepath.Join(work, "a.txt"), base)
	if err := Add(gitDir, work, []string{filepath.Join(work, "a.txt")}); err != nil {
		t.Fatal(err)
	}
	if _, err := Commit(gitDir, "base"); err != nil {
		t.Fatal(err)
	}
	if err := CreateBranch(gitDir, "feature", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Checkout(gitDir, work, "feature"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(work, "a.txt"), theirs)
	if err := Add(gitDir, work, []string{filepath.Join(work, "a.txt")}); err != nil {
		t.Fatal(err)
	}
	if _, err := Commit(gitDir, "feature change"); err != nil {
		t.Fatal(err)
	}
	if _, err := Checkout(gitDir, work, "main"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(work, "a.txt"), ours)
	if err := Add(gitDir, work, []string{filepath.Join(work, "a.txt")}); err != nil {
		t.Fatal(err)
	}
	if _, err := Commit(gitDir, "main change"); err != nil {
		t.Fatal(err)
	}
	return work, gitDir
}

// gitScenario 用真 git 建一个和 baseScenario 内容完全相同的分叉仓库。
func gitScenario(t *testing.T, git, base, ours, theirs string) (work string) {
	t.Helper()
	dir := t.TempDir()
	work = filepath.Join(dir, "gitrepo")
	runGit(t, git, dir, nil, "init", "-b", "main", work)
	env := []string{"GIT_AUTHOR_NAME=Test User", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test User", "GIT_COMMITTER_EMAIL=test@example.com"}
	writeFile(t, filepath.Join(work, "a.txt"), base)
	runGit(t, git, work, env, "add", "a.txt")
	runGit(t, git, work, env, "commit", "-m", "base")
	runGit(t, git, work, env, "branch", "feature")
	runGit(t, git, work, env, "checkout", "feature")
	writeFile(t, filepath.Join(work, "a.txt"), theirs)
	runGit(t, git, work, env, "add", "a.txt")
	runGit(t, git, work, env, "commit", "-m", "feature change")
	runGit(t, git, work, env, "checkout", "main")
	writeFile(t, filepath.Join(work, "a.txt"), ours)
	runGit(t, git, work, env, "add", "a.txt")
	runGit(t, git, work, env, "commit", "-m", "main change")
	return work
}

// runGitCode 在 dir 里跑 git 命令，返回输出与 exit 码（带身份环境变量）。
func runGitCode(t *testing.T, git, dir string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(git, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test User", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test User", "GIT_COMMITTER_EMAIL=test@example.com")
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
	return out.String() + errBuf.String(), code
}

func TestMergeFastForward(t *testing.T) {
	work, gitDir := setupRepo(t)
	commitFile(t, work, gitDir, "a.txt", "v1\n", "c1")
	if err := CreateBranch(gitDir, "ff", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Checkout(gitDir, work, "ff"); err != nil {
		t.Fatal(err)
	}
	commitFile(t, work, gitDir, "a.txt", "v2\n", "c2")
	// 再加一个新文件，验证 fast-forward 也会带过来。
	sha3 := commitFile(t, work, gitDir, "b.txt", "new\n", "c3")
	if _, err := Checkout(gitDir, work, "main"); err != nil {
		t.Fatal(err)
	}
	out, err := mergeString(t, gitDir, work, "ff")
	if err != nil {
		t.Fatalf("fast-forward 合并失败：%v", err)
	}
	if !strings.Contains(out, "Fast-forward") {
		t.Fatalf("fast-forward 应打印 Fast-forward，得到：%s", out)
	}
	head, err := refs.ResolveRef(gitDir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if head != sha3 {
		t.Fatalf("fast-forward 后 HEAD 应为 %s，实际 %s", sha3[:7], head[:7])
	}
	if got := readWorkFile(t, work, "a.txt"); got != "v2\n" {
		t.Fatalf("fast-forward 后 a.txt 应为 v2，实际 %q", got)
	}
	if got := readWorkFile(t, work, "b.txt"); got != "new\n" {
		t.Fatalf("fast-forward 后 b.txt 应存在，实际 %q", got)
	}
	// 不是合并提交：只有一个 parent。
	if n := parentCount(t, gitDir, head); n != 1 {
		t.Fatalf("fast-forward 不应产生合并提交，parent 数 %d", n)
	}
}

func TestMergeCleanThreeWay(t *testing.T) {
	git := requireGit(t)
	base := "l1\nl2\nl3\nl4\nl5\n"
	ours := "l1\nL2\nl3\nl4\nl5\n"   // 改第 2 行
	theirs := "l1\nl2\nl3\nL4\nl5\n" // 改第 4 行
	work, gitDir := baseScenario(t, base, ours, theirs)
	out, err := mergeString(t, gitDir, work, "feature")
	if err != nil {
		t.Fatalf("干净合并失败：%v", err)
	}
	if !strings.Contains(out, "Merge made by minigit.") {
		t.Fatalf("干净合并应打印 Merge made by minigit.，得到：%s", out)
	}
	if got := readWorkFile(t, work, "a.txt"); got != "l1\nL2\nl3\nL4\nl5\n" {
		t.Fatalf("合并后内容不对：%q", got)
	}
	head, err := refs.ResolveRef(gitDir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if n := parentCount(t, gitDir, head); n != 2 {
		t.Fatalf("合并提交应有 2 个 parent，实际 %d", n)
	}
	c, err := readCommit(gitDir, head)
	if err != nil {
		t.Fatal(err)
	}
	// 注意：Parse 返回的信息带 commit 对象末尾的换行符。
	if strings.TrimSpace(c.Message) != "Merge branch 'feature'" {
		t.Fatalf("默认合并信息不对：%q", c.Message)
	}
	// 同样场景用真 git 跑一遍，tree SHA 应相同。
	gitWork := gitScenario(t, git, base, ours, theirs)
	if _, code := runGitCode(t, git, gitWork, "merge", "--no-edit", "-m", "Merge branch 'feature'", "feature"); code != 0 {
		t.Fatal("git 的干净合并应成功")
	}
	if got, want := treeSHA(t, git, work), treeSHA(t, git, gitWork); got != want {
		t.Fatalf("干净合并的 tree SHA 与 git 不同：minigit %s，git %s", got, want)
	}
	// git fsck 直接通过。
	if _, code := runGitCode(t, git, work, "fsck"); code != 0 {
		t.Fatal("git fsck 在 minigit 合并后的仓库上应通过")
	}
}

func TestMergeConflictResolve(t *testing.T) {
	git := requireGit(t)
	base := "l1\nl2\nl3\n"
	ours := "l1\nOURS\nl3\n"
	theirs := "l1\nTHEIRS\nl3\n"
	work, gitDir := baseScenario(t, base, ours, theirs)
	out, err := mergeString(t, gitDir, work, "feature")
	if err == nil {
		t.Fatal("双方改同一行应冲突，merge 应报错")
	}
	if !strings.Contains(out, "a.txt") {
		t.Fatalf("冲突输出应列出冲突文件，得到：%s", out)
	}
	content := readWorkFile(t, work, "a.txt")
	if !strings.Contains(content, "<<<<<<< ours") || !strings.Contains(content, "=======") || !strings.Contains(content, ">>>>>>> theirs") {
		t.Fatalf("冲突文件应含标记，实际：\n%s", content)
	}
	if got, err := merge.ReadMergeHead(gitDir); err != nil || got == "" {
		t.Fatalf("冲突后 MERGE_HEAD 应存在：%q, %v", got, err)
	}
	// status 提示正在合并。
	st, err := ComputeStatus(gitDir, work)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Merging || st.MergeBranch != "feature" {
		t.Fatalf("status 应报告正在合并 feature，得到 Merging=%v Branch=%q", st.Merging, st.MergeBranch)
	}
	var buf bytes.Buffer
	PrintStatus(&buf, st)
	if !strings.Contains(buf.String(), "正在合并 feature（有冲突未解决）") {
		t.Fatalf("status 输出应含合并提示，得到：\n%s", buf.String())
	}
	// 没解决就 commit：应被拒绝。
	if _, err := Commit(gitDir, "should fail"); err == nil {
		t.Fatal("带冲突标记提交应被拒绝")
	}
	// 手动解决：取 ours 的第 2 行 + theirs 的第 3 行（l3 两边一样）。
	writeFile(t, filepath.Join(work, "a.txt"), "l1\nOURS\nl3\n")
	if err := Add(gitDir, work, []string{filepath.Join(work, "a.txt")}); err != nil {
		t.Fatal(err)
	}
	// 不带 -m 提交：信息应取自 MERGE_MSG（这条在 main.go 层；
	// 这里直接传信息，验证 2 个 parent 与状态清理）。
	sha, err := Commit(gitDir, "Merge branch 'feature'")
	if err != nil {
		t.Fatalf("解决冲突后提交失败：%v", err)
	}
	if n := parentCount(t, gitDir, sha); n != 2 {
		t.Fatalf("解决冲突后的提交应有 2 个 parent，实际 %d", n)
	}
	if got, err := merge.ReadMergeHead(gitDir); err != nil || got != "" {
		t.Fatalf("提交后 MERGE_HEAD 应被清除：%q, %v", got, err)
	}
	// 同样场景用真 git 跑一遍：同样解决，tree SHA 应相同。
	gitWork := gitScenario(t, git, base, ours, theirs)
	if _, code := runGitCode(t, git, gitWork, "merge", "--no-edit", "-m", "Merge branch 'feature'", "feature"); code != 1 {
		t.Fatalf("git 在该场景应冲突，exit %d", code)
	}
	writeFile(t, filepath.Join(gitWork, "a.txt"), "l1\nOURS\nl3\n")
	if _, code := runGitCode(t, git, gitWork, "add", "a.txt"); code != 0 {
		t.Fatal("git add 解决后的文件应成功")
	}
	if _, code := runGitCode(t, git, gitWork, "commit", "-m", "Merge branch 'feature'"); code != 0 {
		t.Fatal("git 解决冲突后提交应成功")
	}
	if got, want := treeSHA(t, git, work), treeSHA(t, git, gitWork); got != want {
		t.Fatalf("解决冲突后的 tree SHA 与 git 不同：minigit %s，git %s", got, want)
	}
}

func TestMergeDeleteVsModify(t *testing.T) {
	work, gitDir := setupRepo(t)
	commitFile(t, work, gitDir, "a.txt", "l1\nl2\nl3\n", "base")
	commitFile(t, work, gitDir, "b.txt", "keep\n", "base2")
	if err := CreateBranch(gitDir, "feature", ""); err != nil {
		t.Fatal(err)
	}
	// feature：改 a.txt。
	if _, err := Checkout(gitDir, work, "feature"); err != nil {
		t.Fatal(err)
	}
	commitFile(t, work, gitDir, "a.txt", "l1\nCHANGED\nl3\n", "feature change")
	// main：删 a.txt。
	if _, err := Checkout(gitDir, work, "main"); err != nil {
		t.Fatal(err)
	}
	// 手动删文件并用 add 暂存删除（M5 新行为），再提交。
	if err := os.Remove(filepath.Join(work, "a.txt")); err != nil {
		t.Fatal(err)
	}
	if err := Add(gitDir, work, []string{filepath.Join(work, "a.txt")}); err != nil {
		t.Fatal(err)
	}
	if _, err := Commit(gitDir, "delete a.txt"); err != nil {
		t.Fatal(err)
	}
	// 合并：一方删除一方修改 → 冲突。
	if _, err := mergeString(t, gitDir, work, "feature"); err == nil {
		t.Fatal("删除 vs 修改应冲突")
	}
	content := readWorkFile(t, work, "a.txt")
	if !strings.Contains(content, "<<<<<<< ours") || !strings.Contains(content, "CHANGED") {
		t.Fatalf("删除 vs 修改的冲突文件应含修改方内容与标记，实际：\n%s", content)
	}
	// 按接受删除解决：删文件、add、commit。
	if err := os.Remove(filepath.Join(work, "a.txt")); err != nil {
		t.Fatal(err)
	}
	if err := Add(gitDir, work, []string{filepath.Join(work, "a.txt")}); err != nil {
		t.Fatal(err)
	}
	sha, err := Commit(gitDir, "Merge branch 'feature'")
	if err != nil {
		t.Fatalf("接受删除后提交失败：%v", err)
	}
	if n := parentCount(t, gitDir, sha); n != 2 {
		t.Fatalf("合并提交应有 2 个 parent，实际 %d", n)
	}
	if _, err := os.Stat(filepath.Join(work, "a.txt")); !os.IsNotExist(err) {
		t.Fatal("接受删除后 a.txt 应不存在")
	}
}

func TestMergeBothAdded(t *testing.T) {
	work, gitDir := setupRepo(t)
	commitFile(t, work, gitDir, "a.txt", "base\n", "base")
	if err := CreateBranch(gitDir, "feature", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Checkout(gitDir, work, "feature"); err != nil {
		t.Fatal(err)
	}
	commitFile(t, work, gitDir, "b.txt", "theirs\n", "add b on feature")
	if _, err := Checkout(gitDir, work, "main"); err != nil {
		t.Fatal(err)
	}
	commitFile(t, work, gitDir, "b.txt", "ours\n", "add b on main")
	// 双方都新增 b.txt、内容不同 → 冲突。
	if _, err := mergeString(t, gitDir, work, "feature"); err == nil {
		t.Fatal("双方新增同一文件、内容不同应冲突")
	}
	content := readWorkFile(t, work, "b.txt")
	if !strings.Contains(content, "<<<<<<< ours") || !strings.Contains(content, "ours") || !strings.Contains(content, "theirs") {
		t.Fatalf("双方新增冲突文件不对：\n%s", content)
	}
	// 解决：取 theirs 的内容。
	writeFile(t, filepath.Join(work, "b.txt"), "theirs\n")
	if err := Add(gitDir, work, []string{filepath.Join(work, "b.txt")}); err != nil {
		t.Fatal(err)
	}
	sha, err := Commit(gitDir, "Merge branch 'feature'")
	if err != nil {
		t.Fatalf("解决后提交失败：%v", err)
	}
	if n := parentCount(t, gitDir, sha); n != 2 {
		t.Fatalf("合并提交应有 2 个 parent，实际 %d", n)
	}
}

func TestMergeBothAddedSameContent(t *testing.T) {
	// 双方都新增同一文件、内容相同 → 干净合并，文件要写进去。
	work, gitDir := setupRepo(t)
	commitFile(t, work, gitDir, "a.txt", "base\n", "base")
	if err := CreateBranch(gitDir, "feature", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Checkout(gitDir, work, "feature"); err != nil {
		t.Fatal(err)
	}
	commitFile(t, work, gitDir, "b.txt", "same\n", "add b on feature")
	if _, err := Checkout(gitDir, work, "main"); err != nil {
		t.Fatal(err)
	}
	commitFile(t, work, gitDir, "b.txt", "same\n", "add b on main")
	if _, err := mergeString(t, gitDir, work, "feature"); err != nil {
		t.Fatalf("双方新增相同内容应干净合并：%v", err)
	}
	if got := readWorkFile(t, work, "b.txt"); got != "same\n" {
		t.Fatalf("干净合并后 b.txt 应存在，实际 %q", got)
	}
}

func TestMergeAlreadyUpToDate(t *testing.T) {
	work, gitDir := setupRepo(t)
	commitFile(t, work, gitDir, "a.txt", "v1\n", "c1")
	// 合并自己。
	out, err := mergeString(t, gitDir, work, "main")
	if err != nil {
		t.Fatalf("合并自己不应报错：%v", err)
	}
	if !strings.Contains(out, "Already up to date.") {
		t.Fatalf("合并自己应打印 Already up to date.，得到：%s", out)
	}
	// 合并祖先分支。
	if err := CreateBranch(gitDir, "old", ""); err != nil {
		t.Fatal(err)
	}
	commitFile(t, work, gitDir, "a.txt", "v2\n", "c2")
	out, err = mergeString(t, gitDir, work, "old")
	if err != nil {
		t.Fatalf("合并祖先不应报错：%v", err)
	}
	if !strings.Contains(out, "Already up to date.") {
		t.Fatalf("合并祖先应打印 Already up to date.，得到：%s", out)
	}
}

func TestMergeDirtyRefused(t *testing.T) {
	work, gitDir := setupRepo(t)
	commitFile(t, work, gitDir, "a.txt", "v1\n", "c1")
	if err := CreateBranch(gitDir, "feature", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Checkout(gitDir, work, "feature"); err != nil {
		t.Fatal(err)
	}
	commitFile(t, work, gitDir, "a.txt", "v2\n", "c2")
	if _, err := Checkout(gitDir, work, "main"); err != nil {
		t.Fatal(err)
	}
	// 脏工作区：改了 a.txt 没提交。
	writeFile(t, filepath.Join(work, "a.txt"), "dirty\n")
	if _, err := mergeString(t, gitDir, work, "feature"); err == nil {
		t.Fatal("脏工作区应拒绝合并")
	} else if !strings.Contains(err.Error(), "未提交的修改") {
		t.Fatalf("脏工作区错误信息不对：%v", err)
	}
	// 已暂存的修改也应拒绝。
	writeFile(t, filepath.Join(work, "a.txt"), "v1\n")
	if err := Add(gitDir, work, []string{filepath.Join(work, "a.txt")}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(work, "a.txt"), "staged-dirty\n")
	if err := Add(gitDir, work, []string{filepath.Join(work, "a.txt")}); err != nil {
		t.Fatal(err)
	}
	if _, err := mergeString(t, gitDir, work, "feature"); err == nil {
		t.Fatal("已暂存的修改应拒绝合并")
	}
}

func TestMergeNoCommonAncestor(t *testing.T) {
	work, gitDir := setupRepo(t)
	commitFile(t, work, gitDir, "a.txt", "v1\n", "c1")
	// 造一个无关根提交：直接用 plumbing 写一个无 parent 的 commit，
	// 再建分支指向它。
	headSHA, err := refs.ResolveRef(gitDir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	headCommit, err := readCommit(gitDir, headSHA)
	if err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	author := plumbing.FormatIdentity("Test", "test@example.com", when)
	other, err := plumbing.CommitTree(gitDir, headCommit.Tree, nil, author, author, "unrelated root")
	if err != nil {
		t.Fatal(err)
	}
	if err := refs.UpdateRef(gitDir, "refs/heads/other", other); err != nil {
		t.Fatal(err)
	}
	if _, err := mergeString(t, gitDir, work, "other"); err == nil {
		t.Fatal("无关历史应拒绝合并")
	} else if !strings.Contains(err.Error(), "没有共同祖先") {
		t.Fatalf("无关历史错误信息不对：%v", err)
	}
}

// ---- 差分测试：同一场景 minigit merge vs git merge ----

// markerRe 把冲突标记的标签归一化：git 用 HEAD/<分支名>，
// minigit 用 ours/theirs。
var markerRe = regexp.MustCompile(`(?m)^(<<<<<<< ).*$`)

func normalizeMarkers(s, branch string) string {
	s = markerRe.ReplaceAllString(s, "${1}X")
	s = strings.ReplaceAll(s, ">>>>>>> "+branch, ">>>>>>> X")
	s = strings.ReplaceAll(s, ">>>>>>> theirs", ">>>>>>> X")
	return s
}

// scenario 是差分测试的一个随机场景：base 12 行，
// ours/theirs 各改一次（或新增 b.txt）。
type scenario struct {
	oursLines   []string // 合并后 ours 的 a.txt 内容（nil = 不动）
	theirsLines []string
	oursB       []byte // ours 新增的 b.txt（nil = 不新增）
	theirsB     []byte
	expectClean bool // 预期是否干净合并（由生成器算出）
}

func baseLines() []string {
	var out []string
	for i := 1; i <= 12; i++ {
		out = append(out, fmt.Sprintf("L%02d", i))
	}
	return out
}

// genScenario 按 seed 生成一个场景。改动区域用不相交或完全相同
// 的方式构造，避免 Myers 与 git xdiff 对齐方式不同导致的
// 歧义（插入点不落在对方改动边界上）。
func genScenario(seed int64) scenario {
	r := rand.New(rand.NewSource(seed))
	base := baseLines()
	kind := seed % 5
	var ours, theirs []string
	expectClean := true
	// 复制一份 base 再改。
	apply := func(lines []string, start, end int, repl []string) []string {
		out := append([]string{}, lines[:start]...)
		out = append(out, repl...)
		out = append(out, lines[end:]...)
		return out
	}
	switch kind {
	case 0: // 两边改不相交的行：干净。
		ours = apply(base, 1, 3, []string{"O1", "O2"})
		theirs = apply(base, 7, 9, []string{"T1", "T2"})
	case 1: // 两边改同一段、内容不同：冲突。
		ours = apply(base, 4, 7, []string{"O1", "O2", "O3"})
		theirs = apply(base, 4, 7, []string{"T1", "T2", "T3"})
		expectClean = false
	case 2: // 两边改同一段、内容相同：干净。
		ours = apply(base, 4, 7, []string{"S1", "S2", "S3"})
		theirs = apply(base, 4, 7, []string{"S1", "S2", "S3"})
	case 3: // ours 删一段，theirs 改同一段：冲突。
		ours = apply(base, 4, 7, nil)
		theirs = apply(base, 4, 7, []string{"T1", "T2", "T3"})
		expectClean = false
	case 4: // 同一位置插入不同内容：冲突；不同位置插入：干净。
		if r.Intn(2) == 0 {
			ours = append(append(append([]string{}, base[:6]...), "OI1", "OI2"), base[6:]...)
			theirs = append(append(append([]string{}, base[:6]...), "TI1", "TI2"), base[6:]...)
			expectClean = false
		} else {
			ours = append(append(append([]string{}, base[:2]...), "OI1"), base[2:]...)
			theirs = append(append(append([]string{}, base[:9]...), "TI1"), base[9:]...)
		}
	}
	sc := scenario{oursLines: ours, theirsLines: theirs, expectClean: expectClean}
	// 有时双方都新增 b.txt：内容相同→不影响结果；不同→冲突。
	if seed%3 == 0 {
		if seed%2 == 0 {
			sc.oursB = []byte("shared\n")
			sc.theirsB = []byte("shared\n")
		} else {
			sc.oursB = []byte("ours-b\n")
			sc.theirsB = []byte("theirs-b\n")
			sc.expectClean = false
		}
	}
	return sc
}

func linesToContent(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// buildScenarioMinigit 按场景建 minigit 分叉仓库，停在 main。
func buildScenarioMinigit(t *testing.T, sc scenario) (work, gitDir string) {
	t.Helper()
	work, gitDir = setupRepo(t)
	writeFile(t, filepath.Join(work, "a.txt"), linesToContent(baseLines()))
	if err := Add(gitDir, work, []string{filepath.Join(work, "a.txt")}); err != nil {
		t.Fatal(err)
	}
	if _, err := Commit(gitDir, "base"); err != nil {
		t.Fatal(err)
	}
	if err := CreateBranch(gitDir, "feature", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Checkout(gitDir, work, "feature"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(work, "a.txt"), linesToContent(sc.theirsLines))
	if err := Add(gitDir, work, []string{filepath.Join(work, "a.txt")}); err != nil {
		t.Fatal(err)
	}
	if sc.theirsB != nil {
		writeFile(t, filepath.Join(work, "b.txt"), string(sc.theirsB))
		if err := Add(gitDir, work, []string{filepath.Join(work, "b.txt")}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Commit(gitDir, "feature change"); err != nil {
		t.Fatal(err)
	}
	if _, err := Checkout(gitDir, work, "main"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(work, "a.txt"), linesToContent(sc.oursLines))
	if err := Add(gitDir, work, []string{filepath.Join(work, "a.txt")}); err != nil {
		t.Fatal(err)
	}
	if sc.oursB != nil {
		writeFile(t, filepath.Join(work, "b.txt"), string(sc.oursB))
		if err := Add(gitDir, work, []string{filepath.Join(work, "b.txt")}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Commit(gitDir, "main change"); err != nil {
		t.Fatal(err)
	}
	return work, gitDir
}

// buildScenarioGit 按场景建真 git 分叉仓库，停在 main。
func buildScenarioGit(t *testing.T, git string, sc scenario) (work string) {
	t.Helper()
	dir := t.TempDir()
	work = filepath.Join(dir, "gitrepo")
	runGit(t, git, dir, nil, "init", "-b", "main", work)
	env := []string{"GIT_AUTHOR_NAME=Test User", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test User", "GIT_COMMITTER_EMAIL=test@example.com"}
	writeFile(t, filepath.Join(work, "a.txt"), linesToContent(baseLines()))
	runGit(t, git, work, env, "add", "a.txt")
	runGit(t, git, work, env, "commit", "-m", "base")
	runGit(t, git, work, env, "branch", "feature")
	runGit(t, git, work, env, "checkout", "feature")
	writeFile(t, filepath.Join(work, "a.txt"), linesToContent(sc.theirsLines))
	runGit(t, git, work, env, "add", "a.txt")
	if sc.theirsB != nil {
		writeFile(t, filepath.Join(work, "b.txt"), string(sc.theirsB))
		runGit(t, git, work, env, "add", "b.txt")
	}
	runGit(t, git, work, env, "commit", "-m", "feature change")
	runGit(t, git, work, env, "checkout", "main")
	writeFile(t, filepath.Join(work, "a.txt"), linesToContent(sc.oursLines))
	runGit(t, git, work, env, "add", "a.txt")
	if sc.oursB != nil {
		writeFile(t, filepath.Join(work, "b.txt"), string(sc.oursB))
		runGit(t, git, work, env, "add", "b.txt")
	}
	runGit(t, git, work, env, "commit", "-m", "main change")
	return work
}

func TestMergeDifferentialRandom(t *testing.T) {
	git := requireGit(t)
	const n = 25
	for seed := int64(0); seed < n; seed++ {
		sc := genScenario(seed)
		work, gitDir := buildScenarioMinigit(t, sc)
		gitWork := buildScenarioGit(t, git, sc)

		_, miniErr := mergeString(t, gitDir, work, "feature")
		miniClean := miniErr == nil
		_, gitCode := runGitCode(t, git, gitWork, "merge", "--no-edit", "-m", "Merge branch 'feature'", "feature")
		gitClean := gitCode == 0

		if miniClean != sc.expectClean {
			t.Fatalf("seed %d：minigit 合并是否干净与预期不符：干净=%v，预期干净=%v", seed, miniClean, sc.expectClean)
		}
		if miniClean != gitClean {
			t.Fatalf("seed %d：minigit 与 git 的合并结果不一致：minigit 干净=%v，git 干净=%v", seed, miniClean, gitClean)
		}
		if miniClean {
			// 干净合并：tree SHA 应相同。
			if got, want := treeSHA(t, git, work), treeSHA(t, git, gitWork); got != want {
				t.Fatalf("seed %d：干净合并的 tree SHA 不同：minigit %s，git %s", seed, got, want)
			}
			continue
		}
		// 冲突：冲突文件内容归一化标记标签后应相同。
		for _, f := range []string{"a.txt", "b.txt"} {
			miniData, miniErr := os.ReadFile(filepath.Join(work, f))
			gitData, gitErr := os.ReadFile(filepath.Join(gitWork, f))
			if os.IsNotExist(miniErr) && os.IsNotExist(gitErr) {
				continue
			}
			if (miniErr == nil) != (gitErr == nil) {
				t.Fatalf("seed %d：%s 在一边存在、一边不存在", seed, f)
			}
			if miniErr != nil || gitErr != nil {
				t.Fatalf("seed %d：读 %s 失败：%v / %v", seed, f, miniErr, gitErr)
			}
			if got, want := normalizeMarkers(string(miniData), "feature"), normalizeMarkers(string(gitData), "feature"); got != want {
				t.Fatalf("seed %d：%s 冲突内容与 git 不同：\nminigit：\n%s\ngit：\n%s", seed, f, got, want)
			}
		}
	}
}
