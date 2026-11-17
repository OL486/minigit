# minigit

从零实现 git 的子集（Go，零外部依赖）。和 [minilang](https://github.com/OL486/minilang)、
[minidb](https://github.com/OL486/minidb) 同一系列的 from-scratch 作品：
目标是自己每天能用，并且写出来的仓库**真 git 能读**
（对象、提交、索引格式与 git 字节兼容）。

## 当前状态：M1（对象层）

M1 只做对象层：`init` / `hash-object` / `cat-file` 跑通，对象格式与 git
字节兼容，双向互读已验证。

## 能跑什么

```sh
minigit init [目录]              # 创建仓库（默认当前目录）
minigit hash-object [-w] <文件>   # 算 blob SHA-1；-w 时写入对象库
minigit cat-file -p <sha>        # 按类型打印对象内容
minigit cat-file -t <sha>        # 只打印对象类型
```

- `hash-object` 不加 `-w` 时只算不写（和 git 一致）；输出的 SHA-1 与
  `git hash-object` 完全一致。
- `cat-file` 接受完整 40 位 SHA 或唯一短前缀（至少 4 位）；前缀不唯一或
  不存在时报错。
- `init` 的 HEAD 指向 `refs/heads/main`；`.git/config` 里会写
  user.name / user.email——没有 `GIT_AUTHOR_NAME` /
  `GIT_AUTHOR_EMAIL` 环境变量时先写占位值（Your Name / you@example.com）
  并提示你改 `.git/config`，不报错退出。

## 不能跑什么（后面里程碑的事）

- `add` / `commit` / `status` / `log`（M2，提交流程）
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
make examples   # 演示 init → hash-object -w → cat-file 全流程，并断言与真 git 互读
```

## 怎么测

```sh
go test ./... -count=1
```

测试分三层：对象编解码 round-trip（blob / tree / commit，含随机内容与
1MB 大对象）；`hash-object` 与真 git 对照（100 个随机文件，含空文件、
二进制、1MB）；双向互读（minigit 写的对象用 `git cat-file` 读，
git 写的 blob / tree / commit 用 `minigit cat-file` 读）。

对象格式精确到字节的说明见 [docs/01-object-format.md](docs/01-object-format.md)，
完整规划见仓库外的 `tech-project-plans.md`（项目 2）。
