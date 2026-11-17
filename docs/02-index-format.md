# 索引格式（git index v2）

本文精确记录 minigit 索引文件的字节格式（M4 起），以及 M2/M3 的旧
JSON 索引如何迁移。目标是与真 git 的 index v2 字节兼容：minigit
写的索引，`git status` / `git fsck` / `git ls-files` 能直接读；
真 git 写的索引，minigit 也能读。

## 文件结构

文件由四部分顺序组成，全部多字节整数都是**大端**：

```text
头部（12 字节）
条目 × N（每条长度是 8 的倍数）
扩展（0 个或多个，可选）
校验和（20 字节）
```

## 头部（12 字节）

| 偏移 | 长度 | 内容 |
| --- | --- | --- |
| 0 | 4 | 魔数 `DIRC`（ASCII `D I R C`） |
| 4 | 4 | 版本号 uint32：`2` |
| 8 | 4 | 条目数 uint32 |

## 条目

每条由 62 字节固定字段 + 文件名 + NUL 填充组成，总长度是 8 的
倍数，且至少补 1 个 NUL：

```text
总长度 = (62 + len(name) + 8) & ~7
填充数 = 总长度 - 62 - len(name)        // 1..8 个 NUL
```

固定字段（62 字节，全部大端）：

| 偏移 | 长度 | 内容 |
| --- | --- | --- |
| 0 | 4 | ctime 秒 uint32 |
| 4 | 4 | ctime 纳秒 uint32 |
| 8 | 4 | mtime 秒 uint32 |
| 12 | 4 | mtime 纳秒 uint32 |
| 16 | 4 | dev uint32 |
| 20 | 4 | ino uint32 |
| 24 | 4 | mode uint32（`0100644` = 33188，`0100755` = 33261） |
| 28 | 4 | uid uint32 |
| 32 | 4 | gid uint32 |
| 36 | 4 | size uint32（文件字节数） |
| 40 | 20 | blob SHA-1（二进制，不是十六进制文本） |
| 60 | 2 | flags uint16 |

flags 的位分配（高位在前）：

```text
bit 15    assume-valid（我们总写 0）
bit 14    extended（v2 里必须是 0；v3 才用）
bit 13-12 stage（0 = 正常；1/2/3 是合并冲突阶段，M4 不写）
bit 11-0  文件名长度；文件名 ≥ 0xFFF（4095）时填 0xFFF
```

文件名长度恰好是 0xFFF 或更长时，flags 里填 0xFFF，文件名在条目
里以 NUL 结尾（读的时候扫到第一个 NUL 为止，再按 8 字节对齐；
写的时候 minigit 暂不支持这种超长路径名，见“不做的事”）。

条目按路径（字节序）排序后写盘。

stat 字段全部取自文件真实的 `lstat`：ctime/mtime（秒 + 纳秒）、
dev、ino、uid、gid、size。不要填零——status 的快速判断和 git
的 racy-clean 检测都依赖它们。

## 扩展

条目之后、校验和之前可以有 0 个或多个扩展，每个扩展是：

```text
4 字节签名（如 "TREE"、"REUC"、"link"、"UNTR"、"FSMN"、"EOIE"、"IEOT"）
4 字节大端长度 N
N 字节数据
```

读的时候：签名不认识就按长度跳过；长度越界则报错。minigit 写盘
时**不写任何扩展**（真 git 下次写索引时会自己再生需要的扩展，
比如 `TREE`）。

## 校验和（20 字节）

文件最后 20 字节是前面**全部字节**的 SHA-1。读的时候必须校验：
不符就报错“索引校验和不符”，不要静默接受。

空索引（0 个条目）的文件就是 12 字节头部 + 20 字节校验和，
共 32 字节。

## 旧 JSON 索引的迁移（M2/M3 → M4）

M2/M3 的索引是 JSON 文件（`{"version":1,"entries":[...]}`），
内容以 `{` 开头。`index.Read` 的识别规则：

- 文件不存在 → 空索引，不报错。
- 前 4 字节是 `DIRC` → 按 v2 解析。
- 第 1 个字节是 `{` → 按旧 JSON 解析成条目；JSON 里没有的
  stat 字段（ctime、dev、ino、uid、gid）补零。
- 其他 → 报错“索引损坏”。

JSON 读出来的索引，下次 `Write` 时自动存成 v2 格式——迁移是
读时识别、写时转换，不需要单独的迁移命令。写 v2 时 stat 字段
如果全是零（刚从 JSON 迁过来、还没重新 `add` 过），就原样写零；
status 的快速判断会因此失效（size/mtime 也不全——JSON 里有
size 和 mtime，所以快速判断仍可用；ctime/dev/ino 为零时
git 会重新算内容哈希，结果正确、只是慢一点）。

## 与真 git 的互读要点

- minigit 写出的索引，`git status` / `git ls-files --stage` /
  `git fsck` 必须直接通过（M2 的“index file corrupt”问题
  本轮消除）。
- 真 git 写出的索引（含 `TREE` 等扩展）minigit 必须能读：
  扩展跳过、条目照常解析。
- `git add` 会重写索引（可能带上扩展）；minigit 下次读到时
  照样跳过扩展，写回时扩展会丢——这是有意的简化，扩展内容
  本来就是缓存性质的，丢了不影响正确性。

## 不做的事

- 扩展字段的解析（只跳过，不解释内容）。
- stage 非 0 的条目（合并冲突阶段，M5 的 merge 才用）。
- 文件名长度 ≥ 0xFFF 的超长路径（写盘时直接报错）。
- assume-valid 位（总写 0）。
- index v3/v4（不读不写；读到版本号不是 2 就报错）。
