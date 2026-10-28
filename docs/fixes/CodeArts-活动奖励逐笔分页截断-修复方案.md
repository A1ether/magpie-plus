# CodeArts 活动奖励逐笔被截断导致「未明细部分」吞并真实批次 —— 修复方案

> 文档类型：修复方案
> 关联模块：平台适配器 / CodeArts（华为云）
> 创建时间：2026-10-25
> 最后更新：2026-10-25
> 当前状态：已完成
> 关联改动文件：`src/lib/pagination.ts`（新增）、`src/cli/test-pagination.ts`（新增）、`src/providers/codearts.ts`、`src/providers/codearts-aksk.ts`、`src/providers/codebuddy.ts`、`src/providers/minimax.ts`、`src/cli/codearts-console-probe.ts`、`AGENTS.md`

---

## 1. 问题现象

**结论：卡片上那条「活动奖励积分（未明细部分）」不是虚增额度，而是适配器**枚举手数不够**造成的真实批次被吞并——官方「获取明细」里明明按笔列着 29 笔「每日签到领积分」，看板只认到 20 笔，剩下的 9 笔被压成一条聚合调节项。**

| 项 | 内容 |
| --- | --- |
| 用户观察 | 卡片出现「活动奖励积分（未明细部分）4,339.62 CREDITS，过期 2026/10/26 08:00，11 小时后到期」 |
| 用户对照 | 官方用量页「获取明细」中该值是**多笔「每日签到领积分」合并的结果**；用户通过「消费后扣款变化出现在这些批次上」确认关联关系 |
| 观察到的变化 | 该条目初始为 9000，随消费降到 4339.62（说明扣款确实发生在这些批次上） |
| 影响范围 | ① 逐笔批次缺失（看不到真实的每一笔与各自到期日）；② 「最快到期」判断用到的是订阅到期兜底值而非批次真实 `expireAt`；③ 用量明细「获取」维度（`fetchUsageLedger` 的 grant 记录）同样少了一半批次 |

复现步骤：CodeArts 账号的某个活动领取笔数 **> 20** 时，看板必然出现该调节项；笔数 ≤ 20 时不出现。

---

## 2. 根因分析

**结论：`/portal/promptcenter/v1/ops/credit/campaign/{id}` 是分页接口，适配器既没带分页参数、也没读响应里的 `total`，只吃到第一页（20 条）。**

### 2.1 关键证据（本机 `.run/codearts-probe.log`，只读诊断日志，未新增上游请求）

| 位置 | 内容 | 说明 |
| --- | --- | --- |
| `.run/codearts-probe.log:9-11` | `data.items 长度 = 2`；`total: {claimCount:30, totalClaimed:33000.00, totalConsumed:2474.47}`；`campaignId:1 每日签到领积分 claimCount:29, totalClaimed:29000.00, totalConsumed:1000.00` | 活动列表口径：campaign 1 **共 29 笔**，声称领取 29000 |
| `.run/codearts-probe.log:19` | `data.benefits 长度 = 20`（响应长度 4814） | **明细接口只返回 20 笔** —— 与 29 笔的差值就是凭证 |
| `.run/codearts-probe.log:14-16` | `campaign/4 每日签到.. 长度 = 1`，响应体尾部 `"total":1` | 同一接口带 `total` 字段（该活动恰好 1 笔，看不出截断） |
| `.run/codearts-probe.log:6` | `all_credit_package: amount 33500 / used 2974.47 / remain 30525.53` | 账户级剩余 30525.53 |

对账（决定性）：逐笔口径合计 `33000 − 2474.47 = 30525.53`，与账户级 `remain = 30525.53` **完全相等**——
说明**只要把 29 笔取全，缺口就应当是 0**；当前缺口完全来自「只取到 20 笔」这个枚举缺口。

### 2.2 代码证据

| 位置 | 现状 | 问题 |
| --- | --- | --- |
| `src/providers/codearts.ts:117-143` | `fetchCampaignBenefits()` 循环里对每个 campaign 只发一次 `campaign/{id}`，不传分页参数、不读 `total` | 一次请求 = 20 笔，超出部分直接丢失 |
| `src/providers/codearts.ts:636-653` | 用量明细的 grant 记录走同一接口 + `parseCampaignClaims()`，同样只吃第一页 | 日级「获取」增量少算 |
| `src/providers/codearts.ts:822-835` | 缺口调节项 `gap = bonusRemain − benefitTotal` | **本身是对账兜底，设计正确**；问题在于 `benefitTotal` 因枚举不全而偏小，于是真实批次被并进这条调节项 |
| `src/providers/codearts-aksk.ts:217-231` | 混合模式同款调节项 | 同上 |
| `src/providers/codearts.ts:117` + `:153` | `MAX_CAMPAIGNS = 20`，`.slice(0, MAX_CAMPAIGNS)` | **次级风险（待确认）**：活动**列表**若也被分页，超过 20 个活动同样会丢；当前账号只有 2 个活动，暂不触发 |

### 2.3 为什么「官方页看得到、看板看不到」

官方「获取明细」前端自己会翻页/带窗口参数，所以能列出全部 29 笔；适配器固定单次请求且无参数，拿到的就是服务端默认页。二次确认口径：活动列表接口自报 `claimCount: 29`，与服务端自己返回的 20 笔**自相矛盾**，可判定为「分页未取全」而非「本来只有 20 笔」。

### 2.4 参数命名依据

同属 promptcenter 服务的另一个接口在仓库内已有抓包结论使用 `page` / `size`：
`docs/plans/供应商卡片-用量与增量字段-开发设计方案.md:44` 记录
`GET /portal/promptcenter/v1/ops/credit/admin/usage?startDate&endDate&page&size`。
据此把 `page`/`size` 作为首选命名，并保留 `pageNum`/`pageSize` 作为兜底候选（见 3.2）。

> **待确认**：`campaign/{id}` 是否接受 `page`/`size`。本方案不改配置、不代发上游请求，
> 通过「运行时日志 + 诊断脚本打印 `total`」由人工一次跑命令确认（见 4.2）；
> 若首选命名不被接受，代码会自动尝试兜底命名，并在仍未取全时**显式声明未覆盖**（绝不静默并成缺口）。

---

## 3. 修复方案

**结论：给 `campaign/{id}` 加「读 `total` + 按页取全 + 命名兜底 + 未取全显式声明」四件事，逐笔批次与用量「获取」维度共用这一条 helper；对账调节项保留为兜底，但只在真的对不上时出现。**

### 3.1 改动点清单

| 文件 | 改动 |
| --- | --- |
| `src/providers/codearts.ts` | 新增 `parseCampaignBenefitEntries()`（一次响应 → `{entries, total}`，不过滤 `amount ≤ 0`）与 `fetchCampaignBenefitEntries()`（按页取全：`page/size` → `pageNum/pageSize` → 默认页兜底，最多 `MAX_CAMPAIGN_PAGES=10` 页、每页 `CAMPAIGN_PAGE_SIZE=100`）；`fetchCampaignBenefits()` 改用它并回传 `coverageNote`；缺口调节项的 `note` 追加未取全说明；`fetchUsageLedger` 的 grant 记录改用同一 helper，未取全时不声明覆盖；被取代的 `parseCampaignBenefits()` / `parseCampaignClaims()` 删除 |
| `src/providers/codearts-aksk.ts` | `CodeArtsCampaignDetail` 增加可选 `coverageNote`；缺口调节项 `note` 同步追加未取全说明 |
| `src/cli/codearts-console-probe.ts` | `describe()` 增打 `data.total`；对首个活动额外探一次带 `page=1&size=100` / `pageNum=1&pageSize=100` 的请求，供人工一次确证分页命名 |

### 3.2 取数逻辑（按页取全）

```
对每个 campaign：
  known = 响应 total ?? 活动列表 claimCount ?? 未取到
  依次尝试三种「取数方式」（每种都在独立 seen 集合上重跑）：
    ① page/size  ② pageNum/pageSize  ③ 不带任何参数（默认页，历史行为兜底）
    每种方式内 page = 1,2,…,MAX_CAMPAIGN_PAGES：
      请求 campaign/{id}?<方式参数>&size=CAMPAIGN_PAGE_SIZE
      assertBusinessOk；收集本页全部 benefitId
      若 seen ≥ known → 取全，complete = true，立即返回
      若本页无新增 benefitId（参数未被识别 / 已到末页）→ 换下一种方式
    请求报错 → 记日志、跳过该方式（不把整块明细弄丢）
  三种方式都取不全 → 取「枚举最多」的那次；若连默认页都报错则抛出（上层照旧降级）
  complete = false 时：把「枚举 M 笔 / 上游声称 T 笔」写进日志与批次 note，
                  缺口调节项保留但注明「上游分页未取全」
```

要点说明：

- **进度用 `benefitId` 全集判定**，而不是过滤后的批次：解析层保留 `amount ≤ 0` 的已用尽批次（过滤发生在批次构建处 `src/providers/codearts.ts` 的 `if (benefit.amount <= 0) continue`），若用过滤后的条数判断会误判「已到末页」。
- **只在未取全时才做额外请求**：参数被接受时 page 1 即返回全部（`size=100`），逐笔路径的请求数与现状一致；未取全时每种方式最多 2 次请求（第 2 页无新增即停）。
- **默认页兜底**：即使分页参数名全猜错、或上游拒绝未知查询参数，也只退化到「和修复前一样」，不会因为「加了参数」而丢掉整块活动明细。
- **`known` 缺失时**（响应没有 `total` 且活动 `claimCount` 为 0）不声明已取全，日志与 note 里写明「上游未给 total」。

### 3.3 语义边界（不改的部分）

- 已用尽 / 已过期批次（剩余 ≤ 0）仍不进 `credits` 列表，只累加不影响 `benefitTotal`（口径与账户级桶一致，见 2.1 对账）；
- 缺口调节项**不删除**：它是「逐笔与账户级桶对不上」的最后一道兜底，取全后自然不再出现（`gap < 1`）；
- 用量明细「获取」维度（`fetchUsageLedger`）：未取全时**不再声明已覆盖**（`covers.grant = false`），改由本地采样兜底——宁可少算为「不确定」，也不把截断后的合适当成完整日增量；取全后覆盖判定与修复前一致；
- 超过 `MAX_CAMPAIGNS` 的活动列表分页问题本次**不处理**，仅在 2.2 记录为待确认风险。

### 3.4 防复发（2026-10-26）

**结论：把「列表必分页、取全必显式」从一次性修复升级为项目级不变式，并修掉审计发现的另外两处同源隐患。**

| 措施 | 落地 |
| --- | --- |
| 唯一翻页入口 | `src/lib/pagination.ts` 的 `fetchAllPages()`：`complete` 为必填字段（调用方无法「忘了看」）；首页失败抛出、次页失败保留已取数据并标未取全；`no-progress` 防参数未被识别的死循环 |
| 统一文案 | `incompleteNote()` 生成「已取 M / 上游声称 T；原因」，日志与 note 共用 |
| CodeArts 改造 | `fetchCampaignBenefitEntries()` 只保留「分页命名探测」（`page/size` → `pageNum/pageSize` → 无参数兜底），翻页交给公共 helper；并用活动列表 `claimCount` 做完整性交叉校验 |
| CodeBuddy 修复 | 用量明细（Cookie `codebuddy.ts` + 令牌 `cbTokenRequestUsage()`）改用 `fetchAllPages`；**翻到 10 页上限仍未取全时不再声明 `covers.use`**（此前会把没取到的用量压成 0） |
| MiniMax 修复 | `fetchMinimaxCreditDetails()` 改用 `fetchAllPages`；修掉「`totalCount` 缺省 0 → `items.length >= 0` 恒真 → `complete` 永远为 true」的缺陷 |
| 回归护栏 | `npm run test:pagination`（`src/cli/test-pagination.ts`）8 条离线断言，含「29 条 / 每页 20」与「少一条即 `complete=false`」 |
| 制度约束 | `AGENTS.md` 第 9 节「取数完整性（强制）」6 条硬约束 + 自查清单新增一项；规范全文见 `docs/modules/取数完整性-列表分页与取全判据-模块现状.md`（含其余适配器的审计现状与「待确认」项） |

---

## 4. 回归验证

### 4.1 静态检查（本次执行）

| 项 | 结论 |
| --- | --- |
| `npm run typecheck`（`tsc --noEmit`） | **通过**（无诊断输出） |
| `npm run test:pagination`（离线纯函数断言，不发上游请求） | **8/8 通过**（含「29 条 / 每页 20」「少一条即 complete=false」等回归护栏） |
| 只读检索比对 | `fetchCampaignBenefits` 的返回字段与两处调用方（`codearts.ts` 余额路径、混合路径）一致；`CodeArtsCampaignDetail.coverageNote` 为可选字段，历史构造点不受影响；被删除的 `parseCampaignBenefits` / `parseCampaignClaims` 在 `src/`、`tools/` 内均无引用 |
| 未执行 | `npm run probe`、`npm run capture`、`npm run codearts:console-probe`、启动服务（按 AGENTS.md 第 6 节仅做静态检查） |

### 4.2 人工验证（AI 不代跑，需用户执行）

| 步骤 | 命令 | 预期 |
| --- | --- | --- |
| 1 | `npm run codearts:console-probe` | `campaign/1` 行新增 `data.total = 29`（若如此，即确证分页假设）；并打印「默认 20 笔 / `page=1&size=100` → N 笔」两条长度 |
| 2 | `npm run probe -- codearts --json` | 日志出现 `活动逐笔：… 逐笔 M 笔（上游 total T / claimCount C）`，M 应等于 29（含已用尽时 ≥ 扣掉 amount≤0 的笔数） |
| 3 | 看板刷新 | 「活动奖励积分（未明细部分）」消失，改为逐笔「每日签到领积分 × N」，**各自带真实 `expireAt`**；余额与总额度不变 |
| 4 | 对账 | 逐笔剩余合计 + 套餐 + 按需 = 卡片余额；账户级 `bonus` 桶 = 逐笔合计 |

若步骤 1 打印的是 `data.total = 20`（而非 29），则「截断」不是分页而是**服务端按 `status` 过滤**，本方案不成立，
需另立方案（此时 9 笔在接口层就不可见，只能保留缺口条目并在 note 里说明）——该分支在本方案中已被显式声明覆盖，不会静默出错。

---

## 5. 风险与回滚

| 风险 | 说明 | 缓解 |
| --- | --- | --- |
| 分页参数命名不符 | 服务端忽略未知参数 → 仍返回 20 笔 | 双命名尝试 + 未取全显式声明（日志与 note 都写明），不会把「没取到」伪装成「没有」 |
| 额外请求放量 | 每个 campaign 多打若干 GET | 仅在「未取全」时继续翻页；`size=100` 让常见规模一次命中；上限 10 页 |
| 卡片批次变多 | 该账号会从 3 条变成 30+ 条（29 笔签到 + 套餐 + 注册赠送） | 属用户明确诉求（真实每一笔）；若后续觉得吵，可另立「同到期日合并展示」方案，不在本次范围 |
| 上游结构再变 | 字段名变化导致 `total` 读不到 | `total` 为可选，缺失时退化为短页判据；解析失败只降级不抛错 |

回滚方式：`git revert` 本次提交即可——改动集中在三处函数与两个可选字段，不影响余额/窗口口径（余额与额度窗口始终取 ② / AK/SK 聚合值，未改动）。

---

## 变更记录

| 时间 | 变更内容 | 触发原因 | 影响章节 |
| --- | --- | --- | --- |
| 2026-10-25 | 初版：定位 `campaign/{id}` 分页截断（20/29），给出按页取全 + 未取全显式声明的修复方案 | 用户反馈「未明细部分 4339.62」在官方页找不到，经日志核实为 29 笔签到被截断为 20 笔 | 全篇 |
| 2026-10-25 | 实施落地：`fetchCampaignBenefitEntries()`（三种取数方式 + 默认页兜底）、`parseCampaignBenefitEntries()`、缺口 note 追加未取全说明、grant 维度未取全不声明覆盖、诊断脚本增打 `data.total` 与分页对照；同步 3.1/3.2/3.3/4.1 | 代码实现与方案产生偏差（新增默认页兜底、取消「短页即末页」判据、删除两个被取代的解析函数） | 3.1、3.2、3.3、4.1 |
| 2026-10-26 | **收口 + 泛化防复发**：用户确认逐笔已正常取到。抽出公共不变式 `fetchAllPages()` / `incompleteNote()`（`src/lib/pagination.ts`），CodeArts 改为「分页命名探测 + 公共翻页」；审计其余适配器并修掉两处同源隐患——CodeBuddy 用量明细翻到 10 页上限静默截断（现未取全即不声明 `covers.use`，Cookie 与令牌两处）、MiniMax `totalCount` 缺省 0 导致 `complete` 恒真；新增 `npm run test:pagination` 8 条离线回归；规范写入 `AGENTS.md` 第 9 节与 `docs/modules/取数完整性-列表分页与取全判据-模块现状.md` | 用户要求「确保以后不会出现类似的问题」 | 全篇（新增 3.4 防复发）、4.1、5 |