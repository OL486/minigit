package porcelain

import (
	"fmt"
	"io"
	"strings"

	"github.com/OL486/minigit/internal/object"
	"github.com/OL486/minigit/internal/refs"
)

// CreateBranch 在 startPoint（分支名、完整引用名、"HEAD" 或 commit SHA，
// 为空时默认 HEAD）指向的提交上创建分支 name。
// 分支名已存在、还没有提交、名字不合法时都报错；不产生提交。
func CreateBranch(gitDir, name, startPoint string) error {
	if err := validBranchName(name); err != nil {
		return err
	}
	if _, err := refs.ResolveRef(gitDir, name); err == nil {
		return fmt.Errorf("分支已存在：%s", name)
	} else if !refs.IsNotFound(err) {
		return err
	}
	if startPoint == "" {
		startPoint = "HEAD"
	}
	sha, err := resolveStartPoint(gitDir, startPoint)
	if err != nil {
		return err
	}
	return refs.UpdateRef(gitDir, name, sha)
}

// ListBranches 按 git 的格式列出所有分支，当前分支前标 "* "，
// 其余标两个空格。还没有任何分支时什么都不打印。
func ListBranches(gitDir string, out io.Writer) error {
	names, err := refs.ListBranches(gitDir)
	if err != nil {
		return err
	}
	cur, err := refs.CurrentBranch(gitDir)
	if err != nil {
		return err
	}
	for _, n := range names {
		if n == cur {
			fmt.Fprintf(out, "* %s\n", n)
		} else {
			fmt.Fprintf(out, "  %s\n", n)
		}
	}
	return nil
}

// resolveStartPoint 把 start-point 解析成 commit SHA：先按引用名
// （分支短名、完整引用名、"HEAD"）解析，失败再按 commit SHA（完整
// 或唯一短前缀）解析，并校验对象确实是 commit。
func resolveStartPoint(gitDir, startPoint string) (string, error) {
	if sha, err := refs.ResolveRef(gitDir, startPoint); err == nil {
		return sha, nil
	}
	full, err := object.ResolveSHA(gitDir, startPoint)
	if err != nil {
		return "", fmt.Errorf("start-point 不存在：%s", startPoint)
	}
	typ, _, err := object.ReadObject(gitDir, full)
	if err != nil {
		return "", err
	}
	if typ != object.Commit {
		return "", fmt.Errorf("start-point %s 不是 commit（是 %s）", startPoint, typ)
	}
	return full, nil
}

// validBranchName 按 git check-ref-format 的核心规则校验分支名。
// M3 的限制：名字里不含 "/"（refs.ListBranches 只列 refs/heads 下
// 的顶层文件，层级分支名本轮不支持，见 README）。
func validBranchName(name string) error {
	switch {
	case name == "":
		return fmt.Errorf("分支名不能为空")
	case name == "HEAD":
		return fmt.Errorf("分支名不能是 HEAD")
	case strings.HasPrefix(name, "-"):
		return fmt.Errorf("分支名不能以 - 开头：%q", name)
	case strings.Contains(name, "/"):
		return fmt.Errorf("分支名不能含 /（M3 不支持层级分支名）：%q", name)
	case strings.Contains(name, ".."):
		return fmt.Errorf("分支名不能含 ..：%q", name)
	case strings.Contains(name, " "):
		return fmt.Errorf("分支名不能含空格：%q", name)
	case strings.ContainsAny(name, "~^:?*[\\"):
		return fmt.Errorf("分支名不能含 ~ ^ : ? * [ \\：%q", name)
	case strings.Contains(name, "@{"):
		return fmt.Errorf("分支名不能含 @{：%q", name)
	case strings.HasPrefix(name, ".") || strings.HasSuffix(name, "."):
		return fmt.Errorf("分支名不能以 . 开头或结尾：%q", name)
	case strings.HasSuffix(name, ".lock"):
		return fmt.Errorf("分支名不能以 .lock 结尾：%q", name)
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("分支名不能含控制字符：%q", name)
		}
	}
	return nil
}
