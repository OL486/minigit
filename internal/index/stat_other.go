//go:build !linux

package index

import "os"

// statFields 在非 Linux 平台上拿不到 ctime/dev/ino/uid/gid，
// 返回零值。索引仍可写盘；git 读到零值只会重新算内容哈希，
// 结果正确、只是 status 的快速判断会失效。
func statFields(fi os.FileInfo) (ctimeSec, ctimeNsec int64, dev, ino uint32, uid, gid uint32) {
	return 0, 0, 0, 0, 0, 0
}
