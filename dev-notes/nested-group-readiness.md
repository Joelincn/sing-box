# 嵌套出站组拨测就绪与复用改造（urltest / loadbalance 父组）

- 特性分支：`feat/urltest-nested-readiness`（基线 `mustang@ada6ded85`）
- 合入：`mustang`、`mustang-1.14.0`（`--no-ff` 合并，见文末跟踪信息）
- 日期：2026-09-06 / 2026-09-07
- 测试版本串：`1.14.0-Mustang.ready-test` → `ready-test6`（`96b2186dc`）

## 一、原因：为什么要改

### 1.1 现象

urltest 父组（如 `📽️ TikTok`、`🚀 节点选择`、`🌏 !cn`、`🌌 Google`）成员含 LB
子组（如 `🇸🇬 新加坡节点`）+ selector（如 `家庭宽带`、`其它地区`、`手动选择`）
时，启动/运行中出现：

- 父组把子组当时选中的**单片叶子**当成子组整体判生死，选中坏叶子即
  `TikTok ... 🇸🇬 unavailable`，而子组本体正常（误判连带）；
- 父组拨失败会 `DeleteURLTestHistory` 删掉**全局共享**的叶子 key，把子组
  自己的真数据一并销毁（互删）；
- urltest 父组 `performUpdateCheck` 存盲选首成员，导致"需要重试"条件永假，
  混合成员（组+selector）下部分有数即停，LB 子组躺等整个 interval。

### 1.2 根因链（代码事实）

1. 父组拨测把子组解析成 `Now()` 叶子直拨（`RealTag`，
   `protocol/group/selector.go`），只采样当前选中叶，不经过子组；
   LB 父组同样（`protocol/group/loadbalance.go urlTestLocked`）。
2. history 是全局共享单例（三处 `service.PtrFromContext[urltest.HistoryStorage]`
   同源，key = 叶子 tag），父子读写同一批 key，失败即互删。
3. urltest 父组有提交点（`performUpdateCheck` + `tolerance`），盲选 + 粘滞
   放大单样本误差；LB 父组无提交点（策略只看 `isAvailable` 布尔，不看延迟），
   父组测出的毫秒数对选型毫无意义。
4. 流量错误双记账：子组 `DialContext` 内部已对真叶子 `recordFailure`，
   LB 父组回来又对 `RealTag(子组)` 记一笔，同一次失败两边各 +1
  （`exclude_threshold=2` 时约等于阈值减半）。

### 1.3 为什么 LB 父组也需要改

`🐬 OneDrive` 由 urltest 改为 loadbalance，成员为
`🇭🇰/🇯🇵/🇺🇸（LB 子组）+ 🎈手动选择（selector）` 后，上述 1/2/4 在 LB
父组侧原样存在。LB 选型只关心"有数"（布尔可用），而可用性信息子组用自己的
interval 维护得更好，父组插手只带来破坏、不带来信息。

## 二、改进方向与方案

总原则：**父组对自测型子组（urltest/LB）只读状态，不测、不写、不记账**；
selector 成员保持现状（selector 自身不拨测，key 全靠别人补）。

### 2.1 urltest 父组（`protocol/group/urltest.go`）

| 提交 | 内容 |
|---|---|
| `3cfdae9` | 未就绪子组跳过：`groupMemberReady` 只看子组自身状态（urltest 子组有基于真 history 的 selection；LB 子组有叶子 history），不看父组有无其条目 |
| `720f1a0` | 被跳过时 10 秒后补测（`urlTestUnreadyRetryDelay = 10s`） |
| `85f5e3c` | 空轮重试条件修正 |
| `a461986` | 复用子组延迟：`reuseGroupDelay`（urltest 取 selection 延迟，LB 取可用叶子最小延迟，剔除熔断中的叶子），新鲜度门为父 interval |
| `e046a34` | 仅"被跳成员一次都没量到过"才重试，上限 `maxUnreadyRetries = 30`（约 5 分钟），防死子组空转；全量到过恢复正常 interval |

### 2.2 LB 父组（`protocol/group/loadbalance.go`，`96b2186d`，两刀）

1. **拨测跳过子组**（`urlTestLocked`）：成员是 urltest/LB 子组一律跳过拨测，
   有数则 `reuse` 进结果（只为面板/API 显示，**不写 history**），无数据则
   `skip untested group member`。判定复用 `protocol/group/urltest.go` 的
   `isSelfMeasuringGroup`（两边同包共用）。副作用：手动刷新也不再惊扰子组。
2. **记账跳过子组**（`DialContext`/`ListenPacket` 错误路径）：子组成员报错
   父组不再 `recordFailure`。父组整组拉黑是伪需求——子组全死时叶子 key
   自然变空，策略自动绕行 + `nextFallback` 兜底；掐长连接的权力交还子组
   自己的阈值判断（子组看到的是真叶子失败，比父组的 `Now()` 快照更准）。
3. 明确不做的：`isAvailable` 保持原样（round-robin 下抖动无感）；
   不给 LB 加提交点/快照（违背 LB 实时语义，等于重写策略）。

### 2.3 配置建议（实测结论）

- 父子组 interval 统一用默认 `3m` 即可，分开调优已无必要。
- 统一 3m 后互删只剩"启动并发几秒 + 3 分钟自愈"窗口，本改造将其连根拔掉；
  双记账走流量路径、与 interval 零相关，必须靠代码修。

## 三、效果

### 3.1 单测（`protocol/group/*_nested_test.go`，11 个）

- 复用/陈旧/未就绪/LB 最小值/剔除跳过/叶子直通/重试判定；
- 新增 LB 父组 4 个：复用 urltest 子组、跳过未就绪子组、复用 LB 子组、
  全程不写/不删共享 history（指针级断言）；
- `protocol/group` + `dns/...` 全绿，`go vet` 干净。

### 3.2 线上日志证据（ready-test5/test6，Windows）

- urltest 父组 0011–0018 清一色 `reuse`（TikTok/节点选择/!cn/Twitter/
  Facebook/Telegram/Amazon/Google），无盲测、无 `unavailable` 连带。
- `🐬 OneDrive` 启动后对三个 LB 子组**零拨测行**（旧版必有
  `outbound 🇺🇸/🇯🇵 available` 三行），仅保留 selector 成员
  `🎈 手动选择 available`（设计保留）。
- OneDrive 流量正常摊开（round-robin 到各子组叶子），`protected` 豁免标记正常。
- 子组自测失败（`pro-美国03 EOF`、`lite-香港-03 EOF`、`Biglobe x509` 等）
  本地消化，全程零 `load-balance exclude`，阈值无误触发。
- 手动刷新父组不再扰动子组数字（force 也跳子组，符合设计）。

## 四、跟踪与回退

- 特性分支：`feat/urltest-nested-readiness`（6 笔提交，`3cfdae9`…`96b2186d`）
- 合并方式：`git merge --no-ff`，分别合入 `mustang` 与 `mustang-1.14.0`，
  合并提交信息含本文件名，`git log --merges` 可查。
- 合前 tag：`mustang-pre-nested-readiness`（轻量，定在 `mustang@ada6ded85`，
  与 `mustang-pre-interrupt` 同风格）。
- 回退（三选一）：
  - `git revert -m 1 <merge-commit>`（推荐，历史可读）；
  - `git reset --hard mustang-pre-nested-readiness`（未推送下游时）；
  - 特性分支本身保留，可随时重合。
- 已知局限（有意不修）：LB 当父组时 `isAvailable` 仍看 `Now()` 叶子 key，
  子组频繁切换选择时可用性显示会抖；round-robin 语义下无实质影响，
  若未来出现 consistent-hashing/sticky 重分布异常再评估。
