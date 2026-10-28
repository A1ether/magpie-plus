# 平台适配器 - 接入 Qoder 国际站 - 开发设计方案

> **已被取代（2026-10-25）**：本文档的「单 provider + `options.site` 三取值（cn / global / auto）+ `fallbackSite` 回退」方案，已被
> [`平台适配器-Qoder国内站与国际站彻底分离-开发设计方案.md`](平台适配器-Qoder国内站与国际站彻底分离-开发设计方案.md) 推翻——
> 国内站与国际站改为**两个独立 provider**（`qoder` / `qoder-intl`），站点由平台决定，`options.site` / `fallbackSite` 不再生效
>（本文档 §1 Non-Goal「不新增独立 provider id」同时作废）。本文档只保留为历史证据，不再作为现行约束。

> 文档类型：开发设计方案
> 关联模块：平台适配器
> 创建时间：2026-10-16
> 最后更新：2026-10-18
> 当前状态：已实施，待人工验证（静态检查通过；动态验证见 §9，由用户执行）
> 关联改动文件：`src/providers/qoder.ts`、`src/providers/base.ts`、`src/account-store.ts`、`src/cli/capture.ts`、`src/cli/account.ts`、`src/cli/probe.ts`、`src/server.ts`、`public/index.html`、`public/app.js`、`public/styles.css`、`accounts.example.json`、`.env.example`、`README.md`、`docs/凭证配置指南.md`、`docs/modules/项目整体-代码Wiki.md`

---

## 1. 背景与目标

**结论：Qoder 国际站（`qoder.com`）目前是「半支持」——取数解析层已兼容国际站报文，但卡片链接、凭证抓取、账号添加三处仍写死国内站，且没有站点切换/回退与站点级错误提示。本次把它补齐为 `qoder` 适配器的一等站点模式（`options.site: "cn" | "global" | "auto"`）。**

- 目标：
  1. **配置**：`options.site` 支持 `cn`（默认，向后兼容）/ `global` / `auto`；`global` 账号的认证、端点、面板/用量页链接全部落到 `qoder.com`；
  2. **看板**：国际站卡片的可用状态（region 标签「国际」）、服务指标（余额 / 批次 / 进度条 / 日级用量）与异常告警（站点失败原因、回退说明）正确展示；
  3. **切换与回退**：`auto` 自动探测可用站点；显式站点可用 `options.fallbackSite: true` 开启失败回退；两者都在卡片 note 与日志中如实标注实际生效站点；
  4. **凭证与错误处理**：`npm run capture -- qoder --site global` 一键抓取国际站登录态；站点不匹配（Cookie 属于另一站点）时给出可操作的诊断提示，而不是笼统的「解析失败」。
- Non-Goals（明确不做）：
  - **不新增独立 provider id（如 `qoder-global`）**：`src/providers/index.ts:52-56` 的别名表已把 `qoder.com` / `qoder-global` 归一为 `qoder`，且 `parseQoderUsage` 已双站点命名兼容；新增独立适配器会重复 ~400 行解析逻辑，并把「同平台两个站点」拆成两个平台，与现有账号/缓存/聚合模型不一致；
  - 不改 `src/types.ts` 契约、不改 `aggregate.ts` 排序与聚合口径；
  - 不做「同一次刷新并发打两个站点」（串行探测，失败快、不放大上游压力）；
  - 不改变国内站既有行为（默认 `site: "cn"` 时探测路径、字段解析、批次口径与现在完全一致）。

## 2. 现状调研

**结论：现网代码已具备国际站的解析能力与站点参数，但站点信息没有贯穿到「链接 / 抓取 / 账号添加 / 错误提示」四处，形成 5 个具体缺口（下表）。**

### 2.1 已具备的能力（可复用）

| 能力 | 位置 | 说明 |
| --- | --- | --- |
| 站点 → origin 解析 | `src/providers/qoder.ts:72-77` | `options.origin` 覆盖优先；`site === 'global'` → `https://qoder.com`，否则 `https://qoder.com.cn` |
| 双站点报文兼容 | `src/providers/qoder.ts:140-183` | 兼容大陆站 snake_case（`total_quota.quota_summary`）与国际站 camelCase（`{ data: { totalQuota, sharedQuota, nextResetAt } }`），并累加 `sharedQuota` |
| region 覆盖 | `src/providers/qoder.ts:359` | probe 内 `{ ...qoderAdapter, region: site === 'global' ? 'global' : 'cn' }`，快照构建读 `adapter.region`（`base.ts:269`） |
| 别名归一 | `src/providers/index.ts:52-56` | `qoder.com` / `qoder-global` / `qoder.com.cn` / `qoder.cn` → `qoder` |
| 失败兜底 | `src/service.ts:203-222` | 失败回退上一次成功值并打 `stale`，前端显示「数据可能过期」黄标（`public/app.js:394`） |
| 卡片状态与告警渲染 | `public/app.js:389-410`、`public/app.js:1111-1141` | region 标签、失败 chip、诊断区按 `status + message` 逐平台列出 |

### 2.2 缺口清单（本次要修）

| 编号 | 缺口 | 证据 |
| --- | --- | --- |
| G1 | 卡片「打开面板 / 每日用量」链接固定国内站，国际站账号被带到错站点 | `src/providers/qoder.ts:225-226`（静态 `dashboardUrl` / `usageUrl`）；`base.ts:279-280` 直接取自 adapter |
| G2 | 无法配置/表达「自动选站」；站点配错时报文错误笼统（「响应缺少 total_quota」），不提示站点可能不匹配 | `qoder.ts:144`、`qoder.ts:375-383` |
| G3 | `capture` 无法抓国际站 Cookie：永远打开国内站用量页、优先取 `qoder.com.cn` 的请求头 | `src/cli/capture.ts:182-190`（`usageUrl` / `apiHost` 固定）、`capture.ts:507` |
| G4 | 看板 / CLI 添加 Qoder 账号无法写入 `options.site`：通用实现恒写 `options: {}`，验证时也传 `options: {}` | `src/account-store.ts:500-506`、`src/account-store.ts:418-421` |
| G5 | `site` 从 `origin` 反推的逻辑不可靠（origin 为非 Qoder 域名时被误判为 `global`） | `qoder.ts:311` |

### 2.3 待确认（不编造）

- 国际站 API 路径是否与国内站完全一致（`/api/v2/me/usages/big_model_credits`、`/api/v1/me/userplan`、`/api/v1/me/usages/big_model_credits/histories`）：**待人工用真实国际站 Cookie 跑 `npm run probe -- qoder` 验证**（AI 不代跑，见 §9）。若不一致，只需改 §3 的站点表路径常量，解析逻辑不动。
- 国际站 `totalQuota.quotaSummary` 的字段是否仍为 `usedValue / limitValue / remainingValue`：同上，由 `readSummary`（`qoder.ts:86-100`）的双命名兼容覆盖。
- 国际站设备流主机为 `https://openapi.qoder.sh`（与第三方实现 `@magpie-community/opencode-qoder-auth` 所述一致：授权页在 `qoder.com`、轮询打到 `openapi.qoder.sh`），**待人工用真实国际站账号扫码验证**；其 `/api/v1/deviceToken/refresh` 与 `/api/v2/quota/usage` 是否同主机同路径也待确认（§9）。

### 2.4 2026-10-18 复查：仍有 3 个未打通点

**结论：站点表与 Cookie 链路已完整，但「登录链路」「令牌模式站点」「国际站 .env 槽位命名」三处仍把国际站当成国内站，导致国际站账号只能靠手改配置获得。**

| 编号 | 缺口 | 证据 |
| --- | --- | --- |
| G6 | 扫码登录无法选站点：`login.start` 已读 `input.options.site`，但前端 `/api/login/start` 只发 `{ provider }`，登录块也没有站点下拉 → 国际站用户只能扫到国内站授权页 | `public/app.js:975-977`（只发 provider）、`src/providers/qoder.ts:1042`（已支持但收不到）、`public/index.html:157-178`（登录块无站点控件） |
| G7 | 令牌模式站点错位：`attempt()` 的 openapi 兜底固定用 `sitePlan.candidates[0]`（`auto` 时恒为 cn）；令牌续期 `qoderRefreshToken()` 也固定取 `candidates[0]` → `site: "auto"` 或回退到国际站的账号，会用国内站 openapi 主机刷新/兜底，必然失败 | `src/providers/qoder.ts:224-262`、`src/providers/qoder.ts:130` |
| G8 | 国际站账号的 `.env` 槽位名不符约定：通用添加用 `qoder` + `cookie` 推导出 `QODER_COOKIE`，而文档与示例声明国际站应写 `QODER_GLOBAL_COOKIE`；第二个国际站账号会占用 `QODER_COOKIE_2` | `src/account-store.ts:118-121`（`envNameFor` 只看 credential key）、`docs/凭证配置指南.md:155-157`、`accounts.example.json:48-60` |

> G8 不涉及「已写入的真实变量改名」——`accounts.json` 里存的是 `${VAR}` 引用，改的只是**新建账号时**选的变量名；已有账号引用保持原样（`referencedEnvNames()` 直接读引用）。

## 3. 方案设计

**结论：在 `qoder.ts` 内建「站点表 + 候选链」两层抽象；probe 沿候选链串行探测，首个成功站点即生效站点，并把站点信息写入 note、把链接按站点覆盖进快照；`manageFields` 新增 option 字段使站点可在看板下拉选择。**

数据流：

```
accounts.json（options.site: cn | global | auto，可选 fallbackSite）
      │
      ▼
resolveSitePlan(account)                     ← 站点表 QODER_SITES + 候选链
   ├─ origin 覆盖 / site=cn|global → 单候选（fallbackSite=true 时追加另一站）
   └─ site=auto → 候选链 [cn, global]
      │
      ▼
probe()：沿候选链串行请求
   ├─ 成功 → 生效站点 = 该站点
   │     ├─ buildSnapshot({ ...qoderAdapter, region, dashboardUrl, usageUrl })
   │     └─ note：站点标签 + 回退说明（若生效 ≠ 首选）
   └─ 全部失败 → failure(status 取最可操作者, message 汇总各站原因 + 配置建议)
      │
      ▼
service.ts 并发/缓存/stale 兜底（不改）
      │
      ▼
/api/summary → 卡片：region 标签「国际/国内」、note 站点行、失败 message 进诊断区
```

职责划分：

| 层 | 职责 | 是否改动 |
| --- | --- | --- |
| 适配器 `qoder.ts` | 站点表、候选链、探测、链接覆盖、提示文案 | 主要改动 |
| 契约 `base.ts` | `ManageFieldDescriptor` 增加 `target: 'option'` / `choices` / `defaultValue` | 扩展（向后兼容，默认值等价原行为） |
| 编排 `service.ts` / 聚合 `aggregate.ts` / 契约 `types.ts` | 并发、缓存、排序、快照结构 | 不改 |
| 前端 `app.js` | 按字段声明渲染输入框或下拉框 | 小改（`choices` 分支） |
| 工具箱 | capture 双站点、account CLI option 透传、server 接口透传 | 小改 |

## 4. 接口设计

### 4.1 上游接口（按站点 origin 前缀组装）

| 用途 | 路径 | 站点差异 |
| --- | --- | --- |
| 用量（必需） | `GET {origin}/api/v2/me/usages/big_model_credits` | 待确认（§2.3） |
| 套餐（可选，fail-soft） | `GET {origin}/api/v1/me/userplan` | 待确认 |
| 用量明细 | `GET {origin}/api/v1/me/usages/big_model_credits/histories` | 待确认 |
| 额度流水 | `GET {origin}/api/v1/me/quotas/big_model_credits/histories` | 待确认 |

- 请求头：`Cookie` + `Origin` + `Referer`（按站点） + `Accept-Language`（国内站 `zh-CN` 优先 / 国际站 `en-US` 优先）+ `x-requested-with`；
- 路径与站点元数据集中在 `QODER_SITES` 常量，若实测国际站路径不同，只改这一处（解析层不动）。

### 4.2 站点表与候选链（适配器内部契约）

```ts
const QODER_SITES = {
  cn:     { origin: 'https://qoder.com.cn', dashboardUrl: 'https://qoder.com.cn/account/usage',
            usageUrl: 'https://qoder.cn/account/usage?client=qoder&sourceType=IDE', label: '国内站' },
  global: { origin: 'https://qoder.com',    dashboardUrl: 'https://qoder.com/account/usage',
            usageUrl: 'https://qoder.com/account/usage', label: '国际站' },
};
```

候选链规则（`resolveSitePlan(account)`）：

| 配置 | 候选链 | 回退行为 |
| --- | --- | --- |
| `site: "cn"`（默认）/不写 | `[cn]` | 不回退（失败即失败，message 提示可改 `auto`/`global`） |
| `site: "global"` | `[global]` | 同上 |
| `site: "cn"` + `fallbackSite: true` | `[cn, global]` | 失败自动试另一站，成功则 note 标注回退 |
| `site: "auto"` | `[cn, global]` | 同回退逻辑，但视为正常路径（note 标注生效站点） |
| `options.origin` 覆盖 | 单候选（域名推断站点） | 不回退（自定义域名/代理不受站点切换影响） |
| `site` 取值非法 | 按 `cn` 处理 + note 提示取值无法识别 | 不回退 |

失败状态选取：优先「最可操作」的状态（`unauthorized` > `forbidden` > `rateLimited` > `notFound` > `unavailable` > `error`），
例如国内站 Cookie 失效（401）+ 国际站不可达时，结论是 `unauthorized`（提示重抓 Cookie 才是对的）。

### 4.3 通用账号管理接口（既有接口的字段扩展）

| 接口 | 变更 |
| --- | --- |
| `GET /api/providers` | `manageFields[]` 元素新增 `target`（`credential` 默认 / `option`）、`choices`（下拉候选）、`defaultValue` |
| `POST /api/accounts/add` | body 新增可选 `options`（`Record<string, string>`），透传给 `addManagedAccount` |
| CLI `npm run account` | 新增 `--option site=global`（可重复）；option 字段不占 `.env` 槽位、不进掩码预览 |
| `POST /api/login/start` | **已支持 `options`**（`server.ts:519-532`），本轮由前端补发 `{ site }`；后端无需改动 |
| `ProviderAdapter.envNames` | 由静态映射扩展为 `Record<string, string> \| ((options) => Record<string, string>)`，让「同一平台按站点换变量前缀」成为契约能力（向后兼容：现有三个静态映射行为不变） |

#### 4.4 登录站点选择（前端）

| 项 | 设计 |
| --- | --- |
| 渲染条件 | 适配器只声明了一个 option 字段、且该字段 `choices ∈ {cn, global}` 时，在登录块渲染站点下拉（`public/index.html` 的 `.login__select`）；没有站点声明的平台（CodeBuddy / Trae）不出现任何新控件 |
| 取值来源 | 与手工表单共用同一份 `manageFields`（`/api/providers`），不新造元信息 |
| 请求 | `POST /api/login/start` body 变为 `{ provider, options: { site } }` |
| 落库 | 适配器 `login.poll()` 已在 `options.site` 回传生效站点（`qoder.ts:1115-1119`），`landLoginAccount()` 原样写入 `accounts.json`，因此登录后的账号天然带正确站点、`probe` 走对应 origin |
| 会话隔离 | 站点下拉的值随会话冻结：登录会话发起后改下拉不影响已发起会话（后端 `secret.site` 已固定） |

#### 4.5 令牌模式的站点归属

| 项 | 设计 |
| --- | --- |
| 生效站点 | 令牌续期与 openapi 兜底都改用「本次实际生效站点」= 首选候选；`auto` 时若网页接口在国际站命中，则兜底与续期也用国际站 |
| 实现 | `probeQoderWithToken()` 记住首次成功的 `site`，`qoderRefreshToken()` 接收 `site` 参数而不是自己再算一次 |
| 回退语义 | 续期与兜底只在**同一站点**内进行：不跨站刷新令牌（两个站点的令牌互不通用） |

## 5. 数据模型

| 变更 | 内容 |
| --- | --- |
| `accounts.json` Qoder 条目 | `options.site`：`"cn"`（默认）/ `"global"` / `"auto"`；可选 `options.fallbackSite: true`、`options.origin`（历史行为保留） |
| `accounts.example.json` | `qoder` 条目补注释；新增一条 `"Qoder 国际站"` 示例（`enabled: false`） |
| `.env` 新增键 | `QODER_GLOBAL_COOKIE`（国际站槽位 1）、`QODER_GLOBAL_COOKIE_2`（槽位 N）；国内站沿用 `QODER_COOKIE[_N]` |
| 快照映射 | 不新增字段：生效站点通过 `region`（`cn`/`global`）+ `note`（站点行）+ `dashboardUrl`/`usageUrl`（按站点覆盖）表达 |
| 契约 | `ProviderSnapshot` / `CreditLot` / `QuotaWindow` 均不变；仅 `ManageFieldDescriptor` 扩展 3 个可选字段 |
| 配置迁移 | 无（`site` 默认 `cn` 与现状等价） |

## 6. 影响面评估

- **聚合与排序**：不变。国际站快照与国内站走同一 `buildSnapshot` / 批次口径，正常进入「最快到期」与合计；
- **缓存**：不变（同一账号一个缓存键；`auto` 的探测结果随快照缓存 TTL 生效，不会每次刷新重试）；
- **前端**：卡片渲染由快照驱动，region 标签自动显示「国际」；新增 `manageFields` 的 `choices` → 下拉框分支（`app.js` 一处 if 分支）；
- **抓取工具**：`capture.ts` 新增 `--site` 参数与 Qoder 站点变体，不影响既有平台与既有命令语义（不带 `--site` 行为完全不变）；
- **账号管理**：`account-store.ts` 从「只写 `options: {}`」改为「写入收集到的 option 字段」，对未声明 option 字段的平台无行为差异；
- **回归范围**：`npm run probe`（全平台）、国内站 Qoder 卡片、CodeBuddy 的通用增删（走同一份实现，重点回归）。
- **凭证安全**：国际站 Cookie 与国内站同等对待，只写本地 `.env`（0600、已 gitignore），日志与文档不出现明文。

## 7. 风险与备选方案

| 风险 | 说明 | 缓解 |
| --- | --- | --- |
| 国际站接口路径/字段与国内站不同 | §2.3 待确认 | 站点表集中常量；解析层双命名兼容；失败提示可操作；实测后只改常量 |
| `auto` 增加一次失败请求 | 串行探测最多 2 次请求 | 仅 `auto` / `fallbackSite` 账号受影响；失败响应快（401/结构不符），单账号超时上限仍受 `requestTimeoutMs` 约束 |
| 站点回退掩盖配置错误 | 「配了 global 实际用 cn」会让用户误以为配置生效 | 默认**不**隐式回退；显式 `fallbackSite`/`auto` 才回退，且 note 与日志都标注回退路径与原因 |
| `manageFields` 扩展影响 CodeBuddy 表单 | 现有字段无 `target`，默认 `credential` | 默认值等价原行为；`choices` 缺省不渲染下拉；回归用例见 §9 |
| Qoder 开放「＋ 添加账号」后多出删除按钮 | 声明 `manageAccounts` 后卡片出现管理区 | 与既有 CodeBuddy 一致的交互；删除走通用实现（先备份），符合看板既定习惯 |
| 备选方案（否决） | 新增独立 `qoder-global` 适配器 | 与别名表/账号模型冲突、解析逻辑重复 ~400 行、两个站点无法共享缓存与多账号约定；若未来国际站分化成独立产品（接口彻底不同），再拆不迟 |

## 8. 实施步骤

（2026-10-16 已按下方顺序全部落地，代码见关联改动文件；落地细节以本文件 §3–§5 为准。）

| 步骤 | 产出物 |
| --- | --- |
| ① 契约扩展 | `src/providers/base.ts`：`ManageFieldDescriptor` 增加 `target` / `choices` / `defaultValue`，`ManageOptionField` 语义注释 |
| ② 适配器 | `src/providers/qoder.ts`：站点表、`resolveSitePlan`、候选链探测、链接按站点覆盖、note/错误提示、ledger 按站点、`manageFields`（cookie + site 下拉）声明 |
| ③ 账号通道 | `src/account-store.ts`（option 收集/写入/验证传入/去重纳入 options）、`src/server.ts`（透传 `options`）、`src/cli/account.ts`（`--option k=v`） |
| ④ 前端 | `public/app.js`：`manageFields` 的 `choices` 渲染为 `<select>` |
| ⑤ 抓取 | `src/cli/capture.ts`：`--site cn\|global` + Qoder 站点变体（`QODER_GLOBAL_COOKIE[_N]`） |
| ⑥ 配置与文档 | `accounts.example.json`、`README.md` 平台表、`docs/凭证配置指南.md` 新增「Qoder 国际站」小节与排错行 |
| ⑦ 静态检查 | `npm run typecheck` + IDE 诊断（本仓库约定只做静态检查） |

## 9. 验证方式

- 静态：`npm run typecheck`、`node --check public/app.js`、IDE 诊断（本次由 AI 执行）；
- 动态（由用户执行，AI 不代跑，会请求真实上游）：
  1. `npm run chrome:debug` → 登录 `https://qoder.com` 并停在用量页；
  2. `npm run capture -- qoder --site global` → 预期写入 `QODER_GLOBAL_COOKIE`（终端只显示掩码）；
  3. `npm run probe -- qoder` → 预期 `ok`，输出余额/批次；若报「响应缺少 total_quota」，说明国际站路径待重新取证（见 §2.3）；
  4. 看板核对国际站卡片：region 标签「国际」、note 含站点行、面板/用量链接指向 `qoder.com`；
  5. `npm run probe -- --json` 交叉检查 `region` / `dashboardUrl` / `usageUrl` 字段；
  6. 回归：`npm run probe`（全平台）+ 国内站 Qoder 卡片不变；CodeBuddy「＋ 添加账号」表单仍为单输入框、增删正常；
  7. `auto` 验证：把 `options.site` 改为 `"auto"`，刷新页面，note 应标注实际生效站点。
  8. **登录站点（G6）**：Qoder 卡片「＋ 登录 / 添加账号」→ 站点下拉切「国际站 qoder.com」→ 扫码；
     预期授权页为 `qoder.com/device/selectAccounts…`、落库后 `accounts.json` 的
     `options.site` 为 `global`、凭证变量为 `QODER_GLOBAL_ACCESS_TOKEN` / `QODER_GLOBAL_REFRESH_TOKEN`；
     若国际站设备流主机不可用（§2.3 待确认），登录应在落库前的 probe 阶段失败并提示，**不会写入半截配置**。
  9. **变量命名（G8）**：看板「＋ 添加账号」选 `global` 粘贴国际站 Cookie → 预期新增/更新
     `QODER_GLOBAL_COOKIE`，国内站账号引用的 `QODER_COOKIE` 不受影响（`npm run account -- --list` 交叉核对）。
  10. **令牌续期（G7）**：国际站令牌账号把 `accessToken` 置为无效值后刷新，
      预期日志为「Qoder 国际站登录令牌已失效，尝试用 refreshToken 续期」并打到 `openapi.qoder.sh`，
      而不是国内站 `openapi.qoder.com.cn`。

## 变更记录

| 时间 | 变更内容 | 触发原因 | 影响章节 |
| --- | --- | --- | --- |
| 2026-10-16 | 初版：现状调研（5 个缺口）、站点表 + 候选链方案、契约与工具链改动、风险与验证 | 任务启动：用户要求为看板补齐 Qoder 国际站完整支持 | 全篇 |
| 2026-10-16 | 代码落地：`qoder.ts` 站点表 + 候选链（cn / global / auto / `fallbackSite`）、region 与面板/用量链接按生效站点覆盖、note 标注站点与回退、失败文案给站点专项建议；`base.ts` 的 `ManageFieldDescriptor` 扩展 `target: 'option'` / `choices` / `defaultValue`；`account-store.ts` 收集并写入 options、验证时按所选站点探测、去重纳入 options；`server.ts` 透传 `options`；`app.js`/`styles.css` 渲染站点下拉；`account.ts` 支持 `--site global` 与 `--option k=v`；`capture.ts` 支持 `--site global`（写 `QODER_GLOBAL_COOKIE`，槽位提示带站点参数）；`probe.ts` 输出 note 首段（站点/回退说明）；示例与文档同步（README / 凭证配置指南 / accounts.example.json / .env.example / 代码Wiki 行号）| 用户授权直接实施 | 2.2 / 3 / 4 / 5 / 6 / 8 |
| 2026-10-16 | 静态检查通过：`npm run typecheck`（tsc --noEmit）无错误、IDE 诊断无错误/警告；动态验证（国际站真实 Cookie 的 probe / capture）按 §9 待用户执行 | 收口核对 | 8 / 9 |
| 2026-10-18 | 复查发现 3 个未打通点（G6/G7/G8）并补齐：① 登录块新增站点下拉（`index.html` / `app.js`），`/api/login/start` 带 `options.site`，会话视图回显 `site`（`login-session.ts`）；② 令牌模式 openapi 兜底与 `deviceToken/refresh` 改用「本次实际生效站点」（新增 `QoderTokenFailure` 记录失败站点），不再固定 `candidates[0]`；③ `ProviderAdapter.envNames` 支持函数形式，`envNameFor()` 接收即将写入的 options，Qoder 国际站 Cookie/令牌落到 `QODER_GLOBAL_*`（`account-store.ts` 添加与登录落库两处均透传）；`.env.example` 补 `QODER_GLOBAL_ACCESS_TOKEN` / `QODER_GLOBAL_REFRESH_TOKEN`，README 与凭证指南同步 | 用户要求「为项目添加 Qoder 国际版的账号支持」，复查已有实现后的收口 | 2.4 / 4.3 / 4.4 / 4.5 / 9 |
