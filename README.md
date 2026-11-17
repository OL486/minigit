# minigit

从零实现 git 的子集（Go，零外部依赖）。和 [minilang](https://github.com/OL486/minilang)、
[minidb](https://github.com/OL486/minidb) 同一系列的 from-scratch 作品：
目标是自己每天能用，并且写出来的仓库**真 git 能读**
（对象、提交、索引格式与 git 字节兼容）。

## 当前状态：M5（合并）——每天能用的完整 git 子集

M5 跑通 `merge`：fast-forward 与三路合并。行级合并复用 M4 的
Myers diff，冲突标记 `<<<<<<< ours` / `=======` / `>>>>>>> theirs`，
合并提交带两个 parent（HEAD 与目标 tip），`.git/MERGE_HEAD` /
`.git/MERGE_MSG` 与 git 同名同格式，冲突解决后 `add` + `commit`
（不带 `-m` 时信息取 MERGE_MSG 首行）完成合并；`status` 在合并
进行中顶部提示正在合并。25 组随机场景与 `git merge` 差分对照：
干净合并的 tree SHA 相同，冲突文件内容归一化标记标签后相同。
`git status` / `git fsck` / `git checkout` / `git log --all` 在
minigit 仓库里直接跑都正常。

## 能跑什么

```sh
minigit init [目录]              # 创建仓库（默认当前目录）
minigit hash-object [-w] <文件>   # 算 blob SHA-1；-w 时写入对象库
minigit cat-file -p <sha>        # 按类型打印对象内容
minigit cat-file -t <sha>        # 只打印对象类型
minigit write-tree               # 按暂存区生成 tree 对象，打印 tree SHA
minigit commit-tree <tree> [-p <parent>] -m <message>
                                 # 按 tree 创建 commit 对象，打印 commit SHA
minigit add <文件...>            # 暂存文件（支持目录，递归加，跳过 .git）
minigit commit -m <message>      # 按暂存区提交
minigit status                   # 显示工作区状态
minigit log [--oneline]          # 显示提交历史
minigit branch                   # 列出分支（当前分支前标 *，按字母序）
minigit branch <name> [start]    # 在 HEAD（或 start-point）创建分支
minigit checkout <branch>        # 切换分支
minigit diff                     # 工作区相对索引的未暂存修改（unified）
minigit diff --cached            # 索引相对 HEAD 的已暂存修改（unified）
minigit merge <branch> [-m <message>]
                                 # 合并分支（fast-forward 或三路合并；
                                 # 有冲突时 exit 1，解决后 add + commit）
```

- `branch` 不带参数列出分支；建分支时 start-point 可以是分支名、
  完整引用名、`HEAD` 或 commit SHA（完整或唯一短前缀）。
  分支名不能含 `/`（M3 不支持层级分支名）、不能是 `HEAD`、
  不能含空格、`..`、`~^:?*[`、`\`、`@{`。
- `checkout` 切换分支并更新工作区与索引：目标分支不存在时报错；
  只接受分支名，checkout commit SHA 会报 detached HEAD 不支持；
  切回当前分支时什么都不做（打印 `Already on '<分支>'`）。
  切换成功打印 `Switched to branch '<分支>'`。

- `hash-object` 不加 `-w` 时只算不写（和 git 一致）；输出的 SHA-1 与
  `git hash-object` 完全一致。
- `cat-file` 接受完整 40 位 SHA 或唯一短前缀（至少 4 位）；前缀不唯一或
  不存在时报错。
- `init` 的 HEAD 指向 `refs/heads/main`；`.git/config` 里会写
  user.name / user.email——没有 `GIT_AUTHOR_NAME` /
  `GIT_AUTHOR_EMAIL` 环境变量时先写占位值（Your Name / you@example.com）
  并提示你改 `.git/config`，不报错退出。
- `commit` 的作者与提交者取 `.git/config` 的 user.name / user.email
  （环境变量 `GIT_AUTHOR_*` / `GIT_COMMITTER_*` 优先）；暂存区为空、
  或暂存内容与 HEAD 完全相同时报 nothing to commit，不产生空提交。
- `status` 的输出格式（固定）：

```text
On branch main
[No commits yet]              # 还没有任何提交时多这一行
Changes to be committed:
  new file:   a.txt
  modified:   b.txt
  deleted:    c.txt

Changes not staged for commit:
  modified:   d.txt
  deleted:    e.txt

Untracked files:
  f.txt
```

  干净时只打印 `On branch main` + `nothing to commit, working tree clean`。
  合并进行中（`.git/MERGE_HEAD` 存在）时，`On branch` 下面多一行
  `正在合并 <分支>（有冲突未解决）`。
  索引是 git index v2 格式，`git status` / `git fsck` 直接跑都正常。

- `diff` 的输出格式与 `git diff` 一致（`diff --git` / `index` /
  `---` / `+++` / `@@` hunk，上下文 3 行，纯新增/纯删除/改 mode/
  二进制文件都对齐 git 的写法）；`diff <commit>` 本轮不做，
  会报用法错误。无改动时什么都不输出（exit 0）。

- `merge`：目标已是 HEAD 的祖先时打印 `Already up to date.`；
  HEAD 是目标的祖先时 fast-forward（移动分支指针，不产生提交）；
  否则三路合并（base 是两边的 merge-base）。工作区有已暂存 /
  未暂存的修改时拒绝合并。干净合并创建两个 parent 的提交，
  默认信息 `Merge branch '<name>'`。有冲突时打印冲突文件列表
  并 exit 1，冲突文件写带标记的内容到工作区，
  `.git/MERGE_HEAD` 与 `.git/MERGE_MSG` 写好，等用户手动解决后
  `add` + `commit`（不带 `-m` 时信息取 MERGE_MSG 首行）；
  这次 `commit` 带两个 parent，成功后清除合并状态。
  冲突文件里还有 `<<<<<<< ` 标记时提交会被拒绝。
  接受删除那一边时：删掉工作区文件再 `add <文件>` 即可
  （`add` 已删除的被追踪文件就是暂存删除）。`merge --abort` 不做。
  三路合并的规则表、merge-base 算法、与 git 的差异点见
  [docs/03-merge.md](docs/03-merge.md)。

## 不能跑什么（不在规划里）

- `diff <commit>`（只做了工作区/索引两档）、`rm`（没有 rm 命令；
  删文件要手动从工作区删，再 `add <文件>` 暂存删除）
- `merge --abort`（放弃合并：手动删 `.git/MERGE_HEAD` 与
  `.git/MERGE_MSG`）、`branch -d`（删分支）、detached HEAD、
  层级分支名（名字里含 `/`）
- 网络传输（clone / fetch / push / pull 一律不做）、packfile、
  rebase、stash、tag 对象、重命名检测、sparse checkout、
  `.gitignore`（见规划不做清单）

## 怎么跑

```sh
# 需要 Go 1.22+。工具链在 ~/go/bin 时先加 PATH：
export PATH="$HOME/go/bin:$PATH"

make check      # go vet + 全部测试
make examples   # 演示 init → add → commit → log → status 全流程，
                # 再演示 branch / checkout（M3），diff 与
                # git diff 对照、git status / git fsck 直接跑（M4），
                # 最后演示 merge：fast-forward、干净合并、冲突 +
                # 手动解决 + 提交，并与 git merge 对照 tree SHA（M5），
                # 并断言 git log 能读提交历史、git fsck 通过
```

## 怎么测

```sh
go test ./... -count=1
```

测试分五层：对象编解码 round-trip（blob / tree / commit，含随机内容与
1MB 大对象）；`hash-object` 与真 git 对照（100 个随机文件，含空文件、
二进制、1MB）；双向互读（minigit 写的对象用 `git cat-file` 读，
git 写的 blob / tree / commit 用 `minigit cat-file` 读）；
提交流程（add → commit → log 全流程、status 三种状态、
`git log` 读历史、`git fsck` 直接跑通过）；分支切换（建分支、两边提交、
切换还原、脏切换被挡、未追踪文件被挡/放行、
`git branch` / `git log --all` 读分支与历史）；diff 与索引
（Myers 与暴力 LCS 对照 10,000 组随机输入；`minigit diff` /
`diff --cached` 与 `git diff` 归一化后逐行一致，含随机文件树；
索引 v2 round-trip、JSON 旧索引迁移、0xFFF 超长名、扩展跳过、
校验和/截断损坏；`git status` 在 minigit 仓库里直接跑正常）；
合并（行级合并单元测试：干净合并、同内容、单边改、双方删、
同点插入、重叠冲突、删除 vs 修改、边界插入、空文件、多冲突区、
结尾换行符规则；merge-base / 祖先检测 / MERGE_HEAD 与 MERGE_MSG
读写；fast-forward、干净三路合并、冲突 + 手动解决 + 提交、
删除 vs 修改、双方新增、Already up to date、脏工作区拒绝、
无关历史拒绝；25 组随机场景与 `git merge` 差分对照：干净合并
的 tree SHA 相同，冲突文件内容归一化标记标签后相同）。

对象格式精确到字节的说明见 [docs/01-object-format.md](docs/01-object-format.md)，
索引 v2 格式见 [docs/02-index-format.md](docs/02-index-format.md)，
三路合并见 [docs/03-merge.md](docs/03-merge.md)，
完整规划见仓库外的 `tech-project-plans.md`（项目 2）。
