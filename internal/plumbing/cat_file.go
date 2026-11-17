package plumbing

import (
	"fmt"
	"io"

	"github.com/OL486/minigit/internal/object"
)

// CatFile 实现 cat-file 的两种模式（二选一）：
//   - showType 为 true：只打印对象类型（-t）
//   - pretty 为 true：按类型美化打印（-p）：blob 与 commit 打印原始内容，
//     tree 按 "<mode> <type> <sha>\t<name>" 逐行打印（mode 补齐 6 位，
//     与 git cat-file -p 的输出一致）。
func CatFile(out io.Writer, gitDir, sha string, showType, pretty bool) error {
	typ, data, err := object.ReadObject(gitDir, sha)
	if err != nil {
		return err
	}
	if showType {
		fmt.Fprintln(out, typ)
		return nil
	}
	switch typ {
	case object.Blob, object.Commit:
		if _, err := out.Write(data); err != nil {
			return fmt.Errorf("输出失败：%w", err)
		}
		return nil
	case object.Tree:
		entries, err := object.Decode(data)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if _, err := fmt.Fprintf(out, "%s %s %s\t%s\n", e.DisplayMode(), e.Type(), e.SHA, e.Name); err != nil {
				return fmt.Errorf("输出失败：%w", err)
			}
		}
		return nil
	default:
		return fmt.Errorf("未知对象类型：%q", typ)
	}
}
