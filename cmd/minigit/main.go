// Command minigit 是 mini-git 的入口，只做子命令分发。
// 每个子命令一个函数；M1 只有 init / hash-object / cat-file。
package main

import (
	"fmt"
	"os"

	"github.com/OL486/minigit/internal/plumbing"
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

M1 只支持以上三个命令；add / commit / status / log / branch / checkout /
diff / merge 是 M2 以后的事。
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
