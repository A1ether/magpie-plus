# 账号管理 · 网页端登录抓取 Cookie 开发设计方案

> 文档类型：开发设计方案
> 关联模块：账号管理（登录方式扩展 / CDP 抓取 / 凭证落库）、平台适配器（Qoder）
> 创建时间：2026-10-18
> 最后更新：2026-10-18
> 当前状态：实施中（§8 步骤 1–8 代码已全部落地，`npm run typecheck` 通过；动态验证未执行）
> 关联改动文件：`src/lib/cdp.ts`（新增）、`src/providers/base.ts`、`src/providers/index.ts`、`src/providers/qoder.ts`、`src/login-session.ts`、`src/account-store.ts`、`src/server.ts`、`public/index.html`、`public/app.js`、`public/styles.css`、`README.md`、`docs/凭证配置指南.md`、`docs/modules/项目整体-代码Wiki.md`、`docs/fixes/Qoder-令牌模式批次明细缺失-修复方案.md`

---

## 1. 背景与目标

**结论：用户在 Qoder 卡片上被「逐包批次明细需 Cookie 模式」拦住——知道要切 Cookie 模式，但没有任何入口能做到；本期让「网页端登录」成为与扫码并列的一等登录方式，登录完成即自动抓到 Cookie 并落库，卡片随即出现逐包明细与昨日/今日用量增量。**

### 1.1 要解决的问题

| # | 问题 | 现状证据 |
| --- | --- | --- |
| P1 | 令牌模式结构上拿不到逐包明细：Bearer 打网页用量接口 401，`quota_detail[]` 只存在于网页会话面 | `docs/fixes/Qoder-令牌模式批次明细缺失-修复方案.md` §2.1 证据 E1–E8 |
| P2 | 卡片只提示「需 Cookie 模式」，但**看板上没有任何切到 Cookie 模式的入口** | `src/providers/qoder.ts:1217`（`login` 仅声明设备流扫码）；`manageFields` 的 Cookie 字段是「手工粘贴」而非登录 |
| P3 | 网页端登录后本应自动带来的「昨日/今日用量与增量」也一直显示 `—` | `src/providers/qoder.ts:1186-1190`：`fetchUsageLedger()` 无 Cookie 时直接 `return null` → 落回本地采样 |

### 1.2 目标

1. Qoder 卡片的登录区提供**两个并列入口**：原有「扫码登录」（设备令牌）与新增「网页端登录」（抓 Cookie）。
2. 点「网页端登录」后：服务端自动确保调试 Chrome 在跑并打开 Qoder 用量页 → 用户在该窗口完成登录 → 服务端通过 CDP 侦听真实请求头抓到 Cookie → 复用既有登录会话层做 probe 验证与落库 → 前端状态机报 `ready`。
3. 落库后：卡片立刻出现逐包批次（每个资源包总额/剩余/到期），且「昨日/今日用量与增量」由网页明细接口取到（不再显示 `—`）。
4. 机制**平台无关**：第二个登录方法由适配器声明驱动，前端不认平台名；本期只在 Qoder 落地，后续其他平台可复用。

### 1.3 Non-Goals（明确不做）

| 不做 | 原因 |
| --- | --- |
| 服务端代替用户完成登录（填账号密码 / 过验证码 / 处理扫码） | 涉及真实凭证输入与风控，超出看板职责；且上游 WAF 会校验行为特征 |
| 打包 / 内嵌 Chromium 或引入 Playwright 等运行时依赖 | 违背仓库「运行时零第三方依赖」约束（`package.json` 只有 `devDependencies`） |
| 改动 `src/cli/capture.ts` 的既有 CLI 行为 | 该脚本是「手工抓取」的稳定入口；本期只新增独立的 CDP 模块，不回改它 |
| 用 Cookie 模式替换令牌模式 / 自动迁移历史账号 | 两种凭证各自可用；本期只新增入口，是否切换由用户决定 |
| 国际站（`qoder.com`）网页登录的端到端保证 | 与 `docs/plans/平台适配器-接入Qoder国际站-开发设计方案.md` 一致，站点差异**待人工验证**；代码按同一套逻辑兼容 |

---

## 2. 现状调研

**结论：登录会话层（`login-session.ts`）本身已是「平台无关 + 声明驱动」的，可直接承载第二个登录方法；真正的缺口有三个——① 适配器只能声明**一个** `login`；② 前端 `state.login` 只按平台记会话；③ 没有任何「从服务端驱动调试 Chrome 抓 Cookie」的能力（`capture.ts` 不可复用）。**

### 2.1 可直接复用的部分

| 能力 | 位置 | 复用方式 |
| --- | --- | --- |
| 登录会话状态机（`waiting`/`landing`/`ready`/`failed`/`expired`/`cancelled`、后台轮询、TTL、终态缓存、`unref`） | `src/login-session.ts:150-292` | 原样复用；新增方法只需实现 `start` + `poll` |
| 落库（probe 验证 → 去重 → `.env` 槽位 → 备份 → 写盘） | `src/account-store.ts:722-880`（`landLoginAccount`） | 原样复用；抓到的 Cookie 必须能通过 `probe` |
| 特权接口闸门 | `src/server.ts:99-121`（`privilegedGuard`） | 新增路由沿用（回环 Host + 回环 Origin + `X-Power-Meter-Intent`） |
| 前端登录面板（二维码 / 链接 / 状态 / 取消 / 站点选择） | `public/app.js:909-1114`、`public/index.html:157-184` | 扩展为多按钮 + 按方法记会话 |
| 站点选择与 `envNames` | `src/providers/qoder.ts:1134-1177` | 网页登录同样带 `options.site`，落到同一套 `.env` 变量名 |
| 网页明细接口（逐笔用量 + 额度流水） | `src/providers/qoder.ts:655-720`（`fetchLedgerAtSite`） | **无需改动**：只要账号有 Cookie，四个日级字段自动由上游值填充 |
| 调试 Chrome 启动脚本 | `tools/chrome-debug.sh` | 由服务端按需拉起（复用其 `--remote-debugging-port` + 独立 profile 约定） |

### 2.2 三个缺口的证据

| # | 缺口 | 位置 | 说明 |
| --- | --- | --- | --- |
| G1 | 适配器只能声明一个登录方法 | `src/providers/base.ts:186`（`login?: ProviderLogin`）、`src/providers/index.ts:111-113`（`describeRegistry` 只导出 `{label,hint,qr}`） | 第二个入口无处声明，前端也无从知晓 |
| G2 | 前端会话只按平台记 | `public/app.js:90`、`public/app.js:922`（`state.login?.providerId === providerId`）、`public/app.js:999-1002`（任意按钮都会取消既有会话） | 两个按钮共用一条会话 → 互踩 |
| G3 | 无服务端 CDP 能力 | `src/cli/capture.ts` 全篇 | 该文件**不可作为模块引入**：文件末尾 `capture.ts:831` 是裸 `main();`，`capture.ts:668` 有 `process.exit(1)`，`pw()` 用阻塞 `execFileSync`（`capture.ts:94`），且 `SESSION`/`CDP_ENDPOINT`/`ARGS`/`DRY_RUN` 等是**导入期**从宿主 `process.argv` 解析的模块级常量（`capture.ts:30-35`）；依赖外部 `curl`（`capture.ts:658`）。仅 `cookieAppliesTo()`（`:154`）与 `extractRunCodeResult()`（`:457`）两个纯函数可复用 |

### 2.3 关键技术可行性（本期新增调研）

| # | 结论 | 证据 |
| --- | --- | --- |
| F1 | **可用 Node 内置 `WebSocket` 直连 CDP，零运行时依赖** | Node v26.8.1 下 `'WebSocket' in globalThis === true`；`@types/node` 已在 `http.d.ts:2085` 导出 `WebSocket`；写入 `src/__wsprobe.ts` 后 `npx tsc --noEmit` **exit 0**（探针文件已删除） |
| F2 | 调试 Chrome 提供标准 CDP 端点 | `tools/chrome-debug.sh` 以 `--remote-debugging-port=$PORT` + 独立 `--user-data-dir` 启动（Chrome 136+ 禁止默认 profile 开调试端口，脚本头部注释已说明）；`capture.ts:658` 用 `GET /json/version` 做前置探活 |
| F3 | **必须抓「浏览器真实发出的 Cookie 头」，不能只用 cookie jar 拼装** | `src/cli/capture.ts:445-452` 的踩坑记录：jar 里 CodeBuddy 的 Keycloak Cookie 拼起来约 9KB，超 nginx 的 client header 上限直接 401；浏览器只发当前域+路径真正需要的那部分 |
| F4 | 日级四字段无需额外开发 | `src/service.ts:415-438`（`withDailyUsage`）：`ledger.covers.use === true` → 用量取上游值；`covers.grant === true` → 增量取上游值。而 `fetchLedgerAtSite`（`qoder.ts:655-720`）同时写入 `kind:'use'`（`credits`）与额度增加流水（`change_amount`） |

---

## 3. 方案设计

**结论：新增一个平台无关的 CDP 抓取模块（`src/lib/cdp.ts`），把它包成 Qoder 的**第二个** `ProviderLogin`；适配器改为声明 `loginMethods[]`，会话层与前端各加一个「方法」维度；抓到的 Cookie 走既有 `landLoginAccount()` 落库，因此 probe 验证、去重、槽位分配、备份全部免费获得。**

### 3.1 总体思路

```
[看板 Qoder 卡片]
   ├─ [扫码登录]        → 设备流 PKCE（既有）           → accessToken/refreshToken
   └─ [网页端登录]      → CDP 抓 Cookie（本期新增）      → cookie
                                    │
        ①确保调试 Chrome 在跑（未跑则拉起 tools/chrome-debug.sh 同款命令）
                                    │
        ②CDP 打开 {origin}/account/usage，侦听 xhr/fetch 请求头
                                    │
        ③拿到目标域 Cookie → 组 {cookie: "..."} + options{site}
                                    │
                          login-session 会话层（既有，零改动）
                                    │
                          landLoginAccount（既有，零改动）
                          ├─ probe 真实验证（必须 ok 才落库）
                          ├─ 按 options.loginAccountId 去重
                          ├─ .env 槽位（cn → QODER_COOKIE / global → QODER_GLOBAL_COOKIE）
                          └─ 备份 .run/ → 写 .env(0600) + accounts.json
                                    │
                          [卡片刷新] → Cookie 模式生效
                          ├─ 逐包明细：total_quota.quota_detail[]
                          └─ 昨日/今日用量与增量：histories 接口
```

### 3.2 数据流（一次「网页端登录」的完整时序）

| 步 | 触发 | 动作 | 产出 |
| --- | --- | --- | --- |
| 1 | 用户点「网页端登录」 | `POST /api/login/start`，body 带 `provider` + `method: 'web'` + `options.site` | 会话 `waiting` |
| 2 | 服务端 `start()` | `ensureDebugChrome()`：`GET /json/version` 探活；失败则按 `tools/chrome-debug.sh` 同款参数 spawn 并等待就绪 | 调试 Chrome 可用 |
| 3 | 服务端 `start()` | 新建/复用标签页导航到 `{origin}/account/usage`，`Network.enable`，返回 `secret = {site, targetId, deadline}`；`authUrl` 给该页面地址（前端可点开） | `authUrl` + `ttlMs` |
| 4 | 前端轮询 | 每 1.5s `GET /api/login/poll?id=` | 状态/提示 |
| 5 | 服务端 `poll()` | 读已侦听到的请求头；未命中则触发一次 `Page.reload` 重新采集 | `waiting` 或 Cookie |
| 6 | 拿到 Cookie | 组 `credentials={cookie}`、`options={site}`，返回 `ready` | 交给会话层落库 |
| 7 | 会话层 | `landLoginAccount()`：probe 验证 → 去重 → 写 `.env`/`accounts.json`（备份 `.run/`） | `ready` + 卡片名 |
| 8 | 前端 | 终态后清缓存、`load(true)` 重拉 | 卡片出现逐包明细 + 日级四字段 |

### 3.3 职责划分

| 层 | 文件 | 职责 |
| --- | --- | --- |
| CDP 客户端 | `src/lib/cdp.ts`（新增） | 纯协议层：探活、建页、attach、`Network.enable`、收集请求头、`Page.reload`。**不认识任何平台** |
| 适配器 | `src/providers/qoder.ts` | 声明第二个登录方法；决定「去哪个 URL 抓、要哪些域的 Cookie、落哪些 options」 |
| 契约 | `src/providers/base.ts` | `ProviderLogin` 增 `id`/`kind`；适配器增 `loginMethods`；`LoginStartInput` 增 `method` |
| 会话层 | `src/login-session.ts` | 按 `method` 选方法、按 `provider+method` 记会话；其余状态机不动 |
| 落库 | `src/account-store.ts` | 零改动（Cookie 只是另一种 `credentials`） |
| 路由 | `src/server.ts` | `/api/login/start` 透传 `method`；`/api/providers` 导出 `loginMethods` |
| 前端 | `public/app.js`、`index.html`、`styles.css` | 按声明渲染 N 个按钮；`state.login` 增方法维度 |

### 3.4 契约扩展设计（关键决策）

**决策：`ProviderAdapter.login` 保留（向后兼容），新增 `loginMethods?: ProviderLogin[]`；未声明 `loginMethods` 时，会话层把 `login` 当作唯一的默认方法。**

理由：

- 三个平台（CodeBuddy/Qoder/Trae）已有 `login`，直接改成数组要动四处调用点，收益为零；
- 只有 Qoder 需要第二个方法，`loginMethods` 可与 `login` **并存**：`login` 仍是主方法（扫码），`loginMethods` 提供带 `id` 的完整清单；
- 会话层用 `resolveLoginMethod(adapter, method)` 统一取方法：`method` 为空 → `adapter.login`；否则在 `loginMethods` 里按 `id` 查。

`ProviderLogin` 新增字段：

| 字段 | 类型 | 必填 | 含义 |
| --- | --- | --- | --- |
| `id` | `string` | 否 | 方法标识（如 `'web'`）。用于会话选择与 `state.login` 的键 |
| `kind` | `'scan' \| 'link' \| 'web'` | 否 | 仅用于前端文案与图标（默认按 `qr` 推导） |
| `cleanup` | `(secret) => void \| Promise<void>` | 否 | **实施期新增**：会话终态（ready/failed/expired/cancelled）后由会话层 `settle()` 调用，释放登录期资源。「网页端登录」的 CDP 标签页与 WebSocket 挂在 secret 里，不释放会一直占用调试 Chrome；无此类资源的平台不必声明 |

**实施偏差（已按此落地）**：Qoder 的第二个方法不是适配器对象内的 `webLogin` 属性，而是模块级常量 `qoderWebLogin: ProviderLogin`（`src/providers/qoder.ts:1132`）——TS 对象字面量不允许 `ProviderAdapter` 未声明的属性，独立常量也让类型显式。`loginMethods` 在适配器对象外组装：`qoderAdapter.loginMethods = [qoderAdapter.login, qoderWebLogin]`（`src/providers/qoder.ts:1524`）。 |

### 3.5 CDP 抓取算法（`src/lib/cdp.ts`）

```
ensureDebugChrome(endpoint, chromePath?) -> { ok, started }
  ① GET {endpoint}/json/version   (Node fetch，替代 capture.ts 的外部 curl)
  ② 失败 → spawn(chromePath, ['--remote-debugging-port=<port>', '--user-data-dir=<profile>',
                              '--no-first-run', '--no-default-browser-check', ...])
     参数与 tools/chrome-debug.sh 一致；spawn 后轮询 /json/version 直到就绪或超时
  ③ 就绪 → 返回；同时返回本次是否由我们拉起（用于收口时提示用户可关掉）

captureCookieHeader(endpoint, { url, hosts, waitMs, onProgress }) -> string | null
  ① GET {endpoint}/json/list  → 找已有 page target；无则 Target.createTarget(url)
  ② WebSocket 连 {webSocketDebuggerUrl}，Target.attachToTarget({flatten:true})
  ③ Network.enable；订阅 Network.requestWillBeSent
  ④ Page.navigate(url)；若 waitMs 内未命中则 Page.reload 再等一轮
  ⑤ 命中判据：request 的 resourceType 为 XHR/Fetch 且 url 主机属于 hosts，
     且 headers.Cookie 非空 → 取该 Cookie 头
  ⑥ 全部未命中 → 返回 null（由适配器决定是 waiting 还是 failed）
```

要点（均来自 `capture.ts` 的既有经验，见 §2.3 F3）：

- **取请求头而非 cookie jar**：jar 拼装会因超大 Cookie 触发上游 header 上限而 401；
- **按主机过滤**：`cookieAppliesTo()` 的「父域 + 子域都要」原则沿用，但这里直接用浏览器实际发出的头，天然满足；
- **不新增依赖**：Node 内置 `fetch` + `WebSocket`；
- **不阻塞事件循环**：全异步，`spawn` 用 `detached` + `unref`，不 `execFileSync`。

### 3.6 Qoder 第二个登录方法的实现

| 项 | 值 |
| --- | --- |
| `id` | `'web'` |
| `label` | `网页端登录（抓 Cookie）` |
| `hint` | `自动打开调试 Chrome 的 Qoder 用量页；你在那个窗口登录后，本页面自动完成添加。` |
| `qr` | `false`（不产生二维码，靠本机浏览器会话） |
| `intervalMs` | `1_500` |
| `start()` | 确保 Chrome → 打开 `{origin}/account/usage` → `secret = { site, deadline }`；`authUrl` = 该用量页地址（供「在新窗口打开」） |
| `poll()` | 调 `captureCookieHeader()`；命中 → `ready`，`credentials={cookie}`，`options={site}`；未命中且未超时 → `waiting`；超时 → `failed` |
| 站点 | 复用 `options.site`（`cn`/`global`），`origin` 由既有 `QODER_SITES` 解析，`.env` 变量名由既有 `envNames` 决定 |

**账号身份（去重键）**：`accountId` 取「Cookie 抓取时所在页面的主机 + 账号昵称」，优先用网页域能拿到的昵称，拿不到则退化为站点 + Cookie 指纹（`sha256` 前 16 位）。**待确认**：网页面是否直接返回 uid；若后续实测有更权威字段，改 `resolveWebIdentity()` 一处即可。

---

## 4. 接口设计

### 4.1 `ProviderAdapter` 契约扩展

| 变更 | 位置 | 说明 |
| --- | --- | --- |
| `ProviderLogin.id?: string` | `src/providers/base.ts:113` | 方法标识 |
| `ProviderLogin.kind?: 'scan' \| 'link' \| 'web'` | 同上 | 前端文案/图标依据 |
| `ProviderAdapter.loginMethods?: ProviderLogin[]` | `src/providers/base.ts:186` 附近 | 完整方法清单；与 `login` 并存时 `login` 为默认 |
| `LoginStartInput.method?: string` | `src/providers/base.ts:72-82` | 选择用哪个方法 |
| `resolveLoginMethod(adapter, method)` | `src/providers/base.ts`（新增导出） | 统一取方法，避免会话层/路由各写一套 |

### 4.2 HTTP API

| 路径 | 方法 | 变更 | 入参 | 出参 |
| --- | --- | --- | --- | --- |
| `/api/login/start` | POST | **扩展** | 新增可选 `method: string` | 不变（`{ok, session}`）；`session` 增 `method` |
| `/api/login/poll` | GET | 不变 | — | `session` 增 `method` |
| `/api/login/cancel` | POST | 不变 | — | `session` 增 `method` |
| `/api/providers` | GET | **扩展** | — | provider 增 `loginMethods: [{id,label,hint,qr,kind}]`（保留 `login` 兼容） |

错误码沿用现状：未知平台 404、未声明登录 403、参数缺失 400、上游/抓取失败 502（由会话层 `settle('failed')` 表达）。

### 4.3 CLI

本期不改 CLI。`npm run capture` 仍是手工抓取入口，二者并存（文档中说明区别）。

---

## 5. 数据模型

### 5.1 `accounts.json`

无结构变更。网页端登录产出与扫码登录同形：

```jsonc
{
  "provider": "qoder",
  "displayName": "Qoder · <昵称>",
  "enabled": true,
  "credentials": { "cookie": "${QODER_COOKIE}" },   // 明文只在 .env
  "options": { "site": "cn", "loginAccountId": "<指纹>", "loginName": "<昵称>" }
}
```

### 5.2 `.env`

复用既有变量名，**不新增键**：

| 站点 | 变量 |
| --- | --- |
| `cn`（默认） | `QODER_COOKIE`（第 N 个账号 `QODER_COOKIE_N`） |
| `global` | `QODER_GLOBAL_COOKIE` |

### 5.3 内存会话

`LoginSession` 增 `method: string`；`state.login` 由 `{providerId, session, timer}` 扩展为带 `method` 的键（`providerId + '#' + method`）。会话过期/终态语义不变。

### 5.4 迁移

无。既有令牌账号保持原样；用户新增 Cookie 账号即多一张卡（见 §6 影响面与 §7 风险）。

---

## 6. 影响面评估

| 面 | 影响 | 说明 |
| --- | --- | --- |
| 既有登录方法 | 无行为变化 | `login` 仍是默认方法；不传 `method` 时路径与现在完全一致 |
| CodeBuddy / Trae | 无影响 | 未声明 `loginMethods`，前端只渲染一个按钮 |
| Qoder 令牌账号 | 不变 | 不自动迁移；卡片 note 仍提示「逐包明细需 Cookie 模式」 |
| 前端 | 登录区改为声明驱动多按钮；`state.login` 增方法维度 | 若不改，第二个按钮无处渲染 |
| 缓存 / 采样键 | 新账号 = 新 `provider#displayName` | 与既有「一账号一卡片」一致 |
| 「最快到期」排序 | Cookie 模式下**变准** | 逐包带真实 `expires_at`，不再依赖合成的「本期重置」事件 |
| 日级四字段 | Cookie 模式下由 `—` 变为上游值 | `withDailyUsage` 既有逻辑自动生效（§2.3 F4） |
| 进程 | 服务端会按需拉起一个 Chrome | 仅在用户点击「网页端登录」且 Chrome 未运行时；见 §7 风险 R1 |
| 凭证安全 | 新增一条「Cookie 自动抓取」链路 | 不落日志、不出本机；沿用 `privilegedGuard`（§7 R3） |

---

## 7. 风险与备选方案

| # | 风险 | 影响 | 缓解 |
| --- | --- | --- | --- |
| R1 | 服务端 spawn Chrome 属于「从 web 请求拉起本机进程」 | 误触/滥用 | 仅在特权闸门通过 + 用户显式点击后触发；已运行时只探活不重启；`detached` + `unref`；路径与参数固定为 `tools/chrome-debug.sh` 同款，不接受任意参数 |
| R2 | 抓取时机不稳（用户还没登录、页面没发起 xhr） | 一直 `waiting` 直到超时 | 轮询里主动 `Page.reload` 重新采集；TTL 内可重试；超时给出明确文案与「可改用手工 Cookie」的兜底提示 |
| R3 | 凭证泄露 | Cookie 是本机登录态 | 全链路不出本机：不经 HTTP 返回、不写日志；`.env`(0600) + `accounts.json` 只存 `${VAR}` 引用；文档与回复不出现明文 |
| R4 | 上游对调试 Chrome 的自动化特征风控 | 抓取失败 | 只读浏览器**真实发出**的请求头（不伪造 UA/头），不做注入式操作；失败退化为手工 Cookie 入口 |
| R5 | `accountId` 指纹不稳定（Cookie 轮换） | 重新登录可能新建卡片而非更新 | 指纹仅作去重键；卡片可手工改名/删除，影响可控。**已确认（2026-10-21）**：网页侧 `/api/v1/me` 返回稳定 `id`（`01a07fb0-…`）与昵称 `name`，昵称已接入命名（见 `docs/fixes/Qoder-Cookie模式卡片标题拿不到昵称-修复方案.md`）；是否把去重键从指纹换成 `id` 仍需一次配置迁移，**待确认** |
| R6 | 国际站差异未验证 | 国际站网页登录可能失败 | 与既有国际站方案同口径：代码兼容同一逻辑，实测差异只需改站点表一处；标 `待确认` |
| R7 | 前端两个按钮互踩 | 一个会话被另一个取消 | 会话按 `provider+method` 记；按钮各管各的；同方法重复点击仍按既有「旧会话作废」语义 |
| 备选 A | 只做「引导式半自动」（给步骤 + 复制命令） | — | **被否决**：仍需用户手动跑 `npm run capture`，正是用户抱怨的「不知道怎么切」 |
| 备选 B | 复用 `playwright-cli`（shell out） | — | **被否决**：需外部 CLI 与阻塞 `execFileSync`，服务端调用会阻塞事件循环并共享会话名 `pm` |
| 备选 C | 改造 `capture.ts` 成可复用模块 | — | **被否决**：需拆 `main()`、去 `process.exit`、把 `process.argv` 全局改成参数，改动面大于新增一个独立模块，且会扰动稳定的手工抓取脚本 |
| 回滚 | — | — | 回滚 `src/lib/cdp.ts`（删除）+ `base.ts`/`index.ts`/`qoder.ts`/`login-session.ts`/`server.ts`/`public/*` 的本期改动；无配置迁移、无数据契约变更 |

---

## 8. 实施步骤

| # | 层 | 任务 | 产出物 |
| --- | --- | --- | --- |
| 1 | `lib` | 新增 `src/lib/cdp.ts`：`ensureDebugChrome()`、`captureCookieHeader()`、CDP JSON-RPC 封装（内置 `fetch` + `WebSocket`，全异步） | 平台无关的抓取能力 |
| 2 | `providers` | `base.ts`：`ProviderLogin` 增 `id`/`kind`；`ProviderAdapter` 增 `loginMethods`；`LoginStartInput` 增 `method`；导出 `resolveLoginMethod()` | 契约扩展 |
| 3 | `providers` | `qoder.ts`：新增第二个 `ProviderLogin`（`id:'web'`，`start`/`poll` 走 `src/lib/cdp.ts`）；`loginMethods` 声明两个方法 | Qoder 可网页端登录 |
| 4 | `providers` | `index.ts`：`describeRegistry()` 增 `loginMethods`，保留 `login` | 前端可发现第二方法 |
| 5 | 编排 | `login-session.ts`：`startLogin` 按 `method` 选方法、按 `provider+method` 记 `ACTIVE`/`SESSIONS`；`view()` 带 `method` | 多方法会话隔离 |
| 6 | 路由 | `server.ts`：`/api/login/start` 透传 `method`；`/api/login/cancel` 按 id 定位（不变） | 接口打通 |
| 7 | 前端 | `index.html` 登录区改为按钮容器；`app.js` 按 `loginMethods` 渲染 N 个按钮、`state.login` 增方法维度、poll 守卫同步；`styles.css` 补次要按钮样式 | 双入口 UI |
| 8 | 文档 | 更新 `README.md`、`docs/凭证配置指南.md`、`docs/modules/项目整体-代码Wiki.md`；在 `docs/fixes/Qoder-令牌模式批次明细缺失-修复方案.md` 加交叉引用与状态说明 | 文档与代码一致 |

---

## 9. 验证方式

> 按仓库规范（AGENTS.md §6），本期只做**静态检查**（`npm run typecheck` + 只读检索）；下表动态验证**均未执行**，由人工完成。

| # | 步骤 | 预期结果 |
| --- | --- | --- |
| 1 | `npm run typecheck` | 通过 |
| 2 | `npm run probe -- qoder`（人工，令牌账号） | 行为不变：仍为账户级分桶，note 仍提示「逐包明细需 Cookie 模式」 |
| 3 | 看板 Qoder 卡片点「网页端登录」 | 状态依次为：启动/连接调试 Chrome → 已打开用量页，请在新窗口登录 → 抓取中 → 已添加 |
| 4 | 在调试 Chrome 窗口登录 Qoder 后 | 会话转 `ready`；`.env` 出现/更新 `QODER_COOKIE`（`cn`）或 `QODER_GLOBAL_COOKIE`（`global`）；`accounts.json` 新增条目且 `options.site` 正确；`.run/` 有备份 |
| 5 | 卡片刷新 | **逐包明细**：每个资源包一条，含总额/剩余/到期（与 qoder.com.cn「用量」页对照一致） |
| 6 | 卡片「每日用量」区 | **昨日用量 / 昨日增量 / 今日用量 / 今日增量** 不再是 `—`，来源标注为上游 |
| 7 | 再点一次「网页端登录」 | 同账号应**更新**原条目（不新建卡片）——验证 `loginAccountId` 去重 |
| 8 | CodeBuddy / Trae 卡片 | 登录区仍只有一个按钮，行为与改动前一致（回归） |
| 9 | 「最快到期」时间线 | Qoder 条目按真实 `expires_at` 参与排序，不再只有合成的「本期重置」 |

---

## 变更记录

| 时间 | 变更内容 | 触发原因 | 影响章节 |
| --- | --- | --- | --- |
| 2026-10-18 | 初版：确立「网页端登录」为第二个一等登录方法；新增平台无关 CDP 模块设计；契约扩展 `loginMethods`；明确零运行时依赖（内置 `WebSocket`）与「抓真实请求头而非 cookie jar」 | 用户反馈「不知道怎么看板切 Cookie 模式，需要网页端登录后就有逐包明细与昨日/今日增量」 | 全篇 |
| 2026-10-18 | 落地代码：新增 `src/lib/cdp.ts`；`base.ts` 增 `loginMethods`/`method`/`resolveLoginMethod`；`qoder.ts` 增 `id:'web'` 方法；`index.ts` 导出 `loginMethods`；`login-session.ts` 按方法隔离会话；`server.ts` 透传 `method`；前端按声明渲染多按钮 | 按 §3 方案实施 | §3.3-3.6、§4、§8 |
| 2026-10-18 | **补完 §8 步骤 3–7（前一轮会话中断，文档声称已落地但代码缺失）**：`qoder.ts` 新增模块级 `qoderWebLogin`（start：ensureDebugChrome + openCookieCollector；poll：cookieFor 命中 → sha256 指纹去重键 → ready；cleanup 释放采集器）+ 对象外组装 `loginMethods`；`index.ts` `describeRegistry()` 导出 `loginMethods`（未声明多方法的平台回退单方法清单，`kind` 按 `qr` 推导）；`base.ts` `ProviderLogin` 增 `cleanup()` 钩子；`login-session.ts` 会话带 `method`、`ACTIVE` 键改 `provider#method`、`settle()` 调 `cleanup`、`startLogin` 用 `resolveLoginMethod` 选方法；`server.ts` `/api/login/start` 透传 `method` 并放宽 403 门槛（`login` 或 `loginMethods` 任一即可）；前端 `index.html` 登录头改 `.login__buttons` 容器、`app.js` `renderLogin` 重写为按 `loginMethods` 渲染按钮组（web 方法渲染为次要样式、不显示链接/二维码/复制）、`state.login` 增 `method` 维度、`styles.css` 补 `.login__start--secondary` 与 `.login__buttons`。`npm run typecheck` 通过 | 恢复被中断的实施：文档与代码不一致（变更记录声称完成但 grep 零命中） | §3.4、§3.6、§8、变更记录 |
| 2026-10-21 | 关闭 R5 的「网页侧是否有稳定 uid / 能否拿昵称」待确认项：实测 `/api/v1/me` 返回 `{ id, username(UUID), name }`；`poll()` 改为把昵称填进 `account.name`（原为写死空串），命名交给既有落库链路 | 用户要求 Qoder Cookie 模式卡片标题与 CodeBuddy / Trae 一致（显示账号名） | §3.6、§5 R5、变更记录 |
