# 平台适配器 - 接入 MiniMax Code 签到积分 - 开发设计方案

> 文档类型：开发设计方案
> 关联模块：平台适配器
> 创建时间：2026-10-10
> 最后更新：2026-10-11
> 当前状态：已实施；卡片补充额度进度条（见 §10），待用户复测
> 关联改动文件：`src/providers/minimax.ts`、`src/providers/index.ts`、`src/cli/capture.ts`、`accounts.example.json`、`.env.example`、`README.md`

---

## 1. 背景与目标

**结论：把 MiniMax Code 的「签到积分」（以及购买积分）接入看板，展示积分余额、逐笔批次与到期时间；平台控制台 `platform.minimax.cn/console/plan*` 显示积分 0 的问题不修（那是另一套口径），本项目直接取 MiniMax Code 侧的积分池。**

- 目标：
  1. MiniMax 每日签到积分（400 / 爆点 1000，到账后 30 天有效）在卡片上可见：余额 + 批次到期（进入「最快到期」排序）；
  2. 「昨日/今日增量」维度用积分到账明细（签到、购买）走上游精确值；
  3. 凭证可通过 `npm run capture` 一键抓取（与其它平台一致）。
- Non-Goals：
  - 不做签到动作（不调用领取接口，只读状态）；
  - 不接 M Plan 用量窗口（5 小时/每周百分比，官方有公开接口但本账号未订阅，后续可另开方案）；
  - 不显示平台控制台的「已购积分/钱包」口径（与签到积分不是同一笔账，冲突源于两套系统）。

## 2. 现状调研

**结论：官方没有公开「签到积分」API（文档只教在 MiniMax Code 设置里看）；但 MiniMax Code 桌面端自身调用内部云接口取数，接口与字段已从本机安装包中静态取证，可复刻。**

### 2.1 官方口径（证据）

| 项 | 结论 | 来源 |
| --- | --- | --- |
| 签到规则 | 每日 400 积分；连续第 4、7 天 1000 积分；整周 4000；到账后 30 天有效 | `https://agent.minimaxi.com/docs/code/account/usage` |
| 官方查看方式 | 仅「MiniMax Code 设置 → 用量」页面；无公开 API | 同上 |
| 官方 M Plan 用量接口 | `GET {www.minimaxi.com}/v1/token_plan/remains`（Bearer 订阅 Key），返回 `model_remains[]`（5h/周窗口百分比），**不含签到积分** | `https://platform.minimaxi.com/docs/token-plan/faq`、`https://platform.minimax.io/docs/m-plan/faq` |
| 第三方佐证 | dsh-quota-panel、coding-usage-bar 等社区实现同样只从 `token_plan/remains` 取用量窗口，未见签到积分实现 | npm: `dsh-quota-panel`、github: `hanzhangzzz/coding-usage-bar` |

### 2.2 MiniMax Code 客户端内部接口（取证）

> 取证方式：对用户本机 `/Applications/MiniMax Code.app`（Electron + Next.js）的 `app.asar` 做**只读**静态检索与分块反解（未运行、未抓包）。

| 用途 | 接口 | 证据（asar 内路径 → 关键逻辑） |
| --- | --- | --- |
| 积分明细（签到/购买批次） | `GET {origin}/minimax-cloud/api/v1/credit/details` | `out/_next/static/chunks/24831-*.js` → `get(n,{params:e})`，校验 `details[]` + `total_count` |
| 明细请求参数 | `page_number` / `page_size` / `include_expired: true` | `out/_next/static/chunks/43130-*.js` → `(0,D.xK)({page_number:u,page_size:d,include_expired:!0},{...})`，默认 `pageSize:20` |
| 明细行字段 | `credit_type`(1=购买,2=签到) / `granted_amount` / `remaining_amount` / `consumed_amount` / `granted_at_ms` / `expire_at_ms` | `43130` → `et()` 校验器；`11842` → 表格列映射（获得时间/类别/总量/剩余/到期/状态） |
| 「签到」文案坐实 | `credit_type===2` 的中文标签就是「签到」 | `88822-*.js` → `"usage_credit_type_free":"签到"`；枚举 `[i.Purchased=1,i.Free=2]`（`43130`） |
| 今日签到状态 | `GET {origin}/minimax-cloud/api/v1/signin/status` | `app/(pages)/(mavis)/archon/page-*.js` → `ec.ZP.get("/minimax-cloud/api/v1/signin/status")`；枚举 `1=Upcoming,2=Claimable,3=Claimed,4=Disabled` |
| 请求签名 | `x-timestamp`（秒）+ `x-signature=md5(秒+"I*7Cf%WZ#S&%1RlZJ&C2"+body)` + `yy` 头 + 公共查询参数 | `24831` → axios 请求拦截器（见 4.3 复刻公式） |
| 鉴权区分 | **Web 端用请求头 `token`（localStorage `_token`）+ 同源 SSO Cookie**；桌面端由主进程注入 `Authorization: Bearer` + Electron session Cookie | `24831`（web 分支 `localStorage.getItem("_token")`）+ `dist/main/ipc/proxy.ipc.js`（`net.fetch` 走 session、`SHARED_AUTH_PATH_PREFIXES` 含 `/minimax-cloud/api`）；`16030` 模块 → `Xw="_token"`。**实测教训：只发 token 会 401，Cookie 必须一起带** |
| CN/海外主机 | CN=`https://agent.minimax.cn`、海外=`https://agent.minimax.io` | `24831`/`43130` → `NEXT_PUBLIC_DOMAIN_URL`、`["https://agent.minimax.cn","https://agent.minimaxi.com"]` |

### 2.3 本项目可复用的既有能力

- 声明式适配器契约与构建工具：`src/providers/base.ts:26-75`（`ProviderAdapter`）、`base.ts:117-135`（`makeLot`）、`base.ts:138-149`（`splitByExpiry`）、`base.ts:155-168`（`hiddenLotsNote`）、`base.ts:198-251`（`buildSnapshot`）；
- 明细接口契约：`src/providers/base.ts:73`（`fetchUsageLedger`）、`src/usage-ledger.ts:13-29`（`UsageRecord` / `UsageLedger.covers`）；聚合逻辑 `src/service.ts:250-286`；
- Cookie 抓取框架：`src/cli/capture.ts:102-148`（`Target`/`TARGETS`）、`capture.ts:376-430`（`captureTarget`）；
- 参照实现：`src/providers/codebuddy.ts`（Cookie + 多接口 + 批次）、`src/providers/qoder.ts`（明细接口 `fetchUsageLedger` 写法）。

## 3. 方案设计

**结论：新增 `minimax` 适配器，走「Web 端 token + 复刻签名」访问内部云接口；余额=在有效期批次的剩余之和；批次=逐笔积分明细（含到期时间）。**

数据流：

```
浏览器登录 agent.minimax.cn
   └─ localStorage["_token"]  ──（npm run capture 抓取）──> .env: MINIMAX_CODE_TOKEN
        │
        ▼
provider probe()
   ├─ GET /minimax-cloud/api/v1/credit/details?page_number&page_size&include_expired=true（分页）
   │     → 批次：credit_type(签到/购买) + granted/remaining/consumed + granted_at_ms + expire_at_ms
   ├─ GET /minimax-cloud/api/v1/signin/status（失败不影响主流程）
   │     → 「今日签到：已领取 / 待领取」note
   └─ buildSnapshot：balance=有效期批次剩余之和；credits=未过期批次；note=签到/购买拆分
        │
        ▼
fetchUsageLedger()（缓存 TTL 复用）
   └─ 同样的明细按 granted_at_ms 归集「增量」（grant）；「用量」不覆盖 → 本地采样兜底
        │
        ▼
service.ts 聚合 → /api/summary → 卡片渲染（含「最快到期」）
```

职责划分：
- 适配器只负责「取数 + 归一化 + 生成批次」，不感知缓存与采样（与既有约定一致）；
- 由于内部接口需要签名头，所有请求统一走适配器内的 `signedRequest()` 封装；
- 签到状态只是 note，取不到不报错。

## 4. 接口设计

### 4.1 积分明细（必需）

```
GET {origin}/minimax-cloud/api/v1/credit/details
  ?page_number=1&page_size=100&include_expired=true&<公共参数…>
Header: token / x-timestamp / x-signature / yy
```
响应（实测来自客户端解析代码，字段名以客户端为准）：
```jsonc
{
  "details": [
    {
      "credit_type": 2,            // 1=购买(Purchased) 2=签到(Free)
      "granted_amount": "1,000",   // 可能带千分位字符串
      "remaining_amount": "1,000",
      "consumed_amount": "0",
      "granted_at_ms": 1790..
      "expire_at_ms": 1792..
    }
  ],
  "total_count": 42
}
```
错误码约定：
- HTTP 401 → `unauthorized`（token 失效，提示重新抓取）；
- `base_resp.status_code != 0`（若返回）→ 业务错误（透出 `status_msg`）。

### 4.2 签到状态（可选，fail-soft）

```
GET {origin}/minimax-cloud/api/v1/signin/status
```
响应：`{ base_resp:{status_code}, data:{ scene, days:[{day_no, points, bonus_points, is_today, status}] } }`
- `status`：1=Upcoming、2=Claimable、3=Claimed、4=Disabled；
- 只读取、不调用 `/signin/claim`（不代替用户签到）。

### 4.3 签名复刻（与客户端一致）

以下均在适配器内用 `node:crypto` 实现（无第三方依赖）：

| 字段 | 公式 |
| --- | --- |
| `x-timestamp` | `floor(now/1000)`（Unix 秒） |
| `x-signature` | `md5(秒 + "I*7Cf%WZ#S&%1RlZJ&C2" + body)`，GET 时 body 为空串 |
| `yy` | `md5(encodeURIComponent(pathWithQuery) + "_" + bodyStr + md5(毫秒时间戳) + "ooui")`；GET 的 `bodyStr="{}"`；`pathWithQuery` 用与请求**完全相同**的 `URLSearchParams` 序列化，保证与请求 URL 一致 |
| 公共查询参数 | 顺序固定：`device_platform=web, biz_id=3, app_id=3001, version_code=22201, unix, timezone_offset, sys_language, lang, uuid, device_id, os_name, browser_name, browser_language, browser_platform, user_id, op_ticket?, screen_width, screen_height, token, client=web`。其中 **uuid/user_id/param token 必须用抓取到的真实身份值**（`user_detail_agent.realUserID` / `.token`、`UNIQUE_USER_ID`）；**user_id 是 18 位大整数，必须全程按字符串处理**（转 Number 会因超出 2^53 而改变末位，服务端交叉校验必挂——实测 401 根因）；`device_id` 是标签页随机会话值、`screen_*` 是展示性参数，可给固定值 |
| 排查开关 | `options.skipSignature: true` 时不发送 `x-timestamp`/`x-signature`/`yy`，用于隔离「签名问题 vs 身份问题」（默认关闭，仅排障用） |

### 4.4 CLI / 凭证抓取扩展

- `npm run capture` 新增 MiniMax 目标：打开 `https://agent.minimax.cn`，**一次性抓五件套**——
  `localStorage["_token"]` → `MINIMAX_CODE_TOKEN`；真实请求 Cookie 头 → `MINIMAX_COOKIE`；
  `user_detail_agent` 的 `realUserID`/`op_ticket`/`token` → `MINIMAX_USER_ID`/`MINIMAX_OP_TICKET`/`MINIMAX_PARAM_TOKEN`；
  `UNIQUE_USER_ID` → `MINIMAX_UUID`（新增 `Target.identityStorage` 与 `Result.extraEnv`，Cookie 复用既有逻辑）；
- 备用人工路径：DevTools → Application → Local Storage → `_token`（token）；
  任意同站请求 → Copy as cURL（cookie）。

## 5. 数据模型

| 变更 | 内容 |
| --- | --- |
| `accounts.json` 新增账号 | `provider: "minimax"`，`credentials`：`token`/`cookie`/`userId`（必需）+ `opTicket`/`paramToken`/`uuid`（有则更好），可选 `options.site` |
| `.env` 新增键 | `MINIMAX_CODE_TOKEN`、`MINIMAX_COOKIE`、`MINIMAX_USER_ID`、`MINIMAX_OP_TICKET`、`MINIMAX_PARAM_TOKEN`、`MINIMAX_UUID`；过期后重新 `npm run capture -- minimax` |
| 快照映射 | `balance`=有效期批次 `remaining` 之和（含签到+购买）`CREDITS`；`credits[]`=逐笔批次（`label`：签到积分/购买积分，`expiresAt`=expire_at_ms，`note`=已用量）；`nextExpiryAt` 由批次自然得出；`windows[]`=1 个 billing 窗口（卡片进度条，口径见 §10） |
| `dailyUsage` | `covers = { grant: true, use: false }`：「增量」用上游到账明细，「用量」回退本地采样（`src/service.ts:282-286` 既有逻辑） |
| 配置迁移 | 无（纯新增） |

## 6. 影响面评估

- **聚合与排序**：新批次进入「最快到期」排序与 `DashboardTotals.availableByCurrency.CREDITS`——签到积分 30 天有效，会被正确纳入；不影响既有平台；
- **缓存**：走 `service.ts` 既有快照/明细缓存（300s），无新增缓存层；
- **前端**：无需改动（卡片渲染完全由快照驱动）；补充窗口后卡片自动出现进度条与图例文案（见 §10）；
- **抓取工具**：`capture.ts` 增加一个目标与一个可选分支，不改既有平台行为；
- **凭证安全**：`_token` 属登录态凭证，只写本地 `.env`（已 gitignore），日志与文档不出现明文（沿用 `mask()`）。
- **回归范围**：其余适配器、`/api/summary`、前端卡片；重点确认 `npm run capture` 对既有平台的输出不变。

## 7. 风险与备选方案

| 风险 | 说明 | 缓解 |
| --- | --- | --- |
| 内部接口未受官方承诺 | 属客户端私有接口，可能变更 | 接口集中在 `minimax.ts` 顶部常量；解析对字段缺失容错；失败降级为 `error` + 保留上次数据 |
| 签名/参数校验 | 服务端是否严格校验 `x-signature`/`yy` 未知（**待确认**） | 完全复刻客户端公式与参数顺序；`npm run probe -- minimax` 实测确认；失败时打印脱敏诊断 |
| 只发 token → 401（已实测） | 真实客户端一律携带 SSO 会话 Cookie（浏览器同源自动带；桌面端走 Electron session 的 `net.fetch`），首版漏了 Cookie 导致 401 | 已修复：capture 同时抓 Cookie，provider 同时发送 `Cookie` 头 |
| 身份参数不能写死（已实测） | 公共参数 `user_id=0` / 派生 uuid 会被服务端与 token 身份交叉校验后 401（客户端读 `user_detail_agent` 的真实值） | 已修复：capture 抓取真实 `userId`/`uuid`/param token 与可选 `opTicket`；`requiredCredentialKeys` 增加 `userId` |
| user_id 精度陷阱（已实测根因） | user_id 为 18 位大整数（> 2^53），`Number.parseInt` 后再序列化会改变末几位，服务端校验必挂 | 已修复：user_id 全程字符串（capture 字符串抓取 → `cred()` 字符串透传 → URLSearchParams 原样序列化）；`.env.example` 与文档均标注 |
| token 有效期 | Web 登录态会过期 | 卡面 `unauthorized` 提示 + 重新 `npm run capture`（与其它平台一致） |
| 明细分页截断 | 批次极多时（>10 页）余额会少算 | 分页上限 10×100；未取全时 note 显式声明「统计未覆盖全部」（**待确认**：可后续改为用会员汇总接口 `op_credit_summary` 交叉校验） |
| 备选方案（否决） | 官方 `v1/token_plan/remains`（订阅 Key） | 只返回 M Plan 用量窗口，**不含签到积分**，不满足目标；留作后续「M Plan 用量窗口」方案 |

## 8. 实施步骤

| 步骤 | 产出物 |
| --- | --- |
| ① 适配器 | `src/providers/minimax.ts`：签名请求封装、积分明细解析、签到状态、`probe()`、`fetchUsageLedger()` |
| ② 注册 | `src/providers/index.ts`：注册 `minimaxAdapter` + 别名（`minimax-code` / `agent.minimax.cn` / `agent.minimax.io`） |
| ③ 抓取 | `src/cli/capture.ts`：`Target.tokenKey` + MiniMax 目标（写 `MINIMAX_CODE_TOKEN`） |
| ④ 配置示例 | `accounts.example.json`、`.env.example` |
| ⑤ 文档 | `README.md` 平台表 + 抓取步骤补充 MiniMax |

## 9. 验证方式

- 静态：`npm run typecheck`；
- 动态（由用户执行，AI 不代跑）：
  1. `npm run chrome:debug` → 登录 `https://agent.minimax.cn` → `npm run capture -- minimax`，确认 `.env` 写入 `MINIMAX_CODE_TOKEN`（终端只显示掩码）；
  2. `npm run probe -- minimax`，预期 `status=ok`：`balance` 与 MiniMax Code「积分 0 + 6,198」的 6,198 一致，`credits` 含 5 条签到批次及各到期日；
  3. 看板核对：卡片显示余额、批次与「最快到期」倒计时；
  4. 回归：`npm run probe`（全平台）确认其它平台不受影响。

## 10. 补充需求：卡片额度进度条（2026-10-11）

**结论：MiniMax 卡片此前固定 `windows: []`，进度条及其下方图例文案整块不渲染；现由已取到的积分明细推导一个 billing 窗口，使卡片与 Trae / CodeBuddy / CodeArts 形态一致，前端零改动、上游零新增请求。**

- 问题现象：看板上 MiniMax 卡片只有余额、批次列表与 note，缺少其它平台都有的额度进度条，以及进度条下方的「剩余 X / Y」图例文案。
- 根因：前端只在 `provider.windows` 里存在 `showMeter` 窗口时才渲染 `.meter`（`public/app.js:420-431`）；而 `src/providers/minimax.ts:471` 固定 `windows: []`（该平台没有 M Plan 用量窗口接口）。
- 口径（本次确定）：

| 项 | 取值 | 理由 |
| --- | --- | --- |
| 统计范围 | `expire_at_ms >= now` 的批次（含已用尽批次） | 已过期批次的剩余已作废、消耗属历史，计入只会稀释当前进度 |
| `limit` | 范围内批次 `granted_amount` 之和 | 即「当前有效期内授予的总量」 |
| `remaining` | 范围内批次 `remaining_amount` 之和 | 与卡片余额 `balance` 严格同口径，图例数字与卡面一致 |
| `used` | `limit - remaining`（下限 0） | 由 `makeWindow` 推导 `usedPercent` |
| `label` | `积分额度（有效期内）` | 与 CodeBuddy「积分额度（N 个资源包）」同风格 |
| `boundary` / `expiresAt` | `expiry` / 范围内最早的 `expire_at_ms` | 该平台无周期重置，只有批次过期 |

- 备选（否决）：账户级汇总接口（如 `op_credit_summary`）——尚未完成取证，不做依赖；窗口完全由既有明细推导。
- 不改动：继续传 `fallbackExpiryFromWindow: false`，窗口不进入「最快到期」时间线；前端不做任何修改。

## 变更记录

| 时间 | 变更内容 | 触发原因 | 影响章节 |
| --- | --- | --- | --- |
| 2026-10-10 | 初版：调研（官方文档 + 客户端静态取证）、方案、接口与风险 | 任务启动，用户要求确认官方有无获取签到积分方式并接入 | 全篇 |
| 2026-10-10 | 代码落地：`src/providers/minimax.ts`（签名请求 + 明细解析 + 签到状态 + 明细接口）、注册与别名、capture 支持 localStorage token、示例与 README | 用户已授权实施；实现中修正「签到/购买拆分」口径为仅统计有效期批次，`fetchUsageLedger` 与明细拉取复用同一函数 | 2.3 / 4.4 / 6 |
| 2026-10-10 | 实测第 1 步通过：`npm run capture -- minimax` 成功抓取 `_token` 并写入 `MINIMAX_CODE_TOKEN`；`accounts.json` 补入 minimax 账号（备份至 `.run/`） | 用户执行抓取并粘贴 probe 输出，发现账号未配置 | 9 |
| 2026-10-10 | 实测发现 401 → 根因：首版漏发 SSO 会话 Cookie；修复：capture 同时抓 `MINIMAX_COOKIE` + token，provider 请求携带 Cookie，凭证要求调整为 token+cookie；`accounts.json` 已补 cookie 引用 | `npm run probe -- minimax` 返回 `HTTP 401`；重读客户端代码确认所有真实路径都带 Cookie | 2.2 / 4.3 / 4.4 / 5 / 7 |
| 2026-10-10 | 补 Cookie 后仍 401（第二轮）→ 根因判断：公共参数 `user_id=0`、派生 `uuid` 与 token 身份不一致，服务端交叉校验拒绝；修复：capture 抓取 `user_detail_agent.realUserID` / `op_ticket` / `token` 与 `UNIQUE_USER_ID`，请求改用真实身份值；新增 `options.skipSignature` 隔离开关 | 用户复测仍 `HTTP 401`；AdvisorTool 策略复核与客户端参数读取逐行比对 | 2.2 / 4.3 / 4.4 / 5 / 7 |
| 2026-10-10 | 第三轮修复（实测根因）：user_id 是 18 位大整数，`Number.parseInt` 造成 float64 精度丢失使末位变化 → 改为全程字符串；补 `screen_width/screen_height` 参数；移除账号对 `MINIMAX_OP_TICKET` 的默认引用（本账号无 op_ticket，真实浏览器同样省略） | capture 实测输出 `user_id=562843343905681410`（>2^53）后逐位核对 | 4.3 / 5 / 7 |
| 2026-10-11 | 新增 §10：卡片补充额度进度条——`probe()` 由有效期批次推导 billing 窗口（limit=授予量之和、remaining=剩余之和、boundary=expiry） | 用户反馈 MiniMax 卡片缺少额度进度条与下方图例文案（对比其它平台卡片） | 5 / 6 / 10 |