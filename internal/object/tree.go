package object

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// Entry 是 tree 对象里的一项。
//
// Mode 是 ASCII 八进制，不带前导零：文件 "100644"，目录 "40000"。
// git 在 cat-file -p 里会把它补成 6 位显示（"040000"），见 Entry.DisplayMode。
type Entry struct {
	Mode string // "100644" 或 "40000"
	Name string
	SHA  string // 40 位十六进制
}

// Type 由 mode 推断：目录是 tree，其余是 blob（M1 不处理子模块的 commit 项）。
func (e Entry) Type() Type {
	if e.Mode == "40000" {
		return Tree
	}
	return Blob
}

// DisplayMode 返回 git cat-file -p 的显示形式：补齐到 6 位。
func (e Entry) DisplayMode() string {
	if len(e.Mode) < 6 {
		return strings.Repeat("0", 6-len(e.Mode)) + e.Mode
	}
	return e.Mode
}

// sortKey 是 git 的 tree 排序键：目录名按“名字 + /”参与比较。
func sortKey(e Entry) string {
	if e.Mode == "40000" {
		return e.Name + "/"
	}
	return e.Name
}

// Encode 把条目编码成 tree 对象的原始内容。
// 每项格式： "<mode> <name>\x00" + 20 字节二进制 SHA。
// 条目按 git 的规则排序（目录名带 "/" 比较）。
func Encode(entries []Entry) []byte {
	sorted := append([]Entry(nil), entries...)
	sort.Slice(sorted, func(i, j int) bool { return sortKey(sorted[i]) < sortKey(sorted[j]) })
	var buf bytes.Buffer
	for _, e := range sorted {
		fmt.Fprintf(&buf, "%s %s\x00", e.Mode, e.Name)
		b, err := hex.DecodeString(e.SHA)
		if err != nil || len(b) != 20 {
			// 编码时 SHA 非法是调用方 bug，直接 panic 比写出损坏对象好。
			panic(fmt.Sprintf("tree 条目 SHA 非法：%q", e.SHA))
		}
		buf.Write(b)
	}
	return buf.Bytes()
}

// Decode 解析 tree 对象的原始内容。
func Decode(data []byte) ([]Entry, error) {
	var entries []Entry
	for len(data) > 0 {
		sp := bytes.IndexByte(data, ' ')
		nul := bytes.IndexByte(data, 0)
		if sp < 0 || nul < 0 || sp > nul || nul+21 > len(data) {
			return nil, fmt.Errorf("tree 对象损坏（剩余 %d 字节无法解析）", len(data))
		}
		mode := string(data[:sp])
		name := string(data[sp+1 : nul])
		sha := hex.EncodeToString(data[nul+1 : nul+21])
		entries = append(entries, Entry{Mode: mode, Name: name, SHA: sha})
		data = data[nul+21:]
	}
	return entries, nil
}
