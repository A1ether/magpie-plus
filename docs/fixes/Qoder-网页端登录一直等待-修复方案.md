# Qoder - 网页端登录一直等待 - 修复方案

> 文档类型：修复方案
> 关联模块：平台适配器（Qoder）、CDP 抓取（`src/lib/cdp.ts`）
> 创建时间：2026-10-18
> 最后更新：2026-10-18
> 当前状态：已完成（代码已落地；`npm run typecheck` 通过；已用真实模块对「登录态 / 匿名态」各做一次本地端到端验证，结论见 §4.1；看板人工回归未执行）
> 关联改动文件：`src/lib/cdp.ts`、`src/providers/qoder.ts`

---

## 1. 问题现象

**结论：用户在调试 Chrome 窗口里已经完成 Qoder 登录（页面标题已是账号昵称「研发编程助手」），但看板上的登录状态永远停在「正在等待你在调试 Chrome 窗口里登录…」，直到 5 分钟 TTL 超时，Cookie 从不落库。**

| 项 | 内容 |
| --- | --- |
| 触发环境 | Qoder 国内站（`qoder.com.cn`）；登录方法 = 「网页端登录」（`ProviderLogin.id === 'web'`） |
| 复现步骤 | 1. 看板 Qoder 卡片点「网页端登录」；2. 服务端拉起/复用调试 Chrome 并打开 `https://qoder.com.cn/account/usage`；3. 在该窗口完成登录 |
| 实际结果 | 状态一直是 `waiting`（文案「正在等待你在调试 Chrome 窗口里登录（已等待 N 秒）…」），直至 `expired`；`.env` 无 `QODER_COOKIE`，`accounts.json` 无 Cookie 形态条目 |
| 期望结果 | 登录完成后数秒内转 `ready`，落库 Cookie，卡片出现逐包批次与昨日/今日用量 |
| 影响范围 | 仅影响「网页端登录」；「扫码登录」（设备流）不受影响 |

### 1.1 现场证据

| # | 证据 | 来源 |
| --- | --- | --- |
| R1 | 服务端日志停在「用量页已打开，等待登录」，**没有任何「已抓到 Cookie」记录**，也没有 failed/expired 记录 | `.run/server.log`（`[login:qoder] Qoder 国内站用量页已打开，等待登录`） |
| R2 | 调试 Chrome 里 Qoder 用量页**确实已登录**（页面标题为账号昵称，非「登录」页） | `curl http://127.0.0.1:9222/json/list`：`type=page`、`url=https://qoder.com.cn/account/usage`、`title=研发编程助手` |
| R3 | 最近的登录备份目录里**没有 Cookie 形态条目**（只有移除操作的备份与历史令牌条目） | `.run/qoder-login-2026-10-18T08-38-34-044Z/accounts.json`、`.run/qoder-login-2026-10-18T08-41-38-964Z/accounts.json` |

> 说明：R2 证明「用户侧登录动作已完成」，因此问题不在用户操作，而在服务端采集链路。

---

## 2. 根因分析

**结论：`src/lib/cdp.ts` 的 Cookie 采集两条路径在 Chrome 155 上**同时失效**——主路径读的字段恒为空，兜底路径依赖的字段在 CDP 协议里根本不存在。因此 `cookieFor()` 恒返回 `null`，`poll()` 恒返回 `waiting`，表现为「一直等待」。另有一个独立的次生缺陷：超时后每轮轮询都刷新页面，持续打断用户正在进行的登录。**

### 2.1 主路径失效：`requestWillBeSent` 里没有 Cookie（E1–E2）

`cdp.ts:334-346` 的 `remember()` 从 `params.request.headers` 取 Cookie；`cdp.ts:349-351` 把它挂在 `Network.requestWillBeSent` 上。实测该字段在 Chrome 155 上**从不携带 Cookie**：

| # | 实测 | 数字 |
| --- | --- | --- |
| E1 | `Network.requestWillBeSent` 中带 Cookie 的请求数（XHR/Fetch 共 14 条） | **0** |
| E2 | 按 ResourceType 全量统计（登录态上下文，一次导航） | Script 28/0、Other 14/0、XHR 12/0、Ping 8/0、Stylesheet 4/0、Image 4/0、Fetch 2/0、Document 1/0、Font 1/0（分子=总数，分母=带 Cookie 数） |

即：`remember()` 里 `cookieOfHeaders(record.headers)` 恒为 `''`，`cdp.ts:342` 直接 `return`，`byHost` 永远为空。

### 2.2 兜底路径失效：`ExtraInfo` 事件没有 `host` / `url` 字段（E3）

`cdp.ts:352-362` 的兜底读 `Network.requestWillBeSentExtraInfo`，并在 `:360` 用 `record.host` 决定归置到哪个域：

```ts
const record = params as Record<string, unknown>;
const host = typeof record.host === 'string' ? record.host : '';   // ← 恒为 ''
if (host) fallbackByHost.set(host, cookie);                        // ← 永不执行
```

实测 CDP 协议（`GET /json/protocol`，本机 Chrome 155 自述）里该事件的参数为：

| 参数 | 说明 |
| --- | --- |
| `requestId` | 请求标识（**与 `requestWillBeSent` 同名同值，是关联的唯一钥匙**） |
| `associatedCookies` | 该请求关联的 Cookie 数组（含 `name` / `domain` / `blocked`） |
| `headers` | 实际发出的请求头（**Cookie 就在这里**） |
| `connectTiming` / `clientSecurityState` / `siteHasCookieInOtherPartition` | 连接与安全元信息 |
| ~~`host`~~ / ~~`url`~~ | **不存在** |

即 `host` 恒为空串，Cookie 永远进不了 `fallbackByHost`。**两条路径同时失效 → `cookieFor()`（`cdp.ts:389-397`）恒返回 `null` → `qoder.ts:1177-1189` 恒返回 `waiting`。**

### 2.3 关键修复前提已实测成立（E4–E5）

| # | 前提 | 实测结果 |
| --- | --- | --- |
| E4 | `requestWillBeSent` 的 `request.url` 能解析出 host，且与 `ExtraInfo` 可按 `requestId` 关联 | 带 Cookie 的 ExtraInfo **31/31** 全部关联到 host；`qoder.com.cn` 上取到 Cookie 头（长度 1136，含登录态 `qoder_session_cookie`） |
| E5 | flatten 模式下事件携带 `sessionId`，现有 `sid === sessionId` 过滤可保留 | `requestWillBeSent` 77/77 带 sessionId，`ExtraInfo` 42/42 带 sessionId；按子会话过滤后仍采到 **34** 条带 Cookie 的 ExtraInfo |
| E5b | 两事件到达顺序 | 不固定：同一 `requestId` 上 ExtraInfo 先到 17 次、`requestWillBeSent` 先到 23 次（**必须双向缓冲，不能假设顺序**） |

### 2.4 次生缺陷：reload 无节流，持续打断登录（E6）

`qoder.ts:1181-1183`：

```ts
if (Date.now() - startedAt > QODER_WEB_RELOAD_AFTER_MS) {   // 20_000
  await collector.reload();
}
```

`QODER_WEB_RELOAD_AFTER_MS = 20_000`（`qoder.ts:95`）、轮询间隔 `QODER_WEB_POLL_INTERVAL_MS = 2_000`（`qoder.ts:93`）。等待超过 20 秒后，**每 2 秒刷新一次页面**，会把用户正在填写的登录页反复重置，使登录几乎无法完成。

### 2.5 修复后必须补的判据：不能把「访客 Cookie」当登录成功（E7）

采集修好后，**未登录时同样能抓到 Cookie**（页面会带访客 Cookie 发请求）。若立刻返回 `ready`，会因 `landLoginAccount` 的 probe 验证失败而直接判 `failed`（比「一直等待」更糟）。实测区分信号：

| # | 上下文 | `/api/v1/me` 状态 | 出现的 Cookie 名 | 登录态独有 |
| --- | --- | --- | --- | --- |
| E7a | 匿名上下文（未登录等价，25 秒窗口） | **401** | acw_tc, arms_uid, atpsida, cna, isg, qoder_locale, qoder_visitor_id, sca, tfstk | — |
| E7b | 登录态上下文 | **200** | 上述 + `qoder_session_cookie` | **`qoder_session_cookie`** |

> **两处早先假设已被推翻（独立验证复核）**：
> 1. `arms_uid` / `isg` 曾被当作登录态信号 —— 实测在匿名上下文里**也会出现**（`arms_uid` 最快 3 秒、`isg` 25 秒窗口内未复现但同属阿里风控家族），**不能**用作判据；
> 2. `yunpk` 曾被我列为登录态独有 —— 实测它只出现在第三方域 `gm.mmstat.com` / `log.mmstat.com`（associated domain = `.mmstat.com`），**永远不会**进入 `qoder.com.cn` 的 Cookie 头，而 `cookieFor(['qoder.com.cn'])` 只返回 qoder 域的头 → 该判据**完全无效**，已从设计中移除。

因此判据取两条（互为冗余）：

1. **登录态 Cookie 名**（主判据，国内站实测）：抓到的 Cookie 头里含 `qoder_session_cookie`；
2. **功能性证据**（站点无关的兜底）：页面**自己**请求身份接口 `/api/v1/me` 返回 **2xx**（E7a/E7b 的 401 vs 200 区分度）。

> 第 2 条对国际站（`qoder.com`）尤其重要：其登录态 Cookie 名**未实测**，故该站点 `loginCookieNames` 留空、只走功能性判据。
>
> 注意判据必须**锁定 `/api/v1/me`**：同一匿名上下文里 `/api/v1/operation-slots` 也会返回 200（独立验证实测），笼统地看「有 2xx 就算登录」会误判。

---

## 3. 修复方案

**结论：改动集中在两处、均为小改。① `src/lib/cdp.ts`：把「读 `request.headers`」改为「用 `requestId` 关联 `requestWillBeSent`（提供 host/type）与 `requestWillBeSentExtraInfo`（提供真实 Cookie 头）」，并保留 `sid` 过滤与「只取浏览器真实发出的头」原则；② `src/providers/qoder.ts`：`poll()` 增加登录态判据（Cookie 名 + 功能性证据），并把 `reload()` 改为节流 + 限次，避免打断登录。**

### 3.1 改动点清单

| # | 文件 | 位置 | 改动 | 为什么 |
| --- | --- | --- | --- | --- |
| C1 | `src/lib/cdp.ts` | `openCookieCollector()` 采集区 | 新增 `requestMeta: Map<requestId, {host, path, type}>` 与 `cookieByRequest: Map<requestId, string>`；`requestWillBeSent` 只记元信息、`ExtraInfo` 只记 Cookie 头，再用 `requestId` **双向**归置（R 先到或 E 先到都能命中） | 主路径字段恒空、兜底字段不存在（§2.1/§2.2）；`requestId` 是唯一可行钥匙（E4/E5b） |
| C2 | `src/lib/cdp.ts` | 同上 | `ExtraInfo` 的 `associatedCookies[].domain` 作为**次要**归属信号（仅在拿不到 `requestId` 元信息时进兜底池），并跳过 `blocked === true` 的条目 | 极端情况下 `requestWillBeSent` 缺失（如请求早于 `Network.enable`）仍有机会命中；被拦掉的 Cookie 不代表真实发送 |
| C3 | `src/lib/cdp.ts` | `CookieCollector` 契约 | 新增 `cookieNamesFor(sites)`、`sawSuccessfulRequest(path)`、`idleMs()` | 供适配器做登录态判据与刷新决策（§2.5/§2.4）；只回传「名字 / 布尔 / 毫秒」，不回传 Cookie 值 |
| C4 | `src/lib/cdp.ts` | 新增 `Network.responseReceived` 订阅 | 记录页面**自己**请求成功（2xx）的 pathname | 实现 `sawSuccessfulRequest()`，给站点无关的功能性判据 |
| C5 | `src/providers/qoder.ts` | `qoderWebLogin.poll()` | 加登录态判据：站点声明的登录态 Cookie 名出现 **或** 页面自身 `/api/v1/me` 2xx，才返回 `ready`；否则 `waiting` 并给出区分文案 | §2.5：避免把访客 Cookie 当登录成功而误判 `failed` |
| C6 | `src/providers/qoder.ts` | `poll()` 的刷新分支 | 新增 `QODER_WEB_RELOAD_IDLE_MS = 20_000`（页面**空闲**阈值）、`QODER_WEB_RELOAD_MIN_INTERVAL_MS = 15_000`、`QODER_WEB_RELOAD_MAX = 2`；只在页面空闲且限次时刷新 | §2.4：原实现 20 秒后**每 2 秒**刷新，5 分钟 TTL 内约 140 次，反复重置用户正在填写的登录页 |
| C7 | `src/providers/qoder.ts` | `QoderSiteMeta` + 站点表 | 新增 `loginCookieNames`：国内站 = `['qoder_session_cookie']`；国际站 = `[]`（未确认 → 只走功能性判据） | 判据按站点可配，避免用一个未验证的名字把国际站永久卡在 `waiting` |
| C8 | `src/providers/qoder.ts` | 常量 | 网页端登录 TTL 由 5 分钟放宽到 `QODER_WEB_LOGIN_TTL_MS = 10 分钟` | 人工输密码/扫码期间不该被判超时；仅影响 web 方法 |
| C9 | `src/lib/cdp.ts` | `hostMatches()` | 去掉「目标站是 host 的父域」方向（原第三个子句） | 原规则会让 `qoder.com.cn` 的请求把 `com.cn` 上的 Cookie 也算命中；这里取的是浏览器真实发出的头，归属天然精确，无需放宽 |
| C10 | `src/lib/cdp.ts` | 采集 Map | 新增 `REQUEST_META_LIMIT = 4_000` 与 `trimMaps()`，超限按插入顺序淘汰最旧条目 | 登录会话可达 10 分钟、页面每秒发请求（实测约 150 请求/16 秒），不设上限会无界增长 |

### 3.2 修复后的采集算法（`src/lib/cdp.ts`）

```
① 建页 + attach(flatten) + Network.enable + Page.enable + Page.navigate   （原样保留）
② Network.requestWillBeSent      → requestMeta[requestId] = {host, path, type}
                                   （host 由 request.url 解析；pathname 供 2xx 判据）
③ Network.requestWillBeSentExtraInfo → cookieByRequest[requestId] = headers.Cookie
④ 归置（两个事件谁先到都能命中）：
     place(requestId):
       meta = requestMeta[requestId];  cookie = cookieByRequest[requestId]
       if (!meta || !cookie || !meta.host) return   // 等对方到达时再补一次
       meta.type ∈ {XHR, Fetch} → byHost[meta.host] = cookie          // 首选
       否则                      → fallbackByHost[meta.host] ??= cookie  // 兜底
   - ② 到达时若 cookieByRequest 已有该 id → 立刻 place
   - ③ 到达时若 requestMeta 已有该 id → 立刻 place
   - ③ 到达时若 requestMeta 还没有 → 用 associatedCookies[].domain 进 fallbackByHost（跳过 blocked）
   - 每次写入后 trimMaps()：两个 Map 各留最近 4000 条
⑤ cookieFor(sites)  → 先查 byHost，再查 fallbackByHost（hostMatches：同域或子域）
⑥ Network.responseReceived → 2xx 的 pathname 记入 successPaths
⑦ cookieNamesFor(sites) / sawSuccessfulRequest(path) / idleMs() → 供适配器判据与刷新决策
```

要点（与既有经验一致，未改动）：

- **仍只取「浏览器真实发出的 Cookie 头」**，不拼 cookie jar（`cdp.ts` 头部注释的踩坑记录：jar 拼装会超 nginx header 上限而 401）；
- `sid === sessionId` 过滤保留（E5 实测事件带 sessionId，过滤后仍能采到）；
- `sawSuccessfulRequest()` 用 **pathname 精确匹配**（不用 `endsWith`）：这是「登录成功」的门禁，宽松匹配会把无关后缀也算命中；
- 全异步、零第三方依赖（Node 内置 `fetch` + `WebSocket`）。

### 3.3 修复后的登录判据（`src/providers/qoder.ts`）

```
poll(secret, ctx):
  raw   = collector.cookieFor(hosts)
  names = collector.cookieNamesFor(hosts)
  loggedIn = raw 且 (
       names ∩ site.loginCookieNames ≠ ∅                 // 国内站：qoder_session_cookie
       || collector.sawSuccessfulRequest('/api/v1/me')   // 功能性证据：页面自身身份接口 2xx
     )
  if (!loggedIn):
      有 Cookie 但未登录 → waiting「已抓到…页面 Cookie，但尚未检测到登录态…」
      无 Cookie         → waiting「正在等待你在调试 Chrome 窗口里登录…」
      刷新策略：仅当页面空闲 ≥20 秒、距上次刷新 ≥15 秒、累计 <2 次时才 reload
  else:
      accountId = sha256(cookie) 前 16 位（原样保留）
      → ready，credentials={cookie}，options={site}
```

> **判据为什么不能用风控/统计 Cookie**：`arms_uid`、`isg`、`acw_tc`、`cna`、`tfstk`、`qoder_visitor_id` 在**未登录**的匿名上下文里同样会出现（独立验证实测 `arms_uid` 3 秒内即出现）。`yunpk` 只出现在第三方域 `gm.mmstat.com`，**永远不会**进入 qoder 域的 Cookie 头，作为判据无效（此点由独立验证推翻，已从设计中移除）。

---

## 4. 回归验证

> 按仓库规范（AGENTS.md §6），代码变更只做**静态检查**；下表 §4.1 是本次为确认根因与修复成立所做的**本地只读验证**（不启动服务、不跑 `npm run probe`、不向上游发我方业务请求），§4.2 的人工回归**未执行**。

### 4.1 本地端到端验证（已完成，只读）

用**修复后的真实模块** `src/lib/cdp.ts` 直接对调试 Chrome 跑采集，只打印长度/名字/布尔值：

| # | 场景 | 结果 |
| --- | --- | --- |
| V1 | 登录态 profile（用户已登录的那个 Chrome） | 2 秒内 `cookieFor` 非空（长度 1136），Cookie 名含 `qoder_session_cookie`；`/api/v1/me` 2xx = true；**判据 = true → 会转 `ready` 并落库**（修复前此处恒为 `null`） |
| V2 | 全新隔离的匿名上下文（未登录等价） | `cookieFor` 非空（长度 594，访客 Cookie），但**不含** `qoder_session_cookie`、`/api/v1/me` 2xx = false → **判据 = false → 继续 `waiting`**，不会拿访客 Cookie 去 probe 而误判 `failed` |
| V3 | 独立验证者（另一 agent）复核 | H1–H4 全部成立（H4 为代码推断，其余为实测）；`requestWillBeSent` 带 Cookie 数 **0/149**（复刻应用路径）、**0/76**（干净路径）；`ExtraInfo` 参数名中 **无 host、无 url**；两事件顺序实测 E 先到 14 次 / R 先到 25 次 |

> 验证脚本位于 `/tmp`（`verify-fix.mts`、`verify-anon.mts`），未写入仓库。

### 4.2 人工回归（未执行）

| # | 步骤 | 预期结果 |
| --- | --- | --- |
| 1 | `npm run typecheck` | 通过（本轮已执行，exit 0） |
| 2 | 看板 Qoder 卡片点「网页端登录」（人工） | 调试 Chrome 打开用量页；**未登录时**状态为「已抓到…页面 Cookie，但尚未检测到登录态…」或「等待登录」，且页面**不再每 2 秒被刷新** |
| 3 | 在调试 Chrome 窗口完成登录（人工） | 数秒内转 `ready`；`.env` 出现/更新 `QODER_COOKIE`（cn）或 `QODER_GLOBAL_COOKIE`（global）；`accounts.json` 新增 Cookie 形态条目且 `options.site` 正确；`.run/` 有备份 |
| 4 | 卡片刷新（人工） | 出现逐包明细（每包总额/剩余/到期）；「昨日/今日用量与增量」不再为 `—` |
| 5 | 再点一次「网页端登录」（人工） | 同账号应**更新**原条目（`loginAccountId` 指纹去重），不新建卡片 |
| 6 | 扫码登录 / CodeBuddy / Trae（人工） | 行为与改动前一致（回归）；`src/lib/cdp.ts` 仅被 Qoder 网页端登录使用（只读检索：`src/providers/qoder.ts:2` 是唯一引用方） |
| 7 | 国际站网页登录（人工，可选） | **待确认**：`qoder.com` 登录态 Cookie 名未实测，故该站点 `loginCookieNames` 留空，仅靠功能性判据（`/api/v1/me` 2xx）命中 |

---

## 5. 风险与回滚

| # | 风险 | 影响 | 缓解 |
| --- | --- | --- | --- |
| R1 | `associatedCookies` 的 domain 归置可能把 Cookie 落到「相邻域」 | 采集到非目标域的头 | 仅作**次要**信号、仅在 `requestId` 元信息缺失时使用，且跳过 `blocked` 条目；主路径是 `requestId` 关联（实测 31/31 命中） |
| R2 | 登录态 Cookie 名（`qoder_session_cookie`）可能随上游改版变化 | 主判据失效 | 功能性判据（`/api/v1/me` 2xx）兜底；两条任一成立即 `ready` |
| R3 | 国际站 Cookie 名未验证 | 国际站可能仍等待 | 该站点 `loginCookieNames` 留空 → 只走功能性判据（§4.2 步骤 7） |
| R4 | 刷新被大幅减少后，可能错过「登录后页面不再发请求」的场景 | 延迟命中 | 登录后页面自身会发 XHR（V1 实测 `/api/v1/me` 200）；仍保留空闲触发的兜底刷新（上限 2 次） |
| R5 | 凭证泄露 | — | 全链路不出本机：Cookie 不经 HTTP 返回、不落日志；新 API 只回传 Cookie **名**与布尔值，不返回值；`.env`(0600) + `accounts.json` 只存 `${VAR}` 引用 |
| R6 | 网页端 TTL 放宽到 10 分钟后，未完成登录的会话占用调试标签页更久 | 资源占用 | 标签页在会话终态由 `cleanup()` 关闭；TTL 只影响等待上限 |
| 回滚 | — | — | 回滚 `src/lib/cdp.ts` 与 `src/providers/qoder.ts` 本次改动即可；无配置迁移、无数据契约变更 |

---

## 变更记录

| 时间 | 变更内容 | 触发原因 | 影响章节 |
| --- | --- | --- | --- |
| 2026-10-18 | 初版：定位「一直等待」根因为 `cdp.ts` 采集双路径失效（主路径读的 `request.headers` 恒空；兜底依赖的 `host` 字段在 CDP 协议中不存在），修复改用 `requestId` 关联；并补登录态判据与 reload 节流 | 用户反馈「网页端登录后，在调试 Chrome 里登录完成还是一直等待」 | 全篇 |
| 2026-10-18 | 按独立验证报告修正：① 判据去掉无效的 `yunpk`（只存在于第三方域 `gm.mmstat.com`，永不进入 qoder 域头）、并明确 `arms_uid`/`isg` 在匿名态也会出现；② `sawSuccessfulRequest()` 改为 pathname **精确匹配**（原 `endsWith` 过宽）；③ 刷新策略由「20 秒后每 2 秒刷新」改为「页面空闲 ≥20 秒 + 最小间隔 15 秒 + 上限 2 次」；④ 采集 Map 加 4000 条上限；⑤ `associatedCookies` 兜底跳过 `blocked` 条目；⑥ `hostMatches` 去掉「父域」方向；⑦ 网页端 TTL 5→10 分钟；⑧ 判据按站点可配（国际站留空） | 独立验证者（另一 agent）复核 H1–H4 并指出设计漏洞 | §2.5、§3.1、§3.2、§3.3、§4.1、§5 |
| 2026-10-18 | 补 §4.1 本地端到端验证结论：用修复后的真实模块实测——登录态 profile 2 秒内命中判据（`cookieFor` len=1136、含 `qoder_session_cookie`、`/api/v1/me` 2xx），匿名上下文不误判（len=594、无登录态名、无 2xx）；`npm run typecheck` exit 0 | 修复落地后需确认「修好了」而非仅「改对了」 | §4.1 |
