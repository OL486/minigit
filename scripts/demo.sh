#!/bin/sh
# M1+M2 演示：init → hash-object -w → cat-file（M1），
# 再 add → commit → log → status（M2），并断言 SHA-1 与真 git 一致、
# git log 能读提交历史、git fsck 通过。
# 用法：./scripts/demo.sh <minigit 二进制路径>
set -eu

BIN="$1"
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
cd "$WORK"

export GIT_AUTHOR_NAME="Demo User" GIT_AUTHOR_EMAIL="demo@example.com"
export GIT_COMMITTER_NAME="Demo User" GIT_COMMITTER_EMAIL="demo@example.com"

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

echo "=== M2：add → commit → log → status ==="

# M1 演示留下的两个文件（只用来测 hash-object / cat-file），先删掉，
# 让 M2 从干净的仓库开始。
rm -f hello.txt from-git.txt

# 第一次提交。
printf 'v1\n' > a.txt
printf 'package main\n' > main.go
"$BIN" add a.txt main.go
"$BIN" commit -m "first commit" >/dev/null
C1=$("$BIN" log --oneline | head -n 1)
case "$C1" in *"first commit") ;; *) echo "FAIL：log --oneline 不对：$C1"; exit 1;; esac
echo "第一次提交 OK：$C1"

# 第二次提交：改文件。
printf 'v2\n' > a.txt
"$BIN" add a.txt
"$BIN" commit -m "second commit" >/dev/null
N=$("$BIN" log --oneline | wc -l)
if [ "$N" != "2" ]; then
	echo "FAIL：log 应有 2 条，实际 $N"
	exit 1
fi
echo "第二次提交 OK，log 共 2 条"

# git log 能读 minigit 的提交历史。
GLOG=$(git log --format=%s)
if [ "$GLOG" != "second commit
first commit" ]; then
	echo "FAIL：git log 读历史不对：$GLOG"
	exit 1
fi
echo "git log 读历史 OK"

# status 三种状态。
S=$("$BIN" status)
case "$S" in *"nothing to commit, working tree clean"*) ;; *) echo "FAIL：提交后 status 应干净：$S"; exit 1;; esac
echo "status 干净 OK"

printf 'v3\n' > a.txt
S=$("$BIN" status)
case "$S" in *"Changes not staged for commit:"*"modified:   a.txt"*) ;; *) echo "FAIL：改文件后 status 不对：$S"; exit 1;; esac
echo "status 未暂存修改 OK"

"$BIN" add a.txt
S=$("$BIN" status)
case "$S" in *"Changes to be committed:"*"modified:   a.txt"*) ;; *) echo "FAIL：add 后 status 不对：$S"; exit 1;; esac
echo "status 已暂存修改 OK"

printf 'new\n' > new.txt
S=$("$BIN" status)
case "$S" in *"Untracked files:"*"new.txt"*) ;; *) echo "FAIL：未追踪文件 status 不对：$S"; exit 1;; esac
echo "status 未追踪 OK"

# git fsck 通过。注意：fsck 会读 .git/index，而 M2 的索引是临时
# JSON 格式，直接跑会被 git 判定为 "index file corrupt"，所以
# 这里临时把索引挪走再跑（对象本身是合法的，M4 换 v2 后不需要）。
mv .git/index /tmp/mg-demo-index-json
if ! git fsck >/dev/null 2>&1; then
	echo "FAIL：git fsck 不通过"
	exit 1
fi
mv /tmp/mg-demo-index-json .git/index
echo "git fsck OK（索引临时挪走后跑）"

echo "=== M3：branch / checkout ==="

# M2 演示留下的未追踪文件（只用来测 status），先删掉，
# 让 M3 从干净的仓库开始。
rm -f new.txt

# 建分支：从当前 HEAD（second commit）分出 feature。
"$BIN" branch feature
B=$("$BIN" branch)
if [ "$B" != "  feature
* main" ]; then
	echo "FAIL：branch 列表不对：$B"
	exit 1
fi
echo "branch 建分支 + 列表 OK"

# 在 feature 上改文件提交。
"$BIN" checkout feature >/dev/null
printf 'v-feature\n' > a.txt
"$BIN" add a.txt
"$BIN" commit -m "feature commit" >/dev/null
echo "feature 上提交 OK"

# 切回 main：a.txt 还原成 main 的版本（v2）。
"$BIN" checkout main >/dev/null
A=$(cat a.txt)
if [ "$A" != "v2" ]; then
	echo "FAIL：切回 main 后 a.txt 应为 v2，实际 $A"
	exit 1
fi
S=$("$BIN" status)
case "$S" in *"nothing to commit, working tree clean"*) ;; *) echo "FAIL：切回后 status 应干净：$S"; exit 1;; esac
echo "checkout 切回 main、内容还原、status 干净 OK"

# git branch / git log --all 读 minigit 的分支与历史。
GB=$(git branch)
if [ "$GB" != "  feature
* main" ]; then
	echo "FAIL：git branch 不对：$GB"
	exit 1
fi
GA=$(git log --all --oneline | wc -l)
if [ "$GA" != "3" ]; then
	echo "FAIL：git log --all 应有 3 条，实际 $GA"
	exit 1
fi
echo "git branch / git log --all OK"

# 脏切换被挡住：改 a.txt 不提交，目标分支里 a.txt 不同。
printf 'v-dirty\n' > a.txt
if "$BIN" checkout feature 2>/dev/null; then
	echo "FAIL：脏切换应被挡住"
	exit 1
fi
echo "脏切换被挡住 OK"
# 恢复干净：把 a.txt 改回去。
printf 'v2\n' > a.txt

# 未追踪文件被挡住：在 feature 上才有的文件，main 上有个同名不同内容。
"$BIN" checkout feature >/dev/null
printf 'feature only\n' > only.txt
"$BIN" add only.txt
"$BIN" commit -m "add only" >/dev/null
"$BIN" checkout main >/dev/null
printf 'other content\n' > only.txt
if "$BIN" checkout feature 2>/dev/null; then
	echo "FAIL：未追踪文件会被覆盖时应挡住"
	exit 1
fi
echo "未追踪文件被挡住 OK"
rm -f only.txt
"$BIN" checkout feature >/dev/null
O=$(cat only.txt)
if [ "$O" != "feature only" ]; then
	echo "FAIL：切到 feature 后 only.txt 不对：$O"
	exit 1
fi
echo "checkout 到 feature、only.txt 回来 OK"

# 真 git 切 minigit 建的分支：在仓库副本里做——真 git checkout 要读
# .git/index，而我们的索引是临时 JSON 格式，git 读不懂。所以先把
# 副本的索引删掉，用 git read-tree 重建一份 v2 索引，再 checkout。
COPY=$(mktemp -d)
trap 'rm -rf "$WORK" ${COPY:-}' EXIT
cp -a . "$COPY/repo"
cd "$COPY/repo"
rm .git/index
git read-tree --reset -u HEAD >/dev/null 2>&1
git checkout main >/dev/null 2>&1
A=$(cat a.txt)
if [ "$A" != "v2" ]; then
	echo "FAIL：真 git 切到 main 后 a.txt 应为 v2，实际 $A"
	exit 1
fi
git checkout feature >/dev/null 2>&1
A=$(cat a.txt)
if [ "$A" != "v-feature" ]; then
	echo "FAIL：真 git 切到 feature 后 a.txt 应为 v-feature，实际 $A"
	exit 1
fi
echo "真 git checkout 切 minigit 的分支 OK"

echo "demo 全部通过"
