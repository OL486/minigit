// Package merge 实现三路合并：行级 Merge、merge-base 查找、
// fast-forward 检测，以及合并进行中的状态文件（MERGE_HEAD / MERGE_MSG）。
//
// 三路合并的做法：先对 base→ours 和 base→theirs 各跑一遍 Myers diff，
// 得到两组 change（base 区间 → 新内容）；再沿 base 位置走：
//   - 只有一边改了 → 直接取改了的那边；
//   - 两边改了同一处、结果相同 → 取一份；
//   - 两边改了同一处、结果不同 → 冲突，输出标记；
//   - 两边改的区域不重叠 → 各自应用。
//
// 冲突标记用 ours/theirs 作标签（git 用的是 HEAD/分支名，
// 对照测试时归一化标签再比对）。
package merge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/OL486/minigit/internal/diff"
	"github.com/OL486/minigit/internal/object"
)

// Conflict 是合并结果里的一个冲突区域。
type Conflict struct {
	// StartLine 是冲突在合并结果里的起始行号（0 起，
	// 指向 "<<<<<<< ours" 那行）。
	StartLine int
	// EndLine 是冲突在合并结果里的结束行号（不含，
	// 即 ">>>>>>> theirs" 那行的下一行）。
	EndLine int
}

// Merge 对三份行序列做三路合并，返回合并后的行与冲突列表。
// base/ours/theirs 里的行不含换行符。换行符的处理见 MergeBytes。
func Merge(base, ours, theirs []string) ([]string, []Conflict) {
	merged, conflicts, _ := mergeInternal(base, ours, theirs, true, true, true)
	return merged, conflicts
}

// MergeBytes 合并三个文件的内容，返回合并后的字节与冲突列表。
// 结尾换行符按“贡献最后一行那一边”保留：结果的最后一行来自
// 哪一边（或 base），就按那一边的文件有没有结尾换行符来定；
// 最后一行是冲突标记时一定有换行符；结果为空时返回空。
func MergeBytes(base, ours, theirs []byte) ([]byte, []Conflict) {
	baseLines, baseNL := diff.SplitLines(base)
	oursLines, oursNL := diff.SplitLines(ours)
	theirsLines, theirsNL := diff.SplitLines(theirs)
	merged, conflicts, trailingNL := mergeInternal(baseLines, oursLines, theirsLines, baseNL, oursNL, theirsNL)
	if len(merged) == 0 {
		return nil, conflicts
	}
	var b strings.Builder
	for _, l := range merged {
		b.WriteString(l)
		b.WriteString("\n")
	}
	out := b.String()
	if !trailingNL {
		out = strings.TrimSuffix(out, "\n")
	}
	return []byte(out), conflicts
}

// change 是一组连续的非 Equal 编辑：把 base 的 [baseStart, baseEnd)
// 换成 lines。纯插入时 baseStart == baseEnd；纯删除时 lines 为空。
// newStart/newEnd 是这组改动在新文件里的位置（newEnd 是改动之后
// 新文件里的下标），用来判断合并结果的最后一行有没有换行符。
type change struct {
	baseStart, baseEnd int
	newStart, newEnd   int
	lines              []string
}

// changesFromEdits 把 Myers 的编辑脚本转成 change 列表。
// 相邻的非 Equal 编辑并成一个 change；change 按 base 位置递增、
// 互不重叠（同一边的两次改动不可能重叠）。
func changesFromEdits(edits []diff.Edit) []change {
	var out []change
	baseIdx, newIdx := 0, 0
	var cur *change
	flush := func() {
		if cur != nil {
			out = append(out, *cur)
			cur = nil
		}
	}
	for _, e := range edits {
		switch e.Kind {
		case diff.Equal:
			flush()
			baseIdx++
			newIdx++
		case diff.Delete:
			if cur == nil {
				cur = &change{baseStart: baseIdx, baseEnd: baseIdx, newStart: newIdx, newEnd: newIdx}
			}
			cur.baseEnd++
			baseIdx++
		case diff.Insert:
			if cur == nil {
				cur = &change{baseStart: baseIdx, baseEnd: baseIdx, newStart: newIdx, newEnd: newIdx}
			}
			cur.lines = append(cur.lines, e.Line)
			cur.newEnd++
			newIdx++
		}
	}
	flush()
	return out
}

// overlaps 判断两个 change 是否重叠（需要当作一个冲突区处理）。
// 规则：
//   - 两个纯插入：只有位置相同时才重叠；
//   - 插入 vs 区间：插入点严格落在区间内部才重叠，落在边界上不算
//     （插入内容出现在区间替换内容之前/之后，能干净合并）；
//   - 两个区间：base 范围有交集才重叠（相邻不算）。
func overlaps(a, b change) bool {
	aIns := a.baseStart == a.baseEnd
	bIns := b.baseStart == b.baseEnd
	switch {
	case aIns && bIns:
		return a.baseStart == b.baseStart
	case aIns:
		return a.baseStart > b.baseStart && a.baseStart < b.baseEnd
	case bIns:
		return b.baseStart > a.baseStart && b.baseStart < a.baseEnd
	default:
		return a.baseStart < b.baseEnd && b.baseStart < a.baseEnd
	}
}

// applyRegion 把 changes 应用到 base 的 [start, end) 段上，返回结果。
// changes 必须已按 baseStart 排好序、互不重叠，且都在段内。
func applyRegion(base []string, start, end int, changes []change) []string {
	var out []string
	pos := start
	for _, c := range changes {
		out = append(out, base[pos:c.baseStart]...)
		out = append(out, c.lines...)
		pos = c.baseEnd
	}
	out = append(out, base[pos:end]...)
	return out
}

// changeTrailingNL 返回“按 changes 应用后、结果的最后一行有没有
// 换行符”：看最后一个 change 的 newEnd——没到新文件末尾说明
// 最后一行后面还有行，一定有换行符；到了末尾就看新文件本身。
func changeTrailingNL(changes []change, newLen int, newNL bool) bool {
	if len(changes) == 0 {
		return newNL
	}
	last := changes[len(changes)-1]
	if last.newEnd < newLen {
		return true
	}
	return newNL
}

// equalLines 比较两份行序列是否完全相同。
func equalLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// mergeInternal 是 Merge 与 MergeBytes 的公共实现。
// baseNL/oursNL/theirsNL 是三个输入文件有没有结尾换行符；
// 返回的 trailingNL 是合并结果有没有结尾换行符。
func mergeInternal(base, ours, theirs []string, baseNL, oursNL, theirsNL bool) ([]string, []Conflict, bool) {
	co := changesFromEdits(diff.Diff(base, ours))
	ct := changesFromEdits(diff.Diff(base, theirs))
	var out []string
	var conflicts []Conflict
	trailingNL := true
	pos := 0
	i, j := 0, 0

	// emitBase 把 base 的 [pos, to) 原样输出。
	emitBase := func(to int) {
		if to > pos {
			out = append(out, base[pos:to]...)
			// 输出的最后一行是 base 的第 to-1 行：不到 base 末尾时
			// 它后面还有行，一定有换行符；到了末尾就看 base 本身。
			trailingNL = to < len(base) || baseNL
			pos = to
		}
	}

	for i < len(co) || j < len(ct) {
		switch {
		case j >= len(ct):
			// 只有 ours 还有改动。
			c := co[i]
			emitBase(c.baseStart)
			if len(c.lines) > 0 {
				out = append(out, c.lines...)
				trailingNL = changeTrailingNL([]change{c}, len(ours), oursNL)
			}
			pos = c.baseEnd
			i++
		case i >= len(co):
			// 只有 theirs 还有改动。
			c := ct[j]
			emitBase(c.baseStart)
			if len(c.lines) > 0 {
				out = append(out, c.lines...)
				trailingNL = changeTrailingNL([]change{c}, len(theirs), theirsNL)
			}
			pos = c.baseEnd
			j++
		case overlaps(co[i], ct[j]):
			// 冲突区：把所有与当前区域重叠的改动都收进来
			//（改动可能连锁扩展区域），再整体比较两边的结果。
			regionStart := co[i].baseStart
			if ct[j].baseStart < regionStart {
				regionStart = ct[j].baseStart
			}
			regionEnd := co[i].baseEnd
			if ct[j].baseEnd > regionEnd {
				regionEnd = ct[j].baseEnd
			}
			oursCh := []change{co[i]}
			theirsCh := []change{ct[j]}
			i++
			j++
			for {
				extended := false
				for i < len(co) && co[i].baseStart < regionEnd {
					oursCh = append(oursCh, co[i])
					if co[i].baseEnd > regionEnd {
						regionEnd = co[i].baseEnd
						extended = true
					}
					i++
				}
				for j < len(ct) && ct[j].baseStart < regionEnd {
					theirsCh = append(theirsCh, ct[j])
					if ct[j].baseEnd > regionEnd {
						regionEnd = ct[j].baseEnd
						extended = true
					}
					j++
				}
				if !extended {
					break
				}
			}
			emitBase(regionStart)
			oursText := applyRegion(base, regionStart, regionEnd, oursCh)
			theirsText := applyRegion(base, regionStart, regionEnd, theirsCh)
			if equalLines(oursText, theirsText) {
				// 两边改出的结果相同：取一份，不算冲突。
				if len(oursText) > 0 {
					out = append(out, oursText...)
					trailingNL = changeTrailingNL(oursCh, len(ours), oursNL)
				}
			} else {
				start := len(out)
				out = append(out, "<<<<<<< ours")
				out = append(out, oursText...)
				out = append(out, "=======")
				out = append(out, theirsText...)
				out = append(out, ">>>>>>> theirs")
				conflicts = append(conflicts, Conflict{StartLine: start, EndLine: len(out)})
				trailingNL = true // 最后一行是标记行，一定有换行符
			}
			pos = regionEnd
		case co[i].baseStart <= ct[j].baseStart:
			// ours 的改动在前，且与 theirs 不重叠：直接应用。
			c := co[i]
			emitBase(c.baseStart)
			if len(c.lines) > 0 {
				out = append(out, c.lines...)
				trailingNL = changeTrailingNL([]change{c}, len(ours), oursNL)
			}
			pos = c.baseEnd
			i++
		default:
			// theirs 的改动在前，且与 ours 不重叠：直接应用。
			c := ct[j]
			emitBase(c.baseStart)
			if len(c.lines) > 0 {
				out = append(out, c.lines...)
				trailingNL = changeTrailingNL([]change{c}, len(theirs), theirsNL)
			}
			pos = c.baseEnd
			j++
		}
	}
	emitBase(len(base))
	return out, conflicts, trailingNL
}

// readCommit 读出并解析一个 commit 对象（与 porcelain 的同名
// 函数是两份实现：merge 包不能 import porcelain，会循环依赖）。
func readCommit(gitDir, sha string) (*object.ParsedCommit, error) {
	typ, data, err := object.ReadObject(gitDir, sha)
	if err != nil {
		return nil, err
	}
	if typ != object.Commit {
		return nil, fmt.Errorf("对象 %s 不是 commit", sha)
	}
	return object.Parse(data)
}

// collectAncestors 收集 sha 的全部祖先（含自身），沿所有 parent 走。
func collectAncestors(gitDir, sha string) (map[string]bool, error) {
	seen := map[string]bool{}
	queue := []string{sha}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if seen[cur] {
			continue
		}
		seen[cur] = true
		c, err := readCommit(gitDir, cur)
		if err != nil {
			return nil, err
		}
		queue = append(queue, c.Parents...)
	}
	return seen, nil
}

// IsAncestor 报告 a 是否在 b 的祖先链里（含 b 自身）。
func IsAncestor(gitDir, a, b string) (bool, error) {
	ancestors, err := collectAncestors(gitDir, b)
	if err != nil {
		return false, err
	}
	return ancestors[a], nil
}

// FindMergeBase 找 head 与 target 的最近共同祖先：
// 先收集 head 的全部祖先，再从 target 出发 BFS，第一个命中的就是。
// 没有共同祖先（无关历史）时报错，本轮不支持合并。
// 注意：交叉合并（criss-cross）时共同祖先可能有多个，
// 这里只取离 target 最近的一个——和 git 的递归策略选的不一定相同，
// 见 docs/03-merge.md。
func FindMergeBase(gitDir, head, target string) (string, error) {
	ancestors, err := collectAncestors(gitDir, head)
	if err != nil {
		return "", err
	}
	seen := map[string]bool{}
	queue := []string{target}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if seen[cur] {
			continue
		}
		seen[cur] = true
		if ancestors[cur] {
			return cur, nil
		}
		c, err := readCommit(gitDir, cur)
		if err != nil {
			return "", err
		}
		queue = append(queue, c.Parents...)
	}
	return "", fmt.Errorf("没有共同祖先（无关历史，本轮不支持合并）")
}

// WriteMergeState 写 .git/MERGE_HEAD 与 .git/MERGE_MSG。
// msg 是合并提交的默认信息（如 "Merge branch 'feature'"）；
// conflicts 是冲突文件列表（按路径排序），写进 MERGE_MSG 的
// Conflicts 节，供 commit 时检查哪些冲突还没解决。
func WriteMergeState(gitDir, targetSHA, msg string, conflicts []string) error {
	if err := os.WriteFile(filepath.Join(gitDir, "MERGE_HEAD"), []byte(targetSHA+"\n"), 0o644); err != nil {
		return fmt.Errorf("写 MERGE_HEAD 失败：%w", err)
	}
	var b strings.Builder
	b.WriteString(msg)
	if !strings.HasSuffix(msg, "\n") {
		b.WriteString("\n")
	}
	if len(conflicts) > 0 {
		b.WriteString("\nConflicts:\n")
		for _, p := range conflicts {
			b.WriteString("\t" + p + "\n")
		}
	}
	if err := os.WriteFile(filepath.Join(gitDir, "MERGE_MSG"), []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("写 MERGE_MSG 失败：%w", err)
	}
	return nil
}

// ReadMergeHead 读 .git/MERGE_HEAD，返回目标 commit SHA；
// 文件不存在时返回 ""，不报错。内容损坏时报错。
func ReadMergeHead(gitDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(gitDir, "MERGE_HEAD"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("读 MERGE_HEAD 失败：%w", err)
	}
	sha := strings.TrimSpace(string(data))
	if len(sha) != 40 {
		return "", fmt.Errorf("MERGE_HEAD 内容损坏：%q", sha)
	}
	return sha, nil
}

// MergeMessage 读 .git/MERGE_MSG 的首行（合并提交的默认信息）。
func MergeMessage(gitDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(gitDir, "MERGE_MSG"))
	if err != nil {
		return "", fmt.Errorf("读 MERGE_MSG 失败：%w", err)
	}
	return strings.TrimSpace(strings.SplitN(string(data), "\n", 2)[0]), nil
}

// MergeConflicts 读 MERGE_MSG 里 Conflicts 节列出的冲突文件。
// MERGE_MSG 不存在时返回空列表，不报错。
func MergeConflicts(gitDir string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(gitDir, "MERGE_MSG"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("读 MERGE_MSG 失败：%w", err)
	}
	var out []string
	inSection := false
	for _, line := range strings.Split(string(data), "\n") {
		if line == "Conflicts:" {
			inSection = true
			continue
		}
		if inSection {
			if strings.HasPrefix(line, "\t") && len(line) > 1 {
				out = append(out, line[1:])
			} else if line != "" {
				// 非空、非冲突文件行：Conflicts 节结束。
				break
			}
		}
	}
	return out, nil
}

// ClearMergeState 删除 MERGE_HEAD 与 MERGE_MSG（合并提交成功后调用）。
// 文件不存在时不报错。
func ClearMergeState(gitDir string) {
	_ = os.Remove(filepath.Join(gitDir, "MERGE_HEAD"))
	_ = os.Remove(filepath.Join(gitDir, "MERGE_MSG"))
}
