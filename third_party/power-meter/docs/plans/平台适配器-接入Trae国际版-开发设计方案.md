# 平台适配器-接入Trae国际版（trae.ai）-开发设计方案

> 文档类型：开发设计方案
> 关联模块：平台适配器
> 创建时间：2026-10-26
> 最后更新：2026-10-26
> 当前状态：已完成（静态检查通过，动态取数待人工验证）
> 关联改动文件：`src/providers/trae-global.ts`（新增）、`src/providers/trae.ts`（仅新增导出与可选参数）、`src/providers/index.ts`、`src/cli/capture.ts`、`.env.example`、`accounts.example.json`、`README.md`、`docs/凭证配置指南.md`

## 1. 背景与目标

### 1.1 背景

- 仓库已支持 Trae 国内版（`trae`，`trae.cn` / `api.trae.cn`），实现见 `src/providers/trae.ts:1-874`。
- Trae 国际版（`trae.ai`）是**另一套部署**：域名、账号体系、计费口径、模型目录都不同。官方客户端的 boot 配置把两边并列：
  - 国内：`api.trae.cn`（`src/providers/trae.ts:45-46`）
  - 国际：`www.trae.ai`（授权页）、`growsg-normal.trae.ai`（账号/令牌）、`api-sg-central.trae.ai` / `api-us-east.trae.ai`（计费面），见本机安装包 `/Applications/Trae.app/Contents/Resources/app/product.json` 的 `bootConfig.account.trae` / `bootConfig.ug.trae` 与 `packageType: "TRAE_I18N"`。
- 两边的 OAuth **协议同形但主机不同**：客户端 ID 相同（`ono9krqynydwx5`，见 `product.json` 的 `iCubeApp.authConfig.TRAE.stable`，与 `src/providers/trae.ts:67` 一致），回调同样固定 `http://127.0.0.1:<port>/authorize`，但授权页与换令牌主机分别是 `www.trae.ai` 与 `growsg-normal.trae.ai`。
- 计费口径不同：国内是「积分包 + `usage_summary`」（`src/providers/trae.ts:23-42` 的实测注释）；国际版是**订阅制**，v1 计费面返回 `user_entitlement_pack_list`，配额字段是 `basic_usage_limit` / `bonus_usage_limit` / `premium_model_fast_request_limit`，且带 `is_dollar_usage_billing` 开关（按美元用量或「快速请求次数」计量），**没有** `usage_summary`。
- 结论：与 Qoder / CodeBuddy 的先例一致，**国际版是独立平台身份**（provider id `trae-global`），不与国内版共用账号、凭证变量或缓存键。

### 1.2 目标

1. 新增平台 `trae-global`（Trae 国际版），看板以独立卡片出现，`region: 'global'`。
2. 取数：调用国际版 v1 计费面，解析 `user_entitlement_pack_list`，按计量维度输出余额、批次（带到期时间）、窗口。
3. 凭证：支持三条路径，全部复用现有能力——
   - 手工粘贴 Cookie（`TRAE_GLOBAL_COOKIE`，Cookie 换 JWT）；
   - 「打开链接登录」（OAuth + 本地回调，写 `TRAE_GLOBAL_ACCESS_TOKEN` / `TRAE_GLOBAL_REFRESH_TOKEN` / `TRAE_GLOBAL_DEVICE_KEY`）；
   - 命令行抓取 `npm run capture -- trae-global`（兼容 `--site global`）。
4. 凭证变量前缀独立：`TRAE_GLOBAL_*`，与国内版 `TRAE_*` 互不覆盖（`src/account-store.ts:110-133` 的默认推导对 `trae-global` 恰好给出 `TRAE_GLOBAL`，仍显式声明一遍以便阅读）。
5. 国内版行为**零改动**：`trae.ts` 只新增导出与带默认值的可选参数，不改既有分支。

### 1.3 Non-Goals

- 不做账号迁移（国际版与国内版账号互不通用，无存量数据）。
- 不做签到 / 领奖 / 活动（国际版网关上 `/trae/api/v2/ug/*` 不存在，见 `docs/fixes` 之外的社区实测记录；本方案只读，不写账号状态）。
- 不为国际版积分编造人民币单价（`src/credit-value.ts:169` 对非 `CREDITS` 单位直接返回 `null`；国际版无公开积分包单价，保持「不折算」）。
- 不改变「一个平台一个站点」的看板结构。

## 2. 现状调研

### 2.1 现有能力可复用清单

| 能力 | 位置 | 复用方式 |
| --- | --- | --- |
| 适配器注册与别名 | `src/providers/index.ts:16-31`、`:57-88` | 注册 `traeGlobalAdapter`；补 `trae.ai` / `www.trae.ai` / `trae-global` 别名 |
| 平台契约（probe / 登录 / 管理表单） | `src/providers/base.ts:195-300` | 直接实现 |
| 批次与窗口构造 | `src/providers/base.ts:344-362`（`makeLot`）、`:303-341`（`makeWindow`）、`:435-488`（`buildSnapshot`） | 直接复用 |
| 失败/未配置文案 | `src/providers/base.ts:524-548` | 直接复用 |
| OAuth 本地回调会话 | `src/login-session.ts:165-240`、`src/server.ts:606-650`（`/authorize`） | 平台无关；适配器声明 `handleCallback` 即可 |
| Cookie / cURL 解析 | `src/providers/base.ts:595-625` | 直接复用 |
| `.env` 槽位分配 | `src/account-store.ts:137-160` | 声明 `envNames` 即可 |
| 通用增删账号 | `src/account-store.ts:560-667`、`public/app.js` 的 `renderManage` | 声明 `manageAccounts: true` + `manageFields: [cookie]` |
| 命令行抓取 | `src/cli/capture.ts:213-296`（`TARGETS`） | 新增独立 Target + `trae` 的 `siteVariants.global` |
| PKCE / EC 设备密钥 / DeviceProof | `src/providers/trae.ts:95-136` | 从 `trae.ts` 导出（纯新增），国际版复用同一套密码学 |

### 2.2 国际版协议证据

证据分三级：**A** = 本机官方客户端的真实请求日志/安装包常量；**B** = 社区开源实现的实测记录；**C** = 待确认（本机当前网络无法直连 `*.trae.ai`，见 §7 风险）。

| 项 | 结论 | 等级 / 证据 |
| --- | --- | --- |
| 授权页 | `https://www.trae.ai/authorization`，参数与国内版同形（`login_version` / `auth_from=trae` / `login_channel=native_ide` / `client_id` / `auth_callback_url` / `code_challenge` …） | **A** 本机 `Trae.app` `out/main.js` 的 `loginUrlBuilder`（`${consoleHost}/authorization?...`）与 `product.json` 的 `bootConfig.condition.website: ["www.trae.ai","trae.ai"]` |
| 回调 | 固定 `http://127.0.0.1:<port>/authorize` | **A** 同上 `main.js`（`http://127.0.0.1:${port}/authorize`） |
| 客户端 ID | `ono9krqynydwx5`（与国内版相同） | **A** `product.json` 的 `iCubeApp.authConfig.TRAE.stable` |
| 换令牌 / 续期 | `POST {账号主机}/trae/api/v3/oauth/ExchangeToken`（`DeviceProof` 签名同国内版） | **A** 本机 `Trae.app` 日志出现该端点；**B** 社区实现同路径 |
| 账号主机 | `https://growsg-normal.trae.ai`（SG）；另有 `grow-normal.trae.ai` / `grow-normal.traeapi.us` | **A** 本机日志：`growsg-normal.trae.ai/cloudide/api/v3/trae/GetUserInfo`、`/trae/api/v1/pay/ide_user_pay_status`；`product.json` 的 `bootConfig.account.trae` / `ug.trae` |
| Cookie 换 JWT | `POST {计费/账号主机}/cloudide/api/v3/common/GetUserToken`，响应 `{Result:{Token,ExpiredAt,UserID,TenantID}}` | **B** `kivenZhou/traehop`（`api-sg-central.trae.ai` / `api-us-east.trae.ai`）、`Yang-505/Trae-Account-Manager`；响应信封与国内版同形（`src/providers/trae.ts:477-489`） |
| 计费面主机 | `https://api-sg-central.trae.ai`（SG）、`https://api-us-east.trae.ai`（US）；Cookie `store-idc=useast` / `trae-target-idc=useast` 判 US | **B** 上述两个社区实现；**A** `product.json` 的 `bootConfig` 未列出这两个主机（属 web 控制台侧） |
| 用量接口 | `POST {计费面}/trae/api/v1/pay/user_current_entitlement_list`（body `{}` 或 `{require_usage:true}`），**v1**，不是国内的 v2 | **B** `magpie-community/plugins`（`packages/trae/index.mjs` 的 `trae-global.usage.path`）、`kivenZhou/traehop`、`Yang-505/Trae-Account-Manager`；**A** 本机 `main.js` 对非 CN 用 `v1`：`providerCode===CN ? "/trae/api/v2/pay/ide_user_pay_status" : "/trae/api/v1/pay/ide_user_pay_status"` |
| 备用用量接口 | `POST {账号主机}/trae/api/v1/pay/ide_user_ent_usage`（body `{require_usage:true}`），返回同样的 `user_entitlement_pack_list` | **B** `mmqz/cpa-multi-plugins` 的 `plugins/trae/intl_pay.go`（实测 intl 打 v2 会 401/4014） |
| 订阅状态 | `POST {账号主机}/trae/api/v1/pay/ide_user_pay_status` → `user_pay_identity_str`（如 `Free` / `Pro`）、`last_pro_entitlement_expire_time`、`trial_status` | **A** 本机日志实测 200；**B** `edison7009/EchoBird` 的 `quota.rs` 用它取 `plan` |
| 逐笔用量 | `POST {计费面}/trae/api/v1/pay/query_user_usage_group_by_session`（`start_time`/`end_time`/`page_size`/`page_num`，无 `usage_type`） | **C** 仅 `Yang-505/Trae-Account-Manager` 使用；是否对国际版账号返回数据未验证 |
| 签到族 | `/trae/api/v2/ug/*` 在国际版网关**不存在** | **B** 社区记录（`dsh-connect-trae` 的 `INTL_SG_EVIDENCE.md` §2.3）；本方案不调用 |

### 2.3 关键字段（v1 `user_entitlement_pack_list`）

```text
is_dollar_usage_billing            bool
user_entitlement_pack_list[]:
  display_desc                     string（套餐名，如 "Pro plan" / "Free"）
  group_type                       number（1..5 为奖励类包，可为空）
  entitlement_base_info:
    product_type                   number（0 = Free；2 = 增购/礼包；100/6/4/1/8 = 订阅档）
    end_time                       number（秒）
    currency                       number（0/1 为积分口径）
    available_endpoint             number（0 = 通用，1 = Work）
    quota: { basic_usage_limit, bonus_usage_limit, no_bonus_quota,
             premium_model_fast_request_limit, advanced_model_request_limit,
             credits_limit }
    product_extra: { subscription_extra: {quota}, package_extra: {quota} }
  usage: { basic_usage_amount, bonus_usage_amount,
           premium_model_fast_amount, credits_amount }
```

（字段来源：`mmqz/cpa-multi-plugins` 的 `intl_pay.go` 结构体、`edison7009/EchoBird` 的 `quota.rs` 解析、`kivenZhou/traehop` 的 `parseEntitlementsToSummary`。数值为**浮点**，与国内 v2 的整数口径不同。）

## 3. 方案设计

### 3.1 整体思路

- 新文件 `src/providers/trae-global.ts` 承载国际版全部差异（主机表、v1 计费面解析、美元/次数口径），**不改造** `trae.ts` 的既有分支；只从 `trae.ts` 导出可复用的密码学与回调解析工具（纯新增）。
- 取数链路：`Cookie/令牌 → resolveJwt → 计费面候选链 → 解析 pack 列表 → 余额 + 批次 + 窗口 → buildSnapshot`。
- 计量维度按账户选定**一次**，整张卡片统一，避免同一张卡混币种：

| 条件 | 计量维度 | `currency` | 窗口标签 |
| --- | --- | --- | --- |
| `is_dollar_usage_billing === true` 且存在 `basic_usage_limit` | 美元用量 | `USD` | 美元用量（本期） |
| 否则存在 `credits_limit > 0` | 积分 | `CREDITS` | 积分额度（本期） |
| 否则 | 快速请求次数 | `POINTS` | 快速请求次数（本期） |

> 选 `POINTS` 而不是 `CREDITS`：国际版该维度的单位是「请求次数」，叫积分会误导；`credit-value.ts` 对 `POINTS` 不折算，正好符合「没有官方单价就不编数字」。

### 3.2 主机与候选链

| 角色 | 候选（按顺序） |
| --- | --- |
| 授权页 | `https://www.trae.ai`（固定） |
| 账号 / 令牌 | `options.apiHost` → 本区域账号主机 → 另一区域账号主机 |
| 计费面（用量） | `options.usageUrl` 的 origin → 本区域计费面 → 另一区域计费面 |
| 用量路径 | `/trae/api/v1/pay/user_current_entitlement_list` → `/trae/api/v1/pay/ide_user_ent_usage` |

区域由 Cookie 的 `store-idc` / `trae-target-idc` 判定（`useast` → US，其余 → SG），
两区域的主机表（`src/providers/trae-global.ts` 的 `GLOBAL_REGION_HOSTS`）：

| 区域 | 账号 / 令牌 | 计费面 |
| --- | --- | --- |
| SG | `growsg-normal.trae.ai`、`grow-normal.trae.ai` | `api-sg-central.trae.ai`、`growsg-normal.trae.ai` |
| US | `grow-normal.traeapi.us`、`grow-normal.trae.ai` | `api-us-east.trae.ai`、`grow-normal.traeapi.us` |

- 候选链遇到任何失败都继续尝试，但**同一主机上换路径**只在拿到 HTTP 响应时才做：
  网络层失败（无 HTTP 状态）说明整台主机不可达，直接跳下一台，避免白等超时。
- 拿到「带 `user_entitlement_pack_list` 的成功响应」即停；业务码非 0 视为失败。
- `options.apiHost` / `options.usageUrl` 仍作为显式覆盖（与国内版 `src/providers/trae.ts:543`、`:794` 同一约定），便于代理或自定义域名。
- 回调里的 `apiHost` / `consoleHost` 必须过白名单：仅 `*.trae.ai` 与 `*.traeapi.us`（国际版两个部署的域名），否则回落到默认值（对齐 `src/providers/trae.ts:271-281` 的 SSRF 防护）。

### 3.3 职责划分

| 文件 | 职责 |
| --- | --- |
| `src/providers/trae-global.ts` | 国际版适配器：主机表、令牌解析、v1 计费面解析、登录、`probe`、`fetchUsageLedger` |
| `src/providers/trae.ts` | 不变；新增导出 `traePkce` / `traeGenerateDeviceKey` / `traeDeviceKeyFromPem` / `traeDeviceProof` / `traeDeviceInfo` / `traeReadCallback` / `traeCallbackUrl`，`traeHostAllowed` 增加可选域名参数，`mergeLots` 增加可选币种与 id 前缀 |
| `src/providers/index.ts` | 注册 + 别名 |
| `src/cli/capture.ts` | `trae-global` 抓取目标 + `trae` 的 `--site global` 变体 |

## 4. 接口设计

无新增 HTTP API（沿用 `/api/summary`、`/api/login/*`、`/api/accounts*`）。

新增平台适配器契约实现：

| 字段 | 值 |
| --- | --- |
| `id` | `trae-global` |
| `displayName` | `Trae 国际版` |
| `region` | `global` |
| `dashboardUrl` / `usageUrl` | `https://www.trae.ai/account-setting#usage` |
| `requiredCredentialKeys` | `['cookie']` |
| `envNames` | `{ cookie: TRAE_GLOBAL_COOKIE, accessToken: TRAE_GLOBAL_ACCESS_TOKEN, refreshToken: TRAE_GLOBAL_REFRESH_TOKEN, deviceKey: TRAE_GLOBAL_DEVICE_KEY }` |
| `isConfigured` | 有 `cookie` 或 `accessToken` |
| `manageAccounts` / `manageFields` | `true` / `[cookie]` |
| `login` | 「打开链接登录」，`qr: false`，回调 `/authorize` |
| `fetchUsageLedger` | **仅积分口径账户**：逐笔用量（v1 `query_user_usage_group_by_session`，`fetchAllPages()` 取全）+ 当前有效包的发放时间点；`covers` 只声明 `use`（上游不给历史发放流水，增量维度仍走本地采样）；美元 / 次数账户直接返回 `null`（逐笔接口只回积分，接上会混单位） |

别名（`resolveAdapter`）：`trae.ai`、`www.trae.ai`、`trae-global` → `trae-global`。

## 5. 数据模型

- `types.ts` **不变**：国际版用既有 `ProviderSnapshot` / `CreditLot` / `QuotaWindow` / `Currency`。
- `.env` 新增 4 个变量（`.env.example`）：

| 变量 | 含义 |
| --- | --- |
| `TRAE_GLOBAL_COOKIE` | 浏览器登录态 Cookie（换 JWT） |
| `TRAE_GLOBAL_ACCESS_TOKEN` | 登录产出的 `Cloud-IDE-JWT` |
| `TRAE_GLOBAL_REFRESH_TOKEN` | 续期用 refresh token |
| `TRAE_GLOBAL_DEVICE_KEY` | DeviceProof 用的 EC P-256 私钥（PKCS8 PEM 的 base64） |

- `accounts.json` 新增一条示例：`provider: "trae-global"`，`credentials.cookie: "${TRAE_GLOBAL_COOKIE}"`。
- 无配置迁移：`trae` 的既有账号、缓存键、卡片全部不变。

## 6. 影响面评估

| 面 | 影响 |
| --- | --- |
| 国内版 `trae` | 无行为变化；`trae.ts` 仅新增导出与带默认值的可选参数 |
| 聚合 / 缓存 | 无改动；`trae-global` 是新的 provider 键，缓存与本地采样独立 |
| 前端 | 无改动；卡片按 `describeRegistry()` 声明渲染（区域徽标自动显示「国际」） |
| 「最快到期」时间线 | 国际版批次带 `end_time`，会进入时间线；币种为 `USD` / `POINTS` / `CREDITS` 各自成桶 |
| 人民币折算 | 国际版 `USD` / `POINTS` 不折算（`src/credit-value.ts:169`）；若某账号是 `CREDITS` 口径，也无官方单价 → 不折算（不编数字） |
| 日级用量 | 国际版逐笔接口**未实测**；只有积分口径账户才尝试，第一页无记录即判定不可用 → 本地采样兜底 |
| 凭证安全 | 新增变量仍只写本机 `.env`（已 gitignore）；文档与日志不出现明文 |

## 7. 风险与备选方案

| 风险 | 影响 | 应对 |
| --- | --- | --- |
| 国际版计费面主机/路径未在本机实测（本机当前无法直连 `*.trae.ai`） | 首次取数可能落空 | 候选链覆盖 3 个主机 × 2 个路径；失败文案带上实际尝试的主机，便于用户回报；文档标注「待确认」 |
| `user_current_entitlement_list` 在不同网关返回平铺 / `{code,data}` 信封两种形态 | 解析失败 | 解析前统一解信封（与 `mmqz/cpa-multi-plugins` 的容错一致） |
| 国际版计量维度随套餐变化（美元 / 次数 / 积分） | 卡片口径混淆 | 按账户选定单一维度（`pickDimension`：美元计费且 basic/bonus 有额度 → USD；否则有 `credits_limit` → CREDITS；否则 POINTS），窗口标签写明单位；`note` 里补一句口径说明 |
| 逐笔用量接口对国际版账号可能返回空 | 日级用量恒 0 | 首页无数据即判定「不可用」→ 返回 `null` 退回本地采样，不谎报 upstream；增量维度恒定走本地采样 |
| `is_dollar_usage_billing` 为 true 时余额是「美元额度」，与国内积分不可比 | 汇总时不同单位混算 | 汇总层本就按币种分桶（`src/aggregate.ts:159-197`），不跨币种相加 |

**被否决的备选**：

1. **把国际版做成 `trae` 的 `options.site`**：与 Qoder / CodeBuddy 已确立的「站点即平台身份」相悖，会让账号列表、缓存键、登录入口混在一起（见 `docs/plans/前端看板-平台与账号按站点拆分-开发设计方案.md`）。
2. **工厂化重构 `trae.ts`（`createTraeAdapter(siteId)`）**：能少一份代码，但国内版是当前唯一在跑的生产适配器，重构面大、回归成本高；本轮以「新增文件 + 纯新增导出」换取零回归风险。后续如需收敛，再按 Qoder 的模式整体迁移。
3. **复用国内版 v2 `usage_summary` 口径**：国际版不返回该字段，硬套会得到 0 余额。

## 8. 实施步骤

| 步骤 | 产出物 |
| --- | --- |
| 1 | `trae.ts` 导出密码学/回调工具；`mergeLots` 增加可选 `currency` / `idPrefix`；`traeHostAllowed` 增加可选域名参数（默认行为不变） |
| 2 | 新增 `src/providers/trae-global.ts`：主机表、令牌解析（Cookie / refreshToken / 显式 accessToken）、v1 计费面解析、`probe`、`fetchUsageLedger`、OAuth 登录 |
| 3 | `src/providers/index.ts` 注册 `traeGlobalAdapter` + 别名 |
| 4 | `src/cli/capture.ts` 新增 `trae-global` 目标与 `trae` 的 `--site global` 变体 |
| 5 | `.env.example` / `accounts.example.json` / `README.md` / `docs/凭证配置指南.md` 同步 |
| 6 | `npm run typecheck` + 只读核对（不做动态验证） |

## 9. 验证方式

静态（本轮执行）：

- `npm run typecheck` 通过；
- 只读核对：注册表别名、`envNames` 推导、capture 目标、示例配置与实际适配器字段一致；
- **离线夹具自测**（`npx tsx` 跑临时脚本，`fetch` 全部打桩，**不触达上游**）：
  - `parseGlobalUsage`：美元 / 美元 bonus-only / 次数 / 积分（含 `{code,data}` 信封）/ 增购包 / 业务错误 6 组夹具结果符合预期；
  - `probe`：候选链按区域回退、`plan` 取自 `user_pay_identity_str`、余额/批次/窗口口径正确；
  - 错误映射：未配置 → `notConfigured`、HTTP 401 → `unauthorized`、网络层失败 → `error`；
  - 凭证三形态：显式 accessToken 不打 `GetUserToken`；refreshToken + deviceKey 走 `ExchangeToken` 并写回；Cookie 换 JWT；
  - 登录：授权页主机/路径、`auth_callback_url`、PKCE 参数正确，无授权码的回调被拒绝；
  - 回归：国内版 `trae` 仍打 `api.trae.cn`、币种仍为 `CREDITS`；`mergeLots` 默认参数行为不变；
  - 逐笔明细分页：按 AGENTS.md §9 走 `fetchAllPages()`，`complete=false` 时整份退回本地采样（不声明 `covers`）；
  - `covers.grant` 恒为 `false`（只看到当前有效包，看不到历史发放，不能声称增量维度完整）；
  - `npm run test:pagination`：8/8 通过（离线纯函数断言，不发上游）。

动态（人工执行，本轮不跑）：

| 用例 | 步骤 | 预期 |
| --- | --- | --- |
| Cookie 取数 | `.env` 写 `TRAE_GLOBAL_COOKIE`，`accounts.json` 加 `trae-global` 账号 | `npm run probe -- trae-global` 输出 `ok`，余额与 `trae.ai/account-setting#usage` 页面一致 |
| 链接登录 | 卡片「＋ 登录 / 添加账号」→「打开链接登录」 | 浏览器完成授权后自动写入 `TRAE_GLOBAL_*` 并注册账号 |
| 令牌续期 | 等 JWT 过期后再 probe | 用 refreshToken 续期成功，`.env` 中的 accessToken 被刷新 |
| 国内版回归 | `npm run probe -- trae` | 与改动前一致 |
| 抓取 | `npm run capture -- trae-global` | 写入 `TRAE_GLOBAL_COOKIE`；`--site global` 等价 |

## 变更记录

| 时间 | 变更内容 | 触发原因 | 影响章节 |
| --- | --- | --- | --- |
| 2026-10-26 | 初版：确定 `trae-global` 独立平台、v1 计费面候选链、计量维度三选一 | 任务启动 | 全篇 |
| 2026-10-26 | 实施后回填：主机改为按区域分组的静态表（`GLOBAL_REGION_HOSTS`）；回调白名单补 `traeapi.us`；`fetchUsageLedger` 限定为积分口径账户；网络层失败不再逐路径重试；`dimensionAmounts` 改为「取第一个 > 0 的候选」 | 编码时发现取数口径与超时行为与初稿不一致 | §3.2、§4、§6、§7 |
| 2026-10-26 | 逐笔明细分页改为 `fetchAllPages()`（AGENTS.md §9 新增强制规范）：未取全整份退回本地采样，不声明 `covers` | 仓库新增取数完整性硬约束 | §4、§9 |
