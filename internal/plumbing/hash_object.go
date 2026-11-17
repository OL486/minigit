// Package plumbing 是底层命令（单步可组合），对应 git 的 plumbing 层。
package plumbing

import (
	"fmt"
	"os"

	"github.com/OL486/minigit/internal/object"
)

// HashObject 计算文件的 blob SHA-1。write 为 true 时同时把对象写入对象库
// （对应 git hash-object -w）；为 false 时只算不写（git 的默认行为）。
func HashObject(gitDir, path string, write bool) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("读取文件失败：%w", err)
	}
	sha := object.Hash(object.Blob, data)
	if write {
		if _, err := object.WriteObject(gitDir, object.Blob, data); err != nil {
			return "", err
		}
	}
	return sha, nil
}
