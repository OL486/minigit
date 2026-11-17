// Package diff 实现行级 diff：Myers O(ND) 算法 + unified 格式化。
//
// Diff 的输入是按行切好的两个序列（不含换行符），输出最短编辑脚本。
// 算法是 Myers 论文里的正向贪心：对每个编辑距离 D，沿 k 线找能到
// 的最远 x；把每一步的 V 数组都存下来，找完后再从终点回溯出编辑序列。
// 回溯需要 O(D²) 的额外空间（每步一份 V 的拷贝），文件不大时
// 完全够用；换来的是实现简单、不容易错下标。
package diff

// Kind 是编辑操作的类型。
type Kind int

const (
	// Equal 表示两边都有的行（没改）。
	Equal Kind = iota
	// Delete 表示只在旧文件里有的行（删掉）。
	Delete
	// Insert 表示只在新文件里有的行（加上）。
	Insert
)

// Edit 是一条编辑操作。
type Edit struct {
	Kind Kind
	Line string
}

// Diff 对 a、b 求最短编辑脚本（按行比较，返回 Equal/Delete/Insert
// 的序列）。空文件、一方为空、全相同、全不同都能处理。
func Diff(a, b []string) []Edit {
	n, m := len(a), len(b)
	max := n + m
	if max == 0 {
		return nil
	}
	// v 以 k=0 为中心，索引 offset+k，k 的范围是 -max..max。
	offset := max
	v := make([]int, 2*max+1)
	var trace [][]int
	found := false
	dFound := 0
	for d := 0; d <= max; d++ {
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[offset+k-1] < v[offset+k+1]) {
				x = v[offset+k+1] // 向下走（插入）
			} else {
				x = v[offset+k-1] + 1 // 向右走（删除）
			}
			y := x - k
			// 沿对角线走到底（相同的行）。
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			v[offset+k] = x
			if x >= n && y >= m {
				found = true
				dFound = d
				break
			}
		}
		// 存下这一步的 V，供回溯用。
		cp := make([]int, len(v))
		copy(cp, v)
		trace = append(trace, cp)
		if found {
			break
		}
	}
	// 回溯：从 (n, m) 倒着走回 (0, 0)。
	var rev []Edit
	x, y := n, m
	for d := dFound; d > 0; d-- {
		prev := trace[d-1] // D-1 步之后的 V
		k := x - y
		var prevK int
		if k == -d || (k != d && prev[offset+k-1] < prev[offset+k+1]) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}
		prevX := prev[offset+prevK]
		prevY := prevX - prevK
		// 先走对角线（相同的行）。
		for x > prevX && y > prevY {
			rev = append(rev, Edit{Kind: Equal, Line: a[x-1]})
			x--
			y--
		}
		// 再走一步编辑：x 不动是插入，y 不动是删除。
		if x == prevX {
			rev = append(rev, Edit{Kind: Insert, Line: b[y-1]})
			y--
		} else {
			rev = append(rev, Edit{Kind: Delete, Line: a[x-1]})
			x--
		}
	}
	// D=0 剩下的全是对角线。
	for x > 0 && y > 0 {
		rev = append(rev, Edit{Kind: Equal, Line: a[x-1]})
		x--
		y--
	}
	// 翻回来。
	out := make([]Edit, 0, len(rev))
	for i := len(rev) - 1; i >= 0; i-- {
		out = append(out, rev[i])
	}
	return out
}

// SplitLines 把文件内容按行切分（行里不含换行符），并报告文件
// 是否以换行结尾。空文件返回 0 行；最后一行没有换行符时，
// trailingNL 为 false（unified 格式化时要补 "\ No newline at end
// of file" 标记）。
func SplitLines(data []byte) (lines []string, trailingNL bool) {
	if len(data) == 0 {
		return nil, true
	}
	trailingNL = data[len(data)-1] == '\n'
	text := string(data)
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			lines = append(lines, text[start:i])
			start = i + 1
		}
	}
	if start < len(text) {
		lines = append(lines, text[start:])
	}
	return lines, trailingNL
}
