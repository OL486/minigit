# 对象格式（精确到字节）

本文档只写 M1 的对象层。索引格式见 `02-index-format.md`（M2），合并规则见 `03-merge.md`（M5）。

## 对象身份：SHA-1

每个对象由内容唯一决定。先拼出待哈希的字节串：

```
<类型> <内容长度的十进制>\x00<原始内容>
```

例如内容为 `hello world\n`（12 字节）的 blob，待哈希串是
`blob 12\x00hello world\n`，SHA-1 为
`3b18e512dba79e4c8300dd08aeb37f8e728b8dad`（与
`echo "hello world" | git hash-object --stdin` 一致）。

空 blob 的 SHA 是 `e69de29bb2d1d6434b8b29ae775ad8c2e48c5391`。

## 松散对象存储

对象以 zlib 压缩后存为单个文件，路径由 SHA 决定：

```
.git/objects/<SHA 前 2 位>/<SHA 剩余 38 位>
```

例如 SHA `3b18e5…` 存为 `.git/objects/3b/18e512dba79e4c8300dd08aeb37f8e728b8dad`。
文件内容是 zlib 流，解压后就是上面那段 `<类型> <长度>\x00<内容>`。

写入时先写临时文件再改名，避免半截对象留在库里；已存在的对象不重复写。
文件权限 0444（只读），与 git 一致。

## blob

原始内容就是文件内容本身，不做任何加工（不转行尾、不 strip）。

## tree

原始内容是一串条目，每项：

```
<mode 的 ASCII 八进制> <名字>\x00<20 字节二进制 SHA>
```

- mode：文件 `100644`，目录 `40000`（不带前导零；可执行文件 `100755`，M2 再支持）。
- 名字里不含 `/`；子目录以一个 tree 对象表示。
- 条目按名字排序，目录名参与比较时按“名字 + `/`”算。
  例如 `a.txt`（目录）排在 `a.txt2`（文件）前面，因为 `a.txt/` < `a.txt2`。
- SHA 在 tree 里存二进制（20 字节），不在文本里。

`cat-file -p <tree>` 的显示格式（与 git 一致）：

```
100644 blob <40 位 SHA>\t<名字>
040000 tree <40 位 SHA>\t<名字>
```

注意 mode 显示时补齐到 6 位（`40000` 显示为 `040000`）。

## commit

原始内容是文本：

```
tree <sha>
parent <sha>          # 0 个（根提交）或多个（合并提交）
author <名字> <邮箱> <时间戳> <时区>
committer <名字> <邮箱> <时间戳> <时区>

<提交信息>
```

头部与正文之间恰好一个空行。`cat-file -p <commit>` 打印原始内容。
本包的 `Parse` 只解析 tree / parent / author / committer，
不认识的头部（如 gpgsig）忽略——cat-file 不走 Parse，不会丢信息。

## 短 SHA

`cat-file` 接受完整 40 位 SHA，也接受唯一短前缀（至少 4 位）。
前缀对应多个对象时报错“不唯一”；对应 0 个时报错“对象不存在”。
查找时只扫 `.git/objects/<前两位>/` 目录。

## 与 git 的差异（M1）

- 只做松散对象，不做 packfile（大仓库会慢，见规划不做清单）。
- 不做 tag 对象；`cat-file` 遇到未知类型直接报错。
- 不做 `.gitignore`、不做索引、不做引用（分支）——分别是 M4、M2、M3 的事。
