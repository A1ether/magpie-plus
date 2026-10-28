# CodeBuddy-令牌模式用量明细失效-修复方案

> 文档类型：修复方案
> 关联模块：CodeBuddy / 用量明细（usage ledger）
> 创建时间：2026-10-18
> 最后更新：2026-10-18
> 当前状态：已完成（静态检查通过；动态验证未执行，按 AGENTS.md 仅做静态检查）
> 关联改动文件：`src/providers/codebuddy.ts`、`docs/modules/项目整体-代码Wiki.md`、`docs/plans/账号管理-扫码与链接登录-开发设计方案.md`

## 1. 问题现象

**结论：CodeBuddy 账号改用「扫码登录」的令牌模式后，卡片「今日 / 昨日用量」不再来自平台明细，而只由本地采样推算（服务没在观测的时段直接缺失），表现为「只能本地采集」。**

- 复现路径：账号凭证只有 `accessToken` / `refreshToken`（`accounts.json` 里两个 CodeBuddy 账号均为令牌模式，见「证据」），打开看板，卡片用量区 tooltip 显示「来源：本地采样兜底」。
- 触发环境：CodeBuddy 国内站（`www.codebuddy.cn`），令牌模式；Cookie 模式不受影响。
- 影响范围：
  - 「今日 / 昨日用量」（`dailyUsage.usedToday/usedYesterday`）退化为本地采样，服务离线期间的变化整段记到恢复当天。
  - 「今日 / 昨日增量」（`added*`）同样退化。
  - 余额、资源包、最快到期不受影响。
- 证据（只读核对，未请求上游）：
  - `src/providers/codebuddy.ts:558-561`：`fetchUsageLedger()` 开头 `if (!cred(account, ctx, ['cookie', 'cookieHeader'])) return null;`，令牌账号直接返回 `null`；
  - `src/service.ts:387-404`：`ledger === null` 时四字段全部走 `UsageHistory` 本地采样；
  - `accounts.json` 中两个 CodeBuddy 账号的 `credentials` 只有 `accessToken` / `refreshToken`（无 `cookie` 键）。

## 2. 根因分析

**结论：用量明细实现当初按「网页 Cookie 会话」写死，令牌模式下被主动短路；但上游这组网页接口本身也接受插件 Bearer 令牌，未做适配导致能力缺失。**

| 事实 | 证据 |
| --- | --- |
| `fetchUsageLedger()` 只在有 Cookie 时工作，无 Cookie 一律 `return null` | `src/providers/codebuddy.ts:557-562` |
| 注释明确写着「接口只对网页会话开放」（该判断与社区实测不符） | `src/providers/codebuddy.ts:558` |
| 令牌模式余额走 `POST /v2/billing/meter/get-user-resource`（Bearer + 身份头） | `src/providers/codebuddy.ts:65`、`:282-312` |
| 用量明细走 `POST /billing/meter/get-user-request-usage`（Cookie + `x-client-platform: web`） | `src/providers/codebuddy.ts:46`、`:563-600` |
| 令牌失效判定只覆盖 401/403 与业务码 12153 | `src/providers/codebuddy.ts:155-159` |
| 社区实现用同一套 Bearer 令牌直接调用 `/billing/meter/get-user-request-usage`（插件后端主机 `copilot.tencent.com`），请求体同为 `{startTime,endTime,pageNum,pageSize}` | workbuddy2api `admin/backend/session.py:118-125`、`core/converter.py:625`、`admin/backend/usage.py:1-35` |
| 社区实现另用 `Authorization: Bearer` + `X-User-Id` + `X-Client-Platform: web` 调 `/billing/meter/get-user-resource-summary|paid-packages|free-packages` | buddyToken `workbuddy-api.js:118-125`、`:493-505`、`:830-845` |

根因拆成两条：

1. **能力缺口**：`fetchUsageLedger()` 对令牌账号直接返回 `null`，编排层只能回退本地采样（`src/service.ts:388-402`）。
2. **令牌刷新不可复用**：令牌失效时刷新逻辑埋在 `cbProbeWithToken()` 内部（`src/providers/codebuddy.ts:325-341`），明细接口若也用令牌，需要同一套「失效 → 刷新 → 重试 → 写回」；若各自独立刷新，同一轮取数可能用**已轮换的旧 refreshToken**再刷一次，存在把刚写入的新令牌刷废的风险。

## 3. 修复方案

**结论：把「网页会话鉴权」抽象成 Cookie / Bearer 两种形态，令牌账号走同一组明细接口；令牌刷新抽成共享包装器，并复用同一轮已刷新的令牌，避免重复刷新。**

### 3.1 改动点清单

| # | 改动 | 位置 | 说明 |
| --- | --- | --- | --- |
| 1 | 新增账号身份提取 `cbIdentity()` | `src/providers/codebuddy.ts` | 统一取 `uid` / `enterpriseId` / `domain`，供令牌模式各接口复用 |
| 2 | 新增令牌态网页头 `cbWebTokenHeaders()` | 同上 | `Authorization: Bearer` + `X-User-Id` / `X-Enterprise-Id` / `X-Domain` + `X-Client-Platform: web`，对齐 buddyToken 实测形态 |
| 3 | 新增令牌态明细请求 `cbTokenRequestUsage()` | 同上 | 先试插件后端 `copilot.tencent.com/billing/meter/get-user-request-usage`（workbuddy2api 实测），非鉴权失败再试网页域 `www.codebuddy.cn/billing/meter/get-user-request-usage` |
| 4 | 抽取 `cbExtractAccounts()` | 同上 | 兼容两种信封：网页域 `data.Accounts`、插件后端 `data.Response.Data.Accounts`；资源包发放解析同步复用 |
| 5 | 新增 `cbTokenLedger()` | 同上 | 用量逐笔（2 个本地日、`pageSize=500`、最多 10 页）+ 资源包发放（复用探针同一 `get-user-resource`）+ 礼包尝试；礼包失败只放弃 `grant` 覆盖 |
| 6 | 新增 `cbWithToken()` + 令牌缓存 | 同上 | 「调用 → 失效则刷新一次 → 重试」的共享包装；刷新结果用 `WeakMap<AccountConfig,…>` 记到账号对象上，本轮明细复用同一令牌 |
| 7 | `fetchUsageLedger()` 改为按凭证分派 | 同上 | 有 Cookie → 原有 Cookie 路径（行为不变）；无 Cookie 但有令牌 → 令牌路径；两者皆无 → `null` |
| 7b | `parseCodeBuddyTokenPacks()` 改传完整响应体 | 同上 | 否则 `cbExtractTokenAccounts()` 拿不到 `code` 字段，会误判「没有 Accounts」 |
| 8 | `cbIsAuthError()` 扩充 | 同上 | 补业务码 `10085` 与 `unauthorized` 文案（buddyToken 实测 10085 表示凭证失效） |
| 9 | 令牌探测改用共享刷新包装 | 同上 | `cbProbeWithToken()` 不再自己写刷新重试，改调 `cbWithToken()` |
| 10 | 卡片 note 文案更新 | 同上 | 「登录令牌模式，逐笔用量由本地采样兜底」→ 说明用量/获取明细取自令牌接口 |
| 11 | 文档同步 | `docs/modules/项目整体-代码Wiki.md:240`、`docs/plans/账号管理-扫码与链接登录-开发设计方案.md:247` | 删除「令牌模式不接逐笔明细」的过时结论 |

### 3.2 令牌模式取数口径（与 Cookie 模式保持一致）

| 维度 | 接口 | 覆盖声明 |
| --- | --- | --- |
| 用量 `use` | `POST {插件后端}/billing/meter/get-user-request-usage`（失败换网页域）| `covers.use = true`；拿不到即整体抛错 → 编排层回退本地采样 |
| 获取 `grant` | 资源包：`POST {billing}/get-user-resource`（与探针同源，按 `CycleStartTime` + `CycleCapacitySizePrecise` 逐包记一笔）；礼包：`POST {网页域}/billing/meter/check-gift-claimed` | 任一来源失败 → `covers.grant = false`（整体回退本地采样），不出现「只统计一半还标 upstream」 |

- 时间窗与 Cookie 模式一致：本地时区「昨日 00:00:00」到「今日 23:59:59」。
- 单页 `pageSize` 取 500（workbuddy2api 实测该接口可整包返回，`_PAGE_SIZE=500`），上限 10 页；沿用现有「返回条数 < pageSize 即停止」。
- 令牌刷新：业务码 `10085` / HTTP 401 / 403 / `12153` 均视为登录态失效。

### 3.3 为什么这样改

- **不改 Cookie 路径**：历史账号行为完全不变，降低回归面。
- **复用已实测的主机与头**：令牌余额接口已在本仓库跑通（`cbBillingHeaders`），明细则先试同一个插件后端；网页域只作为形态回退。
- **刷新只做一次**：`cbWithToken()` 是唯一刷新入口，`WeakMap` 保证同一轮 `probe` 与 `fetchUsageLedger` 用同一个新令牌。

### 3.4 被否决的方案

| 方案 | 否决原因 |
| --- | --- |
| 令牌账号也去抓网页 Cookie | 违背扫码登录「无需维护 Cookie」的目标，且 Cookie 会过期，回到原问题 |
| 明细里独立实现「失效→刷新」 | 一轮内可能重复消费旧 refreshToken（轮换型），有刷废风险 |
| 令牌模式只补 `use`、`grant` 永远本地采样 | 与 Cookie 模式口径不一致；资源包接口本来就用同一令牌可取 |

## 4. 回归验证

**结论：本次仅做静态检查，未请求上游；下列动态验证需人工执行。**

| 验证项 | 步骤 | 预期结果 |
| --- | --- | --- |
| 类型检查 | `npm run typecheck` | 无错误 |
| 令牌账号取数 | `npm run probe -- codebuddy` | 余额正常；日志无「用量明细获取失败」 |
| 用量来源 | 看板卡片用量区悬停 | tooltip 显示「来源：平台明细」；今日/昨日数值与页面「使用明细」一致 |
| 增量来源 | 同上 | 有礼包/资源包发放时「来源：平台明细」，否则按礼包接口结果 |
| Cookie 账号回归 | 用带 Cookie 的账号跑 probe | 与改动前一致 |
| 令牌过期 | 让账号令牌过期（或等待） | 日志出现一次「尝试用 refreshToken 续期」，`.env` 写回新令牌，本轮明细仍成功 |

## 5. 风险与回滚

| 风险 | 说明 | 应对 |
| --- | --- | --- |
| 网页域不接受 Bearer | `/billing/meter/get-user-request-usage` 在 `www.codebuddy.cn` 上可能只认 Cookie | 令牌模式**先走**插件后端 `copilot.tencent.com`（社区实测可用），网页域仅作回退；两条都失败时按现有机制回退本地采样，卡片不报错 |
| 礼包接口不接受 Bearer | `check-gift-claimed` 失败 | 只把 `covers.grant` 置 false，`use` 仍走上游 |
| 单页 500 条仍被服务端截断 | 上游存在「从窗口起点返回、最多 3000 条」的行为 | 窗口只覆盖 2 个本地日，10×500 页足够；后续如需更长窗口，按「取回最新时间作为下一轮起点」推进 |
| 重复刷新 | 一轮内 probe 与明细各自刷新 | `WeakMap` 共享本轮刷新结果；配置重载后对象被替换，缓存自然失效 |
| 回滚 | 需要恢复旧行为 | 还原 `src/providers/codebuddy.ts` 即可；无配置/契约变更，无数据迁移 |

## 变更记录

| 时间 | 变更内容 | 触发原因 | 影响章节 |
| --- | --- | --- | --- |
| 2026-10-18 | 初版：定位令牌模式明细被短路，给出双凭证鉴权与共享刷新方案 | 用户反馈「CodeBuddy 昨日/今日用量只能本地采集」 | 全篇 |
| 2026-10-18 | 实施完成：`fetchUsageLedger()` 按凭证分派；新增 `cbWebTokenHeaders` / `cbTokenRequestUsage` / `cbTokenLedger` / `cbWithToken` / `cbExtractTokenAccounts`；补业务码 10085；`npm run typecheck` 通过；同步代码 Wiki 与登录方案 | 代码落地 | §3.1、§3.2 |
