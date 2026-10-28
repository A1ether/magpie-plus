# Power Meter 代码 Wiki

> 文档类型：模块现状（项目级 Code Wiki）
> 关联模块：项目整体（服务端 / 适配器层 / 聚合层 / 前端 / CLI）
> 创建时间：2026-10-09
> 最后更新：2026-10-27
> 当前状态：已完成
> 关联改动文件：无（纯文档；2026-10-15 同步了「平台多账号支持」、2026-10-18 同步了「扫码 / 打开链接登录」、2026-10-25 同步了 CodeArts 活动逐笔分页取全、2026-10-26 同步了商汤小浣熊适配器、2026-10-27 同步了卡片右上角「重新登录 / 删除」按钮改造带来的代码变化，见变更记录）

---

## 1. 项目概述

**结论：power-meter 是一个「多平台 AI 账户余额看板」，后端用 Node 内置模块聚合各平台余额/积分/到期时间，前端零构建纯静态，运行时第三方依赖为 0。**

| 维度 | 说明 | 证据 |
| --- | --- | --- |
| 定位 | 一个标签页看完所有 AI 平台账户余额、积分与「最快到期」，按到期排序/高亮 | `README.md:1-4` |
| 运行时依赖 | 0（仅 Node ≥ 20.11 内置模块）；devDependencies 只有 `tsx` / `typescript` / `@types/node` | `package.json:21-25` |
| 语言与编译 | TypeScript（ES2023 + NodeNext，strict），`tsx` 直跑、`tsc` 产物 | `tsconfig.json:1-19` |
| 前端 | 原生 HTML/CSS/JS，`public/` 被后端直接托管，无构建步骤 | `src/server.ts:10`、`README.md:228` |
| 支持平台 | Qoder（国内版/国际版）/ Trae（国内版/国际版）/ CodeBuddy（国内版/国际版）/ CodeArts / A1etherOpenAi / MiniMax Code / 商汤小浣熊 内置适配器 + `custom` 声明式接入 + `demo:*` 示例 | `src/providers/index.ts:20-36` |

---

## 2. 整体架构

### 2.1 分层

**结论：四层结构 —— 适配器层负责「把异构平台压成统一快照」，编排层负责「并发 + 缓存 + 降级 + 热加载」，聚合层负责「合并卡片 + 排序 + 到期时间线」，HTTP 层负责「路由 + 静态托管 + 凭证闸门」。**

```
浏览器 public/（app.js 纯前端渲染）
   │  fetch /api/summary（5 分钟轮询）· /api/reveal（按需读密钥）· /api/accounts*（账号管理）
   ▼
HTTP 层  src/server.ts ──── Node 内置 http，12 个 API + 静态托管 + 本机特权闸门
   │
   ▼
编排层  src/service.ts ─── DashboardService：并发探测、TTL 缓存、last-good 降级、配置热加载、日级用量装配
   │                       ├── lib/cache.ts（TTL + in-flight 去重）
   │                       ├── usage-ledger.ts（平台明细 → 日级四字段）
   │                       └── usage-history.ts（本地采样兜底，.run/usage-history.json）
   ▼
适配器层  src/providers/* ── 每平台一个 ProviderAdapter.probe() → ProviderSnapshot
   │                        └──（可选）fetchUsageLedger() → UsageLedger
   ▼
聚合层  src/aggregate.ts ─── groupSnapshots → buildTotals → buildExpiryTimeline → buildPayload
```

### 2.2 一次取数的完整数据流

1. `server.ts:221-229` 收到 `GET /api/summary` → `service.getPayload()`；
2. `service.ts:323-344` `ensureConfigFresh()` 对比 `accounts.json` 与 `.env` 的 mtime，变了就重新 `loadConfig()` 并清空缓存与 last-good；
3. `service.ts:99-135` `collect()` 用 worker 池（并发上限 6，`service.ts:23`）逐账号调用 `probeAccount()`，结束后统一 `usage.flush()` 落盘；
4. `service.ts:182` 缓存命中（TTL 内）直接返回；未命中则 `cache.resolve(key, () => adapter.probe(account, ctx))`；
5. 各适配器用 Cookie/API Key 请求上游 → 解析 → `base.ts` `buildSnapshot()` 产出 `ProviderSnapshot`（含 `balance` / `credits[]` / `windows[]` / `nextExpiryAt`）；
6. `service.ts:183-189` 成功快照写入 `lastGood`，`usage.observe()` 记一次本地样本，再 `fetchLedger()` 拉平台明细；
7. `service.ts:434-472` `withDailyUsage()` 装配日级四字段（按日择优）：**已结束的日子**用明细 `covers` 覆盖的上游值（缺记录 = 0），**进行中的当天**取「上游 vs 本地实时观测」的较大值（`service.ts:146-168` `reconcileToday()` —— 因为上游按天统计当天会滞后，实测只给到 0.01），未覆盖 / 明细失败的维度回退本地采样，并在 `dailyUsage.sources` 里**逐字段**标注 `upstream`/`local`/`mixed`；
8. 探测失败的账号回退 `lastGood` 并标 `stale: true`，且**不展示日级用量**（`service.ts:190-206`）；
9. `aggregate.ts:252-275` `buildPayload()`：先合并 `groupAccounts` 平台、按服务端权重排序，再生成 `totals`、`expiryTimeline`、`nextExpiry`；
10. 前端 `public/app.js` `load()` 拉取 payload → `render()` 渲染汇总、告警、卡片、用量区、时间线与诊断。

### 2.3 「最快到期」的三层实现

| 层 | 职责 | 证据 |
| --- | --- | --- |
| 适配器拆批次 | 每个平台把上游数据拆成带 `expiresAt` 的 `CreditLot[]`；适配器可声明 `fallbackExpiryFromWindow: false` 放弃窗口兜底 | `src/providers/base.ts:198-216`、`src/providers/trae.ts:313` |
| 聚合层生成时间线 | 只保留未来且有余量的批次；无批次过期时间的平台（Qoder 未返回独立过期时间、只有 `nextResetAt` 时）补一条「本期额度重置（未用完即作废）」合成事件 | `src/aggregate.ts:206-276` |
| 每条事件折人民币 | 生成事件时调 `cnyValueFor()` 填 `cny` / `cnyNote`：主数字 = 平台官方「积分包 / 增购积分」单价；悬停里的基准口径 = 官方金额 ×（DeepSeek 官方单价 ÷ 平台该模型 token 单价），只在平台公布了该模型 token 单价时算。`QUOTA`/`USD`/`CNY` 与无单价平台返回 `null` | `src/credit-value.ts:1-186`、`src/aggregate.ts:210-227` |
| 前端分级高亮 | ≤5 天红色脉冲、6~14 天黄色、≥15 天绿色、无过期灰色排最后（阈值由 payload 下发） | `public/app.js:84-100`、`src/aggregate.ts:289-296` |

---

## 3. 目录结构与模块职责

```
power-meter/
├── server 入口与核心
│   ├── src/server.ts        HTTP 服务 + 路由 + 静态托管 + 凭证闸门
│   ├── src/service.ts       编排层 DashboardService（并发/缓存/降级/热加载）
│   ├── src/aggregate.ts     聚合层（合并卡片/汇总/到期时间线/排序）
│   ├── src/credit-value.ts  积分折人民币（各平台官方单价表 + DeepSeek V4.1 Flash 基准；调价只改此表）
│   ├── src/config.ts        .env 解析 + accounts.json 加载 + ${VAR} 展开
│   ├── src/account-store.ts 平台无关的账号增删（manageFields 驱动；CLI 与 HTTP 共用）
│   ├── src/login-session.ts 平台无关的登录会话层（扫码/链接登录的状态机、后台轮询与落库）
│   ├── src/a1etheropenai-store.ts     A1etherOpenAi 账号本地存储（CLI 与 HTTP 共用）
│   ├── src/usage-ledger.ts  平台明细 → 日级用量/增量（covers 声明覆盖维度）
│   ├── src/usage-history.ts 本地采样兜底（.run/usage-history.json，14 天窗口）
│   └── src/types.ts         统一数据契约
├── src/lib/                 无业务语义的通用件
│   ├── http.ts              带硬超时的 fetch + HTTP/业务错误码映射
│   ├── cache.ts             TTL + in-flight 去重 + last-good 兜底
│   └── json.ts              宽泛数字解析 / JSON 路径取值 / 时间戳归一
├── src/providers/           平台适配器（新增平台只动这里）
├── src/cli/                 probe（探测）/ account（任意平台账号管理）/ a1etheropenai-add / capture（抓凭证）
├── public/                  前端（index.html / styles.css / app.js）
├── tools/chrome-debug.sh    启动带 CDP 的独立 Chrome（capture 前置）
├── deploy/accounts.json     部署用账号配置样例
└── docs/                    调研/指南 + 本 Wiki（plans/fixes/modules）
```

| 模块 | 职责 | 关键入口 |
| --- | --- | --- |
| `server.ts` | 路由分发、JSON 应答、静态资源、`/api/reveal` 三道闸门 | `createServer` 回调 `src/server.ts:201-361` |
| `service.ts` | 把「配置中的账号」变成「快照数组」，屏蔽缓存与失败细节，并装配日级用量 | `DashboardService` `src/service.ts:38` |
| `aggregate.ts` | 把快照数组变成最终 `DashboardPayload` | `buildPayload()` `src/aggregate.ts:252` |
| `config.ts` | 唯一的配置读取口；`.env` 可重复加载且不覆盖 shell 环境变量 | `loadConfig()` `src/config.ts:136` |
| `types.ts` | 全项目数据形状的唯一来源 | `ProviderSnapshot` `src/types.ts:147` |
| `providers/base.ts` | 适配器契约 + 快照/窗口/批次构建工具 + 凭证工具 | `ProviderAdapter` `src/providers/base.ts:26` |
| `usage-ledger.ts` | 平台「使用/获取」逐笔明细 → 今日/昨日四个数字（按本地时区切日） | `summarizeLedger()` `src/usage-ledger.ts:71` |
| `usage-history.ts` | 把每次刷新看到的额度记成本地样本，推算日级指标；平台无明细能力时兜底 | `class UsageHistory` `src/usage-history.ts:56` |
| `a1etheropenai-store.ts` | 「改 .env + 改 accounts.json」的原子写，先验证再落盘、写前备份、只回显掩码（A1etherOpenAi 专用语义） | `addA1etherOpenAiAccount()` `src/a1etheropenai-store.ts:261` |
| `account-store.ts` | 平台无关的账号增删：字段声明（`manageFields`）→ probe 验证 → 去重 → 命名 → 分配 .env 槽位 → 备份落盘；A1etherOpenAi 分派回专用存储层；登录落库 `landLoginAccount()` 按 `options.loginAccountId` 去重 | `addManagedAccount()` `src/account-store.ts:321`、`landLoginAccount()` `src/account-store.ts:645` |
| `login-session.ts` | 平台无关的登录会话层：`waiting/landing/ready/expired/failed/cancelled/invalid` 状态机、单平台单会话、TTL、后台轮询上游、`ready` 前完成 probe 验证与落库 | `startLogin()` `src/login-session.ts:145`、`tick()` `src/login-session.ts:216` |
| `public/app.js` | 拉数、排序筛选搜索、卡片/时间线/诊断渲染、用量区、密钥按需读取、登录入口与二维码轮询 | `load()` `public/app.js:177`、`renderLogin()` `public/app.js:813` |

---

## 4. 关键数据契约（src/types.ts）

**结论：所有适配器输出同一个 `ProviderSnapshot`；`credits[]`（批次）是「最快到期」能力的最小单位；凭证值永远不进快照，只有掩码 `SecretRef`。**

| 类型 | 用途 | 关键字段 | 位置 |
| --- | --- | --- | --- |
| `ProviderStatus` | 9 种状态：`ok/demo/notConfigured/unauthorized/forbidden/rateLimited/notFound/unavailable/error` | `OK_STATUSES = {ok, demo}` 判定成功 | `src/types.ts:10-21` |
| `QuotaWindow` | 周期额度窗口（session/daily/weekly/billing） | `used/limit/remaining`、`boundary: reset\|expiry\|mixed`、`resetsAt/expiresAt` | `src/types.ts:41-55` |
| `CreditLot` | 一笔积分批次，「最快到期」最小单位 | `amount`、`expiresAt`、`basis: remaining\|granted`（`granted` 只表到期、不参与汇总） | `src/types.ts:58-72` |
| `Currency` | `CREDITS/POINTS/QUOTA/USD/CNY`，跨单位不换算、分开汇总 | — | `src/types.ts:39` |
| `SecretRef` | 托管密钥引用（只有掩码） | `key/label/preview/hint` | `src/types.ts:110-119` |
| `ProviderEntry` | 合并卡片里的一条账号 | `id = displayName`（`/api/reveal` 与删除的定位键） | `src/types.ts:129-145` |
| `DailyUsage` | 卡片「每日用量/增量」区的四个数字 + 来源标注 | `todayUsed/yesterdayUsed/todayAdded/yesterdayAdded`、`sources: {todayUsed, yesterdayUsed, todayAdded, yesterdayAdded}`（**逐字段**标注来源） | `src/types.ts:96-115` |
| `UsageSource` | 日级指标来源：`upstream`（平台明细）/ `local`（本地采样）/ `mixed` | 前端 tooltip 可见 | `src/types.ts:80` |
| `ProviderSnapshot` | 单账号/单卡片的完整快照 | `balance/credits/windows/nextExpiryAt/secrets/stale/dailyUsage` | `src/types.ts:147-197` |
| `ExpiryHit` | 到期时间线的一条事件 | `provider/lotId/amount/expiresAt/inDays` + `cny/cnyNote`（该笔额度的人民币估值与换算口径明细） | `src/types.ts:210-226` |
| `DashboardPayload` | `/api/summary` 的返回体 | `totals/nextExpiry/expiryTimeline/providers` | `src/types.ts:225-235` |

**快照里的两个易错字段**：`usageUrl` 是「平台用量页」地址（卡片用量区整块作为外链跳转目标），与 `dashboardUrl`（面板入口）不是一回事；空串表示没有可跳转的用量页（`src/types.ts:177-182`）。`dailyUsage` 由编排层在取数成功后填充，适配器构建快照时一律置 `null`（`src/types.ts:192-196`）。

---

## 5. 关键类与函数说明

### 5.1 HTTP 层（src/server.ts）

| 函数 | 职责 | 要点 | 位置 |
| --- | --- | --- | --- |
| `sendJson()` | 统一 JSON 应答 | 一律 `Cache-Control: no-store` | `src/server.ts:25-33` |
| `describeConfig()` | 配置概览 | 只暴露凭证 **key 名**，不暴露值 | `src/server.ts:47-67` |
| `privilegedGuard()` | 本机特权闸门 | ① `Host` 必须回环（或显式 `ALLOW_SECRET_REVEAL=1`）；② 浏览器 `Origin` 也必须回环；③ 必须带自定义头 `X-Power-Meter-Intent`（强制 CORS 预检防 CSRF） | `src/server.ts:81-103` |
| `revealSecret()` | 按需读托管密钥明文 | key 必须在适配器 `revealableSecrets()` 白名单内；必须命中唯一账号（重名 409）；日志只记「谁被读了」 | `src/server.ts:130-179` |
| `serveStatic()` | 托管 `public/` | `resolve` + 前缀校验防目录穿越 | `src/server.ts:181-199` |
| 路由回调 | 9 个 API + 静态兜底 | 见第 7 节 API 表 | `src/server.ts:201-361` |

### 5.2 编排层（src/service.ts）

| 成员 | 职责 | 要点 | 位置 |
| --- | --- | --- | --- |
| `class DashboardService` | 看板编排 | 持有 `configResult` / 两个 `TtlCache` / `lastGood` / `usage` / 两个 mtime | `src/service.ts:38-59` |
| `refresh(filter?)` | 手动刷新 | 清缓存后强制重探，可按平台名/displayName 过滤 | `src/service.ts:79-87` |
| `getPayload()` | 常规取数 | 走 TTL 缓存，前端 5 分钟轮询不会把上游打爆 | `src/service.ts:89-97` |
| `collect()` | 并发探测 | worker 池，`MAX_CONCURRENCY = 6`；一批采样结束统一 `usage.flush()` | `src/service.ts:99-135` |
| `probeAccount()` | 单账号探测 | 未知平台给 `notConfigured` 提示；成功写 `lastGood` + 记一次本地样本；失败回退上次成功值 + `stale: true` 且不展示日级用量；抛异常时仍产出可复制密钥的快照（secrets 与取数解耦） | `src/service.ts:137-237` |
| `fetchLedger()` | 拉平台明细 | 与探测同 TTL 缓存；适配器未实现或抛错一律返回 `null`，不阻塞卡片 | `src/service.ts:243-268` |
| `withDailyUsage()` | 装配日级四字段（按日择优） | 昨日用明细 `covers` 覆盖的上游值（缺记录 = 0）；今日取「上游 vs 本地观测」较大值（`reconcileToday()`，上游按天统计当天滞后）；其余回退本地采样；`sources` 逐字段标注 | `src/service.ts:434-472` |
| `ensureConfigFresh()` | 配置热加载 | 同时监听 `accounts.json` **与 `.env`** 的 mtime（只盯 accounts 会出现「probe 正常但页面刷新无效」）；变更后重建缓存并清空 last-good | `src/service.ts:323-344` |

### 5.3 聚合层（src/aggregate.ts）

| 函数 | 职责 | 要点 | 位置 |
| --- | --- | --- | --- |
| `groupSnapshots()` | 合并 `groupAccounts` 平台 | 每账号压成一条 `ProviderEntry`（`toEntry()`） | `src/aggregate.ts:34-64` |
| `finalizeCard()` | 生成合并卡片 | 条目按额度**升序**（最少的排最前）；状态只要有一个账号可用即 `ok`；混币种只汇总额度最大的一种 | `src/aggregate.ts:66-101` |
| `buildTotals()` | 汇总统计 | 「可用额度」优先取账户余额、无余额退回剩余批次之和（批次是余额的拆分，两者相加会重复计算）；`basis='granted'` 的批次不参与 | `src/aggregate.ts:103-144` |
| `buildExpiryTimeline()` | 到期事件流 | 未来批次升序；未被批次覆盖的 `nextExpiryAt` 补合成事件「本期额度重置（未用完即作废）」 | `src/aggregate.ts:204-250` |
| `buildPayload()` | 组装最终 payload | 排序权重：正常 0 → 无过期信息 1 → 失败 2 | `src/aggregate.ts:252-275`、`sortRank()` `283-285` |

### 5.4 配置层（src/config.ts）

| 函数 | 职责 | 要点 | 位置 |
| --- | --- | --- | --- |
| `loadDotEnv()` | 极简 .env 解析 | 用 `dotEnvOwned` 记录「上次由 .env 写入的键」：可覆盖自己写的旧值，但绝不覆盖 shell/launchd 设置的环境变量；.env 里删掉的键会还原 | `src/config.ts:49-87` |
| `expandEnv()` | `${VAR}` 展开 | 缺失变量记入 `missing` 集合、展开为空串，最终进 `missingEnvVars` 给诊断区提示 | `src/config.ts:95-104` |
| `loadConfig()` | 总装配置 | 支持「数组」或 `{"accounts": [...]}` 两种 accounts.json 结构；只为**已启用**账号收集缺失变量 | `src/config.ts:136-199` |

### 5.5 通用件（src/lib/）

| 函数/类 | 职责 | 位置 |
| --- | --- | --- |
| `fetchJson()` | 带硬超时（`AbortSignal.timeout`）的 JSON 请求，HTTP 错误映射为 `HttpError.kind`（401→unauthorized…） | `src/lib/http.ts:66-105` |
| `assertBusinessOk()` | 兜业务错误码（200 + `code!=0` / `success=false` 的国内平台习惯） | `src/lib/http.ts:108-121` |
| `browserUserAgent()` | UA 取 `.env` 的 `HTTP_USER_AGENT`（CodeBuddy WAF 校验 UA，不可省） | `src/lib/http.ts:17-20` |
| `class TtlCache` | TTL + in-flight 去重（并发刷新同一 key 只打一次上游）+ 失败时保留 stale 值、TTL 缩到 15s 防连续失败放大 | `src/lib/cache.ts:52-81` |
| `asNumber()/firstNumber()` | 宽泛数字解析（`"1,234"` / `"12.5%"` / `"$3.2"`） | `src/lib/json.ts:2-17` |
| `pick()` | 多候选 key 取值（兼容 camelCase/snake_case） | `src/lib/json.ts:33-40` |
| `getPath()` | JSON 路径取值：`a.b[0].c` 与 `a|b`（候选字段）两种写法，是声明式映射的基础 | `src/lib/json.ts:46-71` |
| `toIso()` | 时间归一：兼容秒/毫秒/微秒时间戳与多种字符串 | `src/lib/json.ts:74-90` |

### 5.6 适配器契约与工具（src/providers/base.ts）

| 成员 | 职责 | 位置 |
| --- | --- | --- |
| `interface ProviderAdapter` | 平台适配器契约：`probe()` 必须实现；可选声明 `groupAccounts` / `neverExpires` / `manageAccounts` / `manageFields` / `manageHint` / `envNames` / `isConfigured()` / `login` / `revealableSecrets()` / `fetchUsageLedger()` | `src/providers/base.ts:57-123` |
| `interface ManageFieldDescriptor` | 「添加账号」表单的字段声明（`key`/`label`/`required`/`multiline`/`secret`，及 `target: credential\|option` / `choices` / `defaultValue` / 成组的 `pairedWith` / `flagGroup`），驱动前端表单（option+choices 渲染下拉框、成组字段联动必填）与通用增删实现（`isFieldRequired()` 统一判必填） | `src/providers/base.ts:28-78`、`src/account-store.ts`（`isFieldRequired`） |
| `interface ProviderLogin` | 「扫码 / 打开链接登录」能力：`label` / `hint` / `qr` / `intervalMs` / `start()` / `poll()` / `handleCallback()`（Trae 回调专用）；前端只拿 `authUrl`，令牌留在服务端 | `src/providers/base.ts`（`LoginStartInput` / `LoginStartResult` / `LoginPollResult` / `ProviderLogin`） |
| `ProbeContext.persistCredentials()` | 令牌刷新后写回 `.env` 的钩子（`service.ts` 注入，按账号实际引用的变量名落盘） | `src/providers/base.ts:26`、`src/service.ts:59` |
| `makeWindow()` | 归一化窗口：`remaining` 与百分比由 `used/limit` 推导 | `src/providers/base.ts:144-163` |
| `makeLot()` | 归一化批次：金额取整到 2 位、`basis` 默认 `remaining` | `src/providers/base.ts:165-184` |
| `buildSnapshot()` | 组装快照并计算 `nextExpiryAt`：优先未来批次的最早 `expiresAt`，无批次时用窗口 `expiresAt`/`reset` 时间兜底（可被 `fallbackExpiryFromWindow: false` 关闭） | `src/providers/base.ts:246-303` |
| `maskSecret()` / `describeSecrets()` | 掩码（保前 6 后 4）+ 把适配器声明的凭证转成只有掩码的 `SecretRef[]` | `src/providers/base.ts:305-333` |
| `cred()` / `optString()` / `optBool()` | 从 credentials（优先）/ options 取值 | `src/providers/base.ts:362-399` |
| `extractHeaderValue()` / `normalizeCookieHeader()` | 兼容三种粘贴形式（Copy as cURL / 整行 header / 裸值）与折行 Cookie | `src/providers/base.ts:406-435` |
| `extractExpiry()` | 从 14+ 种命名字段里抽过期时间（含嵌套 `entitlement_base_info` 等） | `src/providers/base.ts:438-479` |
| `notConfigured()` / `failure()` | 缺凭证 / 失败的统一出口 | `src/providers/base.ts:335-360` |

### 5.7 注册表（src/providers/index.ts）

- `REGISTRY`：注册所有内置适配器（qoder/qoder-intl/trae/trae-global/codebuddy/codebuddy-intl/codearts/a1etheropenai/minimax/xiaohuanxiong/qwenwork + custom + demo:*），`src/providers/index.ts:18-37`；
- `resolveAdapter()`：精确匹配 + 别名回退（`qoder.cn`→`qoder`、`agent.minimax.cn`→`minimax`、`xiaohuanxiong.com`/`raccoon`→`xiaohuanxiong`、`qwenwork.cn`/`qwen-office`→`qwenwork` 等，`src/providers/index.ts:42-52`、`63-107`）；
- `describeRegistry()`：给 `/api/providers` 输出适配器清单（含前端表单依赖的 `groupAccounts` / `manageAccounts` / `manageFields` / `manageHint` 声明，以及登录入口依赖的 `login: { label, hint, qr }`），`src/providers/index.ts:79-113`。

### 5.8 日级用量双通道（src/usage-ledger.ts + src/usage-history.ts）

**结论：日级「用量/增量」优先取平台明细接口，明细没覆盖的维度回退本地采样，并在 `dailyUsage.sources` 里逐字段如实标注来源；两个通道互补，不是二选一。** 另有一条**按日**规则：**已结束的日子**（昨日及更早）以平台明细为准（缺记录 = 0 是确定值）；**进行中的当天**（今日）取「上游 vs 本地实时观测」的**较大值**（`src/service.ts:146-168`）——上游按天统计是「统计模型的总量」（不能按笔获取），当天严重滞后：实测 2026-10-25 01:31 上游今天 = 0.01 而余额当天已下降 135.68，旧实现（无条件信任上游）因此显示 ≈0（见 `docs/fixes/用量明细-CodeArts今日用量恒为0-修复方案.md`）。

| 通道 | 实现 | 口径 | 关键点 | 位置 |
| --- | --- | --- | --- | --- |
| 平台明细 | `fetchUsageLedger()` → `summarizeLedger()` | 逐笔 `use`/`grant` 记录，按**本地时区** 00:00–24:00 归集 | `covers: {use, grant}` 声明该平台能**完整覆盖**哪些维度；只有覆盖的维度才用上游值，**缺记录 = 0**（是确定值而非缺失）——该「= 0」只对**已结束的日子**成立，当天要由编排层按日择优 | `src/usage-ledger.ts:26-29`、`71-107` |
| 本地采样 | `usage.observe()` → `metricsFor()` | 每次刷新把卡片即将展示的额度记一个样本，按日聚合增减量 | `KEEP_DAYS = 14`、`MAX_SAMPLES_PER_KEY = 4000`、`HEARTBEAT_MS = 15 分钟`（数值不变也补心跳，好表达「零用量」）；**无样本 = `null`**（缺失而非 0） | `src/usage-history.ts:20-26`、`68+` |

**口径（2026-10-10 用户确认，`src/types.ts:91-94`）**：「用量」= 该日减少量之和、「增量」= 该日增加量之和，两者**互不抵消、各自独立累计**；采样值取 `displayedAmount()`（有账户余额取余额，否则取剩余批次之和），保证时间序列与卡片口径一致（`src/service.ts:140-144`、`357`）。落盘路径 `.run/usage-history.json`（已 gitignore），一批采样结束统一 `flush()` 一次。

---

## 6. 平台适配器详解

**结论：适配器文件各有一份「已实测的接口说明」写在文件头注释里（`qoder.ts` 承载国内版 / 国际版两个平台身份）；CodeBuddy/CodeArts/MiniMax/商汤小浣熊走抓包或客户端静态取证定位的接口，A1etherOpenAi 走官方 OpenAPI，CodeBuddy 曾走声明式映射后升级为专用适配器。**

| 适配器 | 鉴权 | 上游接口 | 解析要点 | 位置 |
| --- | --- | --- | --- | --- |
| `qoder.ts` | **两个平台共用一份实现**（`createQoderAdapter(siteId)` 产出 `qoder` 国内版 / `qoder-intl` 国际版，站点固定、无候选链）。Cookie（国内版 `QODER_COOKIE` / 国际版 `QODER_GLOBAL_COOKIE`，两站点登录态互不通用）<br>或扫码登录的设备令牌（国内版 `QODER_ACCESS_TOKEN` / `QODER_REFRESH_TOKEN`，国际版 `QODER_GLOBAL_*`；变量名由适配器的静态 `envNames` 决定，`login` 声明） | Cookie 模式：`GET {origin}/api/v2/me/usages/big_model_credits`（主）+ `GET {origin}/api/v1/me/userplan`（套餐名，可失败）；令牌模式先试同一网页接口（Bearer，保留批次到期），失败退 `GET {openapi}/api/v2/quota/usage`；origin 由适配器绑定的站点 `QODER_SITES[siteId]`（cn → qoder.com.cn / global → qoder.com）给出，`options.origin` 可覆盖 | 兼容大陆站 snake_case 与国际站 camelCase；Cookie 模式：`total_quota.quota_detail[]` → 逐包批次，明细为空时退回 `resource_package_quota` / `dedicated_resource_package_quota` 的 `quota_detail[]`；**令牌模式只有账户级分桶**（`user_quota` / `add_on_quota` / `org_resource_package`）与可选 `dedicated_resource_packages[]`——分桶 ≠ 逐包明细，note 会明确提示「逐包明细需 Cookie 模式」，专属包逐条带 `expires_at` 与包总量；令牌探测同时用 `GET {openapi}/api/v1/userinfo` 解析昵称，占位名「平台 · 登录账号」会自动改成「平台 · 昵称」（`persistAccountName`）；**Cookie 模式同样解析昵称**：走同一条信任面的网页接口 `GET {origin}/api/v1/me`（`name` 是昵称、`username` 是 UUID 须排除），两个身份接口共用 `parseQoderIdentity()`，网页端登录也把昵称填进 `account.name`；9999 年 `expires_at` 占位按「不过期」忽略；免费档套餐名不展示；过去时间的 `nextResetAt` 忽略；**单站点**：一次刷新只打本平台站点；旧 `options.site` 显式跨站 → 不发上游请求直接给迁移指引，`auto` → 按本平台站点探测并在 note 标注已废弃；`manageAccounts` + `manageFields`（只有 cookie）接入通用增删；设备流 PKCE 扫码登录（站点由平台决定，`options` 不再写 `site`）；改名前显示名「Qoder」由 `legacyDisplayNames` 兼容占位名 | 站点表与工厂 `src/providers/qoder.ts`（`QODER_SITES` / `createQoderAdapter`）、解析与探测同文件、登录 `src/providers/qoder.ts:1036+` |
| `qoder-intl`（同上实现） | 同 `qoder.ts`（`region: global`，面板 `https://qoder.com/account/usage`，开放接口主机 `openapi.qoder.sh`） | 别名 `qoder.com` / `qoder-global` | 见上一行 | `src/providers/index.ts`（`REGISTRY` 中 `qoder` 之后注册） |
| `trae.ts` | Cookie → 自动续签 JWT<br>或「打开链接登录」写入的 `TRAE_ACCESS_TOKEN` / `TRAE_REFRESH_TOKEN` / `TRAE_DEVICE_KEY` | ① `POST .../GetUserToken` 换 Token（Cookie 模式）；② `POST .../user_current_entitlement_list`（`Cloud-IDE-JWT` 头）；令牌模式用 `ExchangeToken` + `DeviceProof`（EC P-256 签名）刷新 | `usage.credits_amount` 是**已用量**，剩余 = limit − used；同名同到期包归并（`mergeLots()`）；JWT 按 Cookie 哈希缓存；`resolveJwt()` 顺序：显式 accessToken → refreshToken 刷新 → Cookie 换 JWT；OAuth 回调固定 `127.0.0.1`，不支持扫码 | `src/providers/trae.ts:43-320`、登录 `src/providers/trae.ts:612+` |
| `trae-global.ts` | Cookie（`TRAE_GLOBAL_COOKIE`，换 JWT）<br>或「打开链接登录」写入的 `TRAE_GLOBAL_ACCESS_TOKEN` / `TRAE_GLOBAL_REFRESH_TOKEN` / `TRAE_GLOBAL_DEVICE_KEY` | ① `POST {账号主机}/cloudide/api/v3/common/GetUserToken`（Cookie → JWT）；② `POST {计费面}/trae/api/v1/pay/user_current_entitlement_list`（主）/ `ide_user_ent_usage`（兜底）；③ `POST {账号主机}/trae/api/v1/pay/ide_user_pay_status`（套餐名，best-effort）；令牌模式用 `ExchangeToken` + `DeviceProof`（与国内版同一套密码学，从 `trae.ts` 导出） | **国际版是独立平台**（`region: global`），与国内版域名/账号/凭证变量（`TRAE_GLOBAL_*`）互不通用；主机按 Cookie 的 `store-idc` 分 SG / US 两区域（`GLOBAL_REGION_HOSTS`），`options.apiHost` / `options.usageUrl` 可覆盖；计量维度三选一（`pickDimension`）：美元计费且 basic/bonus 有额度 → `USD`，否则有 `credits_limit` → `CREDITS`，否则 `POINTS`（进阶模型请求次数）；`dimensionAmounts` 取「第一个 > 0 的候选」（上游把不适用的一侧写成 0）；窗口分母含已用尽的包（避免 0/0），批次只列还有剩余的；`fetchUsageLedger` **只对积分口径账户**生效（逐笔接口只回积分，美元/次数账户返回 null 走本地采样），逐笔分页走 `fetchAllPages()`、未取全整份退回本地采样，`covers` 只声明 `use`（上游不给历史发放流水）；回调白名单只认 `*.trae.ai` / `*.traeapi.us`；网络层失败（`HttpError.status === 0`）报 `error` 而不是「平台不可用」；批次 id 前缀 `trae-global-`（与国内版不撞） | `src/providers/trae-global.ts`（主机表 `GLOBAL_REGION_HOSTS`、解析 `parseGlobalUsage`、`probe`、登录）、复用 `src/providers/trae.ts` 的 OAuth 工具 |
| `codebuddy.ts` | Cookie（**必须含 www 子域**）+ `x-client-platform: web`<br>或扫码登录的插件令牌 `CODEBUDDY_ACCESS_TOKEN` / `CODEBUDDY_REFRESH_TOKEN` | ① `POST .../get-user-resource-summary`（账户级）；② `POST .../get-user-resource-free-packages`（逐包，需带 ① 的 PackageCodes）；令牌模式走 `POST https://www.codebuddy.cn/v2/billing/meter/get-user-resource`（插件身份头） | 批次标 `basis='remaining'`（实测各包剩余之和 = 账户级剩余）；详情接口失败不影响余额；**多账号**：`manageAccounts: true` + `manageFields: [{key:'cookie'}]`（看板/CLI 增删，每个账号一个 `CODEBUDDY_COOKIE[_N]` 槽位），不声明 `groupAccounts`——积分包带真实到期时间，合并会丢批次与最快到期；**两种凭证都接逐笔明细**：用量 `POST {copilot.tencent.com 或 www.codebuddy.cn}/billing/meter/get-user-request-usage`（Bearer + `X-Client-Platform: web`，插件后端优先、网页域回退），获取 = 资源包发放（同一 `get-user-resource`）+ 礼包；令牌失效刷新一次并在同一轮内复用（`cbWithToken` + `WeakMap`），避免重复消费 refreshToken。见 `docs/fixes/CodeBuddy-令牌模式用量明细失效-修复方案.md` | `src/providers/codebuddy.ts:157-200`、`src/providers/codebuddy.ts:290-390`、明细 `src/providers/codebuddy.ts:490-620`、登录 `src/providers/codebuddy.ts:750+` |
| `codearts.ts` | AK/SK（`accessKey` + `secretKey`，走 snap-access 签名网关；`manageFields` 成对必填）或 Cookie（含 `devclouddevuibjtcftk`）+ 头 `x-requested-with`/`x-language`；`isConfigured` 判定「AK/SK 或 Cookie」 | ① `/portal/rest/me`；② `/portal/snap-manager/v1/package/overview`；③ `POST .../countdown-iam5`（订阅到期）；④⑤ `/portal/promptcenter/v1/ops/credit/...`（活动逐笔明细，**⑤ 按页取全**：`page/size` → `pageNum/pageSize` → 默认页兜底，取全判据 `total`/`claimCount`） | 聚合桶 ↔ 逐笔明细对账，差值 ≥1 才补「未明细部分」调节项（并注明是否未取全）；活动上限 `MAX_CAMPAIGNS=20`；明细失败退回聚合口径；有 AK/SK 时余额/窗口走 snap-access（`codearts-aksk.ts`），Cookie 只补活动逐笔；纯 AK/SK 账号直接以 AK/SK 结果为准（不回落到 `notConfigured`）；看板「＋ 添加账号」字段：AccessKey / SecretKey（成对必填）/ Cookie（可选） | `src/providers/codearts.ts:102-151`（⑤ 分页取全）、`:338-536`（解析层与适配器定义） |
| `a1etheropenai.ts` | `Authorization: Bearer <apiKey>`（无 Cookie） | `GET /api/wallet/pai-project-usage/overview`（必备）+ `GET /api/user/info`（标识，可失败） | 只用 `quota_remaining`（QUOTA），**不请求钱包 USD**（系统性虚高且不参与消耗）；`groupAccounts: true` + `neverExpires: true` + `manageAccounts: true` + `manageFields`（两把密钥的表单声明，增删仍走专用存储层）；业务错误码（200+code=401）也映射成状态 | `src/providers/a1etheropenai.ts:67-176` |
| `minimax.ts` | Web 端 `token` 头 + **SSO 会话 Cookie** + 真实登录身份（`user_id`/`op_ticket`/`uuid`）+ 两个 md5 防爬签名头 | `GET /minimax-cloud/api/v1/credit/details`（必备）+ `GET /minimax-cloud/api/v1/signin/status`（只读状态） | **无公开 API**，复刻桌面客户端内部云接口（静态取证自 `app.asar`）；签名盐硬编码、公共参数顺序必须与签名时序列化一致；写死 `user_id=0` 会被 401；`fetchUsageLedger` 只覆盖 `grant` 维度，`use` 回退本地采样；卡片进度条（`windows[]`）由**有效期批次的授予量 vs 剩余量**推导（该平台无账户级汇总接口，已过期批次不计入） | `src/providers/minimax.ts:23-55`、`src/providers/minimax.ts:419-500` |
| `xiaohuanxiong.ts` | **只认 `Authorization: Bearer`**（Cookie 不参与鉴权）。令牌从网页登录态 Cookie 推导：`xhx_token`（IAM JWT）优先 → `raccoon_token`；续期用 `raccoon_refresh_token`（`POST /api/web/auth/v1/refresh`），续期结果经 `persistCredentials` 写回 `.env`，同一轮内用 `WeakMap` 缓存避免重复轮换 | ① `GET /api/web/points/v1/balance`（四类积分当前剩余）<br>② `GET /api/web/points/v2/bills?paging.limit=&type=all&cursor=`（积分明细，游标分页）<br>③ `GET /api/web/auth/v1/entitlement_info`（订阅到期）<br>④ `GET /api/web/auth/v1/user_info`（昵称） | **官方无查询 API**，复刻网页端内部接口（静态取证自桌面端 `app.asar`，路由经未登录请求实测）；鉴权判断放在**解信封之后**（HTTP 200 + `code=200001/200003/200009` 也算令牌失效）；批次金额取各桶账户级剩余、到期时间取明细同类发放的 `expire_at`（每月退回订阅到期、每日退回北京时间当日 24:00），加油包永久有效不参与「最快到期」；`biz_type` 含 `expire` 的过期流水不计入用量/增量；`fetchUsageLedger` 同时覆盖 `use` 与 `grant`，翻页未取全则整份退回本地采样；`manageAccounts` + `manageFields`（cookie / token / refreshToken，三者均非必填但至少一个）+ 网页端登录（CDP 旁听 Cookie 头） | `src/providers/xiaohuanxiong.ts:24-74`（接口与口径）、`:210-305`（令牌缓存与续期）、`:310-650`（解析与批次）、`:826-953`（明细与 `probe`） |
| `qwenwork.ts` | **网页登录态 Cookie**（`QWENWORK_COOKIE`）；前端 `withCredentials: true`，生产无 `VITE_DEV_JWT` 故不带 `Authorization` | ① `GET /user/balance`（剩余可用）<br>② `GET /user/wallets`（三桶 `daily_credits`/`monthly_credits`/`longterm_credits` 的 `total_balance`）<br>③ `GET /user/info`（昵称、`plan.next_refresh_date`/`next_due_date`、`is_biz`）<br>④ `GET /user/billings?limit=&cursor=&source=all`（逐笔） | **官方无查询 API**，接口由前端产物（`index-*.js` + 用量页分包）静态取证、路由经未登录请求实测；信封 `{code:'ok',data}`，`code=invalid-credential` 归 401；三桶各一条批次：每日（北京时间当日 24:00）、每月（`next_refresh_date`→订阅到期→北京时间本月末推算，推算会注明）、长期（无确切日期则不标、不参与最快到期）；构成合计与可用总额不一致时 note 注明；`fetchUsageLedger` 游标分页走 `fetchAllPages()`（把 `has_more` 翻译成 total，未取全整份退回本地采样），`covers` 同时覆盖 `use`+`grant`；`manageAccounts` + `manageFields`（仅 cookie）+ 网页端登录（登录判据 = 页面自己请求 `/user/balance` 拿到 2xx，因登录态 Cookie 名随登录方式变化） | `src/providers/qwenwork.ts`（接口与口径 `:26-59`、解信封与请求 `:85-153`、三桶解析 `:155-196`、套餐解析 `:198-225`、流水与分页 `:262-353`、批次 `:355-420`、登录 `:430-534`、适配器与 `probe` `:536-694`） |
| `sessionApi.ts`（custom） | 自定义 headers | `options.endpoint` 任意平台 | 声明式 `mapping`（wrap/balance/creditLots/windows/plan/nextExpiry）由 `getPath()` 解析；解析不出任何字段时报错并回显响应顶层字段名 | `src/providers/sessionApi.ts:93-203`、`src/providers/presets.ts:13-40` |
| `demo.ts` | 无 | 本地 fixture | 5 个确定性示例（含一个「无过期时间」样本），界面标注「示例数据」 | `src/providers/demo.ts:42-173` |

---

## 7. HTTP API 一览

| 方法 | 路径 | 闸门 | 说明 | 位置 |
| --- | --- | --- | --- | --- |
| GET | `/api/health` | 无 | 存活探测 | `src/server.ts:217-220` |
| GET | `/api/providers` | 无 | 适配器清单、所需凭证与「添加账号」字段声明 | `src/server.ts:222-225` |
| GET | `/api/config` | 无 | 配置概览（仅凭证 key 名）+ warnings + missingEnvVars | `src/server.ts:227-230` |
| GET | `/api/summary` | 无 | 聚合结果；`?force=1` 跳过缓存 | `src/server.ts:232-241` |
| POST | `/api/refresh` | 无 | 手动刷新，body 可带 `{providers: [...]}` | `src/server.ts:243-256` |
| POST | `/api/reveal` | 本机三道闸门 | 按需读托管密钥明文（白名单 + 唯一账号定位） | `src/server.ts:258-277` |
| GET | `/api/accounts?provider=` | 本机三道闸门 | **通用账号清单**（含停用/缺凭证，只回掩码；A1etherOpenAi 走专用实现） | `src/server.ts:279-292` |
| GET | `/api/accounts/overview` | 本机三道闸门 | **平台与账号总览**：全部内置平台（含 0 账号的平台）+ 各自账号清单，供顶栏弹窗渲染；只回掩码 | `src/server.ts`（`/api/accounts/overview` 分支） |
| POST | `/api/accounts/add` | 同上 | **通用添加账号**：按 `manageFields` 取字段 → `probe()` 验证 → 去重/命名 → 分配 `.env` 槽位 → 备份落盘 | `src/server.ts:294-340` |
| POST | `/api/accounts/remove` | 同上 | **通用删除账号**，顺带清理不再被引用的 `.env` 变量 | `src/server.ts:342-366` |
| GET | `/api/a1etheropenai/accounts` | 同上 | A1etherOpenAi 账号清单（兼容保留；通用接口已覆盖） | `src/server.ts:368-377` |
| POST | `/api/a1etheropenai/accounts` | 同上 | 添加账号：先验证两把密钥再写盘（支持整段粘贴 `text`） | `src/server.ts:379-426` |
| POST | `/api/a1etheropenai/accounts/remove` | 同上 | 删除账号，顺带清理不再被引用的 `.env` 变量 | `src/server.ts:428-448` |
| POST | `/api/login/start` | 本机三道闸门 | 发起扫码 / 链接登录，返回会话与 `authUrl`（不含令牌） | `src/server.ts:460-490` |
| GET | `/api/login/poll?id=` | 同上 | 读登录会话状态（前端 1.5s 轮询；上游轮询在服务端） | `src/server.ts:492-505` |
| POST | `/api/login/cancel` | 同上 | 取消登录会话 | `src/server.ts:507-521` |
| GET | `/authorize` | 回环 Host + 活跃会话绑定 + 域名白名单 | Trae OAuth 回调：只投递授权码，返回静态「登录完成」页（无 `lt`，按活跃会话反查；带 `lt` 的旧链接兼容） | `src/server.ts:576-605` |

---

## 8. 依赖关系

### 8.1 内部模块依赖（→ 表示 import）

```
server.ts ─→ config.ts、account-store.ts、a1etheropenai-store.ts、providers/index.ts、service.ts
service.ts ─→ aggregate.ts、config.ts、lib/cache.ts、providers/{base,index}.ts、types.ts、
              usage-history.ts、usage-ledger.ts
aggregate.ts ─→ lib/json.ts、types.ts
config.ts ─→ lib/json.ts
usage-history.ts ─→ lib/json.ts、usage-ledger.ts（共用 startOfDay）
usage-ledger.ts ─→ types.ts（仅类型）
a1etheropenai-store.ts ─→ config.ts、providers/base.ts(maskSecret)、providers/a1etheropenai.ts、types.ts
account-store.ts ─→ a1etheropenai-store.ts（读写与备份复用 + A1etherOpenAi 分派）、config.ts、providers/{base,index}.ts、types.ts
providers/* ─→ providers/base.ts、lib/{http,json}.ts、config.ts、types.ts、(usage-ledger.ts)
cli/probe.ts ─→ config.ts、providers/index.ts、types.ts
cli/account.ts ─→ account-store.ts、providers/index.ts
cli/a1etheropenai-add.ts ─→ a1etheropenai-store.ts、providers/a1etheropenai.ts、types.ts
cli/capture.ts ─→ lib/json.ts（+ 外部命令 playwright-cli）
public/app.js ─→（运行时）/api/summary、/api/refresh、/api/reveal、/api/providers、/api/accounts*
```

**要点**：「CLI 与 HTTP 共用一份实现」是账号管理的核心约束——`/api/accounts*` 与 `npm run account` 走 `src/account-store.ts` 同一条校验/备份/写盘路径（`src/account-store.ts:1-20`），而 A1etherOpenAi 的增删继续走 `src/a1etheropenai-store.ts`（`/api/a1etheropenai/accounts` 与 `npm run a1etheropenai:add`，`src/a1etheropenai-store.ts:1-13`）；通用层只做 provider 分派，避免动到那条已稳定的链路。

### 8.2 外部依赖

| 类别 | 内容 |
| --- | --- |
| 运行时 npm 依赖 | 无。只用 Node 内置模块：`http` / `fs` / `path` / `url` / `child_process` |
| devDependencies | `typescript ^5.7`、`tsx ^4.19`、`@types/node ^22`（`package.json:21-25`） |
| 系统级外部工具 | `capture.ts` 通过 `execFileSync` 调用 `playwright-cli` 操作调试 Chrome（`src/cli/capture.ts:33-45`）；`tools/chrome-debug.sh` 依赖本机 Chrome（可用 `CHROME_BIN` 覆盖，`tools/chrome-debug.sh:18`） |
| 上游平台 | 各适配器依赖上游接口的字段形状，上游改字段会以明确错误信息暴露（如「响应缺少 total_quota.quota_summary」，Qoder 报错时同时给出本平台站点与凭证变量名，`src/providers/qoder.ts`） |

---

## 9. 配置系统与凭证安全

**结论：配置只有两个文件（`accounts.json` + `.env`），都支持热加载；凭证推荐以 `${VAR}` 存在 `.env`、accounts.json 只放占位符；明文永不出现在 API 响应、日志与文档中。**

| 机制 | 说明 | 证据 |
| --- | --- | --- |
| 环境变量 | `PORT/HOST/CACHE_TTL_SECONDS/REQUEST_TIMEOUT_MS/EXPIRY_*_DAYS/ACCOUNTS_PATH/ALLOW_SECRET_REVEAL/HTTP_USER_AGENT`，默认值见 `DEFAULTS` | `src/config.ts:27-34`、`.env.example:1-14` |
| 凭证引用 | `credentials`/`options` 里任意字符串支持 `${ENV_VAR}` 展开，缺失变量进 `missingEnvVars` | `src/config.ts:95-104` |
| 热加载 | `DashboardService` 轮询两个文件的 mtime；`npm run capture` / `account` / `a1etheropenai:add` 写完即生效，无需重启 | `src/service.ts:345-366` |
| 凭证不下发 | `/api/config` 只回 key 名；快照里 `secrets[]` 只有 `maskSecret()` 掩码（前 6 后 4） | `src/server.ts:59-78`、`src/providers/base.ts:294-322` |
| 三道闸门 | 回环 Host（或显式 `ALLOW_SECRET_REVEAL=1`）+ 回环 Origin + 自定义头 `X-Power-Meter-Intent`（强制 CORS 预检） | `src/server.ts:92-114` |
| A1etherOpenAi 托管 | 默认只暴露项目密钥 `paiApiKey`；账号密钥需 `options.revealApiKey: true` | `src/providers/a1etheropenai.ts:105-115` |
| 写盘安全 | 先验证再写入、备份到 `.run/`（gitignore）、删除时只清「不再被引用」的 `.env` 槽位；通用层为每个平台独立分配槽位（`CODEBUDDY_COOKIE` → `_2`） | `src/account-store.ts:1-20`、`src/account-store.ts:113-145`、`src/a1etheropenai-store.ts:8-13`、`src/a1etheropenai-store.ts:434-499` |
| 多账号标识 | 同平台 `displayName` 必须唯一（缓存键/采样键/删除定位都用它）；`loadConfig()` 对重名发告警，服务端在重名时给缓存键追加序号兜底 | `src/config.ts:181-201`、`src/service.ts:38-58` |

---

## 10. 运行方式

### 10.1 常用命令（package.json scripts）

| 命令 | 作用 | 实际执行 | 位置 |
| --- | --- | --- | --- |
| `npm install` | 安装 dev 依赖 | tsx/typescript/@types/node | `package.json:26-30` |
| `cp accounts.example.json accounts.json` | 初始化账号配置 | — | `README.md:24-26` |
| `npm start` | 启动服务（默认 `http://127.0.0.1:8787`） | `tsx src/server.ts` | `package.json:12` |
| `npm run dev` | 热重载开发模式 | `tsx watch src/server.ts` | `package.json:11` |
| `npm run chrome:debug` | 启动 CDP 调试 Chrome（端口 9222，独立 profile `~/.power-meter/chrome-profile`） | `tools/chrome-debug.sh` | `tools/chrome-debug.sh:14-44` |
| `npm run capture` | 从调试 Chrome 抓四平台登录态写入 `.env`（`--dry-run` 只预览，`--slot N` 写多账号槽位） | `tsx src/cli/capture.ts` | `src/cli/capture.ts:35-44`、`src/cli/capture.ts:667-669` |
| `npm run probe` | 命令行探测取数（支持平台过滤与 `--json`） | `tsx src/cli/probe.ts` | `src/cli/probe.ts:1-15` |
| `npm run account -- ...` | **任意平台**账号增删列：`--list` / `--provider X --cookie '<值>'` / `--env VAR` / `--remove '<名>'` | `tsx src/cli/account.ts` | `src/cli/account.ts:1-19` |
| `npm run a1etheropenai:add -- ...` | 添加/查看/删除 A1etherOpenAi 账号（先验证再写入，自动备份） | `tsx src/cli/a1etheropenai-add.ts` | `src/cli/a1etheropenai-add.ts:55-71` |
| `npm run typecheck` | 类型检查（`tsc --noEmit`） | — | `package.json:22` |
| `npm run build && npm run serve` | 编译到 `dist/` 后以纯 JS 运行 | `tsc -p tsconfig.json` + `node dist/server.js` | `package.json:23-24` |

### 10.2 典型流程

1. **首次运行**：`npm install` → `npm start` → 打开 `http://127.0.0.1:48213`。`accounts.json` 可以不存在——
   看板里添加第一个账号时会自动创建（想带入示例配置再 `cp accounts.example.json accounts.json`）。
2. **接入真实账号**：顶栏「平台与账号」→ 选中平台 →「扫码 / 打开链接登录」或「填写凭证」（CodeBuddy / Qoder / Trae / A1etherOpenAi），
   验证通过后自动落库；已有卡片上「重新登录」展开的是同一套面板（登录按钮组 + 手工兜底），
   追加同平台的另一个账号走顶栏弹窗。
   不在网页入口覆盖范围内的平台（CodeArts / MiniMax / custom）：`npm run chrome:debug` → 在调试窗口登录各平台并打开用量页 →
   `npm run capture` → `npm run probe` 验证 → 刷新页面（凭证写进 `.env` 后**热加载**，无需重启）。
3. **托管 A1etherOpenAi**：顶栏「平台与账号」→ A1etherOpenAi →「登录 / 添加账号」，或 `npm run a1etheropenai:add -- <账号密钥> <项目密钥>`。
4. **部署**：`npm run build && npm run serve`；对外暴露需反代鉴权 + `ALLOW_SECRET_REVEAL=1`（见 `src/server.ts:370-378` 的启动告警）。

### 10.3 CLI 工具职责

| 工具 | 职责 | 关键实现 |
| --- | --- | --- |
| `cli/probe.ts` | 逐账号诊断：状态/耗时/余额/批次/掩码密钥；不走聚合层合并，标题用账号自己的名字 | `src/cli/probe.ts:33-115` |
| `cli/account.ts` | 任意平台的账号增删列：解析 `--provider` / 字段 flag（按适配器 `manageFields`）/ `--env` / `--remove`，真正的校验写盘在 `account-store.ts` | `src/cli/account.ts:1-19`、`main()` `120-215` |
| `cli/a1etheropenai-add.ts` | 参数解析与结果打印；真正的校验写盘在 `a1etheropenai-store.ts`；支持 `--text` 整段粘贴（`parseKeyPairText()` 按「个人/项目」语义分拣） | `src/cli/a1etheropenai-add.ts:105-192`、`src/a1etheropenai-store.ts:502-524` |
| `cli/capture.ts` | 通过 `playwright-cli` 驱动调试 Chrome：收集 Cookie（按「域可覆盖」规则，父域+子域都要）、抓真实 UA、定位用量接口、写入 `.env`（`--slot N` 给变量名加后缀实现多账号）；报告写 `.playwright-cli/capture-report.md` | `src/cli/capture.ts:35-44`、`cookieAppliesTo()` `142-145`、`main()` `620-770` |

> **待确认**：`capture.ts` 的 `TARGETS` 现含 MiniMax Code 抓取分支（需从 `localStorage` 的 `_token` / `user_detail_agent` / `UNIQUE_USER_ID` 取值），本次仅核对了 `minimax.ts` 侧的消费方式，未逐行核对 `capture.ts` 里该分支的实现。

---

## 11. 前端（public/）概览

**结论：零构建单页；渲染只用 `textContent` + `<template>` 克隆（不用 innerHTML 拼上游数据，防注入）；排序/筛选/搜索全部在前端完成，不触发网络请求。**

| 部分 | 职责 | 位置 |
| --- | --- | --- |
| `index.html` | 页面骨架：汇总区 / 告警条 / 工具栏（6 种排序 + 搜索）/ 卡片网格 / 到期时间线 / 诊断区 | `public/index.html:31-74` |
| `app.js` 状态与轮询 | `REFRESH_INTERVAL_MS = 5 分钟`；批次默认展示 3 条、账号条目 2 条（其余折叠） | `public/app.js:1-7` |
| `load()` / `render()` | 先 `loadAdapterMeta()`（`/api/providers`），再拉 `/api/summary` 并全量重渲染 | `public/app.js:177-215` |
| `buildCard()` / `renderEntries()` | 平台卡片（进度条、批次列表、永不过期标签）与合并账号条目 | `public/app.js:381-660` |
| `renderUsage()` | 卡片「每日用量/增量」区：平台声明了 `usageUrl` 时**整块作为外链**跳转平台用量页（新窗口），否则纯展示；数值取 `provider.dailyUsage` 同名键，字段整体缺失时兼容旧后端 | `public/app.js:521-570` |
| `loadAdapterMeta()` / `metaFor()` | 拉一次 `/api/providers`，缓存每个适配器的 `displayName` / `manageFields` / `manageHint`（管理表单据此渲染，前端不认平台名） | `public/app.js:696-741` |
| `renderManage()` / `removeAccount()` | 同一份逻辑服务两个宿主（`cardTarget` 卡片 / `dialogTarget` 顶栏弹窗），登录块由 `/api/providers` 的 `login` 声明驱动，字段由 `manageFields` 动态生成（长文本用 textarea），调 `/api/accounts*`、`/api/login/*`（带 `X-Power-Meter-Intent` 头）；提示键卡片为 `provider#accountId`、弹窗为 `platform#<id>`。卡片右上角按钮组由 `options.cardActions`（`root`/`add`/`relogin`/`remove`）传入并按形态分流：**单账号卡片 = 「重新登录」+「删除」，合并卡片（`groupAccounts`）= 「＋ 登录 / 添加账号」**（卡片级没有单一账号身份，删除留在每条账号行）；`removeAccount()` 返回 `'cancelled'\|'removed'\|'failed'`，第 4 参 `onSettled` 决定提示落管理区结果行（refresh 前）还是「诊断」区（refresh 后，因 `renderDiagnostics` 会 `replaceChildren`） | `public/app.js`（`renderManage` / `removeCardAccount` / `flashNotice` / `hideCardActions`） |
| `renderLogin()` / `scheduleLoginPoll()` | 扫码 / 打开链接登录：申请会话 → `QRCode.toCanvas()` 本地画码（`public/vendor/qrcode.js`，MIT）→ 1.5s 轮询 `/api/login/poll` → `ready` 后刷新看板；Trae 不画码只给链接。**登录块不再有站点下拉**：请求体只带 `provider`，站点由平台决定（国内版 `qoder` / 国际版 `qoder-intl`）。会话按 `providerId` 识别，卡片与弹窗共享 | `public/app.js`（`renderLogin`） |
| `openPlatformsDialog()` / `renderPlatformDialog()` / `buildPlatformRow()` | 顶栏「平台与账号」弹窗：按 `/api/accounts/overview` 列出全部平台（含 0 账号），**一个平台一行**，每行可进入管理视图或删除账号；看板空态也给同一入口。国内版 / 国际版是两个平台身份，因此天然各占一行、各自只列本平台账号 | `public/app.js`（`renderPlatformDialog` / `buildPlatformRow`） |
| `renderConfiguredAccounts()` | 管理面板里的账号清单：合并卡片只列未进条目的（停用/缺凭证），单账号卡片列出全部并标出当前卡片，每行带删除 | `public/app.js:1214-1243` |
| `renderSecrets()` / `requestSecret()` | 托管密钥掩码展示 + 按需调 `/api/reveal` 读取明文复制 | `public/app.js:945-1085` |
| `renderTimeline()` / `renderDiagnostics()` | 到期时间线与配置诊断（缺失变量、warnings）；时间线每条额度后接 `≈ ¥X`（后端给的 `cny`，口径写明在 `title` 里，前端不做换算） | `public/app.js:2205-2238`、`public/app.js:2240-2270` |

---

## 12. 扩展指南：新增一个平台

1. 在 `src/providers/` 新建 `yourPlatform.ts`，实现 `ProviderAdapter`（至少 `probe()`；复用 `cred()` / `makeWindow()` / `makeLot()` / `buildSnapshot()` / `notConfigured()` / `failure()`）；
2. 在 `src/providers/index.ts:18-29` 的注册数组加一行（需要别名再补 `ALIASES`）；
3. `accounts.json` 加一条该平台的账号；
4. 若声明式映射即可满足（有稳定接口、字段规整），**完全不用写代码**：用 `custom` + `options.endpoint/mapping/headers`（`src/providers/presets.ts:33-37`）；
5. 按需追加可选声明：`revealableSecrets`（密钥托管）、`groupAccounts`（多账号一张卡；**额度带真实到期时间的平台别加**，合并会清空 `credits`/`windows`/`nextExpiryAt`）、`neverExpires`（永不过期标签）、`fetchUsageLedger`（日级用量明细）、`usageUrl`（用量区外链跳转）；
6. 想让用户在界面上直接增删账号（多账号支持），再加 `manageAccounts: true` + `manageFields`（字段声明，见 §5.6 的 `ManageFieldDescriptor`）与 `manageHint`；`.env` 变量名默认推导为 `<平台 id>_<字段名>`，不符的用 `envNames` 覆盖。声明之后 `/api/accounts*`、看板表单与 `npm run account` 自动可用，**不用改前端**（`src/account-store.ts:321-521`）。
7. 若还想支持「扫码 / 打开链接登录」，声明 `login`（`start` / `poll`，见 §5.6 的 `ProviderLogin`），并补 `isConfigured` 让「缺凭证」判断认令牌模式；登录成功后由 `src/login-session.ts` 统一验证与落库，前端入口自动出现，**不用改前端**。

---

## 13. 已知风险与待确认项

| 风险/限制 | 说明 | 证据 |
| --- | --- | --- |
| 上游字段变更 | CodeBuddy/CodeArts/MiniMax 接口是抓包或客户端静态取证定位的，上游改字段会以明确错误暴露，届时更新对应单文件 | `src/providers/minimax.ts:23-55` |
| MiniMax 走内部接口 | 无公开 API，复刻客户端内部云接口并硬编码签名盐；上游改签名算法或加设备指纹会直接失效 | `src/providers/minimax.ts:39-51` |
| 日级用量的两套口径 | 明细来源「该日无记录 = 0」（仅对已结束的日子成立）、本地采样「该日无样本 = `null`」，前端 tooltip 按 `sources` **逐字段**标注；「今日」取两者较大值（来源 `mixed`）；**实测两者口径不同账**（上游按天统计是模型总量：10-24 上游 1821.95 而余额零下降） | `src/types.ts:96-115`、`src/service.ts:146-168`、`434-472` |
| 本地采样依赖服务在线 | 服务未运行则采不到样本，「今日用量」会是 `null` 而非 0；样本窗口 14 天 | `src/usage-history.ts:21-26` |
| Cookie 过期 | 所有 Cookie 平台过期后卡片显示 `unauthorized` 并保留旧数据（stale 黄标）；重新登录 + `npm run capture` | `src/service.ts:190-206` |
| Trae JWT 仅 1 天 | 由 `resolveToken()` 用 Cookie 自动续签并缓存，无需人工维护 | `src/providers/trae.ts:181-222` |
| 服务无鉴权 | 默认只监听 `127.0.0.1`；暴露到局域网必须自行加反代鉴权（服务持有所有平台登录态） | `README.md:570-572` |
| 待确认 | `src/cli/capture.ts` 仅通读前 120 行 + 函数清单，其 `TARGETS` 各平台的抓取细节（含 MiniMax 的 localStorage 分支）未逐行核对（不影响本文档其余结论）；本次新增的 `--slot` 只核对了槽位命名与写入分支 | `src/cli/capture.ts:115-120`、`src/cli/capture.ts:35-44` |
| 待确认 | 2026-10-15 的多账号改动已同步账号管理相关条目（第 3、5.6、5.7、6、7、8.1、9、10、11、12 节）的行号；其余条目若引用了同批文件（`base.ts`/`server.ts`/`service.ts`）中未同步的行号，可能与当前代码有偏差 | 本节 |
| 登录协议依赖上游实现 | CodeBuddy 插件认证、Qoder 设备流、Trae OAuth 均按客户端/CLI 静态取证复刻；上游改 state 校验、PKCE 参数或签名格式会直接失效，表现为登录会话 `failed` | `src/providers/codebuddy.ts:658+`、`src/providers/qoder.ts:864+`、`src/providers/trae.ts:612+` |
| Trae 登录必须在本机 | 回调固定 `http://127.0.0.1:<PORT>/authorize`，从其他机器打开看板时无法完成登录（前端不展示二维码，只提示用 Cookie） | `src/providers/trae.ts:625`、`public/app.js:856-860` |
| 令牌模式无逐笔明细 | CodeBuddy / Qoder 的用量明细接口只对网页会话开放；令牌模式下 `fetchUsageLedger` 返回 `null`，日级用量退回本地采样 | `src/providers/codebuddy.ts:556`、`src/providers/qoder.ts:834` |

---

## 变更记录

| 时间 | 变更内容 | 触发原因 | 影响章节 |
| --- | --- | --- | --- |
| 2026-10-09 | 初版：通读全部 `src/`（22 个 TS 文件）、`public/` 骨架、`tools/` 与配置文件后产出 | 「项目代码 Wiki」模块梳理任务 | 全篇 |
| 2026-10-10 | 补齐 `04b5859` / `82eb49c` / `05898fc` 三个提交带来的漂移：新增 5.8 节（日级用量双通道）、登记 MiniMax 适配器、补 `usageUrl` / `DailyUsage` / `UsageSource` 契约、取数数据流补 2 步、修正 `service.ts` / `types.ts` / `aggregate.ts` / `base.ts` / `providers/index.ts` 的过期行号 | 「了解项目」复盘，发现 Wiki 早于最近三个提交 | 1、2.1、2.2、3、4、5.2、5.3、5.6、5.7、5.8、6、8.1、10.3、11、12、13 |
| 2026-10-11 | 第 6 节 MiniMax 行补充「卡片进度条由有效期批次推导」口径（对应 `minimax.ts` 新增窗口逻辑） | MiniMax 卡片补齐额度进度条，适配器新增 `windows[]` | 6 |
| 2026-10-15 | 同步「平台多账号支持」：新增 `account-store.ts`（通用账号增删）与 `cli/account.ts`、`/api/accounts*` 三条通用路由、`ProviderAdapter.manageFields/manageHint/envNames` 声明、CodeBuddy 开启看板增删、`capture --slot N`、`displayName` 唯一性告警与缓存键去重；并按新代码校正相关行号 | 用户提出「添加多账号支持，如 codebuddy，我有两个账号」（见 `docs/plans/账号管理-平台多账号支持-开发设计方案.md`） | 2.1、2.2、3、5.6、5.7、6、7、8.1、9、10、11、12、13 |
| 2026-10-18 | 同步「扫码 / 打开链接登录」：新增 `login-session.ts` 会话层、`/api/login/*` 与 `/authorize` 路由、`ProviderAdapter.login/isConfigured` 与 `ProbeContext.persistCredentials`、三平台令牌探测与刷新、前端登录块与 `public/vendor/qrcode.js`；补三平台鉴权/接口与风险条目 | 用户提出「codebuddy、qoder、trae 的添加方式改成扫码或打开链接登录，一个账号一个卡片」（见 `docs/plans/账号管理-扫码与链接登录-开发设计方案.md`） | 2.1、3、5.6、5.7、6、7、11、12、13 |
| 2026-10-18 | Qoder：明确「令牌模式只有账户级分桶、逐包明细需 Cookie」，新增 `resource_package_quota` / `dedicated_resource_package_quota` 的 `quota_detail[]` 兜底；令牌探测用 `/api/v1/userinfo` 解析昵称，占位名「平台 · 登录账号」自动改为「平台 · 昵称」（`ProbeContext.persistAccountName` + `account-store.adoptResolvedAccountName`） | 用户反馈「并没有修复，还是没有明细；希望显示账号名 A1ether」 | 6、7、13 |
| 2026-10-18 | 新增顶栏「平台与账号」总览入口：`GET /api/accounts/overview`（全部内置平台 + 账号清单）、管理区模板化（卡片与弹窗共用 `renderManage`）、空看板引导按钮；`readAccountsFile()` 在 `accounts.json` 缺失时按空清单处理，网页端首个账号自动建文件 | 用户反馈「没有 qoder 账号就没有添加账号入口；启动后所有操作都应在网页完成」（见 `docs/plans/账号管理-网页端统一账号入口-开发设计方案.md`） | 5.1、7、10.2、11、13 |
| 2026-10-21 | Qoder Cookie 模式补昵称解析：`GET {origin}/api/v1/me`（`name` 是昵称、`username` 是 UUID 须排除）+ 与 openapi `userinfo` 共用 `parseQoderIdentity()`；网页端登录 `poll()` 由写死空串改为填昵称，落库当轮即命名 | 用户要求 Qoder 卡片标题与 CodeBuddy / Trae 一致（显示账号名）（见 `docs/fixes/Qoder-Cookie模式卡片标题拿不到昵称-修复方案.md`） | 5.6、13 |
| 2026-10-21 | 「平台与账号」弹窗按站点拆行（Qoder 国内版 / 国际版），入口锁定站点；`ManagedAccountView` 增非敏感 `options`（仅 `manageFields` 声明为 `option` 的键） | 用户要求添加账号也区分 Qoder 国内版与国际版（见 `docs/plans/前端看板-平台与账号按站点拆分-开发设计方案.md`） | 5.1、7、13 |
| 2026-10-23 | CodeArts 接入网页端增删：`manageAccounts` + `manageFields`（AccessKey / SecretKey 成对必填、Cookie 可选）、`isConfigured` 支持「AK/SK 或 Cookie」、`readCodeArtsAkSk(account, ctx)` 纳入 `cred()` 口径；`ManageFieldDescriptor` 新增 `pairedWith` / `flagGroup`，前端表单联动必填；APIG.0613（IAM 校验失败）映射为 `unauthorized` | 用户要求「继续华为云 CodeArts 网页增删的开发」（见 `docs/plans/账号管理-CodeArts网页端增删账号-开发设计方案.md`） | 5.6、6 |
| 2026-10-25 | 日级四字段改「按日择优」：新增 `reconcileToday()`（`service.ts:146-168`）——**今日取「上游 vs 本地观测」较大值**（上游按天统计是模型总量、当天滞后：实测 0.01 vs 余额下降 135.68）、昨日以上游为准；`DailyUsage.sources` 由按维度改为**逐字段**；CodeArts 按天用量收紧覆盖声明并加诊断日志；`codearts:console-probe` 新增 charts 原始返回 | 用户报告「华为云余额有减少但今日用量还是 0」，并指出该页「使用明细不能按笔获取、是统计模型的总量」（见 `docs/fixes/用量明细-CodeArts今日用量恒为0-修复方案.md`） | 3、5.2、5.7、5.8、11、13 |
| 2026-10-25 | Qoder 国内站与国际站拆成两个平台身份（`qoder` / `qoder-intl`，共用 `qoder.ts` 实现，站点固定）：注册表新增 `qoder-intl`、`ProviderAdapter` 新增可选 `legacyDisplayNames`、`envNames` 由函数改为两个静态表、`CREDIT_PEGS` 按 provider 命中、登录会话去掉 `site`、前端删除站点下拉 / `siteScope` / `platformSections` 等站点拆分逻辑、新增 `npm run migrate:qoder-sites` 迁移脚本与 `docs/plans/平台适配器-Qoder国内站与国际站彻底分离-开发设计方案.md` | 用户要求「将 qoder cn 和 qoder 国际版两个供应商分离的更彻底，当成两个供应商来看待」 | 2.2、5.1、6、7、11、13 |
| 2026-10-25 | CodeArts 活动逐笔改为**按页取全**（`fetchCampaignBenefitEntries()`，`src/providers/codearts.ts:102-151`）：⑤ `campaign/{id}` 三方式取数（`page/size` → `pageNum/pageSize` → 默认页兜底），取全判据为响应 `total` 或活动 `claimCount`；新增 `parseCampaignBenefitEntries()`（不过滤已用尽批次，供翻页判进度）、`CreditCampaignDetail.coverageNote`；缺口调节项仅在真正对不上时出现并注明「未取全」，用量「获取」维度未取全时不声明覆盖；诊断脚本增打 `data.total` 与分页对照。方案见 `docs/fixes/CodeArts-活动奖励逐笔分页截断-修复方案.md` | 用户反馈「活动奖励积分（未明细部分）4,339.62」在官方「获取明细」中其实是多笔每日签到且消费扣款发生在这些批次上，核实为 `campaign/{id}` 默认页 20 条截断了 29 笔 | 2.2、3、6、7、12 |
| 2026-10-26 | 新增「商汤小浣熊」适配器（`src/providers/xiaohuanxiong.ts`，provider id `xiaohuanxiong`）：接口取自桌面端 `app.asar` 静态取证 + 未登录请求实测（`/api/web/points/v1/balance`、`/api/web/points/v2/bills`、`/api/web/auth/v1/entitlement_info`、`/api/web/auth/v1/user_info`、`/api/web/auth/v1/refresh`）；鉴权只认 `Authorization: Bearer`，令牌从 Cookie 的 `xhx_token`/`raccoon_token` 推导、`raccoon_refresh_token` 续期（`WeakMap` 同轮缓存）；四类积分拆批次（每日/每月/奖励取明细 `expire_at`，加油包永久有效）、明细同时覆盖 `use`+`grant`；`capture` 新增 `cookieTokenNames` 支持「令牌藏在 Cookie 里」的平台；`CREDIT_PEGS` 增加油包单价（¥19.9/1,200）；注册表与别名、`accounts.example.json` / `.env.example` / README / 凭证指南同步 | 用户提出「给项目增加对商汤小浣熊的开发支持」（见 `docs/plans/平台适配器-接入商汤小浣熊积分-开发设计方案.md`） | 1、2.2、5.7、6、11 |
| 2026-10-27 | 新增「千问办公」适配器（`src/providers/qwenwork.ts`，provider id `qwenwork`）：接口由前端产物静态取证 + 未登录请求实测（`/user/balance`、`/user/wallets`、`/user/info`、`/user/billings`），鉴权只认网页登录态 Cookie；三桶拆批次（每日 / 每月 / 长期），长期积分拿不到到期时间则不标；`fetchUsageLedger` 走 `fetchAllPages()` 并把 `has_more` 翻译成 total（新增两条游标分页回归断言）；`CREDIT_PEGS` 增加标准版单价（¥78/2,000）；`capture` 新增 `qwenwork` Target；北京时间日 / 月边界抽到 `src/lib/beijing-time.ts`（小浣熊同步改为引用它）；注册表与别名、`accounts.example.json` / `.env.example` / README / 凭证指南同步 | 用户提出「给项目增加对千问办公 https://qwenwork.cn/app/settings/usage 的支持」（见 `docs/plans/平台适配器-接入千问办公积分-开发设计方案.md`） | 2.2、5.7、6、11 |
| 2026-10-27 | 单账号卡片右上角由复合按钮「＋ 登录 / 添加账号」改为**「重新登录」+「删除」**两个按钮：`index.html` 的 `.card__balance` 内新增 `.card__actions` 容器（`root`/`add`/`relogin`/`remove` 四个节点交给 `renderManage` 分流），合并卡片（A1etherOpenAi）保持「＋ 添加账号」、删除仍在账号行上；新增 `removeCardAccount()`（先 `POST /api/login/cancel` 取消本平台活跃会话，避免 `landLoginAccount` 把刚删的账号又建回来；禁用态存 `state.deleting`）、`flashNotice()`（卡片级结果落「诊断」区）、`hideCardActions()`（容器一并隐藏，否则 `margin-left:auto` 在额度行留空 gap）；`removeAccount()` 增第 4 参 `onSettled` 并改为返回 `'cancelled'\|'removed'\|'failed'`，确认框平台名改取适配器声明（原来会拼出「CodeBuddy · 账号 1 账号「CodeBuddy · 账号 1」」）。**后端与适配器零改动** | 用户要求「做了在卡片右上角登录/添加账号的供应商，右上角改为『重新登录』『删除』两个按钮」（见 `docs/plans/供应商卡片-右上角重新登录与删除-开发设计方案.md`） | 10.2、11 |
