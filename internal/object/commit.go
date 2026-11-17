package object

import (
	"bytes"
	"fmt"
	"strings"
)

// Commit 是 commit 对象解析后的结构。
//
// 原始格式（文本）：
//
//	tree <sha>
//	parent <sha>        （可有 0 个或多个）
//	author <名字> <邮箱> <时间戳> <时区>
//	committer <名字> <邮箱> <时间戳> <时区>
//
//	<提交信息>
//
// M1 只做编解码与解析，不创建提交（CreateCommit 是 M2 的事）。
// Parse 不认识的头部（如 gpgsig）直接忽略；cat-file -p 打印的是原始
// 字节，不走 Parse，所以不会丢信息。
type ParsedCommit struct {
	Tree      string // 40 位十六进制
	Parents   []string
	Author    string // "名字 <邮箱> 时间戳 时区"
	Committer string
	Message   string
}

// Parse 解析 commit 对象的原始内容。
func Parse(data []byte) (*ParsedCommit, error) {
	idx := bytes.Index(data, []byte("\n\n"))
	if idx < 0 {
		return nil, fmt.Errorf("commit 对象损坏：缺少头部与正文之间的空行")
	}
	c := &ParsedCommit{}
	for _, line := range strings.Split(string(data[:idx]), "\n") {
		switch {
		case strings.HasPrefix(line, "tree "):
			c.Tree = strings.TrimSpace(line[len("tree "):])
		case strings.HasPrefix(line, "parent "):
			c.Parents = append(c.Parents, strings.TrimSpace(line[len("parent "):]))
		case strings.HasPrefix(line, "author "):
			c.Author = line[len("author "):]
		case strings.HasPrefix(line, "committer "):
			c.Committer = line[len("committer "):]
		default:
			// 未知头部（gpgsig 等）：忽略。原始字节由 cat-file 直接打印。
		}
	}
	c.Message = string(data[idx+2:])
	if c.Tree == "" {
		return nil, fmt.Errorf("commit 对象损坏：缺少 tree 行")
	}
	return c, nil
}

// Encode 把 Commit 编码成 commit 对象的原始内容。
func (c *ParsedCommit) Encode() []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "tree %s\n", c.Tree)
	for _, p := range c.Parents {
		fmt.Fprintf(&b, "parent %s\n", p)
	}
	fmt.Fprintf(&b, "author %s\n", c.Author)
	fmt.Fprintf(&b, "committer %s\n", c.Committer)
	b.WriteString("\n")
	b.WriteString(c.Message)
	return []byte(b.String())
}
