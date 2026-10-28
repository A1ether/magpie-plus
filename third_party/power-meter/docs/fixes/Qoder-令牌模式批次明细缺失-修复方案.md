# Qoder - 令牌模式拿不到批次明细 - 修复方案

> 文档类型：修复方案
> 关联模块：平台适配器（Qoder）、账号存储（展示名）
> 创建时间：2026-10-18
> 最后更新：2026-10-18
> 当前状态：实施中（第二轮代码已落地，`npm run typecheck` 通过；待人工 `npm run probe -- qoder` 与看板核对）
> 关联改动文件：`src/providers/qoder.ts`、`src/providers/base.ts`、`src/account-store.ts`、`src/service.ts`、`src/cli/probe.ts`、`docs/modules/项目整体-代码Wiki.md`、`docs/凭证配置指南.md`、`README.md`

---

## 1. 问题现象

**结论：上一轮「修好了」的判断不成立——卡片只剩一条「赠送/活动额度 2,397」，那是 openapi 的账户级额度分桶，不是用户要的「批次/资源包明细」；同时卡片标题是通用占位名「Qoder · 登录账号」，没有显示平台昵称。**

| 项 | 内容 |
| --- | --- |
| 触发环境 | Qoder 国内站，账号凭证为设备令牌（`accessToken` + `refreshToken`，无 Cookie） |
| 复现步骤 | 1. 看板「＋ 登录」扫码添加 Qoder；2. 打开 Qoder 卡片；3. 与 qoder.com.cn 网页「用量」页对照 |
| 实际结果 | 只列出「赠送/活动额度 2,397 CREDITS」，没有网页上「N Included Credit Packs / 每个包的总额、剩余、到期」；卡片标题为「Qoder · 登录账号」 |
| 期望结果 | 列出真实批次（每个资源包一条，含总额/剩余/到期）；卡片标题显示账号昵称，如「Qoder · A1ether」 |
| 影响范围 | 仅影响 Qoder 令牌模式；余额与进度条数值本身正确 |

---

## 2. 根因分析

**结论：两层原因。① 「批次明细」只存在于网页会话面（Cookie + CSRF）的接口里，Bearer 令牌只能拿到账户级汇总，上一轮把汇总拆成三条并当成「批次」，属于口径误判；② 登录时上游设备流响应没有昵称，落库逻辑又只在新账号创建时命名、更新时永不改名，于是卡片停在通用占位名。**

### 2.1 证据链：为什么令牌模式拿不到逐包明细

| # | 证据 | 位置 |
| --- | --- | --- |
| E1 | 令牌打网页用量接口 401，属预期（两种信任面不同） | `.run/server.log`：`Qoder 国内站网页用量接口（令牌）不可用：HTTP 401 {"errorCode":"Unauthorized",...}`（日志不含凭证） |
| E2 | 网页用量响应由 `withCredentials` + `X-CSRF-TOKEN` + `x-requested-with` 的请求层发出，**不带 Authorization** | 网页 bundle `0.0.727` 请求层；本仓库对应 `headersForSite()` `src/providers/qoder.ts:554-562` |
| E3 | 网页「Add-on Credits」把 `quota_detail[]` 渲染成“{num} Included Credit Packs”，逐包显示 `remaining_value / limit_value / expires_at / source` | 网页 bundle 模块 `91991`（AddOnCredits）与 `31193` |
| E4 | 网页「Dedicated Resource Packages」同样逐包渲染，名称取 `display_labels` 的 `title`，金额为「剩 remaining / 共 limit」 | 网页 bundle 模块 `38980` |
| E5 | 网页个人资源包/专属包来自组织域接口（`/api/v1/organizations/{org}/resource-packages/usage/{quota}`、`.../dedicated-resource-packages/usage/{quota}`），仅网页会话可用 | 网页 bundle 模块 `45984`、`46813` |
| E6 | openapi `/api/v2/quota/usage` 的契约只有账户级分桶 + 可选专属包数组：`user_quota` / `add_on_quota` / `org_resource_package` / `dedicated_resource_packages[]` | `qodercli 1.1.63` 内嵌 normalizer（`normalizeQuotaUsage` / `Kg`）与 statusLine schema |
| E7 | qodercli 自身没有 `quota_detail`、`usages/big_model_credits`、`organization-shared` 等网页接口；`/sash/api/v2/me/usage` 仅在账号有 orgId 时启用，返回的仍是同样的分桶结构 | `qodercli 1.1.63` 静态取证（`isUsagePresentationEnabled()` 判空 orgId；`normalizeUsagePresentation()`） |
| E8 | 上一轮把三个账户级分桶各拆一条，并把 note 写成「批次来自 openapi 额度接口」——数值对，但语义错 | `src/providers/qoder.ts:942-948`（`pushBlockLot`）、`src/providers/qoder.ts:352-360` |

**结论（E1-E8）**：令牌模式能拿到的「真实批次」只有 `dedicated_resource_packages[]` 一种；`user_quota / add_on_quota / org_resource_package` 是账户级汇总，不能当作逐包明细展示。逐包 `quota_detail[]` 必须使用 Cookie 模式（网页会话面）。

### 2.2 现状核对：当前账号的实际构成

| 字段 | 值 | 含义 |
| --- | --- | --- |
| `user_quota.remaining` | 0 | 本期套餐额度已用完（总 2400 的那部分） |
| `add_on_quota.remaining` | 2397 | 赠送/活动额度（网页上对应的是一组 Credit Pack） |
| `org_resource_package` | 无 | 个人账号，无团队共享额度 |
| `dedicated_resource_packages` | 无 | 当前没有模型专属包 |

因此卡片上的「赠送/活动额度 2,397」数值正确，但它只是 `add_on_quota` 的汇总；网页上之所以能看到明细，是因为 Cookie 会话接口额外返回了 `quota_detail[]`。

### 2.3 账号名缺陷（本轮新增）

| # | 证据 | 位置 |
| --- | --- | --- |
| N1 | 设备流 `poll` 只从令牌响应里取 `user_id`，昵称字段通常为空 | `src/providers/qoder.ts:1156-1167` |
| N2 | 落库时昵称为空 → 使用通用占位名 `<适配器> · 登录账号` | `src/account-store.ts:768-777` |
| N3 | 更新已有账号时**保留原名**（displayName 是卡片/缓存/删除的稳定标识），即使后续拿到了昵称也不改名 | `src/account-store.ts:768-790` |
| N4 | 当前 `accounts.json` 的 Qoder 条目 `displayName` 为「Qoder · 登录账号」，`options` 里没有 `loginName` | 本机 `accounts.json`（不含凭证） |
| N5 | 昵称的权威来源是 `GET {openapi}/api/v1/userinfo`（Bearer），返回 `name / username / email / organization_id` 等 | `qodercli 1.1.63`：`/api/v1/userinfo` + `normalizeUserInfo()`；`QoderBridge/docs/协议参考.md` §3.1 |
| N5b | **（2026-10-21 补正）** 上面的「权威来源」只说了令牌模式。Cookie 模式走同一条信任面的网页接口 `GET {origin}/api/v1/me`，返回 `{ id, username, name, email, avatar }`——`name` 即昵称，`username` 是 UUID（不可当昵称）。本方案当时未覆盖这条路径，是 Cookie 模式卡片标题停在占位名的直接原因 | `docs/fixes/Qoder-Cookie模式卡片标题拿不到昵称-修复方案.md` §2.3（含本机缓存里的真实响应） |
| N6 | 服务端目前只有「令牌刷新写回 `.env`」的钩子，没有「把解析出的昵称写回 accounts.json」的通道 | `src/service.ts:63-106`（`persistRefreshedCredentials`）、`src/providers/base.ts:16-27`（`ProbeContext`） |

---

## 3. 修复方案

**结论：① 令牌模式不再把账户级分桶包装成「明细」，note 明确说明边界，并把 `dedicated_resource_packages[]` 解析做扎实；② Cookie 模式补上逐包 `quota_detail[]` 的兜底容器；③ 登录与探测时解析 `/api/v1/userinfo` 昵称，把通用占位名改为「Qoder · 昵称」，用户自定义过的名字不动。**

### 3.1 令牌模式：诚实的分桶 + 真正的专属包

| 项 | 处理 |
| --- | --- |
| 账户级分桶 | 仍保留「套餐额度 / 赠送/活动额度 / 团队共享额度」三条（保证 `Σ credits = balance`），但 note 改为：**令牌模式仅返回账户级额度分桶；逐包批次明细需 Cookie 模式** |
| `dedicated_resource_packages[]` | 逐包拆批次；`remaining` 缺失时用 `total - used` 兜底；名称优先 `display_labels` 的 `title`（含 `value_i18n`），其次 `name`、`description`；到期时间保留 `expires_at` |
| note 文案 | 删除「批次来自 openapi 额度接口」这种把汇总误称为批次的说法；有专属包时注明来源为 `dedicated_resource_packages` |
| 组织账号 | `/sash/api/v2/me/usage` 仍只在 orgId 非空时可用，且结构与 openapi 一致（仅多 `displayMode`），因此**不新增请求**，避免无收益的上游调用（**待确认**：若后续出现企业账号需要区分展示模式再接入） |

### 3.2 Cookie 模式：补逐包明细的兜底容器

网页 bundle 的 AddOnCredits / DedicatedResourcePackages 都消费 `{ quota_summary, quota_detail[] }`，而容器键可能是 `resource_package_quota` / `dedicated_resource_package_quota`，不一定是 `total_quota`。

| 规则 | 处理 |
| --- | --- |
| `total_quota.quota_detail[]` 非空 | 沿用现有 `parseQoderLots()`，行为不变（避免重复计算） |
| `total_quota` 缺失或明细为空 | 依次从 `resource_package_quota` / `dedicated_resource_package_quota` 的 `quota_detail[]` 取逐包批次；`expires_at` 缺失时标注「随本期重置作废」 |
| 汇总口径 | 仍以 `total_quota.quota_summary` 为准；只在缺汇总时用各容器 `quota_summary` 求和 |

> 该容器的真实响应形态尚未在本机实测（无 Cookie 账号）——**待确认**，代码按上述顺序兼容，不影响令牌路径。

### 3.3 账号名：解析 → 采用 → 持久化

| 步骤 | 处理 |
| --- | --- |
| 解析 | 令牌探测成功后，用 Bearer 请求 `GET {openapi}/api/v1/userinfo`，取 `name || username`；**仅当卡片名仍是占位名时才请求**（改完即停，不产生常驻开销）；失败只记日志，不影响取数 |
| 登录 | `login.poll()` 在返回 `ready` 前做同一请求，把昵称填进 `account.name`；设备流响应里的 `user_name` 仍作兜底 |
| 采用 | 仅当当前 `displayName` 仍是通用占位（`<适配器> · 登录账号` 或 `…· 登录账号 N`）时才改名；用户自定义的名字一律不动 |
| 持久化 | `ProbeContext` 新增 `persistAccountName(name) => Promise<string \| null>`；服务端实现写回 `accounts.json`（同平台重名自动加序号），返回最终展示名；CLI / 登录验证不注入该钩子，只影响显示 |
| 即时生效 | 探测结果用最终展示名组装快照，无需等下一轮配置重载 |

### 3.4 改动点清单（已落地）

| 文件 | 改动 | 位置 |
| --- | --- | --- |
| `src/providers/qoder.ts` | 新增 `qoderUserInfo()`（`GET /api/v1/userinfo`，Bearer，best-effort）；令牌探测采用昵称并通过 `persistAccountName` 写回；note 改为「令牌模式仅返回账户级额度分桶…逐包明细需 Cookie 模式」+ 专属包数量说明；`readDedicatedPackages()` 补 `total-used` 兜底与 `display_labels` 标题、note 带包总量；`parseQoderUsage()` 补 `resource_package_quota` / `dedicated_resource_package_quota` 的 `quota_detail[]` 兜底容器；`login.poll()` 在设备流无昵称时补查 userinfo | `src/providers/qoder.ts:128-155`（userinfo）、`:371-410`（令牌快照）、`:860-929`（专属包）、`:932-1060`（统一解析）、`:1278-1290`（登录） |
| `src/providers/base.ts` | `ProbeContext` 新增 `persistAccountName`；`SnapshotInput` 新增 `displayName` / `accountId`（改名当轮快照即生效，操作仍指向正确条目）；新增 `isPlaceholderAccountName()`（探测与落库共用同一份占位名判定） | `src/providers/base.ts:26-36`、`:246-258`、`:318-330`、`:356-368` |
| `src/account-store.ts` | 新增 `uniqueAccountName()`、`adoptResolvedAccountName()`（探测期改名并备份写盘）；`landLoginAccount()` 更新路径在「占位名 + 本次拿到昵称」时改名，并同步写 `displayName`；占位名判定统一用 `base.ts` 的 `isPlaceholderAccountName()` | `src/account-store.ts:646-720`、`:830-855` |
| `src/service.ts` | 新增 `persistResolvedName()` 并注入 `ProbeContext.persistAccountName`（改名会在服务日志里留一行旧名 → 新名，不含凭证） | `src/service.ts:109-141`、`:313-320` |
| `src/cli/probe.ts` | 探针同样注入 `persistAccountName`：CLI 验证一次即可完成「昵称 → 卡片名」的落库，与看板口径一致 | `src/cli/probe.ts:66-76` |
| `docs/modules/项目整体-代码Wiki.md`、`docs/凭证配置指南.md`、`README.md` | 同步「令牌模式只有账户级分桶 / Cookie 模式才有逐包明细」与账号名行为 | 见各文件 Qoder 行 |

---

## 4. 回归验证

> 按仓库规范（AGENTS.md §6），本次只做静态检查；以下动态验证均**未执行**，由人工完成。

| 步骤 | 命令 / 操作 | 预期结果 |
| --- | --- | --- |
| 1 | `npm run typecheck` | 通过 |
| 2 | `npm run probe -- qoder` | 令牌账号：`ok`；note 出现「账户级额度分桶…需 Cookie 模式」；卡片不再宣称是批次明细；同一次运行会把「Qoder · 登录账号」改名为「Qoder · A1ether」（`.run/` 下留备份） |
| 3 | 重启服务后看板刷新 | Qoder 卡片标题变为「Qoder · <昵称>」；`accounts.json` 的 `displayName` 被更新，其余字段不变；服务日志出现一行「展示名自动更新：「Qoder · 登录账号」→「Qoder · <昵称>」」 |
| 4 | 自定义过名字的账号 | 名称保持不变（占位名判定不命中） |
| 5 | 令牌账号如出现 `dedicated_resource_packages` | 每个包一条批次，名称/剩余/到期正确 |
| 6 | Cookie 账号（若有） | 仍解析 `total_quota.quota_detail[]`；如报文只给 `resource_package_quota.quota_detail[]`，也能列出逐包批次 |

---

## 5. 风险与回滚

| 风险 | 影响 | 缓解 |
| --- | --- | --- |
| 改名会改变 `displayName`，进而改变缓存键 / 管理面板定位 | 一次刷新内可能多用一次上游请求 | 只在「通用占位名」时改名；同平台重名自动加序号；写盘前校验目标条目 |
| `persistAccountName` 与登录落库并发写 accounts.json | 极小概率互相覆盖 | 仅改 `displayName` / `options.loginName` 两个字段，读-改-写窗口极短；**待确认**是否需要复用登录会话的写锁 |
| `quota_detail[]` 兜底容器推断有误 | Cookie 模式批次可能重复或缺失 | 仅在 `total_quota` 明细为空时启用兜底；不影响令牌路径 |
| `/api/v1/userinfo` 上游变更 | 昵称获取失败 | 纯 best-effort，失败只记日志，展示名维持原值 |
| 回滚 | — | 回滚 `src/providers/qoder.ts`、`src/providers/base.ts`、`src/account-store.ts`、`src/service.ts`；无配置迁移、无数据契约变更 |

---

## 6. 静态检查结论

- 第一轮：`npm run typecheck` 通过；但功能口径被用户否定（见 §1）。
- 第二轮（本轮）：`npm run typecheck`（`tsc --noEmit`）通过，exit 0；未执行 `npm run probe` / `npm start` / 真实上游请求（按 AGENTS.md §6）。
- 未覆盖风险：`quota_detail[]` 兜底容器的真实报文形态（无 Cookie 账号可验）；`/api/v1/userinfo` 在本账号上的实际返回字段（待人工 probe 时从日志确认，日志不含凭证）；改名后 accounts.json 的最终写入结果。

---

## 变更记录

| 时间 | 变更内容 | 触发原因 | 影响章节 |
| --- | --- | --- | --- |
| 2026-10-18 | 初版：定位「令牌模式批次恒为空」根因，升级 `parseQoderUsage()` 兼容扁平结构并拆出专属资源包批次 | 用户反馈「Qoder 平台获取不到批次明细」 | 全篇 |
| 2026-10-18 | 回填用户实测：批次正常产出、批次之和 = 余额、单位带入 label；状态改为「已完成」；专属资源包分支与 Cookie 路径列为未覆盖项 | 用户执行 `npm run probe -- qoder` | §6、状态 |
| 2026-10-18 | **推翻上一轮结论**：账户级分桶不是批次明细；补充网页 bundle / qodercli 证据链，新增「账号名缺失」缺陷（N1-N6）；方案改为「令牌模式诚实标注 + Cookie 模式逐包兜底 + 昵称解析与持久化」；状态回到「实施中」 | 用户反馈「并没有修复，还是没有明细，另外，我希望显示账号名，如目前的 Qoder 用户是 A1ether」 | §1、§2、§3、§4、§5、状态 |
| 2026-10-18 | 落地第二轮代码：userinfo 昵称解析、占位名改名与写回、令牌模式 note 更正、专属包解析增强、`quota_detail[]` 兜底容器；补充 `displayName`/`accountId` 快照字段；`npm run typecheck` 通过 | 按 §3 方案实施 | §3.4、§6、状态 |
| 2026-10-21 | 补齐本方案遗漏的第三条路径：本方案的昵称解析只覆盖了「令牌模式 + 扫码登录」，**Cookie 模式（这两个账号的实际形态）从未调用任何身份接口**，故标题一直停在占位名。已按 `docs/fixes/Qoder-Cookie模式卡片标题拿不到昵称-修复方案.md` 修复（Cookie 走网页 `/api/v1/me` 取 `name`）；同时修正 N5 的边界——`/api/v1/userinfo` 要 Bearer 是事实，但「网页侧拿不到昵称」不成立 | 用户再次反馈卡片标题仍是「Qoder · 登录账号」 | §2.3 N5、变更记录 |
