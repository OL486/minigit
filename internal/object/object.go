// Package object 实现 git 的松散对象格式：blob / tree / commit。
//
// 对象在磁盘上的格式与 git 完全一致：
//   - 内容 = "<类型> <长度>\x00<原始内容>"，SHA-1 取该字节串的哈希
//   - zlib 压缩后存为 .git/objects/xx/yyyy...（SHA 前两位作目录名）
//
// 本包不做 packfile、不做 tag 对象（见规划“不做清单”）。
package object

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Type 是 git 对象类型。
type Type string

const (
	Blob   Type = "blob"
	Tree   Type = "tree"
	Commit Type = "commit"
)

// Hash 计算对象的 SHA-1：对 "<类型> <长度>\x00<内容>" 取哈希。
func Hash(typ Type, data []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "%s %d\x00", typ, len(data))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

// objectPath 返回对象在对象库中的路径。
func objectPath(gitDir, sha string) string {
	return filepath.Join(gitDir, "objects", sha[:2], sha[2:])
}

// WriteObject 把对象写入对象库（已存在时直接返回，不重复写）。
// 返回对象的 SHA-1（十六进制 40 位）。
func WriteObject(gitDir string, typ Type, data []byte) (string, error) {
	sha := Hash(typ, data)
	path := objectPath(gitDir, sha)
	if _, err := os.Stat(path); err == nil {
		return sha, nil // 已存在
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("创建对象目录失败：%w", err)
	}
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := fmt.Fprintf(zw, "%s %d\x00", typ, len(data)); err != nil {
		return "", err
	}
	if _, err := zw.Write(data); err != nil {
		return "", err
	}
	if err := zw.Close(); err != nil {
		return "", err
	}
	// 先写临时文件再改名，避免半截对象留在库里。
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o444); err != nil {
		return "", fmt.Errorf("写入对象失败：%w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("写入对象失败：%w", err)
	}
	return sha, nil
}

// ReadObject 从对象库读出对象，返回类型与原始内容。
// sha 可以是完整 40 位，也可以是唯一短前缀。
func ReadObject(gitDir, sha string) (Type, []byte, error) {
	full, err := ResolveSHA(gitDir, sha)
	if err != nil {
		return "", nil, err
	}
	f, err := os.Open(objectPath(gitDir, full))
	if err != nil {
		return "", nil, fmt.Errorf("对象不存在：%s", sha)
	}
	defer f.Close()
	zr, err := zlib.NewReader(f)
	if err != nil {
		return "", nil, fmt.Errorf("对象 %s 损坏（zlib 解压失败）：%w", sha, err)
	}
	defer zr.Close()
	raw, err := io.ReadAll(zr)
	if err != nil {
		return "", nil, fmt.Errorf("对象 %s 损坏（读取失败）：%w", sha, err)
	}
	// 解析 "<类型> <长度>\x00<内容>"。
	nul := bytes.IndexByte(raw, 0)
	if nul < 0 {
		return "", nil, fmt.Errorf("对象 %s 损坏（缺少头部）", sha)
	}
	header := string(raw[:nul])
	typStr, lenStr, ok := strings.Cut(header, " ")
	if !ok {
		return "", nil, fmt.Errorf("对象 %s 损坏（头部格式错误）", sha)
	}
	var length int
	if _, err := fmt.Sscanf(lenStr, "%d", &length); err != nil {
		return "", nil, fmt.Errorf("对象 %s 损坏（长度不是数字）：%w", sha, err)
	}
	data := raw[nul+1:]
	if len(data) != length {
		return "", nil, fmt.Errorf("对象 %s 损坏（长度不符：头部 %d，实际 %d）", sha, length, len(data))
	}
	typ := Type(typStr)
	switch typ {
	case Blob, Tree, Commit:
		return typ, data, nil
	default:
		return "", nil, fmt.Errorf("对象 %s 类型未知：%q", sha, typStr)
	}
}

// ResolveSHA 把短前缀解析成完整 40 位 SHA。
// 完整 SHA 直接校验存在性；短前缀在对象库里查找唯一匹配。
func ResolveSHA(gitDir, prefix string) (string, error) {
	prefix = strings.ToLower(prefix)
	if len(prefix) < 4 {
		return "", fmt.Errorf("SHA 前缀太短：%q（至少 4 位）", prefix)
	}
	if !isHex(prefix) {
		return "", fmt.Errorf("无效的 SHA：%q（只允许十六进制）", prefix)
	}
	if len(prefix) == 40 {
		if _, err := os.Stat(objectPath(gitDir, prefix)); err != nil {
			return "", fmt.Errorf("对象不存在：%s", prefix)
		}
		return prefix, nil
	}
	dir := filepath.Join(gitDir, "objects", prefix[:2])
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("对象不存在：%s", prefix)
	}
	var matches []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), prefix[2:]) {
			matches = append(matches, prefix[:2]+e.Name())
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("对象不存在：%s", prefix)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("SHA 前缀 %q 不唯一，匹配到 %d 个对象", prefix, len(matches))
	}
}

func isHex(s string) bool {
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return len(s) > 0
}
