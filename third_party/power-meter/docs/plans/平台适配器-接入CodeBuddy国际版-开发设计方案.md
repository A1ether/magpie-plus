# 平台适配器-接入CodeBuddy国际版（WorkBuddy）-开发设计方案

> 文档类型：开发设计方案
> 关联模块：平台适配器
> 创建时间：2026-10-25
> 最后更新：2026-10-25
> 当前状态：已完成
> 关联改动文件：`src/providers/codebuddy.ts`、`src/providers/index.ts`、`src/cli/capture.ts`、`.env.example`、`accounts.example.json`、`README.md`、`docs/凭证配置指南.md`

## 1. 背景与目标

### 1.1 背景

- 仓库已支持 CodeBuddy 国内版（`codebuddy`，`www.codebuddy.cn` / 插件后端 `copilot.tencent.com`），实现位于 `src/providers/codebuddy.ts`。
- 代码注释与《账号管理-扫码与链接登录-开发设计方案》已确认：**WorkBuddy 客户端 = CodeBuddy 国际版**（`src/providers/codebuddy.ts:53-54` 明确「协议来自官方 WorkBuddy 客户端的实测行为」；`docs/plans/账号管理-扫码与链接登录-开发设计方案.md:37` 记录了「不做 CodeBuddy 国际站（WorkBuddy）」的 v1 决策，本次正是补上它）。
- 国际版与国内版共用腾讯同一套协议（`v2/plugin/auth/*`、`v2/billing/meter/get-user-resource`），但**计费域、账号体系、登录态互不通用**——与国内版必须拆成两个平台（参考 Qoder 国内版 / 国际版彻底分离的先例：`src/providers/qoder.ts` 的 `createQoderAdapter` 工厂 + `qoder-intl` 独立 provider）。

### 1.2 目标

1. 新增平台 `codebuddy-intl`（WorkBuddy 国际版），看板上以独立卡片出现，与国内版互不干扰。
2. **支持在前端添加账号**，覆盖三条路径（全部复用现有能力）：
   - 手动粘贴 Cookie（管理表单，复用平台无关的通用增删实现）；
   - 「扫码登录」（腾讯插件认证，签发 accessToken / refreshToken）；
   - 「网页端登录」（服务端驱动本机调试 Chrome 抓取国际站 Cookie，参考 Qoder 的 `webLogin`）。
3. 命令行抓取支持：`npm run capture -- codebuddy-intl`。
4. 凭证写入独立的 `.env` 变量前缀（`CODEBUDDY_GLOBAL_*`），与国内版 `CODEBUDDY_*` 互不覆盖。

### 1.3 Non-Goals

- 不做账号迁移工具（国内版 / 国际版账号体系独立，无存量 `codebuddy-intl` 账号需要迁移）。
- 不改变国内版 `codebuddy` 的任何行为（存量账号、缓存键、卡片全部不动）。
- 不在本轮实测国际站真实接口（静态检查 + 文档标注「待确认」；见 §7 风险）。
- 不新增「按平台拆站点」的 UI 改造——`codebuddy-intl` 是独立平台，天然落在现有「按平台拆分」的看板结构里。

## 2. 现状调研

### 2.1 现有能力可复用清单

| 能力 | 位置 | 复用方式 |
| --- | --- | --- |
| 平台注册表与别名 | `src/providers/index.ts:16-82` | 注册 `codebuddyIntlAdapter`，补 `workbuddy.ai` 等域名别名 |
| 双模式探测（Cookie 优先 / 令牌兜底） | `src/providers/codebuddy.ts:971-1082` | 工厂化：把 URL / UA / 身份头 / envNames 抽成站点参数 |
| 令牌模式（accessToken / refreshToken / 自动续期 / 写回 .env） | `src/providers/codebuddy.ts:294-471` | 同上，刷新端点与身份头按站点切换 |
| 用量 + 获取明细（`fetchUsageLedger`） | `src/providers/codebuddy.ts:768-862` | 同上，令牌态明细 `cbTokenRequestUsage` 的插件后端主机按站点切换 |
| 扫码登录（腾讯插件认证） | `src/providers/codebuddy.ts:871-968` | 参数化 `CB_PLUGIN_BASE` / `CB_BILLING_ORIGIN` |
| 「网页端登录」CDP 抓取 | `src/providers/qoder.ts:1235-1360`（`makeQoderWebLogin`）、`src/lib/cdp.ts:353-420`（`openCookieCollector` / `ensureDebugChrome` / `cookieNamesOf` / `sawSuccessfulRequest`） | 抽成 CodeBuddy 版 `makeCodeBuddyWebLogin(site)`，登录态判据改为国际站的会话 Cookie 名 |
| 通用「添加 / 删除账号」 | `src/account-store.ts:560-667`（`addAccount`）、`public/app.js`（`renderManage` 按 `manageFields` 渲染） | 声明 `manageAccounts: true` + `manageFields: [cookie]` 即自动获得前端表单 |
| 登录会话层（发起 / 轮询 / 落库） | `src/login-session.ts:165-310` | 平台无关，声明 `loginMethods` 后自动可用 |
| 命令行抓取 | `src/cli/capture.ts:253-260`（`codebuddy` Target） | 新增 `codebuddy-intl` Target 或 `codebuddy` 的 `siteVariants` |
| `.env` 槽位分配 | `src/account-store.ts:600-639`（`envNameFor` / `slotEnvName`） | 适配器声明 `envNames` 静态表即可 |

### 2.2 关键证据（文件:行号）

- 国际版身份：`src/providers/codebuddy.ts:53-54`「协议来自官方 WorkBuddy 客户端的实测行为」；`src/providers/codebuddy.ts:76-87` 的 UA 常量已按 `WorkBuddy/<ver>` 形态拼装。
- 双模式判断：`src/providers/codebuddy.ts:971-985`（无 Cookie 且无 accessToken → `notConfigured`；否则 Cookie 优先）。
- 令牌端点：`src/providers/codebuddy.ts:63-65`（`CB_PLUGIN_BASE = https://copilot.tencent.com`、`CB_BILLING_ORIGIN = https://www.codebuddy.cn`、`CB_USER_RESOURCE_URL`）。
- 扫码登录协议：`src/providers/codebuddy.ts:53-59` 的五步协议注释（state → token → account → refresh → billing）。
- 登录态判据（网页端抓取）：`src/providers/qoder.ts:1284-1293`（`loginCookieNames` + `sawSuccessfulRequest('/api/v1/me')`）。
- Qoder 国际版独立平台先例：`src/providers/index.ts:18-30`（`qoderIntlAdapter` 注册）、`src/providers/qoder.ts:1374-1385`（`QODER_ENV_NAMES` 静态表）、`src/cli/capture.ts:236-244`（独立 Target）。
- `.env` 国际版前缀先例：`.env.example:32-38`（`QODER_GLOBAL_COOKIE` / `QODER_GLOBAL_ACCESS_TOKEN` / `QODER_GLOBAL_REFRESH_TOKEN`）。

## 3. 方案设计

### 3.1 整体思路

**在 `src/providers/codebuddy.ts` 内做「站点工厂化」**，与 Qoder 的 `createQoderAdapter(siteId)` 同构：

1. 把现有 `codebuddyAdapter` 抽成 `createCodeBuddyAdapter(siteId: 'cn' | 'global')`；
2. 站点差异收敛为一张 `CODEBUDDY_SITES` 元信息表（origin / pluginBase / UA / envNames / 登录态 Cookie 名 / 文案）；
3. 导出两个适配器：`codebuddyAdapter`（现有，行为不变）+ `codebuddyIntlAdapter`（新增，国际版）；
4. 在 `src/providers/index.ts` 注册 `codebuddyIntlAdapter` 并补域名别名；
5. `src/cli/capture.ts` 新增 `codebuddy-intl` Target（或 `codebuddy` 的 `siteVariants.global`）；
6. 前端零改动：`describeRegistry()` 已把 `manageAccounts` / `manageFields` / `loginMethods` 完整透出（`src/providers/index.ts:84-151`），看板自动出现「添加账号 / 扫码登录 / 网页端登录」。

### 3.2 数据流

```
用户（看板）                服务端                              上游
   │  添加账号 / 扫码 / 网页端登录
   ▼
public/app.js ──► /api/login/start ──► src/login-session.ts
                                          │  start / poll
                                          ▼
                              src/providers/codebuddy.ts（国际版站点参数）
                                          │  POST {pluginBase}/v2/plugin/auth/state
                                          │  GET  {pluginBase}/v2/plugin/auth/token
                                          │  POST {billingOrigin}/v2/billing/meter/get-user-resource
                                          ▼
                                    WorkBuddy 国际版后端
                                          │
   ◄──────────────────────────────  ready + credentials（accessToken/refreshToken）
   │
   ▼
src/account-store.ts  landLoginAccount()
   │  ① probe 验证（真实取数）
   │  ② 分配 .env 槽位（CODEBUDDY_GLOBAL_*）
   │  ③ 写 accounts.json（provider: codebuddy-intl）
   ▼
看板新卡片出现，后续 probe / fetchUsageLedger 走同一适配器
```

### 3.3 职责划分

| 层 | 文件 | 职责 |
| --- | --- | --- |
| 适配器层 | `src/providers/codebuddy.ts` | 站点工厂、双模式探测、令牌续期、登录方法（扫码 + 网页端）、字段声明 |
| 注册层 | `src/providers/index.ts` | 注册 `codebuddyIntlAdapter`、补域名别名 |
| CLI 层 | `src/cli/capture.ts` | `codebuddy-intl` 抓取目标（usageUrl / apiHost / cookieSites / envKey） |
| 配置层 | `.env.example`、`accounts.example.json` | 新增 `CODEBUDDY_GLOBAL_*` 占位符与示例账号 |
| 文档层 | `README.md`、`docs/凭证配置指南.md` | 补国际版说明（引用变量名，不含明文） |
| 前端层 | `public/app.js` | **零改动**（按 `describeRegistry()` 声明自动渲染） |
| 编排层 | `src/service.ts`、`src/aggregate.ts` | **零改动**（按 provider id 分发，无平台硬编码） |

## 4. 接口设计

### 4.1 平台契约（`ProviderAdapter`）

| 字段 | 国内版（现有） | 国际版（新增） |
| --- | --- | --- |
| `id` | `codebuddy` | `codebuddy-intl` |
| `displayName` | `CodeBuddy` | `WorkBuddy 国际版` |
| `region` | `cn` | `global` |
| `dashboardUrl` / `usageUrl` | `https://www.codebuddy.cn/profile/plans-usage` | 国际站用量页（域名待确认，见 §7） |
| `requiredCredentialKeys` | `['cookie']` | `['cookie']` |
| `envNames` | `{ accessToken: 'CODEBUDDY_ACCESS_TOKEN', refreshToken: 'CODEBUDDY_REFRESH_TOKEN' }` | `{ cookie: 'CODEBUDDY_GLOBAL_COOKIE', accessToken: 'CODEBUDDY_GLOBAL_ACCESS_TOKEN', refreshToken: 'CODEBUDDY_GLOBAL_REFRESH_TOKEN' }` |
| `isConfigured` | `cookie \|\| accessToken` | 同左 |
| `manageAccounts` / `manageFields` | `true` + `[cookie]` | `true` + `[cookie]` |
| `loginMethods` | `[扫码登录]` | `[扫码登录, 网页端登录]` |

### 4.2 新增 / 变更的 HTTP API

无。`/api/login/*`、`/api/accounts/*`、`/api/providers`、`/api/summary` 全部平台无关，自动覆盖新平台。

### 4.3 上游接口（按站点参数化）

| 用途 | 国内版 | 国际版（站点参数） |
| --- | --- | --- |
| 扫码 state / token / account | `https://copilot.tencent.com/v2/plugin/auth/*`（`platform=workbuddy`） | `https://www.workbuddy.ai/v2/plugin/auth/*`（`platform=workbuddy-ai`，2026-10-25 修复：多个独立实现实测确认，修复前误用国内版主机） |
| 余额 / 资源包（令牌模式） | `https://www.codebuddy.cn/v2/billing/meter/get-user-resource` | `{billingOrigin}/v2/billing/meter/get-user-resource` |
| 余额 / 资源包（Cookie 模式） | `https://www.codebuddy.cn/billing/meter/get-user-resource-summary` 等 | `{billingOrigin}/billing/meter/get-user-resource-summary` 等 |
| 逐笔用量明细 | `https://copilot.tencent.com/billing/meter/get-user-request-usage` → 网页域兜底 | `{pluginBase}/billing/meter/get-user-request-usage` → `{billingOrigin}` 兜底 |
| 礼包 | `https://www.codebuddy.cn/billing/meter/check-gift-claimed` | `{billingOrigin}/billing/meter/check-gift-claimed` |

### 4.4 CLI 命令

```bash
# 国际版（独立平台）
npm run capture -- codebuddy-intl

# 兼容写法（siteVariants 等价）
npm run capture -- codebuddy --site global
```

输出：抓取到的 Cookie 写入 `CODEBUDDY_GLOBAL_COOKIE`（第 N 个账号为 `CODEBUDDY_GLOBAL_COOKIE_N`），并提示 `npm run account -- --provider codebuddy-intl` 完成注册。

## 5. 数据模型

### 5.1 `types.ts` 契约

**零变更**。`ProviderSnapshot` / `CreditLot` / `QuotaWindow` / `UsageLedger` 均平台无关。

### 5.2 `accounts.json` 新增示例

```json
{
  "provider": "codebuddy-intl",
  "displayName": "WorkBuddy 国际版",
  "enabled": true,
  "credentials": {
    "cookie": "${CODEBUDDY_GLOBAL_COOKIE}"
  },
  "options": {}
}
```

或令牌模式（扫码登录自动写入）：

```json
{
  "provider": "codebuddy-intl",
  "displayName": "WorkBuddy 国际版 · 登录账号",
  "enabled": true,
  "credentials": {
    "accessToken": "${CODEBUDDY_GLOBAL_ACCESS_TOKEN}",
    "refreshToken": "${CODEBUDDY_GLOBAL_REFRESH_TOKEN}"
  },
  "options": {
    "uid": "<平台用户ID>",
    "enterpriseId": "<可选>",
    "domain": "<可选>"
  }
}
```

### 5.3 `.env` 新增配置项

| 变量 | 用途 | 写入方式 |
| --- | --- | --- |
| `CODEBUDDY_GLOBAL_COOKIE` | 国际版会话 Cookie（手动 / capture / 网页端登录） | 手动粘贴或 `npm run capture -- codebuddy-intl` |
| `CODEBUDDY_GLOBAL_ACCESS_TOKEN` | 国际版插件令牌（取数必需） | 扫码登录自动写入 |
| `CODEBUDDY_GLOBAL_REFRESH_TOKEN` | 国际版续期令牌（自动写回） | 扫码登录自动写入 |

> 是否涉及已有配置迁移：**否**。存量 `CODEBUDDY_*` 与 `codebuddy` 账号完全不动。

## 6. 影响面评估

| 影响点 | 是否受影响 | 说明 |
| --- | --- | --- |
| 适配器 `codebuddy` | 否 | 工厂化后行为不变；`codebuddyAdapter` 仍导出，注册顺序不变 |
| 适配器 `qoder` / `trae` / `codearts` / … | 否 | 无交叉引用 |
| 聚合 `src/aggregate.ts` | 否 | 按 provider id 分发；`codebuddy-intl` 不声明 `groupAccounts`，每账号一张卡片 |
| 前端卡片渲染 | 新增一张卡片 | `public/app.js` 零改动；「最快到期」排序按 `expiresAt` 正常参与 |
| 缓存策略 | 否 | 缓存键含 provider id，与国内版隔离 |
| 既有 API 消费方 | 否 | `/api/providers` 多一项，前端按声明渲染 |
| CLI `npm run probe` | 新增可选参数 | `npm run probe -- codebuddy-intl` 可用 |
| CLI `npm run capture` | 新增目标 | `codebuddy-intl` 或 `codebuddy --site global` |
| CLI `npm run account` | 新增可选 provider | `npm run account -- --provider codebuddy-intl` |
| 文档 | 需更新 | `README.md`、`docs/凭证配置指南.md` 补国际版入口 |

## 7. 风险与备选方案

### 7.1 主要风险

| 风险 | 表现 | 缓解 |
| --- | --- | --- |
| 国际站域名 / 插件后端主机未实测 | 扫码登录或取数 404 / 401 | 代码注释已确认「协议同源」；v1 先按 `copilot.tencent.com` + 国际站域名（如 `workbuddy.ai` / `codebuddy.ai`，**待确认**）实现，probe 失败时提示改用手工 Cookie；提供 `CODEBUDDY_GLOBAL_BILLING_ORIGIN` / `CODEBUDDY_GLOBAL_PLUGIN_BASE` 环境变量覆盖 |
| 国际版会话 Cookie 名与国内版不同 | 网页端登录一直「等待登录」 | `loginCookieNames` 做成站点参数；判据同时接受「页面自己请求身份接口 2xx」（`sawSuccessfulRequest`），不依赖单一 Cookie 名 |
| 国际版 UA 被 WAF 拦截 | 请求被拒绝 | UA 常量已按 `WorkBuddy/<ver>` 形态；保留 `CODEBUDDY_CLIENT_UA` / `CODEBUDDY_CLIENT_VERSION` 环境变量覆盖 |
| 用户误把国内版 Cookie 填到国际版 | 401 / 取数为空 | `manageHint` 与 `credentialHint` 明确写「国际版（WorkBuddy）」；`accounts.example.json` 用注释隔开 |

### 7.2 被否决的备选方案

| 方案 | 否决原因 |
| --- | --- |
| 在现有 `codebuddy` 平台加 `site` 选项 | 违反「Qoder 国内版 / 国际版彻底分离」已确立的架构决策；站点从「账号选项」上移成「平台身份」是为了避免缓存键 / 登录入口 / 卡片筛选的跨站混乱 |
| 复制一份 `codebuddy.ts` 为 `codebuddy-intl.ts` | 双模式探测、令牌续期、用量明细等 ~400 行逻辑重复，上游改版时要改两处；工厂化与 Qoder 一致，维护成本最低 |
| 只支持令牌（扫码），不支持 Cookie | 令牌模式拿不到逐包批次明细（`src/providers/qoder.ts:1228-1231` 的踩坑记录），CodeBuddy 的积分包带真实到期时间，必须保留 Cookie 模式 |

## 8. 实施步骤

按 `providers` → `cli` → `config/docs` 分层，每步产出物可独立静态检查：

| 步骤 | 文件 | 产出物 |
| --- | --- | --- |
| 1. 站点工厂化 | `src/providers/codebuddy.ts` | `CODEBUDDY_SITES` 元信息表 + `createCodeBuddyAdapter(siteId)`；`codebuddyAdapter` 行为不变 |
| 2. 国际版适配器 | `src/providers/codebuddy.ts` | `codebuddyIntlAdapter`（含 `loginMethods: [扫码, 网页端登录]`） |
| 3. 注册与别名 | `src/providers/index.ts` | 注册 `codebuddyIntlAdapter`；补 `workbuddy.ai` / `codebuddy.ai` 等别名（待确认后回填） |
| 4. CLI 抓取 | `src/cli/capture.ts` | `codebuddy-intl` Target（`envKey: CODEBUDDY_GLOBAL_COOKIE`） |
| 5. 配置示例 | `.env.example`、`accounts.example.json` | 新增 `CODEBUDDY_GLOBAL_*` 占位符与注释 |
| 6. 文档 | `README.md`、`docs/凭证配置指南.md` | 补国际版添加方式（不含明文） |
| 7. 静态检查 | 全部改动文件 | `npm run typecheck` 通过；阅读上下游调用确认无遗漏 |

## 9. 验证方式

> 按 AGENTS.md §6，本次仅做静态检查；动态验证（probe / capture / 登录）由用户人工执行。

### 9.1 自测用例（人工执行）

| 用例 | 命令 / 操作 | 预期结果 |
| --- | --- | --- |
| 类型检查 | `npm run typecheck` | 0 错误 |
| 国际版扫码登录 | 看板 → WorkBuddy 国际版卡片 → 扫码登录 | 二维码出现；手机授权后自动落库，卡片出现 |
| 国际版网页端登录 | 看板 → WorkBuddy 国际版卡片 → 网页端登录 | 自动打开调试 Chrome；登录后自动落库 |
| 国际版手动 Cookie | 看板 → WorkBuddy 国际版卡片 → 添加账号 → 粘贴 Cookie | 验证通过后写入 `CODEBUDDY_GLOBAL_COOKIE` |
| 国际版 CLI 抓取 | `npm run capture -- codebuddy-intl` | 打开国际站用量页，抓到 Cookie 写入 `CODEBUDDY_GLOBAL_COOKIE` |
| 国际版取数 | `npm run probe -- codebuddy-intl` | 返回余额 / 资源包 / 到期时间，与国内版格式一致 |
| 国内版回归 | `npm run probe -- codebuddy` | 行为完全不变 |
| 配置热加载 | 修改 `.env` 的 `CODEBUDDY_GLOBAL_COOKIE` | 页面点「立即刷新」即为新凭证取数结果 |

### 9.2 涉及平台与账号

- 平台：CodeBuddy 国内版（回归）、WorkBuddy 国际版（新增）。
- 账号：需用户自备国际版账号（Cookie 或扫码授权）。

### 9.3 回归范围

- `src/providers/codebuddy.ts` 的全部既有调用方（`src/providers/index.ts`、`src/service.ts`、`src/cli/probe.ts`）。
- 前端「添加 / 删除账号」面板（`public/app.js` 的 `renderManage`）。
- 登录会话层（`src/login-session.ts`）对多方法平台的处理（`resolveLoginMethod`）。

## 变更记录

| 时间 | 变更内容 | 触发原因 | 影响章节 |
| --- | --- | --- | --- |
| 2026-10-25 | 初版 | 任务启动 | 全篇 |
| 2026-10-25 | 状态改为「已完成」，关联改动文件补全；国际站域名确认为 codebuddy.ai；实现落地（站点工厂化 + 双登录方法 + CLI + 配置示例） | 用户确认方案并实施 | 元信息区、§1.3、§4、§5、§7、§8、§9 |
| 2026-10-25 | 修复：国际版扫码登录误用国内版主机（`copilot.tencent.com`），改为 `www.workbuddy.ai` + `platform=workbuddy-ai`；补 `platform` 字段到站点表 | 用户实测发现扫码链接指向国内版 | §4.3 |
