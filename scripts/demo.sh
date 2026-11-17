#!/bin/sh
# M1 演示：init → hash-object -w → cat-file 全流程，
# 并断言 SHA-1 与真 git 一致、真 git 能读 minigit 写出的对象。
# 用法：./scripts/demo.sh <minigit 二进制路径>
set -eu

BIN="$1"
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
cd "$WORK"

"$BIN" init demo >/dev/null
cd demo

printf 'hello minigit\n' > hello.txt
SHA_MINI=$("$BIN" hash-object -w hello.txt)
SHA_GIT=$(git hash-object hello.txt)
if [ "$SHA_MINI" != "$SHA_GIT" ]; then
	echo "FAIL：SHA 不一致：minigit $SHA_MINI vs git $SHA_GIT"
	exit 1
fi
echo "hash-object 一致：$SHA_MINI"

OUT=$("$BIN" cat-file -p "$SHA_MINI")
if [ "$OUT" != "hello minigit" ]; then
	echo "FAIL：cat-file 内容不对：$OUT"
	exit 1
fi
T=$("$BIN" cat-file -t "$SHA_MINI")
if [ "$T" != "blob" ]; then
	echo "FAIL：cat-file -t 不对：$T"
	exit 1
fi
echo "cat-file -p / -t 正常"

# 真 git 读 minigit 写出的对象（互读）。
GOUT=$(git cat-file -p "$SHA_MINI")
if [ "$GOUT" != "hello minigit" ]; then
	echo "FAIL：git 读不到 minigit 的对象"
	exit 1
fi
echo "互读 OK：git cat-file 能读 minigit 写出的对象"

# minigit 读真 git 写出的对象（反向互读）。
printf 'from git\n' > from-git.txt
GSHA=$(git hash-object -w from-git.txt)
MOUT=$("$BIN" cat-file -p "$GSHA")
if [ "$MOUT" != "from git" ]; then
	echo "FAIL：minigit 读不到 git 的对象"
	exit 1
fi
echo "互读 OK：minigit cat-file 能读 git 写出的对象"

# 短前缀。
SHORT=$(printf '%s' "$SHA_MINI" | cut -c1-7)
SOUT=$("$BIN" cat-file -p "$SHORT")
if [ "$SOUT" != "hello minigit" ]; then
	echo "FAIL：短前缀 cat-file 不对"
	exit 1
fi
echo "短前缀 OK：$SHORT"

echo "demo 全部通过"
