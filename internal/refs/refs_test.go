package refs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/OL486/minigit/internal/repo"
)

const testSHA = "0123456789abcdef0123456789abcdef01234567"

func setupRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitDir, _, err := repo.Init(filepath.Join(dir, "proj"))
	if err != nil {
		t.Fatal(err)
	}
	return gitDir
}

func TestHeadAndUpdateResolve(t *testing.T) {
	gitDir := setupRepo(t)
	// 还没有任何提交：HEAD 指向 main，但解析应报“不存在”。
	if _, err := ResolveRef(gitDir, "HEAD"); !IsNotFound(err) {
		t.Fatalf("未提交时 HEAD 应报引用不存在，实际 %v", err)
	}
	if b, err := CurrentBranch(gitDir); err != nil || b != "main" {
		t.Fatalf("当前分支应为 main：%q %v", b, err)
	}
	// 写 main 分支，再解析。
	if err := UpdateRef(gitDir, "HEAD", testSHA); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"HEAD", "refs/heads/main", "main"} {
		got, err := ResolveRef(gitDir, name)
		if err != nil || got != testSHA {
			t.Fatalf("ResolveRef(%q)：期望 %s，实际 %s %v", name, testSHA, got, err)
		}
	}
	// 引用文件内容是 SHA + 换行。
	data, err := os.ReadFile(filepath.Join(gitDir, "refs", "heads", "main"))
	if err != nil || string(data) != testSHA+"\n" {
		t.Fatalf("引用文件内容不对：%q %v", data, err)
	}
	// 无效 SHA 拒绝写入。
	if err := UpdateRef(gitDir, "HEAD", "xyz"); err == nil {
		t.Fatal("无效 SHA 应拒绝写入")
	}
}

func TestListBranches(t *testing.T) {
	gitDir := setupRepo(t)
	if err := UpdateRef(gitDir, "refs/heads/main", testSHA); err != nil {
		t.Fatal(err)
	}
	if err := UpdateRef(gitDir, "refs/heads/feature", testSHA); err != nil {
		t.Fatal(err)
	}
	branches, err := ListBranches(gitDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 2 || branches[0] != "feature" || branches[1] != "main" {
		t.Fatalf("分支列表不对：%v", branches)
	}
}

func TestPackedRefs(t *testing.T) {
	gitDir := setupRepo(t)
	packed := "# pack-refs with: peeled fully-peeled sorted\n" +
		testSHA + " refs/heads/packed\n" +
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa refs/heads/other\n"
	if err := os.WriteFile(filepath.Join(gitDir, "packed-refs"), []byte(packed), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveRef(gitDir, "packed")
	if err != nil || got != testSHA {
		t.Fatalf("packed-refs 里的分支应能解析：%s %v", got, err)
	}
	// 松散引用优先于 packed-refs。
	other := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if err := UpdateRef(gitDir, "refs/heads/packed", other); err != nil {
		t.Fatal(err)
	}
	got, err = ResolveRef(gitDir, "packed")
	if err != nil || got != other {
		t.Fatalf("松散引用应优先：%s %v", got, err)
	}
	branches, err := ListBranches(gitDir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, b := range branches {
		if b == "other" {
			found = true
		}
	}
	if !found {
		t.Fatalf("packed-refs 里的分支应出现在列表里：%v", branches)
	}
}
