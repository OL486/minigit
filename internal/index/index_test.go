package index

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadMissingIsEmpty(t *testing.T) {
	dir := t.TempDir()
	idx, err := Read(filepath.Join(dir, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	if idx.Version != 1 || len(idx.Entries) != 0 {
		t.Fatalf("缺失的索引应返回空索引：%+v", idx)
	}
}

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	idx, err := Read(gitDir)
	if err != nil {
		t.Fatal(err)
	}
	idx.Upsert(Entry{Path: "b.txt", SHA: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Mode: "100644", Size: 3, MtimeSec: 100, MtimeNsec: 200})
	idx.Upsert(Entry{Path: "a.txt", SHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Mode: "100644", Size: 1, MtimeSec: 10, MtimeNsec: 20})
	idx.Upsert(Entry{Path: "src/c.go", SHA: "cccccccccccccccccccccccccccccccccccccccc", Mode: "100755", Size: 9, MtimeSec: 30, MtimeNsec: 40})
	if err := idx.Write(gitDir); err != nil {
		t.Fatal(err)
	}
	// 读回来：条目按路径排序，内容一致。
	got, err := Read(gitDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 3 {
		t.Fatalf("期望 3 条，实际 %d", len(got.Entries))
	}
	want := []string{"a.txt", "b.txt", "src/c.go"}
	for i, p := range want {
		if got.Entries[i].Path != p {
			t.Fatalf("第 %d 条期望 %s，实际 %s", i, p, got.Entries[i].Path)
		}
	}
	if e, ok := got.Get("src/c.go"); !ok || e.Mode != "100755" || e.Size != 9 {
		t.Fatalf("Get src/c.go 不对：%+v %v", e, ok)
	}
	if _, ok := got.Get("nope.txt"); ok {
		t.Fatal("不存在的路径应查不到")
	}
}

func TestUpsertUpdatesAndRemove(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	idx, _ := Read(gitDir)
	idx.Upsert(Entry{Path: "a.txt", SHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Mode: "100644", Size: 1})
	idx.Upsert(Entry{Path: "a.txt", SHA: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Mode: "100644", Size: 2})
	if len(idx.Entries) != 1 {
		t.Fatalf("同路径 Upsert 应更新而不是新增：%d 条", len(idx.Entries))
	}
	if e, _ := idx.Get("a.txt"); e.SHA != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" || e.Size != 2 {
		t.Fatalf("Upsert 没更新内容：%+v", e)
	}
	if !idx.Remove("a.txt") {
		t.Fatal("Remove 应返回 true")
	}
	if len(idx.Entries) != 0 {
		t.Fatal("Remove 后应为空")
	}
	if idx.Remove("a.txt") {
		t.Fatal("删不存在的应返回 false")
	}
}

func TestCorruptIndex(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "index"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(gitDir); err == nil {
		t.Fatal("损坏的索引应报错")
	}
}
