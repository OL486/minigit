// Package index 实现 minigit 的暂存区（索引）。
//
// M4 起用与 git 字节兼容的 index v2 格式（见 docs/02-index-format.md）：
// 文件头 DIRC + 版本 2 + 条目数；条目含 ctime/mtime、dev、ino、mode、
// uid、gid、size、SHA-1、flags、文件名（8 字节对齐）；文件尾是全部
// 内容的 SHA-1 校验和。扩展字段只跳过不解析，写盘时不写扩展。
//
// M2/M3 的旧索引是 JSON 格式（内容以 '{' 开头）。Read 能识别并按
// 旧格式解析，缺失的 stat 字段补零；下次 Write 时自动存成 v2。
package index

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

// Entry 是索引里的一项。
type Entry struct {
	Path      string `json:"path"`       // 相对工作区根目录的路径（/ 分隔）
	SHA       string `json:"sha"`        // blob SHA-1（40 位十六进制）
	Mode      string `json:"mode"`       // "100644" 或 "100755"
	Size      int64  `json:"size"`       // 文件字节数
	MtimeSec  int64  `json:"mtime_sec"`  // 修改时间（秒）
	MtimeNsec int64  `json:"mtime_nsec"` // 修改时间（纳秒部分）
	CtimeSec  int64  `json:"ctime_sec"`  // 创建/状态改变时间（秒）
	CtimeNsec int64  `json:"ctime_nsec"` // 创建/状态改变时间（纳秒部分）
	Dev       uint32 `json:"dev"`        // 设备号
	Ino       uint32 `json:"ino"`        // inode 号
	Uid       uint32 `json:"uid"`        // 文件属主
	Gid       uint32 `json:"gid"`        // 文件属组
}

// Index 是整个暂存区。Entries 始终按 Path 排序。
type Index struct {
	Version int     `json:"version"` // 2（v2 格式）；旧 JSON 是 1
	Entries []Entry `json:"entries"`
}

// indexPath 返回索引文件路径。
func indexPath(gitDir string) string {
	return filepath.Join(gitDir, "index")
}

// EntryFromStat 用文件信息补全索引条目的 stat 字段
// （size、mtime、ctime、dev、ino、uid、gid）。
func EntryFromStat(path, sha, mode string, fi os.FileInfo) Entry {
	ctimeSec, ctimeNsec, dev, ino, uid, gid := statFields(fi)
	return Entry{
		Path:      path,
		SHA:       sha,
		Mode:      mode,
		Size:      fi.Size(),
		MtimeSec:  fi.ModTime().Unix(),
		MtimeNsec: int64(fi.ModTime().Nanosecond()),
		CtimeSec:  ctimeSec,
		CtimeNsec: ctimeNsec,
		Dev:       dev,
		Ino:       ino,
		Uid:       uid,
		Gid:       gid,
	}
}

// Read 读出索引。索引文件不存在时返回空索引，不报错。
// 识别规则：前 4 字节是 "DIRC" → v2；第 1 个字节是 '{' →
// 旧 JSON 格式；其他 → 报错。
func Read(gitDir string) (*Index, error) {
	data, err := os.ReadFile(indexPath(gitDir))
	if err != nil {
		if os.IsNotExist(err) {
			return &Index{Version: 2}, nil
		}
		return nil, fmt.Errorf("读取索引失败：%w", err)
	}
	if len(data) >= 4 && string(data[:4]) == "DIRC" {
		return parseV2(data)
	}
	if len(data) > 0 && data[0] == '{' {
		return parseJSON(data)
	}
	return nil, fmt.Errorf("索引损坏（既不是 DIRC 也不是 JSON）")
}

// parseJSON 解析 M2/M3 的旧 JSON 索引。JSON 里没有的 stat 字段补零。
func parseJSON(data []byte) (*Index, error) {
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

// parseV2 解析 git index v2 文件。格式见 docs/02-index-format.md。
func parseV2(data []byte) (*Index, error) {
	const headerLen = 12
	const checksumLen = 20
	const entryFixed = 62
	if len(data) < headerLen+checksumLen {
		return nil, fmt.Errorf("索引损坏（文件太短：%d 字节）", len(data))
	}
	// 校验和：最后 20 字节是之前全部字节的 SHA-1。
	sum := sha1.Sum(data[:len(data)-checksumLen])
	if !bytes.Equal(sum[:], data[len(data)-checksumLen:]) {
		return nil, fmt.Errorf("索引损坏（校验和不符）")
	}
	version := binary.BigEndian.Uint32(data[4:8])
	if version != 2 {
		return nil, fmt.Errorf("索引版本不支持：%d（只支持 v2）", version)
	}
	count := binary.BigEndian.Uint32(data[8:12])
	bodyEnd := len(data) - checksumLen // 条目 + 扩展的结束位置
	pos := headerLen
	idx := &Index{Version: 2}
	for i := uint32(0); i < count; i++ {
		if pos+entryFixed > bodyEnd {
			return nil, fmt.Errorf("索引损坏（第 %d 个条目越界）", i)
		}
		e := Entry{
			CtimeSec:  int64(binary.BigEndian.Uint32(data[pos+0:])),
			CtimeNsec: int64(binary.BigEndian.Uint32(data[pos+4:])),
			MtimeSec:  int64(binary.BigEndian.Uint32(data[pos+8:])),
			MtimeNsec: int64(binary.BigEndian.Uint32(data[pos+12:])),
			Dev:       binary.BigEndian.Uint32(data[pos+16:]),
			Ino:       binary.BigEndian.Uint32(data[pos+20:]),
			Uid:       binary.BigEndian.Uint32(data[pos+28:]),
			Gid:       binary.BigEndian.Uint32(data[pos+32:]),
			Size:      int64(binary.BigEndian.Uint32(data[pos+36:])),
		}
		mode := binary.BigEndian.Uint32(data[pos+24:])
		e.Mode = fmt.Sprintf("%06o", mode)
		e.SHA = hex.EncodeToString(data[pos+40 : pos+60])
		flags := binary.BigEndian.Uint16(data[pos+60:])
		nameLen := int(flags & 0xFFF)
		nameStart := pos + entryFixed
		var nameEnd int
		if nameLen < 0xFFF {
			nameEnd = nameStart + nameLen
			if nameEnd > bodyEnd {
				return nil, fmt.Errorf("索引损坏（第 %d 个条目的文件名越界）", i)
			}
			e.Path = string(data[nameStart:nameEnd])
			// 总长度 8 字节对齐，至少补 1 个 NUL。
			pos = nameEnd + padLen(entryFixed+nameLen)
		} else {
			// 超长文件名：以 NUL 结尾。
			nul := bytes.IndexByte(data[nameStart:bodyEnd], 0)
			if nul < 0 {
				return nil, fmt.Errorf("索引损坏（第 %d 个条目的文件名没有 NUL 结尾）", i)
			}
			e.Path = string(data[nameStart : nameStart+nul])
			// 超长文件名自带 NUL 终止符，之后补到 8 字节对齐即可
			//（已是倍数时不用再补，终止符本身就是那个 NUL）。
			consumed := entryFixed + nul + 1
			pos = nameStart + nul + 1 + (8-consumed%8)%8
		}
		if pos > bodyEnd {
			return nil, fmt.Errorf("索引损坏（第 %d 个条目越界）", i)
		}
		idx.Entries = append(idx.Entries, e)
	}
	// 扩展：签名 + 长度 + 数据，不认识就跳过；长度越界则报错。
	for pos < bodyEnd {
		if pos+8 > bodyEnd {
			return nil, fmt.Errorf("索引损坏（扩展头部越界）")
		}
		sig := string(data[pos : pos+4])
		extLen := int(binary.BigEndian.Uint32(data[pos+4:]))
		pos += 8 + extLen
		if pos > bodyEnd {
			return nil, fmt.Errorf("索引损坏（扩展 %q 长度越界）", sig)
		}
	}
	if pos != bodyEnd {
		return nil, fmt.Errorf("索引损坏（条目与扩展之后还有 %d 字节垃圾）", bodyEnd-pos)
	}
	sort.Slice(idx.Entries, func(i, j int) bool { return idx.Entries[i].Path < idx.Entries[j].Path })
	return idx, nil
}

// padLen 返回普通条目（文件名不含 NUL 终止符）需要的填充字节数：
// 总长度是 62 + 文件名长度，填充后必须是 8 的倍数，且至少有 1 个
// NUL（它就是文件名的终止符），所以结果是 1..8。
func padLen(total int) int {
	r := total % 8
	if r == 0 {
		return 8
	}
	return 8 - r
}

// Write 把索引按 v2 格式一次性写盘（临时文件 + 改名，避免半截索引）。
// 扩展不写；文件名长度 ≥ 0xFFF 的条目直接报错。
func (idx *Index) Write(gitDir string) error {
	sort.Slice(idx.Entries, func(i, j int) bool { return idx.Entries[i].Path < idx.Entries[j].Path })
	var buf bytes.Buffer
	buf.WriteString("DIRC")
	var tmp [4]byte
	binary.BigEndian.PutUint32(tmp[:], 2)
	buf.Write(tmp[:])
	binary.BigEndian.PutUint32(tmp[:], uint32(len(idx.Entries)))
	buf.Write(tmp[:])
	for _, e := range idx.Entries {
		if len(e.Path) >= 0xFFF {
			return fmt.Errorf("写索引失败：文件名太长（≥ %d 字节）：%s", 0xFFF, e.Path)
		}
		mode, err := strconv.ParseUint(e.Mode, 8, 32)
		if err != nil {
			return fmt.Errorf("写索引失败：条目 %s 的 mode 非法：%q", e.Path, e.Mode)
		}
		sha, err := hex.DecodeString(e.SHA)
		if err != nil || len(sha) != 20 {
			return fmt.Errorf("写索引失败：条目 %s 的 SHA 非法：%q", e.Path, e.SHA)
		}
		var entry [62]byte
		binary.BigEndian.PutUint32(entry[0:], uint32(e.CtimeSec))
		binary.BigEndian.PutUint32(entry[4:], uint32(e.CtimeNsec))
		binary.BigEndian.PutUint32(entry[8:], uint32(e.MtimeSec))
		binary.BigEndian.PutUint32(entry[12:], uint32(e.MtimeNsec))
		binary.BigEndian.PutUint32(entry[16:], e.Dev)
		binary.BigEndian.PutUint32(entry[20:], e.Ino)
		binary.BigEndian.PutUint32(entry[24:], uint32(mode))
		binary.BigEndian.PutUint32(entry[28:], e.Uid)
		binary.BigEndian.PutUint32(entry[32:], e.Gid)
		binary.BigEndian.PutUint32(entry[36:], uint32(e.Size))
		copy(entry[40:], sha)
		flags := uint16(len(e.Path))
		if len(e.Path) >= 0xFFF {
			flags = 0xFFF
		}
		binary.BigEndian.PutUint16(entry[60:], flags)
		buf.Write(entry[:])
		buf.WriteString(e.Path)
		// 填充 NUL 到 8 字节对齐（至少 1 个，它就是文件名的终止符）。
		buf.Write(bytes.Repeat([]byte{0}, padLen(62+len(e.Path))))
	}
	sum := sha1.Sum(buf.Bytes())
	buf.Write(sum[:])
	path := indexPath(gitDir)
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("写入索引失败：%w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
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
