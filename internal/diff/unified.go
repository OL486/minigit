package diff

import (
	"fmt"
	"strings"
)

// hunk 上下文行数（和 git 默认一致）。
const contextLines = 3

// DiffBytes 对两个文件内容求 diff：按行切分后用 Myers 算。
// 再处理“只差一个末尾换行”的情况：行序列相同、但一个文件有末尾
// 换行、另一个没有——git 会把它显示为最后一行的删 + 加，
// 这里把最后一个 Equal 换成 Delete + Insert 来复现。
// 返回编辑序列、两边的行列表和两边是否有末尾换行。
func DiffBytes(oldData, newData []byte) (edits []Edit, oldLines, newLines []string, oldTrailingNL, newTrailingNL bool) {
	oldLines, oldTrailingNL = SplitLines(oldData)
	newLines, newTrailingNL = SplitLines(newData)
	edits = Diff(oldLines, newLines)
	if len(edits) > 0 && oldTrailingNL != newTrailingNL && len(oldLines) > 0 {
		onlyEqual := true
		for _, e := range edits {
			if e.Kind != Equal {
				onlyEqual = false
				break
			}
		}
		if onlyEqual {
			// 最后一行删了再加，内容不变，只是换行符变了。
			edits = edits[:len(edits)-1]
			edits = append(edits,
				Edit{Kind: Delete, Line: oldLines[len(oldLines)-1]},
				Edit{Kind: Insert, Line: newLines[len(newLines)-1]})
		}
	}
	return edits, oldLines, newLines, oldTrailingNL, newTrailingNL
}

// FormatUnified 把编辑序列格式化成 unified diff 的 --- / +++ / hunk
// 部分（不含 "diff --git" 和 "index" 头，那两行是 porcelain 的事）。
// oldLabel / newLabel 是显示路径，如 "a/foo.txt"、"b/foo.txt"；
// 新文件传 "/dev/null" 当 oldLabel，删文件时反过来。
// edits 里没有任何改动时返回 ""。
func FormatUnified(oldLabel, newLabel string, oldLines, newLines []string, oldTrailingNL, newTrailingNL bool, edits []Edit) string {
	// 先给每条编辑标上它在两边的行号（1-based；没有的那边记 0）。
	type ann struct {
		e       Edit
		oldLine int
		newLine int
	}
	var annotated []ann
	oldPos, newPos := 0, 0 // 0-based 下一行下标
	for _, e := range edits {
		a := ann{e: e}
		switch e.Kind {
		case Equal:
			a.oldLine, a.newLine = oldPos+1, newPos+1
			oldPos++
			newPos++
		case Delete:
			a.oldLine = oldPos + 1
			oldPos++
		case Insert:
			a.newLine = newPos + 1
			newPos++
		}
		annotated = append(annotated, a)
	}
	// 找改动所在的位置，分 hunk：两次改动之间 Equal 行 ≤ 2*context
	// 时合进同一个 hunk。
	var hunks [][]ann
	i := 0
	for i < len(annotated) {
		// 跳过前导 Equal，找第一个改动。
		for i < len(annotated) && annotated[i].e.Kind == Equal {
			i++
		}
		if i >= len(annotated) {
			break
		}
		// hunk 从这个改动往前数 context 行 Equal 开始。
		start := i - contextLines
		if start < 0 {
			start = 0
		}
		// 往后扩：一直到下一个改动离得够远（中间 Equal 行 > 2*context）。
		end := i
		for {
			// 找下一个改动。
			j := end + 1
			for j < len(annotated) && annotated[j].e.Kind == Equal {
				j++
			}
			if j >= len(annotated) {
				// 后面没有改动了：尾部上下文最多留
				// contextLines 行，多的 Equal 行不进 hunk。
				if end+contextLines < len(annotated) {
					end = end + contextLines
				} else {
					end = len(annotated) - 1
				}
				break
			}
			// end 到 j 之间的 Equal 行数。
			if j-end-1 > 2*contextLines {
				end = end + contextLines
				if end >= len(annotated) {
					end = len(annotated) - 1
				}
				break
			}
			end = j // 合进来，继续找下一个。
		}
		hunks = append(hunks, annotated[start:end+1])
		i = end + 1
	}
	if len(hunks) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "--- %s\n", oldLabel)
	fmt.Fprintf(&b, "+++ %s\n", newLabel)
	for _, h := range hunks {
		// 旧边范围：从第一个带旧行的编辑开始；新边同理。
		// 纯插入的 hunk 没有旧行，oldStart 默认为 0
		//（"@@ -0,0" 正是 git 的写法）。
		oldStart, oldCount, newStart, newCount := -1, 0, -1, 0
		for _, a := range h {
			if a.e.Kind != Insert {
				if oldStart < 0 {
					oldStart = a.oldLine - 1
				}
				oldCount++
			}
			if a.e.Kind != Delete {
				if newStart < 0 {
					newStart = a.newLine - 1
				}
				newCount++
			}
		}
		if oldStart < 0 {
			oldStart = 0
		}
		if newStart < 0 {
			newStart = 0
		}
		fmt.Fprintf(&b, "@@ -%s +%s @@\n",
			formatRange(oldStart, oldCount), formatRange(newStart, newCount))
		// 逐行输出，顺便处理“文件末尾没有换行”的标记。
		oIdx, nIdx := oldStart, newStart // 0-based 当前行下标
		for _, a := range h {
			switch a.e.Kind {
			case Equal:
				fmt.Fprintf(&b, " %s\n", a.e.Line)
				if oIdx == len(oldLines)-1 && !oldTrailingNL {
					b.WriteString("\\ No newline at end of file\n")
				} else if nIdx == len(newLines)-1 && !newTrailingNL {
					b.WriteString("\\ No newline at end of file\n")
				}
				oIdx++
				nIdx++
			case Delete:
				fmt.Fprintf(&b, "-%s\n", a.e.Line)
				if oIdx == len(oldLines)-1 && !oldTrailingNL {
					b.WriteString("\\ No newline at end of file\n")
				}
				oIdx++
			case Insert:
				fmt.Fprintf(&b, "+%s\n", a.e.Line)
				if nIdx == len(newLines)-1 && !newTrailingNL {
					b.WriteString("\\ No newline at end of file\n")
				}
				nIdx++
			}
		}
	}
	return b.String()
}

// formatRange 格式化 hunk 头里的 "-l,c" / "+l,c"：
// count 为 1 时省略 ",1"；count 为 0 时 l 是 hunk 前一行的行号
// （0-based 的 start 正好就是这个数）。
func formatRange(start0, count int) string {
	if count == 0 {
		return fmt.Sprintf("%d,0", start0)
	}
	if count == 1 {
		return fmt.Sprintf("%d", start0+1)
	}
	return fmt.Sprintf("%d,%d", start0+1, count)
}
