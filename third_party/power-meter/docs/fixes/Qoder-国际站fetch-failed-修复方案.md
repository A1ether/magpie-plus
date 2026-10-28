# Qoder 国际站 · 卡片「请求失败：fetch failed」 修复方案

> 文档类型：修复方案
> 关联模块：HTTP 传输层（`src/lib/http.ts`）+ Qoder 适配器（`src/providers/qoder.ts`）
> 创建时间：2026-10-23
> 最后更新：2026-10-23
> 当前状态：已完成（`npm run typecheck` 通过；`npm run probe -- qoder` 实测国际站恢复 ok，见 §4.1）
> 关联改动文件：`src/lib/direct-egress.ts`（新增）、`src/lib/http.ts`、`src/providers/qoder.ts`、`.env.example`、`docs/凭证配置指南.md`

---

## 1. 问题现象

**结论**：Qoder 国际站账号（`options.site: "global"`）的卡片不再显示余额，而是显示失败文案「国际站：请求失败：fetch failed；若该 Cookie 属于另一站点，可把 options.site 改为 "auto" 自动探测，或加 "fallbackSite": true 开启回退」。国内站账号正常。

| 项 | 内容 |
| --- | --- |
| 复现步骤 | 启动看板 → 「立即刷新」→ Qoder · A1ether 2（`site: global`）卡片失败 |
| 触发环境 | 本机 macOS，FlClash（TUN + mixed-port 7890，`mode: rule`）作为出口；Node 26 内置 `fetch`（undici） |
| 影响范围 | 仅 Qoder 国际站取数（`https://qoder.com`），国内站 `qoder.com.cn` 不受影响；其它平台卡片正常 |
| 误导点 | 文案把「网络层失败」解释成「Cookie 站点不匹配」，诱导用户去改 `options.site` / `fallbackSite`，改了也没用 |

---

## 2. 根因分析

**结论**：不是 Cookie 过期，也不是站点归属配错，而是**本机代理出口到 `qoder.com` 的 TLS 握手被重置**。同一台机器用物理网卡直连 `qoder.com` 完全可达（返回真实业务 401），走 TUN 出口则一律 `ECONNRESET`。

### 2.1 证据：失败发生在拿到任何 HTTP 响应之前

`.run/server.log` 里每轮刷新都是同一行（`src/providers/qoder.ts:1655-1658` 的 `ctx.log`）：

```
[qoder] Qoder 国际站取数失败：请求失败：fetch failed
```

该文案来自 `src/lib/http.ts:85-88`：`fetch()` 抛异常（没有 `Response`）时统一包装成 `HttpError(..., 0)`。也就是说上游**一个字节都没返回**，与 Cookie、站点、WAF 无关。

Node 侧的原生错误链（实测）：

```text
name: TypeError  message: fetch failed
cause: Error Client network socket disconnected before secure TLS connection was established
       code: ECONNRESET
```

`ECONNRESET` + "before secure TLS connection was established" = 出网路径在 TLS 握手阶段被中断。

### 2.2 证据：只有走 TUN 出口才失败，物理网卡直连正常

| 探测 | 结果 |
| --- | --- |
| `fetch('https://qoder.com/api/v1/me')`（默认路由 → utun6 → 海外节点） | 连续 5 次全部 `NET:ECONNRESET` |
| `fetch('https://qoder.com.cn/api/v1/me')`（同机、同路径） | 5/5 返回 `401`（说明代码与 UA 没问题） |
| 同一出口访问 `www.alibabacloud.com` / `google.com` | `200`（说明代理本身没坏，是这一条出网路径对 `qoder.com` 被掐） |
| `openssl s_client` / Python `ssl` 直连 `qoder.com:443`（经 TUN，两枚 DoH 真实 A 记录都试） | 均 `SSLEOFError`（不论 SNI 是 `qoder.com` 还是 `example.com`） |
| 同上，但 socket **绑定物理网卡地址 `192.168.30.30`** 再连 DoH 解析出的真实 IP | `TLS OK TLSv1.2`，随后 `GET /api/v1/me` 返回 `401 {"errorCode":"Unauthorized"}` |

最后一行是关键：同样的 TLS 客户端、同样的 SNI、同样的 IP，只因**源地址绑到物理网卡（绕过 TUN 路由）**就通了。说明 `qoder.com` 从本机境内线直连可达，是代理出口节点把这条连接重置了。

佐证：系统 DNS 在 TUN 下返回的是 **fake-ip**（`198.18.x.x`，FlClash `dns.enhanced-mode: fake-ip`），该地址只在隧道内有意义；绑物理网卡时必须换成 DoH 解析到的真实 IP，否则连不上。这一点决定了修复方案必须「真实 IP + 物理网卡源地址」同时具备。

### 2.3 证据：代码侧只做了「单通道」请求

| 位置 | 现状 |
| --- | --- |
| `src/lib/http.ts:66-88` | `fetchJson` 只有一条通道：全局 `fetch`（跟随系统路由，即 TUN），失败即抛 `HttpError(..., 0)`，无任何退路 |
| `src/lib/http.ts:86-87` | 原生错误 `cause.code`（`ECONNRESET` / `ETIMEDOUT`）被丢掉，只剩 "fetch failed"，诊断信息量为零 |
| `src/providers/qoder.ts:829-834` | 只要失败就提示「站点可能与 Cookie 不匹配 / 改 auto / 加 fallbackSite」——对网络层失败属于**错误归因** |

### 2.4 结论链

1. TLS 握手在出网路径被重置 → `fetch` 抛 `TypeError` → 无 HTTP 状态 → `HttpError(status=0)`；
2. `qoder.ts` 把 `status=0` 归进「站点不匹配」分支 → 卡片展示误导性建议；
3. 该主机用物理网卡直连可达 → 只要请求层具备「直连重试」能力，卡片即可恢复，且不依赖用户换代理节点或改配置。

---

## 3. 修复方案

**结论**：在 `fetchJson` 的网络层失败分支加一次「直连重试」：用 DoH 解析真实 A 记录 + 绑定物理网卡源地址 + 原生 `node:https` 重发同一请求；同时把原生错误码写进失败文案，并让 Qoder 的失败提示区分「网络层失败」与「站点不匹配」。不改任何凭证、站点、缓存语义。

### 3.1 改动点清单

| 文件 | 改动 | 为什么 |
| --- | --- | --- |
| `src/lib/direct-egress.ts`（新增） | 物理网卡地址挑选、DoH 真实 IP 解析（带 10 分钟缓存、过滤 fake-ip/回环/保留段）、`node:https` 直连请求（SNI + Host + 同源重定向） | 绕开 TUN 的唯一可行手法：源地址绑物理网卡 + 目标必须是真实 IP |
| `src/lib/http.ts` | ① 新增 `directRetryEnabled()`（`DIRECT_EGRESS_RETRY`，默认 `auto`）与 `directEligible()`；② `fetch` 抛错时先尝试一次直连重试，成功则返回直连结果并在 `init.log` 里留痕；③ 失败主机记 10 分钟「优先直连」，后续请求不再白等一次必败的 `fetch`；④ 兜底日志按主机只记一次；⑤ 失败文案改用 `describeNetworkError()`，把 `cause.code` 拼进去 | 让「一个坏出口」不再等于「一张坏卡片」；保留可关闭开关，避免在无 TUN 环境制造额外请求。③④ 是因为一次刷新会打 5+ 条历史明细请求，逐条先失败再重试既慢又刷屏（实测日志 5 行/轮 → 1 行/轮） |
| `src/providers/qoder.ts` | ① `buildFailureMessage()` 增加网络层失败分支（`status===0` / `ECONNRESET` 等），不再建议改 `site`；② 401/403 单独给「Cookie 过期 → capture」提示；③ `fetchUsageAtSite` / `fetchLedgerAtSite` / `qoderCookieIdentity` 透传 `log: ctx.log` | 纠正错误归因；用户照原提示改配置是无效操作。日志让下次排查能直接看到「已改直连」 |
| `.env.example` | 补 `DIRECT_EGRESS_RETRY=auto` 及说明 | 新增开关必须可发现、可关闭 |
| `docs/凭证配置指南.md` | 「站点切换与回退」小节加「网络层失败 ≠ 站点选错」；排错表加 `fetch failed` / `ECONNRESET` 一行 | 用户查排错表时能直接跳到本方案 |

> 实际实现较初稿收敛：不再改动 `src/config.ts`（开关是纯环境变量、由 `process.env` 惰性读取，与 `HTTP_USER_AGENT` 同一手法，避免为它扩 `settings` 契约）。

### 3.2 直连重试的判定边界

| 场景 | 行为 |
| --- | --- |
| `fetch` 成功拿到任意 HTTP 状态（含 4xx/5xx） | **不重试**，原样返回/抛错（业务问题不该换通道重放） |
| `fetch` 网络层抛错（无响应） | 尝试一次直连；成功后返回直连结果，日志记「首次失败原因 + 已改直连」 |
| 目标是 `localhost` / IP 字面量 / 非 https | 不重试 |
| DoH 解析不到可用地址，或物理网卡上没有可用 IPv4 | 不重试，抛原始错误（文案已带原生错误码） |
| 某主机已确认「默认出口失败、直连可用」 | 10 分钟内**下一次起直接走直连**（不再先发一次必败的 `fetch`），到期后重新试探默认出口 |
| `DIRECT_EGRESS_RETRY=off` | 完全关闭，行为与修复前一致 |
| 无 TUN 的普通环境（直连本来就正常） | 首次请求就成功，重试分支不会触发，零额外开销 |

### 3.3 为什么不用别的方案

| 备选 | 否决原因 |
| --- | --- |
| 让用户换代理节点 / 给 `qoder.com` 加 DIRECT 规则 | 有效但不可交付：属于环境侧手工作业，换节点/换网络后复发；本次已在代理侧确认是节点问题，代码层仍应具备自愈能力 |
| 在应用内读取系统代理（`HTTP_PROXY`） | Node 的 `fetch` 不认 `scutil --proxy`；TUN 环境里也绕不开路由劫持，实测 `curl -x http://127.0.0.1:7890 https://qoder.com` 同样 `SSLEOFError` |
| 把 `options.site` 改成 `auto` | 与根因无关：`auto` 首选 cn，cn 站用国际站 Cookie 只会 401，最终仍失败，只是多一次请求 |
| 引入 `undici` 的 `ProxyAgent` / 第三方依赖 | 仓库约定「运行时零第三方依赖」，且解决不了 TUN 路由问题 |

---

## 4. 回归验证

**结论**：本次改动不触碰凭证、站点候选链、缓存与聚合口径，回归面集中在「HTTP 传输层」和「Qoder 失败文案」两处。

### 4.1 已执行的验证（2026-10-23）

| 验证项 | 命令 | 实测结果 |
| --- | --- | --- |
| 类型检查 | `npm run typecheck` | 通过（无输出，exit 0） |
| 直连通道可用性 | `npx tsx .run/verify-direct-egress.ts` | `directFetch` 返回 **401**（到达上游），`via=192.168.30.30→qoder.com 真实 IP` |
| 直连兜底自动触发 | 同上 §4 | 日志：`请求失败（fetch failed：ECONNRESET …），已改直连（192.168.30.30→…）→ HTTP 401` |
| 正常站点不触发兜底 | 同上 §5（`qoder.com.cn`） | 兜底日志 0 条（首次 `fetch` 直接成功） |
| 开关生效 | 同上 §6（`DIRECT_EGRESS_RETRY=off`） | 回到 `HttpError.status=0` + 「fetch failed」，即修复前行为 |
| 适配器层（假 Cookie，不含任何真实凭证） | `npx tsx .run/verify-qoder-global.ts` | 国际站/国内站均 `unauthorized`（HTTP 401），国际站日志出现「已改直连…→ HTTP 401」；修复前国际站是 `error` + `fetch failed` |
| 看板恢复（重启后） | `curl /api/summary` | `Qoder · A1ether` ok / 2843 CREDITS / region=cn；`Qoder · A1ether 2` ok / 854 CREDITS / region=global，dashboardUrl 指向 qoder.com；单轮刷新日志从 5 行降为 1 行 |
| **真实取数** | `npm run probe -- qoder` | 两个 Qoder 账号均 `ok`：国内站与 **国际站 595ms / 余额 854 CREDITS / 最快到期 2026-11-13**，批次明细完整 |

> 说明：`.run/verify-*.ts` 是本次排查用的临时脚本（`.run/` 已 gitignore），不进入版本库；`npm run probe` 使用了 `.env` 中的真实 Cookie，输出只含余额与批次，未打印任何凭证明文。

### 4.2 后续人工回归建议

| 回归点 | 验证方式（人工执行） | 预期结果 |
| --- | --- | --- |
| Qoder 国际站取数恢复 | `npm run probe -- qoder` | 国际站行 status=ok，卡片显示余额与批次；`.run/server.log` 出现「首次失败 … 已改直连」留痕 |
| 国内站不受影响 | 同上 | 国内站行与改造前一致（ok，无直连日志） |
| 失败文案归因 | 断开网络（或关代理 TUN）后再刷新 | 文案为网络层说明（含 `ECONNRESET` 等原生错误码），不再出现「把 site 改成 auto」 |
| 其它平台无回归 | `npm run probe` | 各平台状态与改造前一致 |
| 开关可关闭 | `.env` 加 `DIRECT_EGRESS_RETRY=off` 后重跑 | 行为回到改造前（国际站仍失败，无直连重试） |

> 本次按仓库规范做了静态检查（`npm run typecheck`）与上表的定向验证；`npm run probe` 与临时校验脚本为本次排查中实际执行，均已如实记录（见 §4.1）。

---

## 5. 风险与回滚

| 风险 | 影响 | 缓解 |
| --- | --- | --- |
| 直连通道在「只有代理能出网」的环境里必然失败 | 多一次 ~百毫秒级请求 | 仅在首次网络失败后触发；失败即抛原始错误，不影响状态判定 |
| 绑定物理网卡地址假定「境内可直连该主机」 | 若某主机只能走代理，则重试无效（仍失败，行为等同修复前） | 重试失败不改写错误，文案已包含两类原因 |
| DoH 解析引入额外 DNS 依赖 | 三个 DoH 端点串行回退，超时各 4s | 结果缓存 10 分钟；解析失败直接放弃重试 |
| 关闭开关被误设为 `off` 导致问题复发 | 国际站又变回失败 | 默认 `auto`；失败文案会明确「直连重试当前被关闭」 |
| 直连重试把「本该由代理访问」的站点改走境内线路 | 可能取到地域不同的上游响应（如按 IP 分区的站点） | 只对「首次请求完全失败」生效；成功时日志留痕，便于事后发现 |

**回滚**：`.env` 设 `DIRECT_EGRESS_RETRY=off` 即可在不动代码的情况下回到修复前行为；彻底回滚则还原 `src/lib/http.ts`、删除 `src/lib/direct-egress.ts`、还原 `src/providers/qoder.ts` 的 `buildFailureMessage`。

---

## 变更记录

| 时间 | 变更内容 | 触发原因 | 影响章节 |
| --- | --- | --- | --- |
| 2026-10-23 | 初版：定位为 TUN 出口对 qoder.com 的 TLS 重置（非 Cookie/站点问题），给出「DoH 真实 IP + 物理网卡源地址」直连重试方案与失败文案纠偏 | 用户反馈国际站卡片报 `请求失败：fetch failed` | 全篇 |
| 2026-10-23 | 代码落地：`src/lib/direct-egress.ts` 新增直连通道；`http.ts` 接入兜底 + `describeNetworkError` + `DIRECT_EGRESS_RETRY` 开关；`qoder.ts` 网络层/401 文案纠偏 + 请求日志透传；`.env.example`、`docs/凭证配置指南.md` 同步 | 方案确认后实施 | §3.1、§4.1、§5 |
| 2026-10-23 | 验证补充：`npm run probe -- qoder` 实测国际站 595ms / 854 CREDITS 恢复；假 Cookie 校验显示国际站由 `error` 变为 `unauthorized`（已到达上游）；`DIRECT_EGRESS_RETRY=off` 可复现修复前行为 | 完成动态验证，需回填实测结果 | §4.1、§5 |
| 2026-10-23 | 性能收敛：失败主机记 10 分钟「优先直连」+ 兜底日志按主机去重（一次刷新 5 行 → 1 行）；重启看板后 `/api/summary` 两账号均 ok | 首版每轮刷新重复白等 TLS 重置、日志刷屏 | §3.1、§3.2、§4.1 |
