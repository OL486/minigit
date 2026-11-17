package index

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadMissingIsEmpty(t *testing.T) {
	dir := t.TempDir()
	idx, err := Read(filepath.Join(dir, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	if idx.Version != 2 || len(idx.Entries) != 0 {
		t.Fatalf("缺失的索引应返回空索引：%+v", idx)
	}
}

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	idx, err := Read(gitDir)
	if err != nil {
		t.Fatal(err)
	}
	idx.Upsert(Entry{Path: "b.txt", SHA: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Mode: "100644", Size: 3, MtimeSec: 100, MtimeNsec: 200, CtimeSec: 101, CtimeNsec: 201, Dev: 2049, Ino: 12345, Uid: 1000, Gid: 1000})
	idx.Upsert(Entry{Path: "a.txt", SHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Mode: "100644", Size: 1, MtimeSec: 10, MtimeNsec: 20, CtimeSec: 11, CtimeNsec: 21, Dev: 2049, Ino: 12346, Uid: 1000, Gid: 1000})
	idx.Upsert(Entry{Path: "src/c.go", SHA: "cccccccccccccccccccccccccccccccccccccccc", Mode: "100755", Size: 9, MtimeSec: 30, MtimeNsec: 40, CtimeSec: 31, CtimeNsec: 41, Dev: 2049, Ino: 12347, Uid: 1000, Gid: 1000})
	if err := idx.Write(gitDir); err != nil {
		t.Fatal(err)
	}
	// 写出的是 v2 格式（DIRC 开头）。
	raw, err := os.ReadFile(filepath.Join(gitDir, "index"))
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 4 || string(raw[:4]) != "DIRC" {
		t.Fatalf("写出的索引应以 DIRC 开头：%q", raw[:4])
	}
	// 读回来：条目按路径排序，内容一致。
	got, err := Read(gitDir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 2 {
		t.Fatalf("读回来的版本应为 2：%d", got.Version)
	}
	if len(got.Entries) != 3 {
		t.Fatalf("期望 3 条，实际 %d", len(got.Entries))
	}
	want := []string{"a.txt", "b.txt", "src/c.go"}
	for i, p := range want {
		if got.Entries[i].Path != p {
			t.Fatalf("第 %d 条期望 %s，实际 %s", i, p, got.Entries[i].Path)
		}
	}
	if e, ok := got.Get("src/c.go"); !ok || e.Mode != "100755" || e.Size != 9 {
		t.Fatalf("Get src/c.go 不对：%+v %v", e, ok)
	}
	// stat 字段 round-trip。
	if e, _ := got.Get("b.txt"); e.CtimeSec != 101 || e.CtimeNsec != 201 || e.Dev != 2049 || e.Ino != 12345 || e.Uid != 1000 || e.Gid != 1000 || e.MtimeSec != 100 || e.MtimeNsec != 200 {
		t.Fatalf("b.txt 的 stat 字段 round-trip 不对：%+v", e)
	}
	if _, ok := got.Get("nope.txt"); ok {
		t.Fatal("不存在的路径应查不到")
	}
}

func TestUpsertUpdatesAndRemove(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	idx, _ := Read(gitDir)
	idx.Upsert(Entry{Path: "a.txt", SHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Mode: "100644", Size: 1})
	idx.Upsert(Entry{Path: "a.txt", SHA: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Mode: "100644", Size: 2})
	if len(idx.Entries) != 1 {
		t.Fatalf("同路径 Upsert 应更新而不是新增：%d 条", len(idx.Entries))
	}
	if e, _ := idx.Get("a.txt"); e.SHA != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" || e.Size != 2 {
		t.Fatalf("Upsert 没更新内容：%+v", e)
	}
	if !idx.Remove("a.txt") {
		t.Fatal("Remove 应返回 true")
	}
	if len(idx.Entries) != 0 {
		t.Fatal("Remove 后应为空")
	}
	if idx.Remove("a.txt") {
		t.Fatal("删不存在的应返回 false")
	}
}

func TestCorruptIndex(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 旧 JSON 格式损坏。
	if err := os.WriteFile(filepath.Join(gitDir, "index"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(gitDir); err == nil {
		t.Fatal("损坏的索引应报错")
	}
	// 既不是 DIRC 也不是 JSON。
	if err := os.WriteFile(filepath.Join(gitDir, "index"), []byte("garbage data here"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(gitDir); err == nil {
		t.Fatal("垃圾内容应报错")
	}
	// v2 校验和不对。
	idx := &Index{Version: 2}
	idx.Upsert(Entry{Path: "a.txt", SHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Mode: "100644", Size: 1})
	if err := idx.Write(gitDir); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(gitDir, "index"))
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 0xFF // 改坏校验和
	if err := os.WriteFile(filepath.Join(gitDir, "index"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(gitDir); err == nil {
		t.Fatal("校验和不对的索引应报错")
	}
	// v2 文件被截断。
	if err := idx.Write(gitDir); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(filepath.Join(gitDir, "index"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "index"), raw[:len(raw)-10], 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(gitDir); err == nil {
		t.Fatal("截断的索引应报错")
	}
}

// TestMigrateJSON：M2/M3 的旧 JSON 索引能被读出来，
// 下次 Write 后自动变成合法的 v2，再读内容一致。
func TestMigrateJSON(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := `{"version":1,"entries":[` +
		`{"path":"a.txt","sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","mode":"100644","size":5,"mtime_sec":100,"mtime_nsec":200},` +
		`{"path":"sub/b.txt","sha":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","mode":"100755","size":7,"mtime_sec":300,"mtime_nsec":400}` +
		`]}`
	if err := os.WriteFile(filepath.Join(gitDir, "index"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := Read(gitDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Entries) != 2 {
		t.Fatalf("旧 JSON 应读出 2 条：%d", len(idx.Entries))
	}
	if e, _ := idx.Get("a.txt"); e.Size != 5 || e.MtimeSec != 100 {
		t.Fatalf("旧 JSON 条目内容不对：%+v", e)
	}
	// JSON 里没有的 stat 字段补零。
	if e, _ := idx.Get("a.txt"); e.CtimeSec != 0 || e.Dev != 0 || e.Ino != 0 {
		t.Fatalf("JSON 缺失的 stat 字段应补零：%+v", e)
	}
	// 写回去：应变成 v2，再读内容一致。
	if err := idx.Write(gitDir); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(gitDir, "index"))
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 4 || string(raw[:4]) != "DIRC" {
		t.Fatal("迁移后写出的应是 v2（DIRC 开头）")
	}
	got, err := Read(gitDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 2 {
		t.Fatalf("迁移后应仍有 2 条：%d", len(got.Entries))
	}
	if e, _ := got.Get("sub/b.txt"); e.Mode != "100755" || e.Size != 7 || e.SHA != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("迁移后条目不对：%+v", e)
	}
}

// buildV2 拼一个 v2 索引文件（测试用）：头部 + 给定的条目字节
// + 给定的扩展字节 + SHA-1 校验和。
func buildV2(entries, extensions []byte, count uint32) []byte {
	var buf bytes.Buffer
	buf.WriteString("DIRC")
	var tmp [4]byte
	binary.BigEndian.PutUint32(tmp[:], 2)
	buf.Write(tmp[:])
	binary.BigEndian.PutUint32(tmp[:], count)
	buf.Write(tmp[:])
	buf.Write(entries)
	buf.Write(extensions)
	sum := sha1.Sum(buf.Bytes())
	buf.Write(sum[:])
	return buf.Bytes()
}

// v2Entry 拼一个 v2 条目（62 字节固定字段 + 文件名 + 填充）。
// nameLenOverride >= 0 时强制 flags 里的文件名长度字段
// （用来造 0xFFF 的超长名条目）。
func v2Entry(name string, nameLenOverride int) []byte {
	var entry [62]byte
	binary.BigEndian.PutUint32(entry[24:], 0o100644)
	binary.BigEndian.PutUint32(entry[36:], uint32(len(name)))
	sha, _ := hex.DecodeString("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	copy(entry[40:], sha)
	flags := len(name)
	if nameLenOverride >= 0 {
		flags = nameLenOverride
	}
	binary.BigEndian.PutUint16(entry[60:], uint16(flags))
	var buf bytes.Buffer
	buf.Write(entry[:])
	buf.WriteString(name)
	total := 62 + len(name)
	pad := 8 - total%8
	if pad == 0 {
		pad = 8
	}
	buf.Write(bytes.Repeat([]byte{0}, pad))
	return buf.Bytes()
}

// TestLongName：flags 长度字段为 0xFFF 的超长文件名，
// 读的时候按 NUL 扫描、再 8 字节对齐。
func TestLongName(t *testing.T) {
	name := strings.Repeat("x", 4095) // 正好 0xFFF
	data := buildV2(v2Entry(name, 0xFFF), nil, 1)
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "index"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := Read(gitDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Entries) != 1 || idx.Entries[0].Path != name {
		t.Fatalf("超长文件名读不对：%d 条", len(idx.Entries))
	}
}

// TestExtensionsSkipped：条目之后的扩展（认识的 TREE 和不认识的）
// 都要能跳过，条目照常解析。
func TestExtensionsSkipped(t *testing.T) {
	var ext bytes.Buffer
	ext.WriteString("TREE")
	var tmp [4]byte
	binary.BigEndian.PutUint32(tmp[:], 4)
	ext.Write(tmp[:])
	ext.Write([]byte{1, 2, 3, 4})
	ext.WriteString("XXXX") // 不认识的扩展
	binary.BigEndian.PutUint32(tmp[:], 2)
	ext.Write(tmp[:])
	ext.Write([]byte{9, 9})
	data := buildV2(v2Entry("a.txt", -1), ext.Bytes(), 1)
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "index"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := Read(gitDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Entries) != 1 || idx.Entries[0].Path != "a.txt" {
		t.Fatalf("带扩展的索引读不对：%+v", idx.Entries)
	}
}

// TestWriteRejectsLongName：文件名 ≥ 0xFFF 时写盘直接报错。
func TestWriteRejectsLongName(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	idx := &Index{Version: 2}
	idx.Upsert(Entry{Path: strings.Repeat("y", 4095), SHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Mode: "100644"})
	if err := idx.Write(gitDir); err == nil {
		t.Fatal("超长文件名写盘应报错")
	}
}
