package diff

import (
	"math/rand"
	"testing"
)

// applyEdits 把编辑序列作用在 a 上，应得到 b。
func applyEdits(t *testing.T, a []string, edits []Edit) []string {
	t.Helper()
	var out []string
	i := 0 // a 里的下标
	for _, e := range edits {
		switch e.Kind {
		case Equal:
			if i >= len(a) || a[i] != e.Line {
				t.Fatalf("Equal 编辑与原序列不符：位置 %d 期望 %q", i, e.Line)
			}
			out = append(out, e.Line)
			i++
		case Delete:
			if i >= len(a) || a[i] != e.Line {
				t.Fatalf("Delete 编辑与原序列不符：位置 %d 期望 %q", i, e.Line)
			}
			i++
		case Insert:
			out = append(out, e.Line)
		}
	}
	if i != len(a) {
		t.Fatalf("编辑序列没消费完原序列：消费 %d，共 %d", i, len(a))
	}
	return out
}

// bruteLCS 用动态规划求最长公共子序列的长度（暴力对照用）。
func bruteLCS(a, b []string) int {
	n, m := len(a), len(b)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] > dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	return dp[0][0]
}

// checkDiff 校验一次 diff 结果：应用后得到 b，且编辑步数最少
// （非 Equal 步数 = len(a)+len(b)-2*LCS）。
func checkDiff(t *testing.T, a, b []string) {
	t.Helper()
	edits := Diff(a, b)
	got := applyEdits(t, a, edits)
	if len(got) != len(b) {
		t.Fatalf("应用编辑后长度不对：%d vs %d", len(got), len(b))
	}
	for i := range got {
		if got[i] != b[i] {
			t.Fatalf("应用编辑后第 %d 行不对：%q vs %q", i, got[i], b[i])
		}
	}
	nonEqual := 0
	for _, e := range edits {
		if e.Kind != Equal {
			nonEqual++
		}
	}
	want := len(a) + len(b) - 2*bruteLCS(a, b)
	if nonEqual != want {
		t.Fatalf("编辑步数不是最少：%d vs %d（a=%q b=%q）", nonEqual, want, a, b)
	}
}

func TestDiffBoundaries(t *testing.T) {
	cases := []struct {
		name string
		a, b []string
	}{
		{"都空", nil, nil},
		{"旧空", nil, []string{"x", "y"}},
		{"新空", []string{"x", "y"}, nil},
		{"全相同", []string{"a", "b", "c"}, []string{"a", "b", "c"}},
		{"全不同", []string{"a", "b"}, []string{"c", "d"}},
		{"单行相同", []string{"a"}, []string{"a"}},
		{"单行不同", []string{"a"}, []string{"b"}},
		{"首行插入", []string{"b", "c"}, []string{"a", "b", "c"}},
		{"末行删除", []string{"a", "b", "c"}, []string{"a", "b"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			checkDiff(t, c.a, c.b)
		})
	}
}

// randomLines 生成 n 行随机内容，行取自一个小字母表
// （字母表小 → 重复行多，diff 更难算）。
func randomLines(r *rand.Rand, n, alphabet int) []string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = "line" + string(rune('a'+r.Intn(alphabet)))
	}
	return lines
}

func TestDiffAgainstBruteForce(t *testing.T) {
	r := rand.New(rand.NewSource(42)) // 固定种子，失败可复现
	for i := 0; i < 10000; i++ {
		na := r.Intn(15)
		nb := r.Intn(15)
		a := randomLines(r, na, 3)
		b := randomLines(r, nb, 3)
		checkDiff(t, a, b)
	}
}

func TestDiffRepeatedLines(t *testing.T) {
	// 已知难例：重复行很多的文件，贪心容易走错。
	cases := []struct {
		name string
		a, b []string
	}{
		{"全是同一行", []string{"x", "x", "x", "x"}, []string{"x", "x"}},
		{"交错重复", []string{"a", "b", "a", "b", "a"}, []string{"b", "a", "b", "a", "b"}},
		{"长重复前缀", []string{"a", "a", "a", "a", "a", "b"}, []string{"a", "a", "a", "a", "a", "c"}},
		{"重复行里改一行", []string{"k", "k", "k", "k", "k"}, []string{"k", "k", "X", "k", "k"}},
		{"ABAB 改中间", []string{"a", "b", "a", "b", "a", "b"}, []string{"a", "b", "b", "a", "b"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			checkDiff(t, c.a, c.b)
		})
	}
}

func TestSplitLines(t *testing.T) {
	lines, nl := SplitLines(nil)
	if len(lines) != 0 || !nl {
		t.Fatalf("空文件应为 0 行、有末尾换行：%v %v", lines, nl)
	}
	lines, nl = SplitLines([]byte("a\nb\n"))
	if len(lines) != 2 || lines[0] != "a" || lines[1] != "b" || !nl {
		t.Fatalf("有末尾换行解析不对：%v %v", lines, nl)
	}
	lines, nl = SplitLines([]byte("a\nb"))
	if len(lines) != 2 || lines[1] != "b" || nl {
		t.Fatalf("无末尾换行解析不对：%v %v", lines, nl)
	}
	lines, nl = SplitLines([]byte("\n"))
	if len(lines) != 1 || lines[0] != "" || !nl {
		t.Fatalf("只有一个换行解析不对：%v %v", lines, nl)
	}
}

func TestDiffBytesTrailingNewline(t *testing.T) {
	// 只差一个末尾换行：git 会显示为最后一行的删 + 加。
	edits, _, _, oldNL, newNL := DiffBytes([]byte("a\nb"), []byte("a\nb\n"))
	if oldNL || !newNL {
		t.Fatalf("末尾换行标记不对：%v %v", oldNL, newNL)
	}
	if len(edits) != 3 || edits[0].Kind != Equal ||
		edits[1].Kind != Delete || edits[2].Kind != Insert {
		t.Fatalf("只差末尾换行应为 Equal+Delete+Insert：%v", edits)
	}
	edits, _, _, _, _ = DiffBytes([]byte("a\nb\n"), []byte("a\nb\n"))
	for _, e := range edits {
		if e.Kind != Equal {
			t.Fatalf("完全相同应全是 Equal：%v", edits)
		}
	}
}

func TestFormatUnified(t *testing.T) {
	cases := []struct {
		name               string
		old, new           []string
		oldNL, newNL       bool
		oldLabel, newLabel string
		want               string
	}{
		{
			name:  "改一行",
			old:   []string{"a", "b", "c"},
			new:   []string{"a", "B", "c"},
			oldNL: true, newNL: true,
			oldLabel: "a/f.txt", newLabel: "b/f.txt",
			want: "--- a/f.txt\n+++ b/f.txt\n@@ -1,3 +1,3 @@\n a\n-b\n+B\n c\n",
		},
		{
			name:  "纯插入",
			old:   []string{},
			new:   []string{"x", "y"},
			oldNL: true, newNL: true,
			oldLabel: "/dev/null", newLabel: "b/f.txt",
			want: "--- /dev/null\n+++ b/f.txt\n@@ -0,0 +1,2 @@\n+x\n+y\n",
		},
		{
			name:  "纯删除",
			old:   []string{"x", "y"},
			new:   []string{},
			oldNL: true, newNL: true,
			oldLabel: "a/f.txt", newLabel: "/dev/null",
			want: "--- a/f.txt\n+++ /dev/null\n@@ -1,2 +0,0 @@\n-x\n-y\n",
		},
		{
			name:  "行数 1 省略 ,1",
			old:   []string{"a"},
			new:   []string{"b"},
			oldNL: true, newNL: true,
			oldLabel: "a/f.txt", newLabel: "b/f.txt",
			want: "--- a/f.txt\n+++ b/f.txt\n@@ -1 +1 @@\n-a\n+b\n",
		},
		{
			name:  "无末尾换行标记",
			old:   []string{"a", "b"},
			new:   []string{"a", "B"},
			oldNL: false, newNL: false,
			oldLabel: "a/f.txt", newLabel: "b/f.txt",
			want: "--- a/f.txt\n+++ b/f.txt\n@@ -1,2 +1,2 @@\n a\n-b\n\\ No newline at end of file\n+B\n\\ No newline at end of file\n",
		},
		{
			name:  "两处改动合成两个 hunk",
			old:   []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"},
			new:   []string{"1", "X", "3", "4", "5", "6", "7", "8", "9", "Y"},
			oldNL: true, newNL: true,
			oldLabel: "a/f.txt", newLabel: "b/f.txt",
			want: "--- a/f.txt\n+++ b/f.txt\n" +
				"@@ -1,5 +1,5 @@\n 1\n-2\n+X\n 3\n 4\n 5\n" +
				"@@ -7,4 +7,4 @@\n 7\n 8\n 9\n-10\n+Y\n",
		},
		{
			name:  "改动离得近合成一个 hunk",
			old:   []string{"1", "2", "3", "4", "5", "6", "7"},
			new:   []string{"1", "X", "3", "4", "Y", "6", "7"},
			oldNL: true, newNL: true,
			oldLabel: "a/f.txt", newLabel: "b/f.txt",
			want: "--- a/f.txt\n+++ b/f.txt\n@@ -1,7 +1,7 @@\n 1\n-2\n+X\n 3\n 4\n-5\n+Y\n 6\n 7\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			edits := Diff(c.old, c.new)
			got := FormatUnified(c.oldLabel, c.newLabel, c.old, c.new, c.oldNL, c.newNL, edits)
			if got != c.want {
				t.Fatalf("格式化不对：\n得到：\n%s\n期望：\n%s", got, c.want)
			}
		})
	}
}

func TestFormatUnifiedNoChanges(t *testing.T) {
	edits := Diff([]string{"a", "b"}, []string{"a", "b"})
	if got := FormatUnified("a/f.txt", "b/f.txt", []string{"a", "b"}, []string{"a", "b"}, true, true, edits); got != "" {
		t.Fatalf("没有改动应返回空字符串：%q", got)
	}
}
