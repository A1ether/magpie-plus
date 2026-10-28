# Trae · 打开链接登录「网络错误」 修复方案

> 文档类型：修复方案
> 关联模块：Trae 登录（`src/providers/trae.ts` / `src/login-session.ts` / `src/server.ts`）
> 创建时间：2026-10-18
> 最后更新：2026-10-18
> 当前状态：已完成（静态检查通过；动态验证由人工执行，见 §4）
> 关联改动文件：`src/providers/trae.ts`、`src/login-session.ts`、`src/server.ts`、`README.md`、`docs/modules/项目整体-代码Wiki.md`、`docs/plans/账号管理-扫码与链接登录-开发设计方案.md`

---

## 1. 问题现象

**结论**：点「打开链接登录」后，浏览器打开 `www.trae.cn/authorization?...`，页面直接显示「登录失败 / 网络错误，请刷新页面重试。」，扫码/授权环节根本没有出现，回调也从未到达本机服务。

| 项 | 内容 |
| --- | --- |
| 复现步骤 | 看板 → 顶栏「平台与账号」/ Trae 卡片 → 「登录 / 添加账号」→「打开链接登录」→ 在浏览器打开生成的授权链接 |
| 触发环境 | Trae CN（`www.trae.cn`），服务默认监听 `127.0.0.1`，授权链接由 `src/providers/trae.ts` 的 `login.start` 生成 |
| 影响范围 | Trae 的「打开链接登录」**完全不可用**；Cookie 手工添加与既有取数不受影响 |
| 用户截图 | URL 前缀 `trae.cn/authorization?login_version=1&auth_from=trae&...&auth_type=local&client_id=ono9krqynydwx5&redirect=0...`，页面文案「登录失败 / 网络错误，请刷新页面重试。」 |

---

## 2. 根因分析

**结论**：我们把会话 id 拼进了 `auth_callback_url`（`…/authorize?lt=<sessionId>`），而 Trae 官方授权页对回调地址做**严格正则校验**，只接受**裸** `http://127.0.0.1:<port>/authorize`。校验失败后页面进入「登录失败」状态，且不设置具体错误文案，于是显示默认兜底文案「网络错误，请刷新页面重试。」。

### 2.1 本仓库侧证据

| 位置 | 现状 |
| --- | --- |
| `src/providers/trae.ts:625`（修复前） | `const callbackUrl = \`${input.callbackBase…}/authorize?lt=${encodeURIComponent(input.sessionId)}\`` —— 回调地址带了 `?lt=` |
| `src/providers/trae.ts:635`（修复前） | 该值直接作为 `auth_callback_url` 拼进授权链接 |
| `src/server.ts:582-583`（修复前） | `/authorize` 路由**强制要求** `lt` 参数：`const sessionId = url.searchParams.get('lt') ?? ''`，没有 `lt` 就 404 |
| `src/login-session.ts:307-316` | `deliverLoginCallback(sessionId, …)` 只按会话 id 精确匹配 |

即：`?lt=` 是「为了让回调能找到会话」而加的，但它同时违反上游校验。

### 2.2 上游侧证据（关键）

证据来自 Trae 官网授权页实际加载的 async chunk（`https://lf-cdn.trae.com.cn/obj/trae-com-cn/trae_website_prod_cn/static/js/async/authorization/page.39801783d1.js`，对应路由 `authorization/page`，chunk id `55521`）中 `auth_from === "trae"` 的原生 IDE 授权组件 `D`：

```js
// 1) 读取并解码回调地址
Z = decodeURIComponent(U.get("auth_callback_url") || "");

// 2) 挂载时严格校验：只接受裸地址
if (!/^http:\/\/127\.0\.0\.1:(\d+)\/authorize$/.test(Z)) {
  V(!1), ew("invalidUrl"), ep(3);   // ep(3) = 进入「登录失败」状态
  return;
}

// 3) 失败态文案（未设置具体原因时使用默认值）
T = "网络错误，请刷新页面重试。";
// … 3===状态 时渲染： main-title = "登录失败"，sub-title = ex || "登录失败，发生了未知错误"
//    而 ex 的初始值就是 T
```

与本机 Trae CN 客户端 `out/main.js` 的 `buildLoginUrl` 对照，官方生成的回调地址是**不带任何查询串**的：

```js
k = `http://127.0.0.1:${i}/authorize`;
// … &auth_callback_url=${k}&machine_id=…&device_id=…
```

两处一致说明：`auth_callback_url` 必须逐字满足 `^http://127.0.0.1:\d+/authorize$`。

### 2.3 结论链

1. 我们把 `?lt=<uuid>` 拼进回调地址 → `trae.ts:625`；
2. 授权页正则不匹配 → 直接进入失败态且不写错误文案；
3. 默认文案恰好是「网络错误，请刷新页面重试。」 → 与截图完全一致；
4. 页面未发起任何授权请求，回调自然也不会到达 `/authorize`。

> 另有两点非本次失败原因、但需一并修正的偏差（已核对上游源码）：
> - 授权页回跳时写入的是 `userInfo`（用户信息 JSON），而非 `userJwt`；当前解析只读 `userJwt`，会多打一次 `GetUserInfo`（功能不坏，但可省一次请求）。
> - 官方回调地址固定是 `127.0.0.1`，不是 `localhost`；当 `HOST=localhost` 时同样会被正则拒绝。

---

## 3. 修复方案

**结论**：回调地址改为裸 `/authorize`；`/authorize` 路由在缺少 `lt` 时改为「投递给当前活跃的可回调会话」；顺带兼容上游回跳的 `userInfo` 字段。**不改**授权链接的其它参数（`client_id` / PKCE / `device_id` 等），把变量收敛到最小。

### 3.1 改动点清单

| # | 文件 | 改动 | 为什么 |
| --- | --- | --- | --- |
| 1 | `src/providers/trae.ts:649` | `login.start` 改用 `traeCallbackUrl()`：输出裸 `/authorize`，主机固定 `127.0.0.1` | 满足上游正则；`HOST=localhost` 时也能用 |
| 2 | `src/login-session.ts:325` | 新增 `deliverLoginCallbackToActiveProvider(query)`：遍历当前活跃会话，投递给声明了 `handleCallback` 的适配器 | 没有 `lt` 时仍能定位会话；同一平台单会话已由 `ACTIVE` 保证 |
| 3 | `src/server.ts:586-594` | `/authorize`：有 `lt` 走原逻辑，无 `lt` 走新函数；两者都失败才 404 | 保持向后兼容（旧链接/手工带 `lt` 仍可用） |
| 4 | `src/providers/trae.ts:249`、`src/providers/trae.ts:736-748` | `traeReadCallback` 增加 `userInfo`；`poll` 在 `userJwt` 缺失时解析 `userInfo` 取 `UserID` / `ScreenName` | 与上游回跳字段一致，可省一次 `GetUserInfo` |

### 3.2 安全性

- `/authorize` 仍保留原有约束：只接受回环 `Host`（`src/server.ts:577-581`）；回调只投递授权码，响应不回传任何令牌。
- 没有 `lt` 时只能命中**当前活跃**会话，且只有 Trae 声明了 `handleCallback`（`grep` 结果：仅 `src/providers/trae.ts:659`）。伪造回调即使命中会话，也会因 PKCE verifier 不匹配而在 `ExchangeToken` 失败。
- 不新增任何日志输出，不记录授权码 / verifier / 私钥。

### 3.3 对现有逻辑的影响

| 对象 | 影响 |
| --- | --- |
| Trae Cookie / accessToken 取数 | 无 |
| 其它平台登录（CodeBuddy / Qoder） | 无：它们不使用 `handleCallback`，`/authorize` 只服务 Trae |
| 旧版带 `lt` 的登录链接 | 兼容：有 `lt` 仍按会话 id 投递 |
| `HOST` 配置为 `0.0.0.0` / LAN 地址 | 回调地址仍会拼成该主机，但上游正则只认 `127.0.0.1`，Trae 登录在本机浏览器场景应保持 `HOST=127.0.0.1`（默认值） |

---

## 4. 回归验证

> 按 AGENTS.md §6，AI 只做静态检查，以下动态步骤由人工执行。

| # | 操作 | 预期 |
| --- | --- | --- |
| 1 | `npm run typecheck` | 通过 |
| 2 | 看板 → Trae →「打开链接登录」 | 授权页不再出现「登录失败 / 网络错误」，而是「登录以使用 TRAE」或先跳登录页 |
| 3 | 在授权页点「登录并打开 TRAE」 | 浏览器跳到 `http://127.0.0.1:<port>/authorize?…authCodeInfo=…`，页面显示「登录完成」 |
| 4 | 回到看板 | 登录会话自动进入 `ready`，Trae 卡片出现（或更新） |
| 5 | 用未登录的浏览器再试 | 先跳 `trae.cn/login`，登录后回到授权页并可继续 |
| 6 | 其它平台扫码登录 | 行为不变 |

### 4.1 待确认

- 上游 `ExchangeToken` 是否额外校验 `x_device_id` / `x_app_type` 等风控参数（官方 IDE 会带）。本次不改；若第 3 步仍失败，再按 `out/main.js` 的 `x_*` 查询串补齐。**待确认**。

---

## 5. 风险与回滚

| 风险 | 等级 | 应对 |
| --- | --- | --- |
| 去掉 `lt` 后回调无法定位会话 | 低 | 活跃会话按 provider 唯一（`login-session.ts:71-72`、`197`），Trae 是唯一 `handleCallback` 实现 |
| `HOST=::1` 时 `127.0.0.1` 不可达 | 低 | 默认 `127.0.0.1`；文档注明 Trae 登录需保持默认回环地址 |
| 上游授权页行为再次变化 | 中 | 失败文案与校验点已记录在本方案 §2.2，可快速比对 |

**回滚**：还原上述 4 处改动即可；`accounts.json` / `.env` 结构未变，无数据迁移。

---

## 变更记录

| 时间 | 变更内容 | 触发原因 | 影响章节 |
| --- | --- | --- | --- |
| 2026-10-18 | 初版：定位 `auth_callback_url` 携带 `?lt=` 触发上游正则校验失败，确定修复方案 | 用户反馈 Trae「打开链接登录」显示「登录失败 / 网络错误」 | 全篇 |
| 2026-10-18 | 实施完成：① `trae.ts` 新增 `traeCallbackUrl()`，回调地址固定为裸 `http://127.0.0.1:<port>/authorize`；② `login-session.ts` 新增 `deliverLoginCallbackToActiveProvider()`；③ `server.ts` 的 `/authorize` 无 `lt` 时按活跃会话反查，带 `lt` 仍兼容；④ 回调解析新增 `userInfo`；⑤ 同步 README API 表、代码 Wiki 路由表、登录方案 §3.6 与变更记录；⑥ 静态检查：`npm run typecheck` 通过，未执行任何动态验证 | 实施收口 | §3、§4 |
