package object

import (
	"bytes"
	"encoding/hex"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHashKnownVectors(t *testing.T) {
	// 空 blob 的 SHA 是 git 里最著名的常量。
	if got := Hash(Blob, nil); got != "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391" {
		t.Fatalf("空 blob SHA 不对：%s", got)
	}
	// echo "hello world" | git hash-object --stdin 的结果。
	if got := Hash(Blob, []byte("hello world\n")); got != "3b18e512dba79e4c8300dd08aeb37f8e728b8dad" {
		t.Fatalf("hello world blob SHA 不对：%s", got)
	}
}

func TestWriteReadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(filepath.Join(gitDir, "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	rng := rand.New(rand.NewSource(42))
	sizes := []int{0, 1, 17, 100, 4096, 1 << 20}
	for _, typ := range []Type{Blob, Tree, Commit} {
		for _, n := range sizes {
			data := make([]byte, n)
			rng.Read(data)
			sha, err := WriteObject(gitDir, typ, data)
			if err != nil {
				t.Fatalf("WriteObject(%s, %d 字节) 失败：%v", typ, n, err)
			}
			if sha != Hash(typ, data) {
				t.Fatalf("WriteObject 返回的 SHA 与 Hash 不一致：%s vs %s", sha, Hash(typ, data))
			}
			gotTyp, got, err := ReadObject(gitDir, sha)
			if err != nil {
				t.Fatalf("ReadObject(%s) 失败：%v", sha, err)
			}
			if gotTyp != typ || !bytes.Equal(got, data) {
				t.Fatalf("往返不一致：类型 %s/%s，内容相等=%v", gotTyp, typ, bytes.Equal(got, data))
			}
		}
	}
	// 重复写同一对象：第二次应直接返回，不报错。
	sha1, err := WriteObject(gitDir, Blob, []byte("dup"))
	if err != nil {
		t.Fatal(err)
	}
	sha2, err := WriteObject(gitDir, Blob, []byte("dup"))
	if err != nil || sha1 != sha2 {
		t.Fatalf("重复写对象失败：%v / %s vs %s", err, sha1, sha2)
	}
}

func TestTreeEncodeDecode(t *testing.T) {
	emptyBlob := "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391"
	other := Hash(Blob, []byte("x"))
	entries := []Entry{
		{Mode: "40000", Name: "dir", SHA: other},
		{Mode: "100644", Name: "a.txt", SHA: emptyBlob},
		{Mode: "100644", Name: "b.txt", SHA: other},
	}
	enc := Encode(entries)
	// 手工拼期望字节：排序后是 a.txt、b.txt、dir（目录按 "dir/" 比较，仍在 b.txt 之后）。
	var want bytes.Buffer
	for _, e := range []Entry{
		{Mode: "100644", Name: "a.txt", SHA: emptyBlob},
		{Mode: "100644", Name: "b.txt", SHA: other},
		{Mode: "40000", Name: "dir", SHA: other},
	} {
		want.WriteString(e.Mode + " " + e.Name + "\x00")
		b, _ := hex.DecodeString(e.SHA)
		want.Write(b)
	}
	if !bytes.Equal(enc, want.Bytes()) {
		t.Fatalf("tree 编码字节不对：\n%x\n%x", enc, want.Bytes())
	}
	dec, err := Decode(enc)
	if err != nil {
		t.Fatal(err)
	}
	if len(dec) != 3 || dec[0].Name != "a.txt" || dec[2].Mode != "40000" || dec[2].SHA != other {
		t.Fatalf("tree 解码不对：%+v", dec)
	}
	// 目录排序键：名字 "a" 的文件与名字 "a-" 的目录——"a" < "a-/" 所以文件在前；
	// 而名字 "a.txt" 的目录与名字 "a.txt2" 的文件比，"a.txt/" < "a.txt2"，目录在前。
	enc2 := Encode([]Entry{
		{Mode: "100644", Name: "a.txt2", SHA: emptyBlob},
		{Mode: "40000", Name: "a.txt", SHA: other},
	})
	dec2, err := Decode(enc2)
	if err != nil {
		t.Fatal(err)
	}
	if dec2[0].Name != "a.txt" || dec2[0].Mode != "40000" {
		t.Fatalf("目录排序键不对：%+v", dec2)
	}
	if _, err := Decode([]byte("100644 broken")); err == nil {
		t.Fatal("损坏的 tree 应报错")
	}
}

func TestCommitParseEncode(t *testing.T) {
	raw := "tree 4b825dc642cb6eb9a060e54bf8d69288fbee4904\n" +
		"parent 3b18e512dba79e4c8300dd08aeb37f8e728b8dad\n" +
		"author RikStar <rik@example.com> 1794897000 +0800\n" +
		"committer RikStar <rik@example.com> 1794897000 +0800\n" +
		"\n" +
		"test commit\n"
	c, err := Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if c.Tree != "4b825dc642cb6eb9a060e54bf8d69288fbee4904" || len(c.Parents) != 1 ||
		c.Author != "RikStar <rik@example.com> 1794897000 +0800" || c.Message != "test commit\n" {
		t.Fatalf("commit 解析不对：%+v", c)
	}
	if got := string(c.Encode()); got != raw {
		t.Fatalf("commit 编解码往返不对：\n%q\n%q", got, raw)
	}
	// 无 parent（根提交）与多 parent（合并提交）都要能解析。
	root := "tree 4b825dc642cb6eb9a060e54bf8d69288fbee4904\nauthor A <a@x> 1 +0000\ncommitter A <a@x> 1 +0000\n\nroot\n"
	rc, err := Parse([]byte(root))
	if err != nil || len(rc.Parents) != 0 {
		t.Fatalf("根提交解析失败：%v %+v", err, rc)
	}
	if _, err := Parse([]byte("no blank line here")); err == nil {
		t.Fatal("缺少空行分隔的 commit 应报错")
	}
	if _, err := Parse([]byte("author A <a@x> 1 +0000\n\nmsg\n")); err == nil {
		t.Fatal("缺少 tree 行的 commit 应报错")
	}
}

func TestResolveSHA(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(filepath.Join(gitDir, "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	sha, err := WriteObject(gitDir, Blob, []byte("resolve me"))
	if err != nil {
		t.Fatal(err)
	}
	// 完整 SHA 与 7 位前缀都能解析。
	if got, err := ResolveSHA(gitDir, sha); err != nil || got != sha {
		t.Fatalf("完整 SHA 解析失败：%v %s", err, got)
	}
	if got, err := ResolveSHA(gitDir, sha[:7]); err != nil || got != sha {
		t.Fatalf("7 位前缀解析失败：%v %s", err, got)
	}
	// 不存在的 SHA、前缀太短、非法字符都要报错。
	for _, bad := range []string{"0000000000000000000000000000000000000000", "deadbee", "ab", "zzzzzzzz"} {
		if _, err := ResolveSHA(gitDir, bad); err == nil {
			t.Fatalf("ResolveSHA(%q) 应报错", bad)
		}
	}
	// 造两个共享 4 位前缀的对象，验证“不唯一”报错。
	rng := rand.New(rand.NewSource(7))
	seen := map[string]string{} // 4 位前缀 -> SHA
	var ambiguous string
	for i := 0; i < 5000; i++ {
		data := make([]byte, 16)
		rng.Read(data)
		s, err := WriteObject(gitDir, Blob, data)
		if err != nil {
			t.Fatal(err)
		}
		if prev, ok := seen[s[:4]]; ok && prev != s {
			ambiguous = s[:4]
			break
		}
		seen[s[:4]] = s
	}
	if ambiguous == "" {
		t.Fatal("5000 个对象都没撞出 4 位前缀，异常")
	}
	if _, err := ResolveSHA(gitDir, ambiguous); err == nil || !strings.Contains(err.Error(), "不唯一") {
		t.Fatalf("共享前缀应报“不唯一”，实际：%v", err)
	}
}

func TestReadObjectCorrupt(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	objDir := filepath.Join(gitDir, "objects", "ab")
	if err := os.MkdirAll(objDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 不是 zlib 流。
	if err := os.WriteFile(filepath.Join(objDir, "cdef0123456789abcdef0123456789abcdef01"), []byte("not zlib at all"), 0o444); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadObject(gitDir, "abcdef0123456789abcdef0123456789abcdef01"); err == nil {
		t.Fatal("损坏的对象应报错")
	}
}
