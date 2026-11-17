#!/bin/sh
# M1+M2+M3+M4+M5 演示：init → hash-object -w → cat-file（M1），
# 再 add → commit → log → status（M2），branch / checkout（M3），
# 改文件 → minigit diff 与 git diff 对照 → add →
# git status 直接跑 → git fsck 直接跑（M4），最后 merge：
# 干净合并与 git merge 对照 tree SHA、fast-forward、
# 冲突 + 手动解决 + 提交、git fsck 直接跑（M5）。
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

# git fsck 直接通过。M4 索引换成 v2 格式后不再需要挪走索引
#（M2 时索引是临时 JSON 格式，fsck 会报 "index file corrupt"）。
if ! git fsck >/dev/null 2>&1; then
	echo "FAIL：git fsck 不通过"
	exit 1
fi
echo "git fsck OK（直接跑，索引不挪走）"

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

# 真 git 切 minigit 建的分支：在仓库副本里做——M4 索引已是 v2 格式，
# git 直接能读，不再需要删索引 + git read-tree 重建。
COPY=$(mktemp -d)
trap 'rm -rf "$WORK" ${COPY:-}' EXIT
cp -a . "$COPY/repo"
cd "$COPY/repo"
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
echo "真 git checkout 切 minigit 的分支 OK（直接读 v2 索引）"

echo "=== M4：diff 与索引对齐 ==="

# 回到原仓库（当前在 feature 分支，a.txt 是 v-feature）。
cd "$WORK/demo"

# 改文件：minigit diff 与 git diff 归一化后逐行一致
#（归一化掉 index 行里的 SHA 前缀；minigit 固定取 7 位，
# git 的缩写长度可能不同，SHA 本身由 hash-object 测试保证）。
printf 'v-feature-2\nsecond line\n' > a.txt
"$BIN" diff > /tmp/mg-demo-mini-diff.txt
git diff | sed -E 's/index [0-9a-f]+\.\.[0-9a-f]+/index X..X/' > /tmp/mg-demo-git-diff.txt
sed -E 's/index [0-9a-f]+\.\.[0-9a-f]+/index X..X/' /tmp/mg-demo-mini-diff.txt > /tmp/mg-demo-mini-diff-norm.txt
if ! cmp -s /tmp/mg-demo-mini-diff-norm.txt /tmp/mg-demo-git-diff.txt; then
	echo "FAIL：minigit diff 与 git diff 不一致"
	diff /tmp/mg-demo-mini-diff-norm.txt /tmp/mg-demo-git-diff.txt || true
	exit 1
fi
echo "minigit diff 与 git diff 一致 OK"

# 暂存后：minigit diff --cached 与 git diff --cached 对照。
"$BIN" add a.txt
"$BIN" diff --cached > /tmp/mg-demo-mini-diff-c.txt
git diff --cached | sed -E 's/index [0-9a-f]+\.\.[0-9a-f]+/index X..X/' > /tmp/mg-demo-git-diff-c.txt
sed -E 's/index [0-9a-f]+\.\.[0-9a-f]+/index X..X/' /tmp/mg-demo-mini-diff-c.txt > /tmp/mg-demo-mini-diff-c-norm.txt
if ! cmp -s /tmp/mg-demo-mini-diff-c-norm.txt /tmp/mg-demo-git-diff-c.txt; then
	echo "FAIL：minigit diff --cached 与 git diff --cached 不一致"
	diff /tmp/mg-demo-mini-diff-c-norm.txt /tmp/mg-demo-git-diff-c.txt || true
	exit 1
fi
echo "minigit diff --cached 与 git diff --cached 一致 OK"

# 真 git 的 git status 在 minigit 仓库里直接跑正常。
GS=$(git status)
case "$GS" in *"modified:   a.txt"*) ;; *) echo "FAIL：git status 看不到已暂存的 a.txt：$GS"; exit 1;; esac
echo "git status 直接跑 OK（能看到已暂存修改）"

# git fsck 直接跑通过（M2 的已知限制本轮消除）。
if ! git fsck >/dev/null 2>&1; then
	echo "FAIL：git fsck 不通过"
	exit 1
fi
echo "git fsck 直接跑 OK"

# === M5：merge ===
echo "=== M5：merge ==="
cd "$WORK"
"$BIN" init demo-merge >/dev/null
cd demo-merge

# 基础提交：a.txt 3 行 + b.txt。
printf 'line1\nline2\nline3\n' > a.txt
printf 'base\n' > b.txt
"$BIN" add a.txt b.txt
"$BIN" commit -m "base" >/dev/null

# 分支 feature：改 a.txt 第 3 行。
"$BIN" branch feature
"$BIN" checkout feature >/dev/null
printf 'line1\nline2\nline3-feature\n' > a.txt
"$BIN" add a.txt
"$BIN" commit -m "feature change" >/dev/null

# 回 main：改 a.txt 第 1 行（与 feature 改的不同行，干净合并）。
"$BIN" checkout main >/dev/null
printf 'line1-main\nline2\nline3\n' > a.txt
"$BIN" add a.txt
"$BIN" commit -m "main change" >/dev/null

# 干净合并 feature。
"$BIN" merge feature > merge-out.txt
cat merge-out.txt
TREE_MINI=$(git rev-parse 'HEAD^{tree}')

# 同样场景用真 git 跑一遍，对照 tree SHA。
GITWORK=$(mktemp -d)
trap 'rm -rf "$WORK" ${COPY:-} ${GITWORK:-}' EXIT
git init -q -b main "$GITWORK/grepo"
cd "$GITWORK/grepo"
printf 'line1\nline2\nline3\n' > a.txt
printf 'base\n' > b.txt
git add a.txt b.txt
git commit -qm "base"
git branch feature
git checkout -q feature
printf 'line1\nline2\nline3-feature\n' > a.txt
git add a.txt
git commit -qm "feature change"
git checkout -q main
printf 'line1-main\nline2\nline3\n' > a.txt
git add a.txt
git commit -qm "main change"
git merge -q --no-edit -m "Merge branch 'feature'" feature
TREE_GIT=$(git rev-parse 'HEAD^{tree}')
cd "$WORK/demo-merge"
if [ "$TREE_MINI" != "$TREE_GIT" ]; then
	echo "FAIL：干净合并的 tree SHA 与 git 不同：minigit $TREE_MINI vs git $TREE_GIT"
	exit 1
fi
echo "干净合并 tree SHA 与 git 一致 OK：$TREE_MINI"

# fast-forward：从 main 再分一个分支，只往前走。
"$BIN" branch ff
"$BIN" checkout ff >/dev/null
printf 'line1-main\nline2\nline3-feature\nline4\n' > a.txt
"$BIN" add a.txt
"$BIN" commit -m "ff commit" >/dev/null
"$BIN" checkout main >/dev/null
"$BIN" merge ff > ff-out.txt
case "$(cat ff-out.txt)" in *"Fast-forward"*) ;; *) echo "FAIL：应 fast-forward，得到：$(cat ff-out.txt)"; exit 1;; esac
FF_HEAD=$(git rev-parse HEAD)
FF_TIP=$(git rev-parse ff)
if [ "$FF_HEAD" != "$FF_TIP" ]; then
	echo "FAIL：fast-forward 后 HEAD 应与 ff 相同"
	exit 1
fi
echo "fast-forward OK"

# 冲突：双方改 a.txt 第 1 行。
"$BIN" branch conflict
"$BIN" checkout conflict >/dev/null
printf 'line1-conflict\nline2\nline3-feature\nline4\n' > a.txt
"$BIN" add a.txt
"$BIN" commit -m "conflict change" >/dev/null
"$BIN" checkout main >/dev/null
printf 'line1-main2\nline2\nline3-feature\nline4\n' > a.txt
"$BIN" add a.txt
"$BIN" commit -m "main change 2" >/dev/null
if "$BIN" merge conflict 2>/dev/null; then
	echo "FAIL：冲突合并应 exit 1"
	exit 1
fi
grep -q '<<<<<<< ours' a.txt || { echo "FAIL：冲突文件缺少 <<<<<<< ours 标记"; exit 1; }
grep -q '>>>>>>> theirs' a.txt || { echo "FAIL：冲突文件缺少 >>>>>>> theirs 标记"; exit 1; }
echo "冲突标记 OK"
# status 提示正在合并。
S=$("$BIN" status)
case "$S" in *"正在合并 conflict（有冲突未解决）"*) ;; *) echo "FAIL：status 应提示正在合并：$S"; exit 1;; esac
echo "status 合并提示 OK"
# 手动解决：取 main 的第 1 行，add，commit（不带 -m，信息取 MERGE_MSG）。
printf 'line1-main2\nline2\nline3-feature\nline4\n' > a.txt
"$BIN" add a.txt
"$BIN" commit >/dev/null
P=$(git cat-file -p HEAD | grep -c '^parent ')
if [ "$P" != "2" ]; then
	echo "FAIL：合并提交应有 2 个 parent，实际 $P"
	exit 1
fi
echo "解决冲突后提交、2 个 parent OK"
# git fsck 直接通过。
if ! git fsck >/dev/null 2>&1; then
	echo "FAIL：git fsck 不通过"
	exit 1
fi
echo "git fsck 直接跑 OK"

echo "demo 全部通过"
