// Package index 实现 minigit 的暂存区（索引）。
//
// M2 用简化 JSON 格式，只求流程通：文件 .git/index 存 JSON，条目含
// path、blob SHA-1、mode、size、mtime。README 里写明这是临时格式，
// M4 会换成与 git 字节兼容的 index v2（到时真 git 才能读索引；
// M2 不要求 git status 能读我们的仓库）。
package index

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Entry 是索引里的一项。
type Entry struct {
	Path      string `json:"path"`       // 相对工作区根目录的路径（/ 分隔）
	SHA       string `json:"sha"`        // blob SHA-1（40 位十六进制）
	Mode      string `json:"mode"`       // "100644" 或 "100755"
	Size      int64  `json:"size"`       // 文件字节数
	MtimeSec  int64  `json:"mtime_sec"`  // 修改时间（秒）
	MtimeNsec int64  `json:"mtime_nsec"` // 修改时间（纳秒部分）
}

// Index 是整个暂存区。Entries 始终按 Path 排序。
type Index struct {
	Version int     `json:"version"` // 1
	Entries []Entry `json:"entries"`
}

// indexPath 返回索引文件路径。
func indexPath(gitDir string) string {
	return filepath.Join(gitDir, "index")
}

// Read 读出索引。索引文件不存在时返回空索引，不报错。
func Read(gitDir string) (*Index, error) {
	data, err := os.ReadFile(indexPath(gitDir))
	if err != nil {
		if os.IsNotExist(err) {
			return &Index{Version: 1}, nil
		}
		return nil, fmt.Errorf("读取索引失败：%w", err)
	}
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("索引损坏（JSON 解析失败）：%w", err)
	}
	if idx.Version != 1 {
		return nil, fmt.Errorf("索引版本不支持：%d", idx.Version)
	}
	// 保证排序不变式（即使文件是手改的）。
	sort.Slice(idx.Entries, func(i, j int) bool { return idx.Entries[i].Path < idx.Entries[j].Path })
	return &idx, nil
}

// Write 把索引一次性写盘（临时文件 + 改名，避免半截索引）。
func (idx *Index) Write(gitDir string) error {
	sort.Slice(idx.Entries, func(i, j int) bool { return idx.Entries[i].Path < idx.Entries[j].Path })
	data, err := json.Marshal(idx)
	if err != nil {
		return fmt.Errorf("序列化索引失败：%w", err)
	}
	path := indexPath(gitDir)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("写入索引失败：%w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("写入索引失败：%w", err)
	}
	return nil
}

// Upsert 按 Path 新增或更新一条目。
func (idx *Index) Upsert(e Entry) {
	for i := range idx.Entries {
		if idx.Entries[i].Path == e.Path {
			idx.Entries[i] = e
			return
		}
	}
	idx.Entries = append(idx.Entries, e)
	sort.Slice(idx.Entries, func(i, j int) bool { return idx.Entries[i].Path < idx.Entries[j].Path })
}

// Remove 按 Path 删除一条目，删掉了返回 true。
func (idx *Index) Remove(path string) bool {
	for i := range idx.Entries {
		if idx.Entries[i].Path == path {
			idx.Entries = append(idx.Entries[:i], idx.Entries[i+1:]...)
			return true
		}
	}
	return false
}

// Get 按 Path 查找一条目。
func (idx *Index) Get(path string) (Entry, bool) {
	for _, e := range idx.Entries {
		if e.Path == path {
			return e, true
		}
	}
	return Entry{}, false
}
