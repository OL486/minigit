package porcelain

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/OL486/minigit/internal/refs"
)

// Log 沿 parent 链（只跟第一 parent；M2 没有合并提交）打印提交历史。
// 默认格式：
//
//	commit <完整 SHA>
//	Author: 名字 <邮箱>
//	Date:   Mon Jan 2 15:04:05 2006 +0800
//
//	    <提交信息，每行缩进 4 格>
//
// oneline 为 true 时只打印 "<短 SHA> <首行信息>"。
// 还没有任何提交时报错。
func Log(gitDir string, oneline bool, out io.Writer) error {
	sha, err := refs.ResolveRef(gitDir, "HEAD")
	if err != nil {
		if refs.IsNotFound(err) {
			return fmt.Errorf("还没有提交")
		}
		return err
	}
	first := true
	for sha != "" {
		c, err := readCommit(gitDir, sha)
		if err != nil {
			return err
		}
		if !first && !oneline {
			fmt.Fprintln(out)
		}
		first = false
		if oneline {
			fmt.Fprintf(out, "%s %s\n", sha[:7], firstLine(c.Message))
		} else {
			name, email, when, err := parseIdentity(c.Author)
			if err != nil {
				return fmt.Errorf("提交 %s 的作者行损坏：%w", sha, err)
			}
			fmt.Fprintf(out, "commit %s\n", sha)
			fmt.Fprintf(out, "Author: %s <%s>\n", name, email)
			fmt.Fprintf(out, "Date:   %s\n", when.Format("Mon Jan 2 15:04:05 2006 -0700"))
			fmt.Fprintln(out)
			for _, line := range strings.Split(strings.TrimRight(c.Message, "\n"), "\n") {
				fmt.Fprintf(out, "    %s\n", line)
			}
		}
		if len(c.Parents) == 0 {
			break
		}
		sha = c.Parents[0]
	}
	return nil
}

func firstLine(msg string) string {
	msg = strings.TrimSpace(msg)
	if i := strings.Index(msg, "\n"); i >= 0 {
		return msg[:i]
	}
	return msg
}

// parseIdentity 解析 "名字 <邮箱> 时间戳 时区"。
func parseIdentity(s string) (name, email string, when time.Time, err error) {
	lt := strings.Index(s, "<")
	gt := strings.Index(s, ">")
	if lt < 0 || gt < 0 || gt <= lt {
		return "", "", time.Time{}, fmt.Errorf("身份行格式错误：%q", s)
	}
	name = strings.TrimSpace(s[:lt])
	email = s[lt+1 : gt]
	rest := strings.Fields(strings.TrimSpace(s[gt+1:]))
	if len(rest) != 2 {
		return "", "", time.Time{}, fmt.Errorf("身份行格式错误：%q", s)
	}
	ts, err := strconv.ParseInt(rest[0], 10, 64)
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("身份行时间戳错误：%q", s)
	}
	tz := rest[1]
	if len(tz) != 5 || (tz[0] != '+' && tz[0] != '-') {
		return "", "", time.Time{}, fmt.Errorf("身份行时区错误：%q", s)
	}
	hh, err1 := strconv.Atoi(tz[1:3])
	mm, err2 := strconv.Atoi(tz[3:5])
	if err1 != nil || err2 != nil {
		return "", "", time.Time{}, fmt.Errorf("身份行时区错误：%q", s)
	}
	off := hh*3600 + mm*60
	if tz[0] == '-' {
		off = -off
	}
	when = time.Unix(ts, 0).In(time.FixedZone("", off))
	return name, email, when, nil
}
