package merge

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/OL486/minigit/internal/object"
	"github.com/OL486/minigit/internal/plumbing"
	"github.com/OL486/minigit/internal/repo"
)

func lines(s ...string) []string { return s }

// assertMerge 断言合并结果与冲突数。
func assertMerge(t *testing.T, base, ours, theirs, want []string, wantConflicts int) {
	t.Helper()
	got, conflicts := Merge(base, ours, theirs)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Merge 结果不对：\n得到 %v\n想要 %v", got, want)
	}
	if len(conflicts) != wantConflicts {
		t.Fatalf("冲突数不对：得到 %d，想要 %d（%v）", len(conflicts), wantConflicts, conflicts)
	}
}

// 两边改不同行：自动合并。
func TestMergeClean(t *testing.T) {
	base := lines("a", "b", "c", "d", "e")
	ours := lines("a", "B", "c", "d", "e")
	theirs := lines("a", "b", "c", "D", "e")
	assertMerge(t, base, ours, theirs, lines("a", "B", "c", "D", "e"), 0)
}

// 两边改同一处、结果相同：取一份，不算冲突。
func TestMergeSameChange(t *testing.T) {
	base := lines("a", "b", "c")
	ours := lines("a", "B", "c")
	theirs := lines("a", "B", "c")
	assertMerge(t, base, ours, theirs, lines("a", "B", "c"), 0)
}

// 只有一边改了：直接取改了的那边。
func TestMergeOneSideOnly(t *testing.T) {
	base := lines("a", "b", "c")
	assertMerge(t, base, lines("a", "B", "c"), base, lines("a", "B", "c"), 0)
	assertMerge(t, base, base, lines("a", "b", "C"), lines("a", "b", "C"), 0)
}

// 两边删同一段：干净合并，不算冲突。
func TestMergeBothDeleteSame(t *testing.T) {
	base := lines("a", "b", "c", "d")
	ours := lines("a", "d")
	theirs := lines("a", "d")
	assertMerge(t, base, ours, theirs, lines("a", "d"), 0)
}

// 两边在同一位置插入相同内容：取一份；内容不同：冲突。
func TestMergeInsertSamePoint(t *testing.T) {
	base := lines("a", "c")
	same := lines("a", "X", "c")
	assertMerge(t, base, same, same, same, 0)

	ours := lines("a", "X", "c")
	theirs := lines("a", "Y", "c")
	got, conflicts := Merge(base, ours, theirs)
	want := lines("a", "<<<<<<< ours", "X", "=======", "Y", ">>>>>>> theirs", "c")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("插入冲突结果不对：\n得到 %v\n想要 %v", got, want)
	}
	if len(conflicts) != 1 || conflicts[0].StartLine != 1 || conflicts[0].EndLine != 6 {
		t.Fatalf("冲突位置不对：%v（想要 StartLine=1 EndLine=6）", conflicts)
	}
}

// 改动区域重叠但不完全相同：冲突。注意：冲突区是两边改动
// 范围的并集，区域内只有一边改了的行也留在标记里
// （d 只被 theirs 改、B 只被 ours 改），与 git 的结果一致。
func TestMergeOverlapConflict(t *testing.T) {
	base := lines("a", "b", "c", "d", "e")
	ours := lines("a", "B", "C", "d", "e")   // 改 b、c
	theirs := lines("a", "b", "C", "D", "e") // 改 c、d
	got, conflicts := Merge(base, ours, theirs)
	want := lines("a", "<<<<<<< ours", "B", "C", "d", "=======", "b", "C", "D", ">>>>>>> theirs", "e")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("重叠冲突结果不对：\n得到 %v\n想要 %v", got, want)
	}
	if len(conflicts) != 1 {
		t.Fatalf("冲突数不对：%v", conflicts)
	}
}

// 一方删除一方修改同一段：冲突，删除那边的段为空。
// c 只被 ours 删、theirs 没动它，但它落在并集区域里，
// 所以仍出现在 theirs 一侧的标记内（与 git 一致）。
func TestMergeDeleteVsModify(t *testing.T) {
	base := lines("a", "b", "c")
	ours := lines("a")             // 删了 b、c
	theirs := lines("a", "B", "c") // 改了 b
	got, conflicts := Merge(base, ours, theirs)
	want := lines("a", "<<<<<<< ours", "=======", "B", "c", ">>>>>>> theirs")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("删除 vs 修改结果不对：\n得到 %v\n想要 %v", got, want)
	}
	if len(conflicts) != 1 {
		t.Fatalf("冲突数不对：%v", conflicts)
	}
}

// 插入点落在对方改动区间的边界上：不算重叠，能干净合并。
func TestMergeInsertAtBoundary(t *testing.T) {
	base := lines("a", "b", "c")
	ours := lines("a", "X", "b", "c") // 在 b 之前插入
	theirs := lines("a", "B", "c")    // 改 b
	assertMerge(t, base, ours, theirs, lines("a", "X", "B", "c"), 0)
}

// 空文件：两边都新增、内容不同 → 冲突；内容相同 → 干净。
func TestMergeEmptyBase(t *testing.T) {
	assertMerge(t, nil, lines("x"), lines("y"),
		lines("<<<<<<< ours", "x", "=======", "y", ">>>>>>> theirs"), 1)
	assertMerge(t, nil, lines("x"), lines("x"), lines("x"), 0)
	assertMerge(t, nil, nil, nil, nil, 0)
}

// 多个冲突区：各自分开标记。
func TestMergeMultipleConflicts(t *testing.T) {
	base := lines("a", "b", "c", "d", "e", "f")
	ours := lines("A", "b", "c", "d", "E", "f")
	theirs := lines("a2", "b", "c", "d", "e2", "f")
	got, conflicts := Merge(base, ours, theirs)
	want := lines(
		"<<<<<<< ours", "A", "=======", "a2", ">>>>>>> theirs",
		"b", "c", "d",
		"<<<<<<< ours", "E", "=======", "e2", ">>>>>>> theirs",
		"f",
	)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("多冲突结果不对：\n得到 %v\n想要 %v", got, want)
	}
	if len(conflicts) != 2 {
		t.Fatalf("冲突数不对：%v", conflicts)
	}
}

// MergeBytes 结尾换行符规则。
func TestMergeBytesTrailingNewline(t *testing.T) {
	// 都有结尾换行符 → 结果有。
	out, _ := MergeBytes([]byte("a\nb\n"), []byte("a\nB\n"), []byte("a\nb\n"))
	if string(out) != "a\nB\n" {
		t.Fatalf("都有换行符：得到 %q", out)
	}
	// ours 的最后一行没换行符、且它贡献了最后一行 → 结果没有。
	out, _ = MergeBytes([]byte("a\nb\n"), []byte("a\nB"), []byte("a\nb\n"))
	if string(out) != "a\nB" {
		t.Fatalf("ours 没换行符：得到 %q", out)
	}
	// base 的最后一行没换行符、且它贡献了最后一行（改动在前面）→ 结果没有。
	out, _ = MergeBytes([]byte("a\nb"), []byte("A\nb"), []byte("a\nb"))
	if string(out) != "A\nb" {
		t.Fatalf("base 没换行符、最后一行来自 base：得到 %q", out)
	}
	// base 的最后一行没换行符，但改动在后面、最后一行来自 ours（有换行符）→ 结果有。
	out, _ = MergeBytes([]byte("a\nb"), []byte("a\nB\n"), []byte("a\nb"))
	if string(out) != "a\nB\n" {
		t.Fatalf("最后一行来自 ours：得到 %q", out)
	}
	// base 中间行被删，最后一行还是 base 的最后一行（没换行符）→ 结果没有。
	out, _ = MergeBytes([]byte("a\nb\nc"), []byte("a\nc"), []byte("a\nb\nc"))
	if string(out) != "a\nc" {
		t.Fatalf("删中间行：得到 %q", out)
	}
	// 冲突：最后一行是标记行，一定有换行符。
	out, conflicts := MergeBytes([]byte("a\nb\n"), []byte("a\nB\n"), []byte("a\nC\n"))
	if string(out) != "a\n<<<<<<< ours\nB\n=======\nC\n>>>>>>> theirs\n" {
		t.Fatalf("冲突换行符：得到 %q", out)
	}
	if len(conflicts) != 1 {
		t.Fatalf("冲突数不对：%v", conflicts)
	}
	// 两边都删光：空。
	out, _ = MergeBytes([]byte("a\n"), []byte(""), []byte(""))
	if len(out) != 0 {
		t.Fatalf("删光：得到 %q", out)
	}
}

// ---- merge-base / 祖先检测 ----

// treeOf 写一组文件（单层路径）成 tree 对象，返回 tree SHA。
func treeOf(t *testing.T, gitDir string, files map[string]string) string {
	t.Helper()
	var entries []object.Entry
	for name, content := range files {
		sha, err := object.WriteObject(gitDir, object.Blob, []byte(content))
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, object.Entry{Mode: "100644", Name: name, SHA: sha})
	}
	// 排序：WriteObject 不依赖顺序，但 Encode 后排序更规范。
	sha, err := object.WriteObject(gitDir, object.Tree, object.Encode(entries))
	if err != nil {
		t.Fatal(err)
	}
	return sha
}

// commitWithParents 直接写一个 commit 对象，返回它的 SHA。
func commitWithParents(t *testing.T, gitDir, treeSHA string, parents []string, msg string) string {
	t.Helper()
	when := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	author := plumbing.FormatIdentity("Test", "test@example.com", when)
	sha, err := plumbing.CommitTree(gitDir, treeSHA, parents, author, author, msg)
	if err != nil {
		t.Fatal(err)
	}
	return sha
}

func TestFindMergeBase(t *testing.T) {
	dir := t.TempDir()
	gitDir, _, err := repo.Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	tree := treeOf(t, gitDir, map[string]string{"a.txt": "base\n"})

	// base → c1（main）→ c2；base → f1（feature）→ f2。
	base := commitWithParents(t, gitDir, tree, nil, "base")
	c1 := commitWithParents(t, gitDir, tree, []string{base}, "c1")
	c2 := commitWithParents(t, gitDir, tree, []string{c1}, "c2")
	f1 := commitWithParents(t, gitDir, tree, []string{base}, "f1")
	f2 := commitWithParents(t, gitDir, tree, []string{f1}, "f2")

	if got, err := FindMergeBase(gitDir, c2, f2); err != nil || got != base {
		t.Fatalf("分叉历史的 merge-base：得到 %q（err=%v），想要 %q", got, err, base)
	}
	// 另一边先 merge 过：c2 的祖先里有 f1。
	m := commitWithParents(t, gitDir, tree, []string{c2, f1}, "merge c2+f1")
	if got, err := FindMergeBase(gitDir, m, f2); err != nil || got != f1 {
		t.Fatalf("带合并提交的 merge-base：得到 %q（err=%v），想要 %q", got, err, f1)
	}
	// 同一个提交：base 就是它自己。
	if got, err := FindMergeBase(gitDir, c2, c2); err != nil || got != c2 {
		t.Fatalf("同一提交的 merge-base：得到 %q（err=%v），想要 %q", got, err, c2)
	}
	// 无关历史：没有共同祖先，报错。
	other := commitWithParents(t, gitDir, tree, nil, "unrelated root")
	if _, err := FindMergeBase(gitDir, c2, other); err == nil {
		t.Fatal("无关历史应报错")
	}
}

func TestIsAncestor(t *testing.T) {
	dir := t.TempDir()
	gitDir, _, err := repo.Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	tree := treeOf(t, gitDir, map[string]string{"a.txt": "base\n"})
	a := commitWithParents(t, gitDir, tree, nil, "a")
	b := commitWithParents(t, gitDir, tree, []string{a}, "b")
	c := commitWithParents(t, gitDir, tree, []string{b}, "c")

	cases := []struct {
		x, y string
		want bool
	}{
		{a, a, true}, // 自身也算
		{a, c, true},
		{c, a, false},
		{b, c, true},
		{c, b, false},
	}
	for _, tc := range cases {
		got, err := IsAncestor(gitDir, tc.x, tc.y)
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Fatalf("IsAncestor(%q, %q) = %v，想要 %v", tc.x[:7], tc.y[:7], got, tc.want)
		}
	}
}

// MERGE_HEAD / MERGE_MSG 的写读。
func TestMergeStateFiles(t *testing.T) {
	dir := t.TempDir()
	gitDir, _, err := repo.Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	// 一开始没有合并状态。
	if got, err := ReadMergeHead(gitDir); err != nil || got != "" {
		t.Fatalf("初始 MERGE_HEAD：%q, %v", got, err)
	}
	if got, err := MergeConflicts(gitDir); err != nil || len(got) != 0 {
		t.Fatalf("初始冲突列表：%v, %v", got, err)
	}

	target := "0123456789abcdef0123456789abcdef01234567"
	conflicts := []string{"a.txt", "dir/b.txt"}
	if err := WriteMergeState(gitDir, target, "Merge branch 'feature'", conflicts); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadMergeHead(gitDir); err != nil || got != target {
		t.Fatalf("MERGE_HEAD：%q, %v", got, err)
	}
	if got, err := MergeMessage(gitDir); err != nil || got != "Merge branch 'feature'" {
		t.Fatalf("MERGE_MSG 首行：%q, %v", got, err)
	}
	if got, err := MergeConflicts(gitDir); err != nil || !reflect.DeepEqual(got, conflicts) {
		t.Fatalf("冲突列表：%v, %v", got, err)
	}
	// MERGE_MSG 文件内容本身长这样（git 兼容格式）。
	data, err := os.ReadFile(filepath.Join(gitDir, "MERGE_MSG"))
	if err != nil {
		t.Fatal(err)
	}
	want := "Merge branch 'feature'\n\nConflicts:\n\ta.txt\n\tdir/b.txt\n"
	if string(data) != want {
		t.Fatalf("MERGE_MSG 内容不对：\n得到 %q\n想要 %q", data, want)
	}

	ClearMergeState(gitDir)
	if got, err := ReadMergeHead(gitDir); err != nil || got != "" {
		t.Fatalf("清除后 MERGE_HEAD：%q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(gitDir, "MERGE_MSG")); !os.IsNotExist(err) {
		t.Fatal("清除后 MERGE_MSG 应不存在")
	}
}
