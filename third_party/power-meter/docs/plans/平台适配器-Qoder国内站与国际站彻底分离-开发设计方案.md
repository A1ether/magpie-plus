# 平台适配器 - Qoder 国内站与国际站彻底分离 - 开发设计方案

> 文档类型：开发设计方案
> 关联模块：平台适配器（Qoder）、账号管理、前端看板、凭证抓取
> 创建时间：2026-10-25
> 最后更新：2026-10-25
> 当前状态：已完成（静态检查通过；动态验证按 AGENTS.md §6 未执行，见 §9「人工验证」）
> 关联改动文件：`src/providers/qoder.ts`、`src/providers/index.ts`、`src/providers/base.ts`、`src/account-store.ts`、`src/credit-value.ts`、`src/login-session.ts`、`src/cli/capture.ts`、`src/cli/account.ts`、`src/cli/migrate-qoder-sites.ts`（新增）、`public/app.js`、`public/index.html`、`public/styles.css`、`accounts.example.json`、`README.md`、`docs/凭证配置指南.md`、`docs/modules/项目整体-代码Wiki.md`、`package.json`

---

## 1. 背景与目标

**结论：把 Qoder 从「一个 provider + `options.site` 站点开关」改成两个独立 provider（`qoder` 国内站 / `qoder-intl` 国际站）。卡片上的「本平台已配置的账号」天然只含本平台账号，登录块不再出现站点下拉，看板、弹窗、筛选、缓存、聚合一律按两个平台对待。**

### 1.1 用户诉求（原文）

> 将 qoder cn 和 qoder 国际版两个供应商分离的更彻底，比如图一的本平台已配置卡片，只单独显示国内/国际，卡片上的登陆也不要能选择国内/国际了，就当成两个供应商来看待

### 1.2 现状问题

| 编号 | 现象 | 现状证据 |
| --- | --- | --- |
| P1 | 卡片「本平台已配置的账号」把国内站与国际站账号混在一张清单里，只能靠站点行/文案区分 | `public/app.js:1735-1760`（`renderConfiguredAccounts` 按 `options.site` 过滤，未传 scope 时全列） |
| P2 | 登录块有「站点」下拉，同一张卡片既能加国内站也能加国际站 | `public/index.html:258-260`（`.login__site`）、`public/app.js:1102-1105`（`LOGIN_SITE_LABELS`）、`public/app.js:1238-1258`（下拉渲染与锁定） |
| P3 | 手工表单同样带「站点」下拉（cn / global / auto） | `src/providers/qoder.ts:1434-1456`（`manageFields` 的 `site` 选项字段）、`public/app.js:1500-1525` |
| P4 | 一个平台内部还要做站点探测链（auto / fallbackSite），失败文案教用户改 `options.site` | `src/providers/qoder.ts:536-604`（`QoderSitePlan` / `resolveSitePlan`）、`src/providers/qoder.ts:836-880`（`buildFailureMessage`） |
| P5 | 「平台与账号」弹窗要按 `options.site` 把一个平台拆成多行 | `public/app.js:1796-1860`（`siteFieldOf` / `platformSections`）、`public/app.js:1949-2010`（`buildPlatformRow`） |
| P6 | 卡片 region 徽标与「按平台」子菜单靠 `crossRegionProviders()` 猜站点 | `public/app.js:452-506`、`public/app.js:618-642` |

### 1.3 目标

1. **两个 provider**：`qoder`（国内站，`qoder.com.cn`）、`qoder-intl`（国际站，`qoder.com`）。各自固定 origin / 面板页 / 用量页 / 凭证变量前缀。
2. **登录块与表单不再有站点维度**：Qoder 卡片只有 Cookie 输入框；登录按钮按卡片所属平台走对应站点。
3. **账号天然按平台隔离**：`/api/accounts?provider=qoder-intl` 只返回国际站账号；卡片「本平台已配置的账号」无需过滤即正确。
4. **看板按两个平台渲染**：两张卡片、两个「按平台」子菜单行、两个「平台与账号」弹窗行，站点徽标由各平台自己的 `region` 得出。
5. **凭证变量名不变**：国内站仍 `QODER_COOKIE` / `QODER_ACCESS_TOKEN` / `QODER_REFRESH_TOKEN`，国际站仍 `QODER_GLOBAL_*`，`.env` 不需要改名。
6. **旧账号可迁移**：`provider: "qoder"` + `options.site: "global"` 的既有账号，由一次性脚本改成 `provider: "qoder-intl"`。

### 1.4 Non-Goals（明确不做）

| 不做 | 原因 |
| --- | --- |
| 不保留 `options.site` / `fallbackSite` / `auto` 的运行时语义 | 与「两个供应商」冲突；站点由平台决定，不再由账号选项决定 |
| 不重命名 `.env` 变量（不改 `QODER_GLOBAL_*`） | 用户本机 `.env` 已有真实凭证，改名只会制造迁移风险；变量名与「国际站」语义一致 |
| 不复制两份解析逻辑 | 两个适配器共用同一份 `qoder.ts` 内部实现（Cookie/令牌/登录/解析），只在工厂里绑定站点与 id |
| 不改 `options.origin`（自定义域名/代理）的既有能力 | 历史行为，与站点无关，保留 |
| 不做「一个账号同时探测两个站点」 | 候选链随站点选项一起移除 |

---

## 2. 现状调研

> **说明**：本节记录的是**改动前**（设计阶段）的代码状态，表格里的 `文件:行号` 是当时的位置。
> 拆分实施后这些行号已漂移（`resolveSitePlan` / `adapterForSite` / 站点 option / 前端站点拆分逻辑均已删除），
> 仅作为「为什么这样改」的证据保留；**当前实际状态见 §4.2 与 `docs/modules/项目整体-代码Wiki.md`**。

### 2.1 站点体系与适配器

| 关注点 | 现状 | 证据 |
| --- | --- | --- |
| 站点表 | `QODER_SITES`（cn: `qoder.com.cn` / global: `qoder.com`），含 `label` / `origin` / `dashboardUrl` / `usageUrl` / `acceptLanguage` / `loginCookieNames` | `src/providers/qoder.ts:497-521` |
| 站点计划 | `resolveSitePlan()`：`origin` 覆盖 → 单候选；`site: cn/global` → 单候选（+`fallbackSite`）；`site: auto` → `[cn, global]`；非法值 → 按 cn 并提示 | `src/providers/qoder.ts:565-604` |
| 按站点改快照 | `adapterForSite(site)` 用展开运算符覆盖 `region` / `dashboardUrl` / `usageUrl` | `src/providers/qoder.ts:606-614` |
| 适配器声明 | `id: 'qoder'`、`displayName: 'Qoder'`、`region: 'cn'` | `src/providers/qoder.ts:1392-1400` |
| 凭证变量前缀 | `envNames(options)`：`options.site === 'global'` → `QODER_GLOBAL_*`，否则 `QODER_*` | `src/providers/qoder.ts:1409-1431` |
| 管理表单 | `manageFields`: `cookie` + `site`（`target: 'option'`, `choices: ['cn','global','auto']`） | `src/providers/qoder.ts:1434-1456` |
| 扫码登录 | `login.start` 读 `input.options.site` 决定设备流站点，`poll` 把 `site` 写回 `options` | `src/providers/qoder.ts:1492-1520`、`:1560-1570` |
| 网页端登录 | `qoderWebLogin` 同样读 `input.options.site`，`options: { site }` 落库 | `src/providers/qoder.ts:1263-1390` |
| 探测 | Cookie 模式沿候选链串行探测，`siteNote` / `fallbackNote` / `invalidNote` 拼进 note；失败时 `buildFailureMessage` 教用户改 `options.site` | `src/providers/qoder.ts:1584-1719`、`:836-880` |
| 令牌模式 | `QoderTokenFailure` 携带失败站点；续期回到该站点 | `src/providers/qoder.ts:271-278`、`:300-420` |
| 注册表 | 单条 `qoderAdapter`；别名 `qoder.com` / `qoder.com.cn` / `qoder.cn` / `qoder-cn` / `qoder-global` 全部归一为 `qoder` | `src/providers/index.ts:18-30`、`:51-58` |

### 2.2 账号、缓存与聚合

| 关注点 | 现状 | 证据 |
| --- | --- | --- |
| 环境变量名解析 | `envNameFor(adapter, key, options)` 调 `adapter.envNames(options)` | `src/account-store.ts:119-133` |
| 账号列表 | 按 `account.provider === adapter.id` 过滤；`options` 只透出 `target:'option'` 字段 | `src/account-store.ts:250-282`、`:286-312` |
| 新增账号 | 用 `options` 里的站点值做真实验证，并写进 `accounts.json` 的 `options` | `src/account-store.ts:487-560` |
| 登录落库 | `optionValues` 含 `site`，`envNameFor(..., optionValues)` 决定落哪个变量 | `src/account-store.ts:805-830`、`:900-930` |
| 缓存 / 本地用量键 | `provider#displayName` | `src/service.ts:171-178` |
| 人民币估值 | `CREDIT_PEGS` 两条 `provider: 'qoder'`，用 `region: cn|global` 区分单价 | `src/credit-value.ts:88-107`、`:141-150` |
| 登录会话回显 | `site` 字段只用于状态回显 | `src/login-session.ts:52`、`:74`、`:101`、`:224` |

### 2.3 前端

| 关注点 | 现状 | 证据 |
| --- | --- | --- |
| 站点文案 | `SITE_LABELS = { cn: '国内版', global: '国际版' }`、`siteFilterTerm()` | `public/app.js:429-441` |
| 跨站点判定 | `crossRegionProviders()`：同一 provider 出现多种 region 才拆 | `public/app.js:452-468` |
| 平台分组 | `platformGroups()` 按 `provider#region` 拆桶 | `public/app.js:476-506` |
| 搜索 haystack | `sortedProviders()` 拼 `siteText` | `public/app.js:535-548` |
| 卡片徽标 | `buildCard(provider, thresholds, crossRegion)` 按跨站点决定文案 | `public/app.js:618-642` |
| 管理目标 | `dialogTarget(platform, site)` 带 `siteScope` | `public/app.js:975-993` |
| 登录块 | `renderLogin(..., siteField)` 渲染站点下拉、按 `target.siteScope` 锁定 | `public/app.js:1118`、`:1143-1145`、`:1238-1258` |
| 管理表单 | 从 `manageFields` 找第一个 option 字段当站点；预置并置灰 | `public/app.js:1453-1464`、`:1515-1525` |
| 已配置账号清单 | `renderConfiguredAccounts()` 按 `target.siteScope` 过滤 | `public/app.js:1735-1760` |
| 弹窗拆行 | `siteFieldOf()` / `platformSections()` / `buildPlatformRow()` / `renderPlatformManage()` | `public/app.js:1796-1860`、`:1949-2010`、`:2031-2054` |
| 登录块 DOM | `.login__site` + `.login__site-select` | `public/index.html:258-260` |

### 2.4 抓取与 CLI

| 关注点 | 现状 | 证据 |
| --- | --- | --- |
| 抓取目标 | `qoder` 的 `siteVariants.global` 覆盖 `envKey` / `cookieSites` / `usageUrl` / `apiHost`，并带 `optionFlags: { site: 'global' }` | `src/cli/capture.ts:208-225` |
| 注册提示 | `--site global` 抓完后提示 `npm run account -- --provider qoder --env QODER_GLOBAL_COOKIE --site global` | `src/cli/capture.ts:815-826` |
| 账号 CLI | `--site global` 是 `--option site=global` 的快捷写法 | `src/cli/account.ts:64`、`:220-230` |

### 2.5 可复用能力（不需要重写）

- `fetchUsageAtSite()` / `fetchLedgerAtSite()` / `qoderTokenWebUsage()` / `qoderOpenApiQuota()` / `qoderUserInfo()` / `qoderCookieIdentity()` / `parseQoderUsage()` 全部按「传入站点」工作（`src/providers/qoder.ts:150-460`、`:690-800`），拆分后只是从「候选链里挑一个」变成「适配器固定一个」。
- `buildSnapshot()` 直接读 `adapter.id` / `adapter.region` / `adapter.dashboardUrl`（`src/providers/base.ts:451-460`），两个适配器各自声明即可。
- 通用账号存储层按 `provider` 字段过滤（`src/account-store.ts:181-183`），拆分后天然隔离。

---

## 3. 方案设计

### 3.1 总体思路

**结论：`qoder.ts` 内部保持一份实现，用一个工厂函数按站点产出两个 `ProviderAdapter`；站点从「账号选项」上移到「平台身份」。**

```
上游接口（qoder.com.cn / qoder.com）
        │
        ▼
createQoderAdapter('cn')  ──► qoder      （Qoder 国内版，region=cn，QODER_*）
createQoderAdapter('global') ─► qoder-intl （Qoder 国际版，region=global，QODER_GLOBAL_*）
        │
        ▼
service.ts（按 accounts.json 的 provider 取适配器、缓存键 provider#displayName）
        │
        ▼
aggregate.ts（按 provider 分组；region 来自适配器）
        │
        ▼
public/app.js（一个 provider 一行/一张卡，无站点拆分逻辑）
```

### 3.2 站点解析：从「计划」降为「固定站点」

| 项 | 现状 | 拆分后 |
| --- | --- | --- |
| 站点来源 | `options.site`（cn / global / auto）+ `options.fallbackSite` | 适配器闭包里的 `siteId` |
| 候选链 | `auto` → `[cn, global]`；`fallbackSite` → 两站 | 恒定单站点，无回退 |
| `options.origin` | 覆盖 origin，站点由域名推断 | 仍覆盖 `origin`（`dashboardUrl` / `usageUrl` 保持站点默认） |
| 旧配置残留 | — | `options.site` 显式跨站（cn 平台配 `global` / 反之）→ **探测直接失败**并给出迁移指引；`auto` → 按本平台站点探测并在 note 标注已废弃 |
| note 文案 | `站点：国内站（…，自动探测）` / `已回退到…` / `options.site 取值…无法识别` | 只保留 `站点：国内站（https://qoder.com.cn）` |

### 3.3 平台身份

| provider id | displayName | region | 面板 / 用量页 | 凭证变量 |
| --- | --- | --- | --- | --- |
| `qoder` | `Qoder 国内版` | `cn` | `https://qoder.com.cn/account/usage` / `https://qoder.cn/account/usage?client=qoder&sourceType=IDE` | `QODER_COOKIE` / `QODER_ACCESS_TOKEN` / `QODER_REFRESH_TOKEN` |
| `qoder-intl` | `Qoder 国际版` | `global` | `https://qoder.com/account/usage` | `QODER_GLOBAL_COOKIE` / `QODER_GLOBAL_ACCESS_TOKEN` / `QODER_GLOBAL_REFRESH_TOKEN` |

> 槽位规则不变：第 N 个账号（N≥2）由 `src/account-store.ts` 的 `slotEnvName()` 追加 `_N`。

### 3.4 旧账号兼容与迁移

**结论：`accounts.json` 里 `provider: "qoder"` 且 `options.site: "global"` 的条目必须迁移成 `provider: "qoder-intl"`，否则会被国内站适配器用国际站 Cookie 探测（必然失败）。**

| 场景 | 处理 |
| --- | --- |
| `provider: "qoder"` + `options.site: "global"` | 迁移脚本改写为 `provider: "qoder-intl"`，删除 `site` / `fallbackSite` |
| `provider: "qoder"` + `options.site: "cn"` 或无 | 只删除 `site` / `fallbackSite`，provider 不变 |
| `provider: "qoder"` + `options.site: "auto"` | 不自动改 provider（无法判断 Cookie 归属），脚本打印「需人工确认」，保留原样 |
| 迁移写入 | 先备份到 `.run/qoder-sites-<时间戳>/`，只改 `provider` / `options` 两个字段，凭证与 `${VAR}` 引用不动；支持 `--dry-run` |

迁移脚本入口：`npm run migrate:qoder-sites`（新增 `src/cli/migrate-qoder-sites.ts`）。按 AGENTS.md §7.3，脚本由用户显式执行。

### 3.5 登录

| 方法 | 拆分后行为 |
| --- | --- |
| 扫码登录（设备流） | 站点由适配器决定：`qoder` 用 `qoder.com.cn` + `openapi.qoder.com.cn`；`qoder-intl` 用 `qoder.com` + `openapi.qoder.sh`（沿用现有常量，国际站设备流仍标注「未实测」） |
| 网页端登录（抓 Cookie） | 打开本平台用量页（`qoder` → `qoder.com.cn`，`qoder-intl` → `qoder.com`），登录态判据仍按站点 Cookie 名 + `/api/v1/me` |
| 落库 options | 不再写 `site`；保留 `userId` / `machineId` / `loginAccountId` / `loginName` |

---

## 4. 接口设计

### 4.1 HTTP API（无新增路径，语义变化）

| 接口 | 变化 |
| --- | --- |
| `GET /api/providers` | 返回两个 Qoder 平台：`qoder`（`displayName: "Qoder 国内版"`, `region: "cn"`, `manageFields` 只有 `cookie`）、`qoder-intl`（`displayName: "Qoder 国际版"`, `region: "global"`） |
| `GET /api/accounts?provider=qoder-intl` | 只返回国际站账号；`options` 不再含 `site` |
| `GET /api/accounts/overview` | 两个平台各自一行，不再需要按站点拆行 |
| `POST /api/accounts/add` | body 的 `options` 不再需要 `site`（传了也不生效：`manageFields` 无 option 字段） |
| `POST /api/login/start` | body 的 `options` 不再需要 `site`；返回体去掉 `site` 字段（`src/login-session.ts`） |
| `GET /api/summary` | 两张 Qoder 快照的 `provider` 分别为 `qoder` / `qoder-intl`，`region` 各自固定 |

### 4.2 `ProviderAdapter` 契约

**结论：只新增一个可选字段 `legacyDisplayNames`，其余靠两个适配器的声明差异表达。**

| 字段 | `qoder` | `qoder-intl` |
| --- | --- | --- |
| `id` | `qoder` | `qoder-intl` |
| `displayName` | `Qoder 国内版` | `Qoder 国际版` |
| `region` | `cn` | `global` |
| `envNames` | 静态对象（`QODER_*`） | 静态对象（`QODER_GLOBAL_*`） |
| `manageFields` | `[cookie]` | `[cookie]` |
| `loginMethods` | 扫码 + 网页端（站点固定 cn） | 扫码 + 网页端（站点固定 global） |
| `legacyDisplayNames`（**新增可选字段**） | `['Qoder']` | 未声明（改名前没有「国际版」旧名） |

`legacyDisplayNames?: string[]`（`src/providers/base.ts:190-196`）：适配器改名后仍需识别的旧显示名前缀，
供占位名判定使用——旧账号的 `displayName` 是「Qoder · 登录账号」，登录后要能识别并按昵称改名。
消费方：`src/account-store.ts` 的 `isPlaceholderLoginName()`（`displayName` + `legacyDisplayNames` 逐个比对）
与 `src/providers/qoder.ts` 的 `isQoderPlaceholderName()`。未声明该字段的适配器行为完全不变（向后兼容）。

### 4.3 CLI

| 命令 | 变化 |
| --- | --- |
| `npm run capture -- qoder` | 抓国内站，写 `QODER_COOKIE`（不变） |
| `npm run capture -- qoder --site global` | 抓国际站，写 `QODER_GLOBAL_COOKIE`；注册提示改为 `--provider qoder-intl`（去掉 `--site global`） |
| `npm run capture -- qoder-intl` | 新增等价写法：自动等价于 `qoder --site global` |
| `npm run account -- --provider qoder-intl --env QODER_GLOBAL_COOKIE` | 国际站账号注册 |
| `npm run account -- --provider qoder --site global` | **不再支持**：`site` 不是 option 字段，`--site` 会被忽略 → 改为直接报「未知 flag」不会发生（CLI 不校验未知 flag），文档与提示统一改为 `qoder-intl` |
| `npm run migrate:qoder-sites [--dry-run]` | 新增：迁移本机 `accounts.json` 的旧站点配置 |

---

## 5. 数据模型

### 5.1 `accounts.json`

```jsonc
// 国内站
{ "provider": "qoder",       "displayName": "Qoder · A1ether",   "options": { "loginAccountId": "…", "loginName": "…" } }
// 国际站（迁移后）
{ "provider": "qoder-intl",  "displayName": "Qoder · A1ether 2", "options": { "loginAccountId": "…", "loginName": "…" } }
```

| 字段 | 变化 |
| --- | --- |
| `provider` | 国际站账号由 `qoder` 改为 `qoder-intl` |
| `options.site` | 删除（不再读取） |
| `options.fallbackSite` | 删除（不再读取） |
| `options.origin` | 保留（自定义域名/代理） |
| `credentials.cookie` | 保留 `${QODER_COOKIE}` / `${QODER_GLOBAL_COOKIE}` 引用，不变 |

### 5.2 `ProviderSnapshot`

| 字段 | 变化 |
| --- | --- |
| `provider` | `qoder` / `qoder-intl` |
| `region` | 由适配器固定（`cn` / `global`），不再由探测结果覆盖 |
| `dashboardUrl` / `usageUrl` | 由适配器固定 |

### 5.3 人民币估值（`CREDIT_PEGS`）

| 现状 | 拆分后 |
| --- | --- |
| 两条 `provider: 'qoder'` + `region: 'cn' | 'global'` | 两条：`provider: 'qoder'`（¥59 / 2,000 Credits）、`provider: 'qoder-intl'`（$20 / 1,500 Credits × `USD_CNY`） |

`findCreditPeg(provider, region)` 的「先按 provider+region、再按 provider」逻辑保留（`src/credit-value.ts:141-150`），拆分后按 provider 精确命中。

### 5.4 缓存 / 本地用量

| 项 | 影响 |
| --- | --- |
| 缓存键 `provider#displayName` | 迁移后的国际站账号键从 `qoder#…` 变为 `qoder-intl#…`，**首次刷新会重新取数**（缓存冷启动，属预期） |
| `.run/usage-history.json` | 同一账号的历史采样键随之变化，日级用量从迁移当天重新累计（历史样本保留在文件里但不参与计算）；如需保留可在文档中标注为已知代价 |
| `.env` | 变量名不变，无迁移 |

---

## 6. 影响面评估

| 受影响面 | 影响 | 证据 |
| --- | --- | --- |
| 适配器注册与别名 | 新增 `qoder-intl`；`qoder.com` / `qoder-global` 别名改指向 `qoder-intl` | `src/providers/index.ts:18-30`、`:51-58` |
| Qoder 适配器 | 站点候选链、`adapterForSite`、`site` option、失败文案里的站点切换建议全部移除；两个适配器共用实现 | `src/providers/qoder.ts:536-614`、`:1409-1456`、`:1584-1719` |
| 账号存储 | `declaredOptionsOf` 对 Qoder 返回空（无 option 字段），前端不再按站点分桶；`envNameFor` 走静态 `envNames` | `src/account-store.ts:286-312`、`:119-133` |
| 登录会话 | 去掉 `site` 回显字段 | `src/login-session.ts:52`、`:74`、`:101`、`:224` |
| 人民币估值 | Qoder 国际站单价改为按 `qoder-intl` 命中 | `src/credit-value.ts:88-107` |
| 抓取 CLI | 站点变体可覆盖 `id`；注册提示改为 `--provider qoder-intl`；支持 `qoder-intl` 位置参数 | `src/cli/capture.ts:160-225`、`:815-826` |
| 账号 CLI | 示例文案更新；通用 `--option` 保留（Qoder 已无 option 字段） | `src/cli/account.ts:64`、`:220-230` |
| 前端卡片 | 卡片标题、余额、到期逻辑不变；region 徽标由平台决定（国内 / 国际） | `public/app.js:618-642` |
| 前端分组/筛选 | `crossRegionProviders` / `siteFilterTerm` / `platformGroups` 的站点拆桶逻辑删除，改为按 provider 一行 | `public/app.js:429-506`、`:535-548` |
| 前端弹窗 | `platformSections` / `siteFieldOf` / `siteScope` 删除，一个平台一行 | `public/app.js:1796-1860`、`:1949-2054` |
| 前端登录/表单 | 站点下拉与锁定逻辑删除 | `public/app.js:1102-1105`、`:1238-1258`、`:1453-1464`、`:1515-1525` |
| 「最快到期」时间线 | 不受影响：`ExpiryHit.provider` 变为两个 id，估值按新 provider 命中 | `src/aggregate.ts:214-226` |
| 既有文档 | README / 凭证指南 / 代码 Wiki / 样例配置需同步；旧方案文档的 Non-Goal「不新增独立 provider id」被本次推翻，加指引行 | 见 §8 |

---

## 7. 风险与备选方案

### 7.1 风险

| 风险 | 等级 | 缓解 |
| --- | --- | --- |
| 旧国际站账号未迁移 → 取数失败（国内站适配器拿国际站 Cookie） | 高 | 探测时识别 `options.site: "global"` 直接给出迁移指引；迁移脚本 + 文档 |
| 缓存 / 日级用量键变化导致国际站账号历史用量归零 | 中 | 属一次性代价；在 §5.4 与变更记录中写明 |
| `displayName` 从 `Qoder` 改为 `Qoder 国内版` 导致占位名「Qoder · 登录账号」不再被识别 | 低 | 适配器内兼容旧前缀（`Qoder` 与 `Qoder 国内版` 都判为占位名） |
| 用户脚本/习惯里仍用 `--provider qoder --site global` | 中 | CLI 文档、capture 提示、失败文案统一指向 `qoder-intl`；`--site global` 抓取仍可用（只影响抓取） |
| 国际站设备流主机未实测（沿用 `openapi.qoder.sh`） | 低 | 保持现状；网页端登录与 Cookie 抓取是主路径 |

### 7.2 被否决的备选方案

| 方案 | 否决原因 |
| --- | --- |
| 保留单 provider，仅在前端隐藏站点下拉 | 账号仍混在同一平台下，「本平台已配置的账号」无法自然隔离；后端缓存/凭证/表单仍带站点分支 |
| 新建 `qoder-intl.ts` 完整复制解析逻辑 | ~1,700 行重复，后续上游字段变更要改两处 |
| 继续用 `options.site` 但强制锁定 | 与用户「当成两个供应商」的诉求相反，且旧账号仍可能跨站 |
| 迁移 `QODER_GLOBAL_*` → `QODER_INTL_*` | 无收益、需改用户 `.env`，风险大于收益 |

> **推翻的历史结论**：`docs/plans/平台适配器-接入Qoder国际站-开发设计方案.md` §1 Non-Goal 曾写「不新增独立 provider id（如 `qoder-global`）」。本次按用户要求改为独立 provider，旧文档已加指引行，不再作为现行约束。

---

## 8. 实施步骤

| 步骤 | 内容 | 产出 |
| --- | --- | --- |
| 1 | 本文档（先文档后代码） | `docs/plans/平台适配器-Qoder国内站与国际站彻底分离-开发设计方案.md` |
| 2 | `src/providers/qoder.ts`：删除 `resolveSitePlan` / `adapterForSite` / 站点 option；新增 `createQoderAdapter(siteId)` 产出 `qoderAdapter` + `qoderIntlAdapter`；登录、探测、失败文案改为单站点 | 两个适配器 |
| 3 | `src/providers/index.ts`：注册 `qoder-intl`，调整别名 | 注册表 |
| 4 | `src/credit-value.ts`：两条 peg 改为按 provider id 命中 | 估值表 |
| 5 | `src/cli/capture.ts`：站点变体可覆盖 `id`；注册提示改 `qoder-intl`；支持 `qoder-intl` 位置参数 | 抓取 CLI |
| 6 | `src/cli/account.ts`：示例文案更新 | 账号 CLI |
| 7 | `src/cli/migrate-qoder-sites.ts` + `package.json` 脚本：迁移旧 `options.site` 配置（备份 + `--dry-run`） | 迁移脚本 |
| 8 | `src/login-session.ts`：去掉 `site` 回显 | 会话层 |
| 9 | `public/app.js` / `public/index.html` / `public/styles.css`：删除站点拆分、站点下拉、`siteScope` 全部逻辑 | 前端 |
| 10 | `accounts.example.json`、`README.md`、`docs/凭证配置指南.md`、`docs/modules/项目整体-代码Wiki.md` 同步；旧方案文档加指引行 | 文档与样例 |

**实施结果（2026-10-25）**：步骤 1–10 全部完成。实际改动范围比上表多出两处，均为实施中发现：
`src/providers/base.ts`（新增可选字段 `legacyDisplayNames`，见 §4.2）与 `src/account-store.ts`（占位名判定读该字段）。

---

## 9. 验证方式

**按 AGENTS.md §6，本次只做静态检查，不执行动态验证。**

| 项 | 方式 | 结果（2026-10-25） |
| --- | --- | --- |
| 类型检查 | `npm run typecheck` | ✅ 通过（`tsc --noEmit` 无输出）；前端另跑 `node --check public/app.js` 通过 |
| 残留检索 | `grep -rn "options.site\|fallbackSite\|siteScope\|LOGIN_SITE_LABELS\|siteVariants\|platformSections\|crossRegionProviders\|siteField\|SITE_LABELS\|state.dialog.site" src public` | ✅ `public/` 全部清零；`src/` 仅剩三类**有意保留**的命中：① `qoder.ts` 识别旧 `options.site` / `fallbackSite` 并发迁移指引（§3.2/§3.4）；② `cli/capture.ts` 的 `siteVariants`（抓取工具仍按 `--site global` 抓国际站）；③ `cli/migrate-qoder-sites.ts` 自身 |
| 注册表 | 阅读 `src/providers/index.ts` | ✅ `REGISTRY` 含 `qoder` + `qoder-intl`；别名 `qoder.com` / `qoder-global` → `qoder-intl`，`qoder.com.cn` / `qoder.cn` / `qoder-cn` → `qoder` |
| 前端 | 阅读 `public/app.js` | ✅ 无 `state.dialog.site`、无 `.login__site` 读取；`platformGroups()` 一个平台一行；弹窗一行一个平台 |
| 契约 | 阅读 `src/providers/base.ts` | ✅ 新增可选 `legacyDisplayNames`（见 §4.2），未声明该字段的适配器行为不变 |

**未执行的验证（按 AGENTS.md §6 仅做静态检查）**：未跑 `npm run probe` / `npm run capture` / `npm run build` / 未启动服务、未调用任何上游接口。
因此「两张卡片分别取数正常」「看板显示正确」这类结论**尚未验证**，需由人工按下表执行。

**人工验证（AI 不代跑）**：

| 步骤 | 命令 / 操作 | 预期 |
| --- | --- | --- |
| 迁移旧配置 | `npm run migrate:qoder-sites --dry-run` → `npm run migrate:qoder-sites` | 打印 1 条 `qoder` + `site: global` → `qoder-intl`；备份目录生成 |
| 取数 | `npm run probe -- qoder` / `npm run probe -- qoder-intl` | 两张卡片分别 ok，note 只含本平台站点 |
| 看板 | 刷新浏览器 | 两张卡片（国内 / 国际），各自「本平台已配置的账号」只列本平台账号；登录块无站点下拉 |
| 抓取 | `npm run capture -- qoder-intl` | 写 `QODER_GLOBAL_COOKIE`，结尾提示 `--provider qoder-intl` |

---

## 变更记录

| 时间 | 变更内容 | 触发原因 | 影响章节 |
| --- | --- | --- | --- |
| 2026-10-25 | 初版：确定拆成 `qoder` / `qoder-intl` 两个 provider，固定站点、保留 `QODER_GLOBAL_*` 变量名、新增迁移脚本；记录推翻旧 Non-Goal | 用户要求「把 qoder cn 与国际版彻底分离，当成两个供应商」 | 全篇 |
| 2026-10-25 | 实施完成：后端（适配器工厂 / 注册表 / 估值 / CLI / 迁移脚本 / 会话层）+ 前端（卡片、弹窗、登录块、筛选去掉站点维度）+ 文档（README / 凭证指南 / 代码 Wiki / 样例配置 / 旧方案文档指引行）全部落地；§4.2 补记新增契约字段 `legacyDisplayNames`（原写「不扩展契约」不准确）；§9 回填静态检查结果并标注未执行的动态验证；§2 加「行号已漂移」说明 | 代码实施完成，按 AGENTS.md §4.1 触发文档同步 | 2、4.2、8、9 |
