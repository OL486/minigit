package object

import (
	"sort"
)

// FlattenTree 把 tree 对象递归展开成带完整路径的条目列表，
// 按路径排序。status 的三方比较用它把 HEAD 的 tree 拍平。
func FlattenTree(gitDir, treeSHA string) ([]Entry, error) {
	var out []Entry
	var walk func(sha, prefix string) error
	walk = func(sha, prefix string) error {
		typ, data, err := ReadObject(gitDir, sha)
		if err != nil {
			return err
		}
		if typ != Tree {
			return &NotTreeError{SHA: sha}
		}
		entries, err := Decode(data)
		if err != nil {
			return err
		}
		for _, e := range entries {
			full := prefix + e.Name
			if e.Mode == "40000" {
				if err := walk(e.SHA, full+"/"); err != nil {
					return err
				}
				continue
			}
			out = append(out, Entry{Mode: e.Mode, Name: full, SHA: e.SHA})
		}
		return nil
	}
	if err := walk(treeSHA, ""); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// NotTreeError 表示 FlattenTree 碰到了非 tree 对象。
type NotTreeError struct {
	SHA string
}

func (e *NotTreeError) Error() string {
	return "对象 " + e.SHA + " 不是 tree"
}
