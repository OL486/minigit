// Command minigit 是 mini-git 的入口，只做子命令分发。
// 每个子命令一个函数；M1 只有 init / hash-object / cat-file。
package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/OL486/minigit/internal/index"
	"github.com/OL486/minigit/internal/plumbing"
	"github.com/OL486/minigit/internal/porcelain"
	"github.com/OL486/minigit/internal/refs"
	"github.com/OL486/minigit/internal/repo"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	var err error
	switch os.Args[1] {
	case "init":
		err = cmdInit(os.Args[2:])
	case "hash-object":
		err = cmdHashObject(os.Args[2:])
	case "cat-file":
		err = cmdCatFile(os.Args[2:])
	case "write-tree":
		err = cmdWriteTree(os.Args[2:])
	case "commit-tree":
		err = cmdCommitTree(os.Args[2:])
	case "add":
		err = cmdAdd(os.Args[2:])
	case "commit":
		err = cmdCommit(os.Args[2:])
	case "status":
		err = cmdStatus(os.Args[2:])
	case "log":
		err = cmdLog(os.Args[2:])
	case "branch":
		err = cmdBranch(os.Args[2:])
	case "checkout":
		err = cmdCheckout(os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "minigit: 未知命令 %q\n\n", os.Args[1])
		usage()
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "minigit: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `用法：
  minigit init [目录]              创建仓库（默认当前目录）
  minigit hash-object [-w] <文件>   计算文件的 blob SHA-1（-w 时写入对象库）
  minigit cat-file (-p|-t) <sha>    打印对象内容（-p）或类型（-t）
  minigit write-tree               按暂存区生成 tree 对象，打印 tree SHA
  minigit commit-tree <tree> [-p <parent>] -m <message>
                                   按 tree 创建 commit 对象，打印 commit SHA
  minigit add <文件...>            暂存文件（支持目录，递归加）
  minigit commit -m <message>      按暂存区提交
  minigit status                   显示工作区状态
  minigit log [--oneline]          显示提交历史
  minigit branch                   列出分支（当前分支前标 *）
  minigit branch <name> [start]    在 HEAD（或 start-point）创建分支
  minigit checkout <branch>        切换分支

M3 支持以上命令；diff / merge 是 M4 以后的事。
checkout 只接受分支名，不支持 detached HEAD；branch -d（删分支）本轮不做。
branch 的名字里不能含 /（不支持层级分支名）。
`)
}

func cmdInit(args []string) error {
	dir := "."
	if len(args) > 1 {
		return fmt.Errorf("init 最多接受一个目录参数")
	}
	if len(args) == 1 {
		dir = args[0]
	}
	gitDir, placeholder, err := repo.Init(dir)
	if err != nil {
		return err
	}
	fmt.Printf("已初始化空的 minigit 仓库：%s\n", gitDir)
	if placeholder {
		fmt.Println("提示：.git/config 里的 user.name / user.email 目前是占位值（Your Name / you@example.com），请编辑 .git/config 改成你自己的。")
	}
	return nil
}

func cmdHashObject(args []string) error {
	write := false
	var files []string
	for _, a := range args {
		switch a {
		case "-w":
			write = true
		default:
			if len(a) > 0 && a[0] == '-' {
				return fmt.Errorf("hash-object 不支持的选项：%s", a)
			}
			files = append(files, a)
		}
	}
	if len(files) != 1 {
		return fmt.Errorf("hash-object 需要恰好一个文件参数")
	}
	gitDir := ""
	if write {
		var err error
		gitDir, _, err = repo.FindRepo(".")
		if err != nil {
			return err
		}
	}
	sha, err := plumbing.HashObject(gitDir, files[0], write)
	if err != nil {
		return err
	}
	fmt.Println(sha)
	return nil
}

func cmdCatFile(args []string) error {
	var mode string
	var shas []string
	for _, a := range args {
		switch a {
		case "-p", "-t":
			if mode != "" {
				return fmt.Errorf("cat-file 只能指定 -p 或 -t 其中一个")
			}
			mode = a
		default:
			if len(a) > 0 && a[0] == '-' {
				return fmt.Errorf("cat-file 不支持的选项：%s", a)
			}
			shas = append(shas, a)
		}
	}
	if mode == "" {
		return fmt.Errorf("cat-file 需要 -p 或 -t")
	}
	if len(shas) != 1 {
		return fmt.Errorf("cat-file 需要恰好一个对象 SHA 参数")
	}
	gitDir, _, err := repo.FindRepo(".")
	if err != nil {
		return err
	}
	return plumbing.CatFile(os.Stdout, gitDir, shas[0], mode == "-t", mode == "-p")
}

func cmdWriteTree(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("write-tree 不接受参数")
	}
	gitDir, _, err := repo.FindRepo(".")
	if err != nil {
		return err
	}
	idx, err := index.Read(gitDir)
	if err != nil {
		return err
	}
	sha, err := plumbing.WriteTree(gitDir, idx)
	if err != nil {
		return err
	}
	fmt.Println(sha)
	return nil
}

func cmdCommitTree(args []string) error {
	var treeSHA, message string
	var parents []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-m":
			if i+1 >= len(args) {
				return fmt.Errorf("commit-tree -m 需要提交信息")
			}
			i++
			message = args[i]
		case "-p":
			if i+1 >= len(args) {
				return fmt.Errorf("commit-tree -p 需要 parent SHA")
			}
			i++
			parents = append(parents, args[i])
		default:
			if len(args[i]) > 0 && args[i][0] == '-' {
				return fmt.Errorf("commit-tree 不支持的选项：%s", args[i])
			}
			if treeSHA != "" {
				return fmt.Errorf("commit-tree 需要恰好一个 tree SHA 参数")
			}
			treeSHA = args[i]
		}
	}
	if treeSHA == "" {
		return fmt.Errorf("commit-tree 需要一个 tree SHA 参数")
	}
	if message == "" {
		return fmt.Errorf("commit-tree 需要 -m <message>")
	}
	gitDir, _, err := repo.FindRepo(".")
	if err != nil {
		return err
	}
	if len(parents) == 0 {
		// 默认 parent 是 HEAD（没有提交时为空，即根提交）。
		if head, err := refs.ResolveRef(gitDir, "HEAD"); err == nil {
			parents = []string{head}
		} else if !refs.IsNotFound(err) {
			return err
		}
	}
	author, err := repo.AuthorIdentity(gitDir)
	if err != nil {
		return err
	}
	committer, err := repo.CommitterIdentity(gitDir)
	if err != nil {
		return err
	}
	now := time.Now()
	sha, err := plumbing.CommitTree(gitDir, treeSHA, parents,
		plumbing.FormatIdentity(author.Name, author.Email, now),
		plumbing.FormatIdentity(committer.Name, committer.Email, now),
		message)
	if err != nil {
		return err
	}
	fmt.Println(sha)
	return nil
}

func cmdAdd(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("add 需要至少一个文件参数")
	}
	gitDir, workDir, err := repo.FindRepo(".")
	if err != nil {
		return err
	}
	return porcelain.Add(gitDir, workDir, args)
}

func cmdCommit(args []string) error {
	var message string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-m":
			if i+1 >= len(args) {
				return fmt.Errorf("commit -m 需要提交信息")
			}
			i++
			message = args[i]
		default:
			return fmt.Errorf("commit 不支持的参数：%s（用法：commit -m <message>）", args[i])
		}
	}
	if message == "" {
		return fmt.Errorf("commit 需要 -m <message>")
	}
	gitDir, _, err := repo.FindRepo(".")
	if err != nil {
		return err
	}
	sha, err := porcelain.Commit(gitDir, message)
	if err != nil {
		return err
	}
	branch, err := refs.CurrentBranch(gitDir)
	if err != nil {
		return err
	}
	fmt.Printf("[%s %s] %s\n", branch, sha[:7], firstLine(message))
	return nil
}

func cmdStatus(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("status 不接受参数")
	}
	gitDir, workDir, err := repo.FindRepo(".")
	if err != nil {
		return err
	}
	st, err := porcelain.ComputeStatus(gitDir, workDir)
	if err != nil {
		return err
	}
	porcelain.PrintStatus(os.Stdout, st)
	return nil
}

func cmdLog(args []string) error {
	oneline := false
	for _, a := range args {
		switch a {
		case "--oneline":
			oneline = true
		default:
			return fmt.Errorf("log 不支持的参数：%s", a)
		}
	}
	gitDir, _, err := repo.FindRepo(".")
	if err != nil {
		return err
	}
	return porcelain.Log(gitDir, oneline, os.Stdout)
}

func cmdBranch(args []string) error {
	gitDir, _, err := repo.FindRepo(".")
	if err != nil {
		return err
	}
	switch len(args) {
	case 0:
		return porcelain.ListBranches(gitDir, os.Stdout)
	case 1:
		return porcelain.CreateBranch(gitDir, args[0], "")
	case 2:
		return porcelain.CreateBranch(gitDir, args[0], args[1])
	default:
		return fmt.Errorf("branch 用法：branch [name] [start-point]")
	}
}

func cmdCheckout(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("checkout 需要恰好一个分支名参数")
	}
	gitDir, workDir, err := repo.FindRepo(".")
	if err != nil {
		return err
	}
	switched, err := porcelain.Checkout(gitDir, workDir, args[0])
	if err != nil {
		return err
	}
	if switched {
		fmt.Printf("Switched to branch '%s'\n", args[0])
	} else {
		fmt.Printf("Already on '%s'\n", args[0])
	}
	return nil
}

// firstLine 取提交信息的首行（commit 摘要打印用）。
func firstLine(msg string) string {
	msg = strings.TrimSpace(msg)
	if i := strings.Index(msg, "\n"); i >= 0 {
		return msg[:i]
	}
	return msg
}
