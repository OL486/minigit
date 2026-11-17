package porcelain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/OL486/minigit/internal/refs"
)

func readWorkFile(t *testing.T, work, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(work, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func mustBranch(t *testing.T, gitDir string) string {
	t.Helper()
	b, err := refs.CurrentBranch(gitDir)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestCheckoutSwitchRestoresContent(t *testing.T) {
	work, gitDir := setupRepo(t)
	commitFile(t, work, gitDir, "a.txt", "v1\n", "c1")
	if err := CreateBranch(gitDir, "feature", ""); err != nil {
		t.Fatal(err)
	}
	// 切到 feature，改文件提交。
	switched, err := Checkout(gitDir, work, "feature")
	if err != nil || !switched {
		t.Fatalf("checkout feature 应切换：switched=%v err=%v", switched, err)
	}
	if mustBranch(t, gitDir) != "feature" {
		t.Fatalf("HEAD 应指向 feature，实际 %s", mustBranch(t, gitDir))
	}
	commitFile(t, work, gitDir, "a.txt", "v2-feature\n", "c2-feature")
	// 切回 main：内容还原成 v1。
	switched, err = Checkout(gitDir, work, "main")
	if err != nil || !switched {
		t.Fatalf("checkout main 应切换：switched=%v err=%v", switched, err)
	}
	if got := readWorkFile(t, work, "a.txt"); got != "v1\n" {
		t.Fatalf("切回 main 后 a.txt 应为 v1，实际 %q", got)
	}
	if mustBranch(t, gitDir) != "main" {
		t.Fatalf("HEAD 应指向 main，实际 %s", mustBranch(t, gitDir))
	}
	st, err := ComputeStatus(gitDir, work)
	if err != nil {
		t.Fatal(err)
	}
	if !st.IsClean() {
		t.Fatalf("切回后应干净：%+v", st)
	}
	// 再切到 feature：v2 回来。
	if _, err := Checkout(gitDir, work, "feature"); err != nil {
		t.Fatal(err)
	}
	if got := readWorkFile(t, work, "a.txt"); got != "v2-feature\n" {
		t.Fatalf("切到 feature 后 a.txt 应为 v2-feature，实际 %q", got)
	}
	// 切当前分支：什么都不做。
	switched, err = Checkout(gitDir, work, "feature")
	if err != nil || switched {
		t.Fatalf("切当前分支应返回 switched=false：%v %v", switched, err)
	}
}

func TestCheckoutNewFileOnlyOnBranch(t *testing.T) {
	work, gitDir := setupRepo(t)
	commitFile(t, work, gitDir, "a.txt", "v1\n", "c1")
	if err := CreateBranch(gitDir, "feature", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Checkout(gitDir, work, "feature"); err != nil {
		t.Fatal(err)
	}
	commitFile(t, work, gitDir, "only.txt", "only on feature\n", "c2")
	// 切回 main：only.txt 消失。
	if _, err := Checkout(gitDir, work, "main"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(work, "only.txt")); !os.IsNotExist(err) {
		t.Fatal("切回 main 后 only.txt 应消失")
	}
	// 再切回来：only.txt 回来。
	if _, err := Checkout(gitDir, work, "feature"); err != nil {
		t.Fatal(err)
	}
	if got := readWorkFile(t, work, "only.txt"); got != "only on feature\n" {
		t.Fatalf("切回 feature 后 only.txt 不对：%q", got)
	}
}

func TestCheckoutNestedDirsPruned(t *testing.T) {
	work, gitDir := setupRepo(t)
	commitFile(t, work, gitDir, "a.txt", "v1\n", "c1")
	if err := CreateBranch(gitDir, "feature", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Checkout(gitDir, work, "feature"); err != nil {
		t.Fatal(err)
	}
	commitFile(t, work, gitDir, "sub/deep/c.txt", "deep\n", "c2")
	if _, err := Checkout(gitDir, work, "main"); err != nil {
		t.Fatal(err)
	}
	// 嵌套目录应随文件一起被清理（空目录不留）。
	if _, err := os.Stat(filepath.Join(work, "sub")); !os.IsNotExist(err) {
		t.Fatal("切回 main 后 sub 目录应被清理")
	}
	if _, err := Checkout(gitDir, work, "feature"); err != nil {
		t.Fatal(err)
	}
	if got := readWorkFile(t, work, "sub/deep/c.txt"); got != "deep\n" {
		t.Fatalf("切回 feature 后 sub/deep/c.txt 不对：%q", got)
	}
}

func TestCheckoutBlockedByDirtyTracked(t *testing.T) {
	work, gitDir := setupRepo(t)
	commitFile(t, work, gitDir, "a.txt", "v1\n", "c1")
	if err := CreateBranch(gitDir, "feature", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Checkout(gitDir, work, "feature"); err != nil {
		t.Fatal(err)
	}
	commitFile(t, work, gitDir, "a.txt", "v2-feature\n", "c2")
	if _, err := Checkout(gitDir, work, "main"); err != nil {
		t.Fatal(err)
	}
	// 工作区改了 a.txt（未提交），目标分支里 a.txt 内容不同：挡住。
	writeFile(t, filepath.Join(work, "a.txt"), "v3-dirty\n")
	if _, err := Checkout(gitDir, work, "feature"); err == nil {
		t.Fatal("脏切换应被挡住")
	} else if !strings.Contains(err.Error(), "a.txt") {
		t.Fatalf("报错里应写清是哪个文件：%v", err)
	}
	// HEAD 没动，工作区没动。
	if mustBranch(t, gitDir) != "main" {
		t.Fatalf("被挡住后 HEAD 应还在 main，实际 %s", mustBranch(t, gitDir))
	}
	if got := readWorkFile(t, work, "a.txt"); got != "v3-dirty\n" {
		t.Fatalf("被挡住后工作区不应被动：%q", got)
	}
}

func TestCheckoutCarriesIdenticalModification(t *testing.T) {
	work, gitDir := setupRepo(t)
	commitFile(t, work, gitDir, "a.txt", "v1\n", "c1")
	if err := CreateBranch(gitDir, "feature", ""); err != nil {
		t.Fatal(err)
	}
	// 两边 a.txt 都是 v1，工作区改成 v2（未提交）：切换应放行，修改带走。
	writeFile(t, filepath.Join(work, "a.txt"), "v2-dirty\n")
	if _, err := Checkout(gitDir, work, "feature"); err != nil {
		t.Fatalf("内容相同的修改应带着走：%v", err)
	}
	if got := readWorkFile(t, work, "a.txt"); got != "v2-dirty\n" {
		t.Fatalf("切换后修改应保留：%q", got)
	}
	st, err := ComputeStatus(gitDir, work)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Modified) != 1 || st.Modified[0] != "a.txt" {
		t.Fatalf("切换后 status 应显示 a.txt 已修改：%+v", st)
	}
}

func TestCheckoutBlockedByUntracked(t *testing.T) {
	work, gitDir := setupRepo(t)
	commitFile(t, work, gitDir, "a.txt", "v1\n", "c1")
	if err := CreateBranch(gitDir, "feature", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Checkout(gitDir, work, "feature"); err != nil {
		t.Fatal(err)
	}
	commitFile(t, work, gitDir, "b.txt", "feature version\n", "c2")
	if _, err := Checkout(gitDir, work, "main"); err != nil {
		t.Fatal(err)
	}
	// 未追踪的 b.txt 内容与目标分支不同：挡住。
	writeFile(t, filepath.Join(work, "b.txt"), "other version\n")
	if _, err := Checkout(gitDir, work, "feature"); err == nil {
		t.Fatal("未追踪文件会被覆盖时应挡住")
	} else if !strings.Contains(err.Error(), "b.txt") {
		t.Fatalf("报错里应写清是哪个文件：%v", err)
	}
	if mustBranch(t, gitDir) != "main" {
		t.Fatalf("被挡住后 HEAD 应还在 main，实际 %s", mustBranch(t, gitDir))
	}
}

func TestCheckoutAllowsUntrackedIdentical(t *testing.T) {
	work, gitDir := setupRepo(t)
	commitFile(t, work, gitDir, "a.txt", "v1\n", "c1")
	if err := CreateBranch(gitDir, "feature", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Checkout(gitDir, work, "feature"); err != nil {
		t.Fatal(err)
	}
	commitFile(t, work, gitDir, "b.txt", "feature version\n", "c2")
	if _, err := Checkout(gitDir, work, "main"); err != nil {
		t.Fatal(err)
	}
	// 未追踪的 b.txt 内容恰好就是目标版本：放行，文件变成已追踪。
	writeFile(t, filepath.Join(work, "b.txt"), "feature version\n")
	if _, err := Checkout(gitDir, work, "feature"); err != nil {
		t.Fatalf("内容相同的未追踪文件应放行：%v", err)
	}
	st, err := ComputeStatus(gitDir, work)
	if err != nil {
		t.Fatal(err)
	}
	if !st.IsClean() {
		t.Fatalf("切换后应干净（文件已追踪）：%+v", st)
	}
}

func TestCheckoutBlockedByStaged(t *testing.T) {
	work, gitDir := setupRepo(t)
	commitFile(t, work, gitDir, "a.txt", "v1\n", "c1")
	if err := CreateBranch(gitDir, "feature", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Checkout(gitDir, work, "feature"); err != nil {
		t.Fatal(err)
	}
	commitFile(t, work, gitDir, "a.txt", "v2-feature\n", "c2")
	if _, err := Checkout(gitDir, work, "main"); err != nil {
		t.Fatal(err)
	}
	// 暂存了一个与两边都不同的版本：挡住。
	writeFile(t, filepath.Join(work, "a.txt"), "v3-staged\n")
	if err := Add(gitDir, work, []string{filepath.Join(work, "a.txt")}); err != nil {
		t.Fatal(err)
	}
	if _, err := Checkout(gitDir, work, "feature"); err == nil {
		t.Fatal("已暂存的修改会被覆盖时应挡住")
	}
	if mustBranch(t, gitDir) != "main" {
		t.Fatalf("被挡住后 HEAD 应还在 main，实际 %s", mustBranch(t, gitDir))
	}
}

func TestCheckoutFileDirConflict(t *testing.T) {
	work, gitDir := setupRepo(t)
	commitFile(t, work, gitDir, "a.txt", "v1\n", "c1")
	if err := CreateBranch(gitDir, "feature", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Checkout(gitDir, work, "feature"); err != nil {
		t.Fatal(err)
	}
	// feature 上 d 是个文件。
	commitFile(t, work, gitDir, "d", "file d\n", "c2")
	if _, err := Checkout(gitDir, work, "main"); err != nil {
		t.Fatal(err)
	}
	// main 上 d 是个未追踪目录：切到 feature 应报错，不能静默覆盖。
	writeFile(t, filepath.Join(work, "d", "inside.txt"), "inside\n")
	if _, err := Checkout(gitDir, work, "feature"); err == nil {
		t.Fatal("目录挡住文件写入时应报错")
	}
	if mustBranch(t, gitDir) != "main" {
		t.Fatalf("被挡住后 HEAD 应还在 main，实际 %s", mustBranch(t, gitDir))
	}
}

func TestCheckoutErrors(t *testing.T) {
	work, gitDir := setupRepo(t)
	commitFile(t, work, gitDir, "a.txt", "v1\n", "c1")
	// 不存在的分支。
	if _, err := Checkout(gitDir, work, "nope"); err == nil {
		t.Fatal("切不存在的分支应报错")
	} else if !strings.Contains(err.Error(), "nope") {
		t.Fatalf("报错里应有分支名：%v", err)
	}
	// checkout commit SHA：detached HEAD 不支持。
	sha, err := refs.ResolveRef(gitDir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Checkout(gitDir, work, sha); err == nil {
		t.Fatal("checkout commit SHA 应报错（不支持 detached HEAD）")
	} else if !strings.Contains(err.Error(), "detached") {
		t.Fatalf("报错里应说明 detached HEAD 不支持：%v", err)
	}
	// 切当前分支：不报错，switched=false。
	switched, err := Checkout(gitDir, work, "main")
	if err != nil || switched {
		t.Fatalf("切当前分支应返回 switched=false：%v %v", switched, err)
	}
}
