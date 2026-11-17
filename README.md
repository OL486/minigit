# minigit

从零实现 git 的子集（Go，零外部依赖）。和 [minilang](https://github.com/OL486/minilang)、
[minidb](https://github.com/OL486/minidb) 同一系列的 from-scratch 作品：
目标是自己每天能用，并且写出来的仓库**真 git 能读**
（对象、提交、索引格式与 git 字节兼容）。

## 当前状态：M2（提交流程）

M2 跑通 add / commit / log / status：索引先用简化 JSON 格式
（`.git/index` 存 JSON，只求流程通；M4 会换成与 git 字节兼容的
index v2，到时真 git 才能读索引——M2 不要求 `git status` 能读
我们的仓库），引用（分支）是文件，commit 对象与 git 字节兼容，
`git log` 能读 minigit 的提交历史。

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
```

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
  注意：M2 的索引是临时 JSON 格式，`git status` / `git fsck` 直接跑
  会报 "index file corrupt"——这是已知限制，M4 换成 v2 格式后解决；
  对象本身是合法的（`git log` / `git cat-file` / `git ls-tree` 都能读）。

## 不能跑什么（后面里程碑的事）

- `branch` / `checkout`（M3，分支切换）
- `diff`（M4，Myers diff + 索引对齐）
- `merge`（M5，三路合并）
- 网络传输（clone / fetch / push / pull 一律不做）、packfile、
  rebase、stash、tag 对象、`.gitignore`（见规划不做清单）

## 怎么跑

```sh
# 需要 Go 1.22+。工具链在 ~/go/bin 时先加 PATH：
export PATH="$HOME/go/bin:$PATH"

make check      # go vet + 全部测试
make examples   # 演示 init → add → commit → log → status 全流程，
                # 并断言 git log 能读提交历史、git fsck 通过
```

## 怎么测

```sh
go test ./... -count=1
```

测试分四层：对象编解码 round-trip（blob / tree / commit，含随机内容与
1MB 大对象）；`hash-object` 与真 git 对照（100 个随机文件，含空文件、
二进制、1MB）；双向互读（minigit 写的对象用 `git cat-file` 读，
git 写的 blob / tree / commit 用 `minigit cat-file` 读）；
提交流程（add → commit → log 全流程、status 三种状态、
`git log` 读历史、`git fsck` 通过——fsck 跑之前会临时挪走
JSON 索引，见测试里的注释）。

对象格式精确到字节的说明见 [docs/01-object-format.md](docs/01-object-format.md)，
完整规划见仓库外的 `tech-project-plans.md`（项目 2）。
