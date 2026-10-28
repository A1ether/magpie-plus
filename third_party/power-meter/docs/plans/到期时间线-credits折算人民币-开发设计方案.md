# 到期时间线 - 各平台 credits 折算人民币 - 开发设计方案

> 文档类型：开发设计方案
> 关联模块：到期时间线（聚合层 `src/aggregate.ts` + 前端 `public/app.js`）
> 创建时间：2026-10-25
> 最后更新：2026-10-25
> 当前状态：已完成（代码已落地，静态检查通过；看板人工回归未执行，见 §9）
> 关联改动文件：`src/credit-value.ts`（新增）、`src/types.ts`、`src/aggregate.ts`、`public/app.js`、`public/styles.css`、`README.md`、`docs/modules/项目整体-代码Wiki.md`

---

## 1. 背景与目标

**结论：在「到期时间线」每条事件的额度后面，增加一个人民币估值，让「还有多少额度要过期」能直接换算成钱；换算口径必须写清楚、可核对、可一处修改。**

### 1.1 要做的事

| 目标 | 说明 |
| --- | --- |
| 时间线行内新增人民币 | 每条 `amount + currency` 后面补 `≈ ¥12.34`（如 `500 CREDITS ≈ ¥25.00`） |
| 换算口径可见 | 悬停即出换算依据（平台官方单价出处 / DeepSeek 基准出处 / 套用的比值），不做「黑箱数字」 |
| 口径集中一处 | 单价、比值、出处全部写在 `src/credit-value.ts` 一张表里，调价时只改这一个文件 |
| 用户确认的原则 | ① 官方锚点取各平台「积分加量包 / 增购积分」单价（2026-10-25 用户确认）；② 官方没有可换算规则时，按 DeepSeek V4.1 Flash 为基准；③ A1etherOpenAi 只显示 USD/QUOTA，不折算人民币 |

### 1.2 Non-Goals（本次明确不做）

1. **不做跨单位相加**：`Currency` 是「不做换算、按单位分开计」的契约（`src/types.ts:35-39`），本次只在**每条事件**上加一个估值，**不改** `totals.availableByCurrency` 的聚合口径，也不新增「合计 ¥」。
2. **不改任何适配器的取数逻辑**：换算只依赖「平台 id + 站点 + 币种 + 金额」，不需要新增上游请求。
3. **不折算 A1etherOpenAi**：其 `QUOTA` 与 `USD` 是同一笔钱的两套计量（`src/providers/a1etheropenai.ts:152`），按用户 2026-10-25 的决定只显示原单位。
4. **不给自建 / 示例平台猜价钱**：`custom` / `demo:*` 没有官方单价，本次直接不显示估值（不臆造）。
5. **不改缓存与刷新语义**：估值是纯函数计算，随 `/api/summary` 一起产出，不走独立接口、不落盘。

---

## 2. 现状调研

### 2.1 时间线是怎么生成的

链路：适配器 `probe()` → `ProviderSnapshot.credits[]` → `src/aggregate.ts` 聚合 → `DashboardPayload.expiryTimeline` → `public/app.js` 渲染。

| 环节 | 位置 | 关键事实 |
| --- | --- | --- |
| 事件流生成 | `src/aggregate.ts:206-252` | `buildExpiryTimeline()`：取每条 `lot.expiresAt !== null && lot.amount > 0` 的批次，按过期时间升序；平台只有窗口重置时间时补一条 `window-boundary` 合成事件（`:240-247`） |
| 事件契约 | `src/types.ts:210-219` | `ExpiryHit`：`provider / displayName / lotId / lotLabel / amount / currency / expiresAt / inDays` |
| 平台站点 | `src/types.ts`（`ProviderSnapshot.region`）、`src/providers/qoder.ts:606-611` | `region: 'cn' \| 'global'`，Qoder 按生效站点覆盖，**同一平台两个站点的单价不同** |
| 前端渲染 | `public/app.js:2194-2218` | `renderTimeline()` 逐条拼 `when / name / amount / badge` 四个 span |
| 行样式 | `public/styles.css:1629-1680` | `.timeline li` 是 4 列网格 `minmax(88px,118px) 1fr auto auto` |
| 数字格式化 | `public/app.js:186-196` | `fmtNumber()` / `fmtDate()`，金额统一走 `Intl.NumberFormat` |

### 2.2 可复用的既有能力

| 能力 | 位置 | 用途 |
| --- | --- | --- |
| `Currency` 单位枚举 | `src/types.ts:39` | 只对 `CREDITS` 折算，其余原样显示 |
| 聚合层纯函数 | `src/aggregate.ts:206` | 估值在生成事件时算好，前端零逻辑 |
| `element()` / `title` 悬停 | `public/app.js:247-252` | 行内追加一个 `<em>`，换算口径写进 `title`，不新增交互 |

### 2.3 各平台官方单价（2026-10-25 查证）

| 平台 | 官方锚点（真金白银） | 折算单价 | 出处 |
| --- | --- | --- | --- |
| Trae CN | 增购积分 ¥50 / 1,000 积分 | ¥0.05 / 积分 | `docs.trae.cn` 套餐与计费 |
| 腾讯 CodeBuddy | 个人加量包 ¥50 / 1,000 积分 | ¥0.05 / 积分 | `codebuddy.cn/docs/ide/Account/pricing` |
| 华为云 CodeArts 码道 | 个人 1,000 积分包 ¥50 | ¥0.05 / 积分 | `codearts.huaweicloud.com/pricing.html` |
| Qoder CN | 专业版 ¥59 / 月含 2,000 Credits | ¥0.0295 / Credit | `docs.qoder.cn` 计费说明 |
| Qoder 国际站 | 个人资源包 $20 / 1,500 Credits | $0.0133 / Credit | `docs.qoder.com/zh/account/pricing` |
| MiniMax | 积分包 ¥30 / 4,285 积分（≈ ¥7 / 1,000 积分）；国际站 1,000 Credits = $1 | ¥0.007 / 积分 | `platform.minimax.cn` 积分包；M Plan FAQ |
| A1etherOpenAi | 1 USD = 500,000 quota（仓库实测恒定，`README.md:478`） | 不折算 | 用户 2026-10-25 决定 |

### 2.4 DeepSeek V4.1 Flash 基准（`api-docs.deepseek.com/quick_start/pricing`）

| 项目 | 空闲档（基准） | 高峰档 |
| --- | --- | --- |
| 输入（未命中） | ¥1 / 百万 token | ¥2 / 百万 token |
| 输出 | ¥4 / 百万 token | ¥8 / 百万 token |
| 缓存命中输入 | ¥0.02 / 百万 token | ¥0.04 / 百万 token |

> 高峰时段＝北京时间周一至周五 09:00–12:00、14:00–18:00（不含法定节假日），其余时段空闲。**本方案默认按空闲档折算**，高峰档为其 2 倍。

### 2.5 各平台公布的「模型用量系数」（可用来做基准折算的部分）

| 平台 | 公布的系数 / 单价 | 能否直接折算 | 证据 |
| --- | --- | --- | --- |
| Trae | 该模型 token 单价：输入 ¥2 / 输出 ¥8 / 缓存 ¥0.04（每百万 token）；积分消耗速度 DeepSeek-V4.1-Flash 忙时 0.12×、会员空闲 0.06× | **能**：与 DeepSeek 官方空闲档（¥1 / ¥4 / ¥0.02）比值恰为 0.5（输入输出缓存三项一致） | `docs.trae.cn/enterprise_billing-items`、`docs.trae.cn/ide_limited-time-discount-for-builtin-models` |
| Qoder | Credits 用量参考系数：DeepSeek-Flash 0.1× | **不能**：0.1× 是模型间的相对系数，官方没有给出「1× 对应多少 token」 | `docs.qoder.com/zh/release-notes/model-consumption-reference-update` |
| CodeArts | 模型积分消耗系数：DeepSeek-V4.1-Flash 0.18 | **不能**：官方只说「系数越高消耗越多」，未公布 1× 基准 | `support.huaweicloud.com/price-codeartsagent`、`codearts.huaweicloud.com/changelog.html`（2026/10/20） |
| CodeBuddy | 只说「不同模型 Token 定价不同」，未公布系数表 | **不能** | `codebuddy.cn/docs/ide/Account/credits` |

**结论：Only Trae 目前能算出「基准口径」；Qoder / CodeArts / CodeBuddy 缺「1× 基准 = 多少 token」这一环，本次如实标注为待确认，不臆造数字。**

---

## 3. 方案设计

### 3.1 整体思路

```
        provider + region + currency + amount
                    │
                    ▼
    src/credit-value.ts  cnyValueFor()
    ├── 官方口径：积分加量包单价表（PEG_TABLE）
    └── 基准口径：DeepSeek V4.1 Flash 比值（flashRatio，可空）
                    │
        { cny, basis, altCny, altBasis, note }
                    ▼
   src/aggregate.ts  buildExpiryTimeline() → 每条 ExpiryHit 带 cny / cnyNote
                    ▼
   public/app.js renderTimeline() → 「500 CREDITS ≈ ¥25.00」（title = 口径明细）
```

### 3.2 口径规则（界面主数字）

| 优先级 | 条件 | 主数字 | 悬停补充 |
| --- | --- | --- | --- |
| 1 | 平台有官方积分单价（当前 6 个平台都有） | 官方口径金额 | 官方出处 + 基准口径（有则给算法与出处，无则写明缺哪一环） |
| 2 | 无官方单价但能算基准（未来平台） | DeepSeek 基准金额 | 标注「按 DeepSeek V4.1 Flash 基准折算」 |
| 3 | 两者都没有（`custom` / `demo` / 非 CREDITS 单位） | 不显示 | — |

### 3.3 职责划分

| 层 | 职责 |
| --- | --- |
| `src/credit-value.ts`（新增） | 唯一换算表 + 纯函数，零副作用、零网络 |
| `src/aggregate.ts` | 生成事件时调用，把结果写进 `ExpiryHit` |
| `public/app.js` | 只负责显示与悬停，不做任何换算 |

---

## 4. 接口设计

### 4.1 HTTP API

**无新增、无变更**：`/api/summary` 的响应体只是 `expiryTimeline[]` 每项多两个字段（`cny` / `cnyNote`），前端已有消费方无需改动。

### 4.2 模块契约（`src/credit-value.ts`）

| 导出 | 签名 | 说明 |
| --- | --- | --- |
| `cnyValueFor` | `(input: { provider: string; region: 'cn' \| 'global'; currency: Currency; amount: number }) => CnyValue \| null` | 算不出返回 `null`（界面不显示） |
| `CnyValue` | `{ cny: number; basis: string; altCny: number \| null; altBasis: string \| null; note: string }` | `note` 是拼好的多行口径说明，直接给 `title` |
| `PEGS` / `DEEPSEEK_FLASH` / `USD_CNY` | 常量表 | 调价时只改这里 |

### 4.3 CLI

无新增命令。

---

## 5. 数据模型

### 5.1 契约变更（`src/types.ts`）

| 类型 | 变更 | 说明 |
| --- | --- | --- |
| `ExpiryHit` | 新增 `cny: number \| null` | 官方口径人民币估值（元），`null` = 不折算 |
| `ExpiryHit` | 新增 `cnyNote: string \| null` | 换算口径明细（多行文本，前端直接写进 `title`） |
| `Currency` / `CreditLot` / `ProviderSnapshot` | **不变** | 不引入新币种，不把估值混进 `amount` |

### 5.2 新增常量（`src/credit-value.ts`）

| 常量 | 内容 |
| --- | --- |
| `USD_CNY` | `7.1`（只用于把美元计价的平台价折成人民币，Qoder 国际站） |
| `DEEPSEEK_FLASH` | 空闲档 ¥1 / ¥4 / ¥0.02 每百万 token，`peakMultiple: 2`，附官方定价页 URL |
| `PEGS` | `provider[:region]` → `{ cnyPerUnit, official, flashRatio, flash }`（见 §2.3、§2.5） |

### 5.3 配置项

`.env` / `accounts.json` **无新增键**。基准档位（空闲 / 高峰）与汇率是代码常量，改口径改表即可。

---

## 6. 影响面评估

| 面 | 影响 |
| --- | --- |
| 适配器（`src/providers/*`） | 无改动 |
| 聚合层 | `buildExpiryTimeline()` 多算一个纯函数结果；`totals`、排序、缓存键不变 |
| 前端时间线 | 每条多一个 `<em class="timeline__cny">`；网格列数不变（嵌在 `.timeline__amount` 里），移动端布局不受影响 |
| 其他消费方 | `/api/summary` 新增字段，向后兼容（老前端忽略即可） |
| 「最快到期」语义 | 不受影响：估值只加显示，不参与排序 |

---

## 7. 风险与备选方案

| 风险 / 备选 | 处理 |
| --- | --- |
| **平台调价**导致数字过时 | 单价集中在 `PEGS`，每项都带出处文案；调价只改一处 |
| **DeepSeek 档位**（空闲 / 高峰）选择影响 2 倍 | 默认空闲档，悬停显式写明「空闲档；高峰档为 2 倍」，改常量可切 |
| **美元计价平台**（Qoder 国际站）需要汇率 | 用 `USD_CNY = 7.1` 常量并在悬停写明「按 1 USD = ¥7.1 折算」，避免实时汇率请求 |
| Qoder / CodeArts / CodeBuddy **缺 1× 基准**，基准口径算不出 | 不给估算数字，悬停写明「官方公布了模型系数 X，但没有 1× 基准的 token 定义」；待补后再算 |
| ~~备选：1 积分 = 1 百万 token 按 Flash 价折算~~ | **否决**：MiniMax 6,198 积分会算成 ¥1.55 万，量级明显失真（2026-10-25 用户已排除） |
| ~~备选：无官方规则时统一按 ¥0.05/积分兜底~~ | **否决**：用户要求按平台自身体系 + DeepSeek 基准，而不是跨平台套用同一个价 |
| 活动赠送积分与购买积分真实价值不等 | 官方锚点取「加量包 / 增购」单价（用户确认），悬停写清来源，不做加权 |

---

## 8. 实施步骤

| 步骤 | 产出物 |
| --- | --- |
| 1. 新增 `src/credit-value.ts`：常量表 + `cnyValueFor()` | 换算唯一入口，含出处文案 |
| 2. `src/types.ts`：`ExpiryHit` 增加 `cny` / `cnyNote` | 契约扩展 |
| 3. `src/aggregate.ts`：`buildExpiryTimeline()` 填两个字段 | 服务端算好，前端零逻辑 |
| 4. `public/app.js`：`renderTimeline()` 显示 `≈ ¥X` + `title` 口径 | 行内展示 |
| 5. `public/styles.css`：`.timeline__cny` 次要样式 | 不破坏 4 列网格与移动端断点 |
| 6. 文档同步：`README.md`、`docs/modules/项目整体-代码Wiki.md` | 口径与契约写进入口文档 |
| 7. 静态检查：`npm run typecheck`、`node --check public/app.js` | 见 §9 |

---

## 9. 验证方式

**本次只做静态检查（AGENTS.md §6），不启动服务、不跑 probe、不向上游发请求。**

| 项 | 命令 / 方式 | 结果 |
| --- | --- | --- |
| 类型检查 | `npm run typecheck` | **通过**（exit 0） |
| 前端语法 | `node --check public/app.js` | **通过** |
| 换算纯逻辑自检（离线、只读代码，未起服务、未打上游） | `npx tsx` 跑 `cnyValueFor()` 与 `buildExpiryTimeline()` 的本地脚本 | 见下表 |
| 看板人工回归 | `npm run dev` → 打开看板 → 时间线 | **未执行（AI 不代跑）** |

换算自检实测（2026-10-25）：

| 输入 | 官方口径 | DeepSeek V4.1 Flash 基准 |
| --- | --- | --- |
| Trae 4,900 CREDITS | ¥245 | ¥122.50（比值 0.5） |
| Qoder CN 2,000 CREDITS | ¥59（与「专业版 ¥59/月含 2,000 Credits」对得上） | 无（缺 1× 基准） |
| Qoder 国际站 1,500 CREDITS | ¥142 | 无 |
| CodeBuddy 4,900 CREDITS | ¥245 | 无 |
| CodeArts 6,198 CREDITS | ¥309.90 | 无 |
| MiniMax 6,198 CREDITS | ¥43.39 | 无 |
| A1etherOpenAi 250,000 QUOTA | null（不折算，按原单位显示） | — |
| custom-a 1,000 CREDITS | null（无官方单价，不编数字） | — |

同时确认 `buildExpiryTimeline()` 产出的每条事件都带上了 `cny`（用 Trae 400 CREDITS 的合成快照实测 → `cny: 20`），悬停文案三行齐全。

**人工回归步骤**：`npm run dev` 起服务 → 打开看板 → 「到期时间线」每条额度后应出现 `≈ ¥X`，
鼠标悬停出现「官方口径 / DeepSeek V4.1 Flash 基准 / 说明」三行；A1etherOpenAi 的行只有 `QUOTA`、没有 `≈ ¥`。

---

## 变更记录

| 时间 | 变更内容 | 触发原因 | 影响章节 |
| --- | --- | --- | --- |
| 2026-10-25 | 初版：确定双口径（官方加量包单价为主、DeepSeek V4.1 Flash 为基准）、单位单价表与出处、Trae 可折算而 Qoder/CodeArts/CodeBuddy 待确认 | 任务启动 | 全篇 |
| 2026-10-25 | 实施完成并回填 §9 实测结果：新增 `src/credit-value.ts`，`ExpiryHit` 加 `cny`/`cnyNote`，时间线行内显示 `≈ ¥X` + 悬停口径；同步 README 与代码 Wiki | 代码落地、静态检查通过 | 元信息、§5、§8、§9 |