# Qoder - Cookie 模式卡片标题拿不到昵称 - 修复方案

> 文档类型：修复方案
> 关联模块：平台适配器（Qoder）、账号存储（展示名）
> 创建时间：2026-10-21
> 最后更新：2026-10-21
> 当前状态：已完成（静态检查通过；动态验证见 §4，由用户执行）
> 关联改动文件：`src/providers/qoder.ts`

---

## 1. 问题现象

**结论：Qoder 的两张卡片标题仍是通用占位名「Qoder · 登录账号」「Qoder · 登录账号 2」，而 CodeBuddy / Trae 的卡片标题是「CodeBuddy · 夜雨声烦」「Trae · A1ether」这种「平台 · 昵称」形态。用户期望 Qoder 与它们一致。**

| 项 | 内容 |
| --- | --- |
| 触发环境 | Qoder 两个账号**都是 Cookie 模式**（`credentials` 只有 `cookie`，无 `accessToken`）；国内站 `site: cn`、国际站 `site: global` |
| 复现步骤 | 1. 打开看板；2. 看 Qoder 两张卡片的标题 |
| 实际结果 | `Qoder · 登录账号` / `Qoder · 登录账号 2` |
| 期望结果 | `Qoder · <昵称>`（实测该账号昵称为 `A1ether`），与 CodeBuddy / Trae 同形 |
| 影响范围 | 仅展示名；余额、批次、用量数值均正确 |

本机证据（`accounts.json`，已隐去凭证）：

```jsonc
{ "provider": "qoder", "displayName": "Qoder · 登录账号",   "options": { "site": "cn",     "loginAccountId": "…" } }
{ "provider": "qoder", "displayName": "Qoder · 登录账号 2", "options": { "site": "global", "loginAccountId": "…" } }
```

---

## 2. 根因分析

**结论：昵称解析只写进了「令牌模式」和「扫码登录」两条路径，而这两个账号走的是「Cookie 模式 + 网页端登录」，那条路径从头到尾没有调用过任何身份接口——不是解析写错，是这条分支根本没写。**

### 2.1 三条路径的现状对比

| 路径 | 是否解析昵称 | 证据 |
| --- | --- | --- |
| 令牌模式 `probe()` | ✅ 是（占位名时才查 `GET {openapi}/api/v1/userinfo`） | `src/providers/qoder.ts:397-410`（修复前该行号为 `:398-401`） |
| 扫码登录 `login.poll()` | ✅ 是（设备流无昵称时补查 userinfo） | `src/providers/qoder.ts:1451-1459` |
| **Cookie 模式 `probe()`** | ❌ **否** | `src/providers/qoder.ts:1541-1560`（成功分支直接 `buildSnapshot`，无任何身份请求）；`buildSnapshot` 的 `displayName` 缺省取 `account.displayName`，即占位名（`src/providers/base.ts:438-440`） |
| **网页端登录 `qoderWebLogin.poll()`** | ❌ **否** | `src/providers/qoder.ts:1266-1272`：`account: { id: accountId, name: '' }` |

### 2.2 次生原因：落库时昵称为空 → 建卡即占位名

`landLoginAccount()` 用 `input.accountName`（即 `poll()` 返回的 `account.name`）生成展示名；为空时 `uniqueAccountName()` 退回 `${adapter.displayName} · 登录账号`（`src/account-store.ts:653-667`）。网页端登录把 `name` 写死成空串，于是新账号一开始就叫「登录账号」。

### 2.3 关键证据：Cookie 会话**能**拿到昵称

上一轮 `docs/fixes/Qoder-令牌模式批次明细缺失-修复方案.md` §2.3 N-? 曾假定「网页侧没有权威 uid（openapi /userinfo 要 Bearer，Cookie 拿不到）」（原注释在 `src/providers/qoder.ts:1259-1262`）。**这个假定只对 `/api/v1/userinfo` 成立，对网页身份接口 `/api/v1/me` 不成立**——它本来就是网页自己在登录后调用的接口（`src/providers/qoder.ts:110` 已把该路径定义为 `QODER_WEB_IDENTITY_PATH`，用于登录态判据）。

本机调试 Chrome 的 HTTP 缓存里留有该接口的真实响应（只读取证，未发起新请求）：

| 缓存文件 | `GET /api/v1/me` 响应体（摘录） |
| --- | --- |
| `~/.power-meter/chrome-profile/…/288a731022237c81_0` | `{"id":"01a07fb0-…","username":"f17e9028-…","name":"A1ether ","email":"…","avatar":"/users/…"}` |
| `~/.power-meter/chrome-profile/…/8082ed33e37968ab_0` | `{"id":"01a0708c-…","username":"83f8d612-…","name":"A1ether","email":"","avatar":"https://qoder.com.cn/users/…"}` |

两个可直接使用的事实：

1. **昵称在 `name` 字段**，与 openapi `userinfo` 的 `name` 同名同义 → 两条路径可共用一个解析器；
2. **`username` 是 UUID，不是人名**（`f17e9028-ba34-4e1c-8d1c-7250c0268219`）→ 解析时不能像 `qoderUserInfo()` 那样把 `username` 当作昵称兜底，否则卡片会变成一串 UUID。

### 2.4 为什么「去重键」不能同时改成上游 `id`

`options.loginAccountId` 是登录去重键（`src/account-store.ts:730-733`、`:777`），网页端登录写入的是 Cookie 指纹（`src/providers/qoder.ts:1263`）。若本次顺手改成 `/api/v1/me` 的 `id`，两个已有账号的键就对不上了——重新登录会**新建卡片**而不是更新原条目。故本次只补昵称，`accountId` 维持指纹（稳定性优先）。此项列入 §5 待确认。

---

## 3. 修复方案

**结论：新增一个「用 Cookie 查网页身份」的函数，接到两处——① Cookie 模式的 `probe()` 在卡片名仍是占位名时解析昵称并走既有 `persistAccountName` 写回；② 网页端登录的 `poll()` 把昵称填进 `account.name`，让落库当轮就命名正确。去重键、凭证、缓存键一律不动。**

### 3.1 改动点清单

| # | 位置 | 改动 | 为什么 |
| --- | --- | --- | --- |
| C1 | `src/providers/qoder.ts`（`headersForSite` 之后） | 新增 `parseQoderIdentity(payload)`：取 `id` 与昵称，昵称字段顺序 `name / display_name / displayName / nickname / nick_name`，`username` 仅在**不像 UUID** 时兜底 | §2.3：`name` 是人名、`username` 是 UUID；两条身份接口共用一份解析 |
| C2 | 同上 | 新增 `qoderCookieIdentity(site, cookie, ctx, timeoutMs)`：`GET {site.origin}/api/v1/me`，沿用 `headersForSite()` 的站点化请求头（Cookie / Origin / Referer / Accept-Language） | 复用已实测可用的会话头；不新增依赖、不改站点表 |
| C3 | `probe()` 的 Cookie 成功分支（原 `:1541`） | 与令牌模式同款门禁：**仅当 `account.displayName` 仍是占位名且 `ctx.persistAccountName` 存在**时请求一次；拿到昵称则 `persistAccountName()` 写回，并把返回值作为本轮 `displayName` / `accountId` | 改完即停（`isPlaceholderAccountName` 不再命中），不产生常驻开销；与令牌模式口径完全一致 |
| C4 | `qoderWebLogin.poll()`（原 `:1266`） | 抓齐 Cookie 后解析身份：`account.name` 填昵称（拿不到仍为空串，退回占位名），日志区分「拿到 / 未拿到」 | 落库当轮即命名正确，不必等下一轮刷新才改名 |
| C5 | 过期注释更正（原 `:1259-1262`） | 说明「Cookie 拿不到昵称」的旧结论只对 `/api/v1/userinfo` 成立，网页 `/api/v1/me` 可以 | 避免后续维护者再踩同一处误判 |
| C6 | `qoderUserInfo()`（令牌模式用的旧解析器） | 改为调用同一个 `parseQoderIdentity()` | 顺手修掉旧实现的隐患：旧版 `firstString(pick(payload,'name','username','user_name'))` 在 `name` 缺失时会退回 `username`，而网页侧 `username` 是 UUID；两个入口用一份解析才不会一条路径正常、另一条写出 UUID |
| C7 | `parseQoderIdentity()` 取值方式 | 逐个字段 `firstString(body.name, body.display_name, …)`，**不用** `pick` 串 key | `pick()` 返回第一个非 null 值：遇到 `name: ""` 会停在空串上，后面的 `display_name` / `nickname` 永远读不到。这是 §4.1 反向用例 E 暴露的（不是实测报文问题，但会让另一种报文形态失效） |

### 3.2 不做的事

| 不做 | 原因 |
| --- | --- |
| 不改 `accountId` / `loginAccountId` | §2.4：会让已有账号重新登录时新建卡片 |
| 不改 `landLoginAccount()` / `persistAccountName()` | 这条通道已经能「占位名 → 平台 · 昵称」并自带 `.run/` 备份，Qoder 只是没调用它 |
| 不改 `src/types.ts`、`accounts.json` 结构、`.env` 键 | 无契约变更 |
| 不改卡片标题格式 | 仍是「Qoder · 昵称」，与 CodeBuddy / Trae 同形；站点区分由上一轮的「按平台」分组（`Qoder 国内版` / `Qoder 国际版`）承担 |
| 不给非占位名改名 | 沿用既有规则：用户自定义过的名字一律不动（`src/account-store.ts:677-685`） |

### 3.3 数据流（修复后）

```
Cookie 模式 probe()
  └─ GET {origin}/api/v2/me/usages/big_model_credits   （余额，原有）
  └─ GET {origin}/api/v1/me/userplan                    （套餐，原有）
  └─ 仅当 displayName 是占位名：
       GET {origin}/api/v1/me → { id, username(UUID), name:"A1ether", email }
       → parseQoderIdentity() → name
       → ctx.persistAccountName("A1ether")
       → adoptResolvedAccountName() 备份 .run/ + 写 accounts.json
           displayName: "Qoder · 登录账号" → "Qoder · A1ether"
       → 本轮快照 displayName/accountId 用新名字（不必等热加载）

网页端登录 poll()
  └─ 拿到 Cookie（原有）→ GET {origin}/api/v1/me → name
  └─ return { account: { id: 指纹, name: "A1ether" }, … }
  └─ landLoginAccount() → uniqueAccountName() → "Qoder · A1ether"
```

---

## 4. 回归验证

### 4.1 AI 已执行（静态检查 + 纯逻辑自测，未启动服务、未打上游）

| # | 手段 | 结果 |
| --- | --- | --- |
| 1 | `npm run typecheck`（`tsc --noEmit`） | 通过，无输出 |
| 2 | 逻辑自测 A（`parseQoderIdentity` + 2 份**真实缓存响应**）：从 `src/providers/qoder.ts` 抽出真实函数体，在 Node `vm` 里跑 | 2/2 通过：两份报文都解析出 `name` = `A1ether`（尾随空格已 trim）；`username` 的 UUID 未被当成昵称；`id` 正确 |
| 3 | 逻辑自测 A 反向用例（5 类） | 全通过：仅 UUID `username` → 空昵称（不会把 UUID 写进标题）；`username` 是人名 → 可兜底；`name` 带空格 → trim；`null` / 字符串 → 不抛错返回空；信封 `{data:{…}}` → 能解包 |
| 4 | 逻辑自测 B（命名链路）：抽 `isPlaceholderAccountName`（`base.ts`）+ `uniqueAccountName`（`account-store.ts`）的真实实现 | 门禁 5/5：`Qoder · 登录账号`、`… 2` 判为占位名 → 会查昵称；`Qoder · A1ether` 与自定义名 → 不动；`CodeBuddy · 夜雨声烦` 不被误判。命名 3/3：`Qoder · A1ether`（与 CodeBuddy/Trae 同形）、同昵称第二个为 `Qoder · A1ether 2`、昵称为空退回占位名 |
| 5 | 自测暴露并已修的隐患 | 反向用例 E 带出 `pick()` 遇 `name: ""` 会停止遍历的问题 → 已改为逐字段 `firstString`（§3.1 C7），改后全部用例通过 |

> 自测只覆盖纯逻辑（解析 + 门禁 + 命名）。**未验证**：真实上游 `/api/v1/me` 在本机两个账号上的返回、`accounts.json` 实际写入、页面标题渲染——见 §4.2。

### 4.2 人工执行（本次 AI 不代跑）

| # | 操作 | 预期 |
| --- | --- | --- |
| 1 | 重启服务后打开看板 | Qoder 两张卡标题变为 `Qoder · A1ether`（撞名时第二张为 `Qoder · A1ether 2`）；CodeBuddy / Trae 标题不变 |
| 2 | `jq '.accounts[] | select(.provider=="qoder") | .displayName' accounts.json` | 两条都已是昵称；`.env` 与 `loginAccountId` 不变；`.run/` 下新增一份 `qoder-login-name-*` 备份 |
| 3 | 把某条 qoder 的 `displayName` 手工改回「Qoder · 登录账号」后刷新 | 一轮探测后自动改回昵称，服务日志出现「展示名自动更新」一行 |
| 4 | 把 `displayName` 改成自定义名（如「我的 Qoder」）后刷新 | **不被覆盖**（规则：非占位名不动） |
| 5 | 走一次「网页端登录」（可选） | 落库当轮标题即为 `Qoder · <昵称>`，无需再等一轮刷新 |
| 6 | 回归 | 余额 / 批次 / 昨日今日用量与修复前一致；「按平台」子菜单仍显示 `Qoder 国内版` / `Qoder 国际版` |

---

## 5. 风险与回滚

| # | 风险 | 影响 | 处置 |
| --- | --- | --- | --- |
| R1 | `/api/v1/me` 上游改版或字段改名 | 昵称取不到 | 纯 best-effort：失败只记日志，标题维持原值，不影响取数；多字段兜底（§3.1 C1） |
| R2 | 昵称把 `username`（UUID）当兜底 | 卡片标题变成 UUID | 已加「UUID 形态不采用」判定 + §4.1 反向用例 |
| R3 | 两个账号昵称相同 | 第二张卡被加序号「 2」 | 既有 `uniqueAccountName()` 已处理；站点区分由「按平台」分组的 `Qoder 国内版 / 国际版` 承担 |
| R4 | 每轮探测都多打一次身份接口 | 轻微上游压力 | 只在占位名时才请求，改名后即停；令牌模式早已如此 |
| 回滚 | 删除 C3/C4 两处调用即可恢复原状 | — | 无数据结构变更，`.run/` 下有改名前的 accounts.json 备份 |

**待确认**：是否要把 `options.loginAccountId` 从「Cookie 指纹」换成上游 `id`（`01a07fb0-…`）。换的好处是身份更权威（Cookie 轮换不再算新账号），代价是需要一次配置迁移（否则重新登录会新建卡片）。本次不做。

---

## 变更记录

| 时间 | 变更内容 | 触发原因 | 影响章节 |
| --- | --- | --- | --- |
| 2026-10-21 | 初版：定位 Cookie 模式与网页端登录两条路径从未解析昵称；用 `/api/v1/me` 的真实缓存响应证明 Cookie 可拿到昵称；补 C1~C5 五处改动 | 用户反馈「qoder 登录账号能拿到名称，效果和 codebuddy、trae 一样」 | 全篇 |
| 2026-10-21 | 落地实施并补 §4.1 静态检查与逻辑自测结论 | 代码改动完成 | §3、§4 |
