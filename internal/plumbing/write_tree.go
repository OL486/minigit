package plumbing

import (
	"strings"

	"github.com/OL486/minigit/internal/index"
	"github.com/OL486/minigit/internal/object"
)

// WriteTree 按索引生成 tree 对象（递归建嵌套 tree），返回根 tree 的 SHA。
// 空索引生成空 tree（SHA 4b825dc642cb6eb9a060e54bf8d69288fbee4904，和 git 一致）。
func WriteTree(gitDir string, idx *index.Index) (string, error) {
	root := &dirNode{dirs: map[string]*dirNode{}}
	for _, e := range idx.Entries {
		parts := strings.Split(e.Path, "/")
		node := root
		for _, p := range parts[:len(parts)-1] {
			next, ok := node.dirs[p]
			if !ok {
				next = &dirNode{dirs: map[string]*dirNode{}}
				node.dirs[p] = next
			}
			node = next
		}
		node.files = append(node.files, e)
	}
	return writeNode(gitDir, root)
}

type dirNode struct {
	files []index.Entry
	dirs  map[string]*dirNode
}

func writeNode(gitDir string, node *dirNode) (string, error) {
	var entries []object.Entry
	for _, f := range node.files {
		entries = append(entries, object.Entry{Mode: f.Mode, Name: lastPart(f.Path), SHA: f.SHA})
	}
	for name, sub := range node.dirs {
		sha, err := writeNode(gitDir, sub)
		if err != nil {
			return "", err
		}
		entries = append(entries, object.Entry{Mode: "40000", Name: name, SHA: sha})
	}
	return object.WriteObject(gitDir, object.Tree, object.Encode(entries))
}

func lastPart(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}
