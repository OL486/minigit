# 三路合并（M5）

`minigit merge <branch>`：fast-forward 或三路合并。
合并提交的两个 parent 是 HEAD 与目标分支的 tip，
对象格式与 git 字节兼容，`git log` / `git fsck` 能直接读。

## 流程

```
merge <branch>
  │
  ├─ 目标不存在 → 报错
  ├─ 目标 == HEAD，或目标是 HEAD 的祖先 → "Already up to date."，exit 0
  ├─ 工作区有已暂存 / 未暂存的修改 → 拒绝合并（和 checkout 同样的保护）
  ├─ HEAD 是目标的祖先 → fast-forward（见下）
  └─ 否则 → 三路合并（见下）
```

### fast-forward

直接把当前分支指针移到目标 commit，并按目标 tree 更新工作区与
索引（复用 checkout 的更新逻辑：先算决策、再动文件，写前预检
未追踪文件覆盖与目录挡路）。打印：

```text
Updating aaa1111..bbb2222
Fast-forward
```

不产生合并提交。

### 三路合并

base 是 HEAD 与目标的 merge-base（见下），ours 是 HEAD 的 tree，
theirs 是目标的 tree。三个 tree 各拍平成“路径 → blob”后逐文件
决策，再统一应用（先算决策、再动文件；写前预检未追踪文件覆盖
与目录挡路）。

#### 文件级规则表

| base | ours | theirs | 结果 |
| --- | --- | --- | --- |
| 有 | 有（同 base） | 有（同 base） | 不动 |
| 有 | 有（同 base） | 有（改了） | 取 theirs |
| 有 | 有（改了） | 有（同 base） | 取 ours（不动） |
| 有 | 有（改了） | 有（改了，内容相同） | 内容取一份；mode 只有 theirs 改了时取 theirs 的（见下） |
| 有 | 有（改了） | 有（改了，内容不同） | 行级合并（见下） |
| 有 | 有（改了） | 删了 | 冲突（删除 vs 修改） |
| 有 | 删了 | 有（改了） | 冲突（删除 vs 修改） |
| 有 | 删了 | 删了 | 删掉 |
| 无 | 有 | 无 | 不动（ours 新增的） |
| 无 | 无 | 有 | 取 theirs（新增文件写进来） |
| 无 | 有 | 有（内容相同） | 写一份进来 |
| 无 | 有 | 有（内容不同） | 冲突（双方新增） |

mode（100644 / 100755）的规则：只有一边改了 mode 时取改了的那边；
两边改成同一个时取那个；两边改成不同时保留 ours 的（简化，
git 会把这种情况也当作冲突的一部分处理——见“与 git 的差异”）。

#### 行级合并

对 base→ours 和 base→theirs 各跑一遍 Myers diff（复用
`internal/diff`），得到两组 change（base 区间 → 新内容）；
再沿 base 位置走：

- 只有一边改了 → 直接取改了的那边；
- 两边改了同一处、结果相同 → 取一份，不算冲突；
- 两边改了同一处、结果不同 → 冲突；
- 两边改的区域不重叠 → 各自应用。

“同一处”的判定：两个 change 的 base 范围有交集算重叠；两个纯插入
只有位置相同时算重叠；插入点严格落在对方改动区间内部算重叠，
落在边界上不算（插入内容出现在区间替换内容之前/之后，能干净合并）。
重叠的改动会连锁收进同一个冲突区（一个改动扩展了区域边界后，
又碰上了另一边的下一个改动，就继续收）。

冲突区是两边改动范围的**并集**：区域内每边的完整内容都放进标记，
包括只有一边改了的行（例如 ours 改了 b、c，theirs 改了 c、d，
冲突里 ours 一侧是 `B C d`，theirs 一侧是 `b C D`）——与 git 的
结果一致，不收窄到最小差异行。

冲突标记：

```text
<<<<<<< ours
<ours 在该区域的内容>
=======
<theirs 在该区域的内容>
>>>>>>> theirs
```

标签用 `ours` / `theirs`（git 用的是 `HEAD` / `<分支名>`；
对照测试时归一化标签再比对）。内容没有结尾换行符时补一个，
否则标记行会粘在内容后面（git 补的是 `\ No newline at end of file`，
本轮简化成补换行符）。

结尾换行符：结果的最后一行来自哪一边（或 base），就按那一边的
文件有没有结尾换行符来定；最后一行是冲突标记时一定有换行符；
结果为空时返回空。

二进制文件（前 8000 字节里有 NUL 字节）不做行级合并：双方都改了
就冲突，工作区留 ours 的版本（没有标记可放）。

#### 无冲突：创建合并提交

合并结果写工作区与索引后，创建合并提交：

- parent 有两个：HEAD 与目标 tip；
- message 默认 `Merge branch '<name>'`，也支持 `-m <message>`；
- 打印 `Merge made by minigit.` 与 `[<分支> <短 SHA>] <信息首行>`。

#### 有冲突：停下来等用户解决

- 冲突文件写带标记的内容到工作区；已干净合并的文件正常写工作区
  与索引；冲突文件在索引里保持 HEAD 的条目（新增文件本来就没有
  条目），等用户解决后 `add`；
- 不创建提交，写 `.git/MERGE_HEAD`（目标 commit SHA）与
  `.git/MERGE_MSG`（首行是合并提交的默认信息，后面是 `Conflicts:`
  节，列出冲突文件）；
- 打印冲突文件列表，exit 1。

用户手动解决冲突后 `add` + `commit`：

```sh
# 编辑 a.txt，去掉标记，写好想要的内容
minigit add a.txt
minigit commit          # 不带 -m 时，信息取 MERGE_MSG 的首行
```

`commit` 在 MERGE_HEAD 存在时：

- 把 MERGE_HEAD 作为第二 parent，提交成功后删除 MERGE_HEAD 与
  MERGE_MSG；
- 冲突文件里还有 `<<<<<<< ` 开头的行时拒绝提交；
- 接受删除那一边时：删掉工作区文件再 `add <文件>`（`add` 已删除的
  被追踪文件就是暂存删除；冲突文件本来就不在索引里时 `add` 是
  空操作、成功）；
- 二进制冲突没有标记可查：`add` 选中的版本后直接提交即可，
  commit 不会拦（见“与 git 的差异”）。

`status` 在 MERGE_HEAD 存在时，顶部多打印一行：

```text
On branch main
正在合并 feature（有冲突未解决）
```

## merge-base

`FindMergeBase(head, target)`：先收集 head 的全部祖先（沿所有
parent 走，BFS），再从 target 出发 BFS，第一个命中的就是 merge-base。
没有共同祖先（无关历史）时报错，本轮拒绝合并。

注意：交叉合并（criss-cross，两个分支互相合并过对方）时共同祖先
可能有多个，这里只取离 target 最近的一个——git 的递归策略会先
把共同祖先本身合并出一个虚拟 base，结果可能不同。日常分叉历史
（一个共同祖先）不受影响。

## MERGE_HEAD / MERGE_MSG

| 文件 | 内容 | 写在何时 | 删在何时 |
| --- | --- | --- | --- |
| `.git/MERGE_HEAD` | 目标 commit SHA（一行） | 合并出冲突时 | 合并提交成功后 |
| `.git/MERGE_MSG` | 首行是合并提交的默认信息；后面是 `Conflicts:` 节（`\t<路径>` 每行一个） | 合并出冲突时 | 合并提交成功后 |

`git status` 在 minigit 的冲突仓库里能直接跑（它认 MERGE_HEAD），
但“未合并路径”的判定依赖索引的 stage 信息——minigit 的索引没有
stage，所以 git 会误报 `All conflicts fixed but you are still merging.`
（实际上冲突文件还在，标记也没去掉）；minigit 自己的 `status`
也只提示正在合并，不列冲突文件（冲突文件列表在 `merge` 的输出
与 MERGE_MSG 里）。

## 与 git 的差异

- 冲突标记标签：`ours` / `theirs`，git 是 `HEAD` / `<分支名>`。
- 没有 `merge --abort`：想放弃合并时，手动删 `.git/MERGE_HEAD`
  与 `.git/MERGE_MSG`（工作区里已写入的合并结果与冲突标记
  不会自动还原——需要的话 `checkout` 切到别的分支再切回来，
  或手动改回文件）。
- `checkout` 在合并进行中不拦截：脏检查会挡住大多数情况（冲突
  标记文件算未暂存修改），但冲突全部解决并 `add` 之后切分支
  是放行的，MERGE_HEAD 会留在原地——请先完成合并再切分支。
- mode 冲突不单独处理：两边把 mode 改成不同值时保留 ours 的，
  git 会把它当作冲突的一部分（需要用户确认）。
- criss-cross 历史的 merge-base 选择与 git 的递归策略不同（见上）。
- 二进制冲突：commit 不检查是否解决（没有标记可查），靠用户
  `add` 选中的版本；git 会拦住未解决的二进制冲突。
- 内容没有结尾换行符时，冲突标记前补的是换行符，git 补的是
  `\ No newline at end of file` 行。
- 没有重命名检测：一方重命名、一方修改同一文件时，会按“删除 +
  新增”处理（删除 vs 修改冲突，或干净地删旧加新），git 能认出
  重命名并合并。
- `log` 只跟第一 parent：合并提交的第二 parent 不跟（和 git
  默认的 `git log` 不一样——git 默认会走拓扑序展示两边历史）。
