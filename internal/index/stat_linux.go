//go:build linux

package index

import (
	"os"
	"syscall"
)

// statFields 从文件信息里取出索引需要的 stat 字段：
// ctime（秒 + 纳秒）、dev、ino、uid、gid。取不到时返回零值
// （调用方照常写盘；git 读到零值只会重新算内容哈希，结果仍正确）。
func statFields(fi os.FileInfo) (ctimeSec, ctimeNsec int64, dev, ino uint32, uid, gid uint32) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || st == nil {
		return 0, 0, 0, 0, 0, 0
	}
	return st.Ctim.Sec, st.Ctim.Nsec, uint32(st.Dev), uint32(st.Ino), st.Uid, st.Gid
}
