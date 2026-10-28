# 前端看板 · 高级 Dashboard 菜单与视觉重构 开发设计方案

> 后续方案见 `docs/plans/前端看板-界面体验整体重构-开发设计方案.md`（2026-10-21）：本文记录的是 2026-10-20 已完成的「菜单外壳」重构，新一轮整体重构将取代其视觉语言与信息架构；本文保留作为历史决策记录。

> 文档类型：开发设计方案
> 关联模块：前端看板
> 创建时间：2026-10-20
> 最后更新：2026-10-20
> 当前状态：已完成
> 关联改动文件：`public/index.html`、`public/styles.css`、`public/app.js`

---

## 1. 背景与目标

**结论**：现有看板「能用但不好看」——导航被压在顶栏里、区块只能靠滚动找、告警条整块铺粉底、卡片高度参差、同层元素共用一套表面样式导致层级扁平、emoji 图标带廉价感。本次按抖音教程《6种高级Dashboard菜单》（作者：叨叨AI）的菜单范式重做**视觉语言 + 导航外壳**，落地 5 种菜单模式，业务取数与卡片渲染逻辑保持原样。

### 1.1 要解决的问题

| 编号 | 问题 | 现状证据 |
| --- | --- | --- |
| P1 | 全站没有导航，顶栏只有品牌与三个按钮，区块之间只能靠滚轮找 | `public/index.html:13-30`（顶栏只有 brand + actions）、`public/index.html:32-75`（`main.wrap` 内 6 个 section 纵向堆叠，无任何 `nav`/锚点）；旧版 `public/index.html` 中 `data-section` 属性出现 0 次 |
| P2 | 顶栏同时承担品牌与全部操作，按钮挤成一行 | `public/styles.css:120-124`（`.topbar__actions` 单行 flex，无换行策略）、`public/index.html:22-29`（3 个控件并排） |
| P3 | 告警条整块铺色过重，还用 emoji 当图标 | `public/styles.css:283-286`（`.alert--danger` 整块 `color-mix(danger 10%)` 铺底）、`public/styles.css:288-291`（`.alert--warning` 同样铺底）、`public/styles.css:269-271`（`.alert__icon` 22px emoji）、`public/app.js:336`（图标写死 `🔥` / `⏳`） |
| P4 | 卡片、面板、汇总卡共用同一套 `--surface` + 同一级圆角 + 同一档阴影，层级扁平 | `public/styles.css:188-195`（`.stat`）、`public/styles.css:342-355`（`.card`）、`public/styles.css:1354-1361`（`.panel`）三处 `border-radius: var(--radius)` 与 `background: var(--surface)` 完全一致，仅有阴影差别 |
| P5 | 卡片高度参差：汇总卡靠内容撑高，网格卡片 `align-items: start` 后长短不一 | `public/styles.css:182-186`（`.summary` 自动 fit 列，无 `min-height`）、`public/styles.css:335-340`（`.grid` 固定 `minmax(340px,1fr)` + `align-items: start`） |
| P6 | 响应式只有 640px 一档，平板宽度下没有任何结构变化 | `public/styles.css:1671-1685`（唯一的 `@media (max-width: 640px)`）；全文 `@media` 只有 3 处（`:24` 浅色、`:401` 减少动效、`:1671` 640px） |
| P7 | 前端只有业务渲染，没有任何外壳交互（滚动高亮、快捷键、状态点） | `public/app.js:112-133`（`els` 表列出的全是业务区块 id）、`public/app.js:243-256`（`render()` 依次渲染 6 个区块，无外壳渲染）；对 `scroll-behavior` / `IntersectionObserver` / `requestAnimationFrame` / `matchMedia` 的检索结果为 0 命中 |
| P8 | 配色是蓝紫渐变（`--accent` → `--info`），缺乏品牌识别 | `public/styles.css:11`（`--accent: #6ea8ff`）、`public/styles.css:16`（`--info: #9d7bff`）、`public/styles.css:164-169`（`.btn--primary` 用 accent→info 渐变） |

### 1.2 目标

1. 把导航从顶栏剥离成**左侧固定导航**，用滑动高亮块 + 就地展开子菜单 + 计数徽标解决「区块靠滚动」和「找不到入口」。
2. 增加**命令面板**（⌘K / Ctrl+K）作为键盘侧第二入口，覆盖平台搜索、区块跳转、立即刷新、打开平台与账号。
3. ≤1024px 隐藏左侧导航，改用**底部浮动停靠栏**，移动端仍然可以一键换区块。
4. 视觉换成**石墨黑 + 薄荷绿**：卡片近黑、1px 极细描边、圆角梯度 20/14/10、柔和分层阴影、`color-mix` 做毛玻璃与渐变描边；浅色主题改为纸感 off-white。
5. 修掉第 1.1 节的 P3～P6：告警改为「细分隔线 + 小徽标」、卡片等高、建立 surface 层级梯度、emoji 图标换成 CSS 绘制的状态点。
6. 明暗双主题、减少动效偏好、现有卡片渲染与「最快到期」排序全部保持不变。

### 1.3 Non-Goals（明确不做）

| 不做的事 | 原因 |
| --- | --- |
| 不改后端接口 | 本次是纯前端视觉与导航重构，`/api/summary`、`/api/providers`、`/api/accounts/*`、`/api/login/*`、`/api/reveal` 全部保持原样 |
| 不改数据契约 | `src/types.ts` 的 `ProviderSnapshot` / `CreditLot` / `DashboardPayload` 一行不动 |
| 不引入构建步骤与第三方依赖 | 项目约束是「原生前端 + 运行时零第三方依赖、无构建步骤」；所有交互用原生 DOM / CSS / `dialog` |
| 不做路由、不做多页面 | 不引入 History API 路由或分页，仍是单页 + 锚点式区块切换 |
| 不改凭证安全模型 | 不触碰 `/api/reveal` 的本机特权闸门、不做凭证缓存延长、不把任何凭证写进前端 |
| 不重写业务渲染 | `renderSummary` / `renderAlert` / `renderGrid` / `buildCard` / 登录轮询 / 账号管理等一律保留原实现 |
| 不改 CLI 与适配器 | `src/cli/**`、`src/providers/**`、`src/service.ts`、`src/aggregate.ts` 不在改动范围 |

---

## 2. 现状调研

**结论**：旧版前端是「一列纵向堆叠的业务区块 + 顶栏三按钮」，没有导航层，样式全部靠一套 `--surface` + 单一 `--radius` 堆出来；改造必须动 `index.html` 骨架、`styles.css` 视觉、`app.js` 外壳三处，但业务渲染函数的入口（`render()`）和它依赖的 id / class 必须原样保住。

> **行号口径**：本节引用的旧版行号取自重构前快照——`public/index.html` 228 行（与 `HEAD` 一致）、`public/styles.css` 1685 行、`public/app.js` 1911 行。后两者比 `HEAD` 多出同期无关的登录按钮改动（`styles.css` +18 行、`app.js` +66 行），结构与行号锚点一致。`HEAD` 版本可用 `git show HEAD:public/index.html`、`git show HEAD:public/styles.css` 复现。

### 2.1 `public/index.html` 旧版结构（228 行）

| 位置 | 内容 | 问题 |
| --- | --- | --- |
| `public/index.html:13-30` | `<header class="topbar">`，内含 `.topbar__brand`（⚡ logo + 标题）与 `.topbar__actions`（平台与账号 / 自动刷新 / 立即刷新） | 导航位被品牌占据，没有区块入口 |
| `public/index.html:32-56` | `main.wrap`：`.summary` → `.alert` → `.toolbar` | 三段顺序堆叠，滚动才能看到下面 |
| `public/index.html:58-74` | `.grid#grid` → 两个 `.panel`（时间线 / 诊断） | 同上，且两个面板没有 id（后续锚点无从挂载） |
| `public/index.html:77-80` | `<footer class="footer">` 在 `.wrap` 之外 | 结构上顶栏/footer 夹住 wrap，没有左右分栏的可能 |
| `public/index.html:82-148` | `#card-template`（卡片骨架） | 模板内部的 class 被 `app.js` 逐个 `querySelector` 依赖，必须原样保留 |
| `public/index.html:155-212` | `#manage-template`（登录 / 手工凭证 / 结果提示） | 同上 |
| `public/index.html:215-224` | `#platforms-dialog`（平台与账号弹窗） | 已有 `<dialog>` 用法，命令面板可复用同一范式 |
| 全文 | `data-section` 出现 **0 次**、`<nav>` 出现 **0 次** | 没有任何可供导航绑定的目标标记 |

### 2.2 `public/styles.css` 旧版证据（1685 行）

| 证据位置 | 关键事实 | 对本次改造的含义 |
| --- | --- | --- |
| `public/styles.css:1-22` | `:root` 只有一套令牌：`--bg: #070a12`、`--surface: rgba(20,26,43,.72)`、`--surface-strong`、`--border/-strong`、`--accent: #6ea8ff`、`--ok/--warn/--danger/--info`、`--radius: 18px`、`--radius-sm: 12px`、单一 `--shadow` | 没有主色 `--mint`，没有圆角梯度（只有两级且缺小号），没有表面层级（surface 只有一个值） |
| `public/styles.css:24-43` | 浅色主题 `--bg: #f4f6fb`、`--surface: rgba(255,255,255,.86)` | 冷蓝白，不是纸感；改 off-white 需重定整套浅色令牌 |
| `public/styles.css:77-89` | `.topbar { position: sticky; display: flex; justify-content: space-between }` | 顶栏是唯一的「全局条」，改造后要拆成 rail + topbar 两件 |
| `public/styles.css:120-124` | `.topbar__actions { display: flex; gap: 14px }` | 无 `flex-wrap`，窄屏靠 640px 断点整体改 `flex-direction: column` |
| `public/styles.css:142-152` | `.btn`：`border-radius: 999px`、`background: var(--surface)` | 全站按钮统一胶囊 + 同一种表面，缺少主次层级 |
| `public/styles.css:164-169` | `.btn--primary`：`linear-gradient(135deg, var(--accent), var(--info))` | 蓝→紫渐变是 P8「配色廉价」的直接来源 |
| `public/styles.css:182-186` | `.summary { grid-template-columns: repeat(auto-fit, minmax(190px,1fr)) }` | 汇总卡宽度随视口跳动，且无 `min-height` → 高度参差（P5） |
| `public/styles.css:188-195` | `.stat`：`padding: 16px 18px`、`border-radius: var(--radius)`、`background: var(--surface)`、`backdrop-filter: blur(14px)` | 与 `.card` / `.panel` 完全同款（P4 层级扁平） |
| `public/styles.css:258-267` | `.alert { display:flex; border: 1px solid var(--border-strong); background: var(--surface-strong) }` | 基础态尚可，问题出在下面两条的 modifier |
| `public/styles.css:283-286` | `.alert--danger { border-color: color-mix(danger 55%); background: color-mix(danger 10%, surface-strong) }` | 「整块粉色铺底」的根因：10% 混合铺满整条 + 55% 描边（P3） |
| `public/styles.css:288-291` | `.alert--warning` 同样整块铺色 | 同上，告警太抢视线 |
| `public/styles.css:269-271` | `.alert__icon { font-size: 22px }` | 22px emoji（🔥/⏳）作为唯一状态图形 → 廉价感 |
| `public/styles.css:335-340` | `.grid { grid-template-columns: repeat(auto-fill, minmax(340px,1fr)); align-items: start }` | 340px 固定 + `align-items: start` → 同行卡片不等高（P5） |
| `public/styles.css:342-355` | `.card`：`border-radius: var(--radius)`、`background: var(--surface)`、`box-shadow: var(--shadow)` | 与 `.stat` / `.panel` 同表面同圆角（P4） |
| `public/styles.css:1354-1361` | `.panel` 同上 | 三级容器视觉上无法区分 |
| `public/styles.css:1363-1375` | `.panel__head` 仅 `margin-bottom: 14px`，标题 16px | 面板头没有分隔线、没有右侧操作位 |
| `public/styles.css:1494-1504` | `.dialog`：`width: min(720px, calc(100vw - 32px))`、圆角 `var(--radius)` | 命令面板可作为第二个 `dialog` 复用同一 backdrop 范式 |
| `public/styles.css:1506-1509` | `.dialog::backdrop { background: rgba(3,6,14,.62) }` | 命令面板的 backdrop 需要更轻，避免两层弹窗底一致 |
| `public/styles.css:1548-1555` | `.dialog__body` 固定 `max-height: calc(84vh - 82px)` | 与 head 高度硬耦合，重写时改 `max-height: inherit` 更稳 |
| `public/styles.css:1659-1669` | `.footer` 在 wrap 外、`justify-content: space-between` | 改左右分栏后 footer 挪进 `.main` |
| `public/styles.css:1671-1685` | 唯一断点 `@media (max-width: 640px)` | 没有 1024px 档（P6），移动端停靠栏需要新增断点 |

### 2.3 `public/app.js` 旧版证据（1911 行）

| 证据位置 | 关键事实 |
| --- | --- |
| `public/app.js:112-133` | `const els = { ... }` 共 20 项，全部是业务区块 id（`summary` / `alert` / `grid` / `timeline` / `diagnostics` / `meta` / `subtitle` / `footer-updated` / `refresh-btn` / `auto-refresh` / `sort` / `search` / 两个 `<template>` / 弹窗 5 个 id）。**没有任何导航、停靠栏、命令面板、状态条的元素引用** → 外壳必须新增，且新增时不能破坏这 20 项 |
| `public/app.js:212-241` | `load(force)`：`loadAdapterMeta()` → `fetch('/api/summary')` → `state.data = await response.json()` → `render()`；失败写 `#diagnostics` |
| `public/app.js:243-256` | `render()` 固定 6 步：`renderSummary` → `renderAlert` → `renderSortOptions` → `renderGrid` → `renderTimeline` → `renderDiagnostics`，末尾写 `#meta` / `#subtitle` / `#footer-updated`。**这是外壳渲染唯一应该挂载的位置**（插在业务渲染之后） |
| `public/app.js:258-314` | `renderSummary`：按 `totals` / `expiryThresholds` / `expiryTimeline` 造 4 张 `.stat`（平台总数 / 可用额度合计 / ≤N 天到期 / 到期批次总数），`critical > 0` 时加 `stat--accent` |
| `public/app.js:316-337` | `renderAlert`：`className = alert + alert--danger|alert--warning`（第 324 行整串覆盖 className），第 336 行把图标写死成 `🔥` / `⏳` —— 改造后 className 里必须保留 `alert` 前缀，否则 CSS 新规则失配 |
| `public/app.js:339-347` | `renderSortOptions`：把「仅看有到期风险的（N）」写进 `option[value="risk"]` |
| `public/app.js:349-398` | `sortedProviders`：`risk`/`expiry` 走默认分支，按 `urgencyOf` → `nextExpiryInDays` → 名称三级排序（「最快到期优先」的实现，**本次不得改动**） |
| `public/app.js:400-427` | `renderGrid`：`sortedProviders()` → 空态（无匹配 / 首次使用引导）→ 逐平台 `buildCard` 并一次性 `append` |
| `public/app.js:1816-1838` | `renderTimeline`：按 `expiryTimeline` 造 `li`，`data-level` 三档 |
| `public/app.js:1840-1870` | `renderDiagnostics`：`missingEnvVars` + `warnings` + 状态非 `ok/demo` 的平台，前两类与第三类 `notConfigured` 的告警级别不同 |
| `public/app.js:1872-1876` | `scheduleAutoRefresh`：`setInterval(..., REFRESH_INTERVAL_MS)`，间隔见 `public/app.js:4`（5 分钟） |
| `public/app.js:1878-1899` | 事件绑定：`refreshBtn` / `platformsBtn` / `platformsClose` / 弹窗遮罩点击 / 弹窗 `close` / `autoRefresh change` / `sort change` / `search input` |
| `public/app.js:1901-1911` | `visibilitychange` 回前台补拉 + 登录轮询续期；末尾 `scheduleAutoRefresh(); load(false);` |

### 2.4 可复用的现有能力

| 能力 | 位置 | 复用方式 |
| --- | --- | --- |
| 统一数据入口 | `src/server.ts:239-247` 的 `/api/summary`（含 `warnings` / `missingEnvVars` 注入） | 外壳徽标只读这一份数据，不额外发请求 |
| 排序与筛选管线 | `public/app.js:349-398` + `state.query` / `state.sort` | 「按平台」子菜单与命令面板的「查看 X」直接复用 `state.query`，不另开一套筛选 |
| 紧急度分级 | `public/app.js:172-187`（`levelFor` / `urgencyOf`） | 子菜单状态点、卡片标签、徽标高亮共用同一口径 |
| 金额格式化 | `public/app.js:196-203`（`balanceFor` / `currencyFor`）、`public/app.js:144-147`（`fmtNumber`） | 子菜单右侧余额直接调 |
| 卡片 / 管理模板 | `public/index.html:82-148`、`public/index.html:155-212` | 骨架重写时整块平移，id 与内部 class 不动 |
| `dialog` 范式 | `public/index.html:215-224` + `public/styles.css:1494-1555` | 命令面板照同一套模态规则实现 |

---

## 3. 方案设计

### 3.1 整体思路

**结论**：把页面从「一列区块」改成「左侧导航 + 顶栏 + 内容区 + 移动端停靠栏」的四件套外壳，视觉用一套新的令牌体系重画；外壳逻辑全部**增量**挂在 `render()` 末尾与一次性 `bindShell()` 里，业务渲染函数一行不改。

数据流（本次改造只动最后一跳）：

```
上游平台接口 → 适配器 probe() → service.ts 并发与缓存 → aggregate.ts 聚合
                                                          ↓
                                              GET /api/summary（不变）
                                                          ↓
                            app.js：state.data → render()
                                                          ↓
        ┌───────────────────────────── 业务渲染（不动）─────────────────────────────┐
        │ renderSummary / renderAlert / renderGrid / renderTimeline / renderDiagnostics │
        └──────────────────────────────────────────────────────────────────────────┘
                                                          ↓
        ┌───────────────────────────── 外壳渲染（本次新增）──────────────────────────┐
        │ renderShell(data)：导航徽标 · 状态条 · 「按平台」子菜单 · 高亮块重量         │
        │ bindShell()：scrollspy · 命令面板 · 停靠栏 · ⌘K 快捷键（一次性绑定）        │
        └──────────────────────────────────────────────────────────────────────────┘
```

### 3.2 视觉令牌体系

**结论**：用一套语义令牌替代旧版「一套 surface + 两级圆角」，明暗主题只换令牌值、不换规则。

| 令牌 | 深色（石墨黑） | 浅色（纸感 off-white） | 用途 |
| --- | --- | --- | --- |
| `--bg` | `#08090c` | `#f5f4ef` | 页面画布 |
| `--bg-soft` | `#0b0d12` | `#fbfaf6` | 输入框、次级底板 |
| `--surface` | `#12141a` | `#ffffff` | 卡片 / 面板基础面 |
| `--surface-2` | `#171a21` | `#f7f6f1` | 抬升一档（面板头、内嵌块） |
| `--surface-3` | `#1d2129` | `#eeece4` | 抬升两档（浮层、选中态） |
| `--line` / `--line-strong` | `color-mix(text 8%/16%)` | `rgba(20,23,29,.1/.18)` | 1px 极细描边；浅色下改为更实的灰 |
| `--text` / `--text-2` / `--text-3` | `#f2f4f8` / `#a6aebf` / `#6c7488` | `#14171d` / `#4d5566` / `#7d8494` | 三级文字层级 |
| `--mint` | `#5fe3b0` | `#0f9d6f` | **主色**：高亮块、选中态、正常状态点、主按钮 |
| `--mint-ink` | `#04241a` | `#ffffff` | 主色底上的文字色（保证对比度） |
| `--danger` | `#ff6f61`（珊瑚红） | `#d6453a` | 告警、临界到期、异常状态点 |
| `--warn` | `#ffc24b`（琥珀） | `#a9711a` | 警告、即将到期 |
| `--info` | `#b8a2ff`（淡紫） | `#6b4fd0` | 信息态、次要强调 |
| `--r-lg` | `20px` | 同 | 大容器：卡片、面板、弹窗 |
| `--r-md` | `14px` | 同 | 中容器：内嵌块、告警条、子菜单行 |
| `--r-sm` | `10px` | 同 | 小控件：徽标、输入框、命令项 |
| `--shadow-1` / `--shadow-2` | 1px 贴地阴影 / 40px 柔和外扩 | 同比减压 | 分层：静止一层、抬升一层；不再全站共用一档阴影 |

配图规则：

| 规则 | 做法 |
| --- | --- |
| 毛玻璃 | 卡片/导航用 `color-mix(in srgb, var(--surface) N%, transparent)` + `backdrop-filter: blur() saturate()`，不再用整块不透明底 |
| 渐变描边 | 用 `color-mix` 混合语义色到 `--line` 上做 1px 描边，hover 时才提亮 |
| 告警降噪 | 整块铺底 → **左侧 2px 语义色条 + 细分隔线 + 小徽标**，正文不再染色 |
| 状态图形 | emoji → **CSS 绘制的 6px 圆点**（`border-radius: 50%` + `box-shadow` 光晕），关键态加呼吸动画 |
| 层级梯度 | 页面 `--bg` < 卡片 `--surface` < 抬升块 `--surface-2/3`，靠三级表面而不是靠阴影区分 |

### 3.3 信息架构

| 区域 | 桌面（>1024px） | ≤1024px | 承载内容 |
| --- | --- | --- | --- |
| 左侧导航 `#rail` | 宽 248px 固定吸顶，`.shell` 两列网格的第一列 | 隐藏（`display: none`） | 品牌区、5 个导航项（总览 / 按平台 / 额度卡片 / 到期时间线 / 诊断）、导航项右侧计数徽标、「命令面板」入口 |
| 顶栏 `.topbar` | 内容区顶部，吸顶 | 同（收窄内边距） | 当前区块标题 `#page-title`、副标题 `#subtitle`、状态胶囊 `#status-chip`、搜索/平台与账号/自动刷新/立即刷新 |
| 内容区 `.wrap` | `.shell` 第二列 | 单列 | 汇总卡、告警、工具栏、卡片网格、时间线面板、诊断面板 |
| 移动端停靠栏 `#dock` | 隐藏 | 底部固定浮动胶囊（4 项 + 滑动高亮块） | 总览 / 额度 / 时间线 / 搜索 |
| 命令面板 `#cmd` | `⌘K` / `Ctrl+K` 或两处「搜索」按钮唤起 | 同（停靠栏「搜索」） | 平台搜索、区块跳转、立即刷新、打开平台与账号、切换自动刷新 |

### 3.4 五种菜单模式的交互与数据来源

| # | 模式 | 交互 | 数据来源 |
| --- | --- | --- | --- |
| 1 | **滑动高亮块** | 左侧导航里一个绝对定位的 `.nav__pill` 作为高亮底：宽度/高度取激活项的 `offsetWidth/offsetHeight`，位置取相对容器的 `offsetTop/offsetLeft`；切项时靠 CSS `transition` 滑动。激活项由 scrollspy 驱动：滚动时按视口 28% 处的探针判断「正在看哪个区块」 | 纯 DOM 测量，无接口；激活目标来自 `[data-section]` 集合 |
| 2 | **就地展开子菜单** | 「按平台」分组项点击后就地展开 `#nav-platforms`，一行一个平台：状态点 + 平台名 + 余额；点某行 = 把该平台名写进筛选框并平滑滚到卡片区。展开态存 `state`，数据刷新重建子菜单时不会自动收起。**（2026-10-21 起行模型改为「一个平台一行，跨站点平台按站点拆行」，见 `docs/plans/前端看板-按平台筛选按站点拆分-开发设计方案.md`）** | `/api/summary` 的 `providers[]`（名称、`balanceFor`、`urgencyOf`、`region`） |
| 3 | **状态点与计数微标** | 导航项右侧徽标显示计数：平台数 / 卡片数 / 到期批次数 / 诊断问题数；到期存在 critical 批次时「到期时间线」徽标转暖色高亮；诊断数归零时徽标降透明度；关键状态点做呼吸动画（`prefers-reduced-motion` 下关闭） | `totals.providers`、当前筛选后的卡片数、`expiryTimeline`、`missingEnvVars + warnings + 异常平台数` |
| 4 | **命令面板** | `⌘K`/`Ctrl+K` 唤起 `#cmd`（原生 `<dialog>`，`showModal()`）；输入即时过滤；`↑↓` 循环选择、`↵` 执行、`Esc` 关闭；鼠标悬停与键盘共用同一个选中索引 | 命令清单现算：区块跳转 4 条 + 操作 3 条（刷新 / 打开平台与账号 / 切换自动刷新）+ 每个平台一条「查看 X」 |
| 5 | **底部停靠栏** | ≤1024px 时左侧导航隐藏，底部出现浮动胶囊停靠栏，内置第二块高亮块（同 `positionPill` 逻辑换横轴）；点击换区块并点亮 | 与左侧导航同源（`data-target`），数据一致 |

**未落地的第 6 种「顶部横排加下落面板」**：本项目的区块入口已经有两个（左侧导航 + 命令面板），再加一条顶部横排下拉会形成**三重重复入口**，用户每次切换区块都要在三个位置里选一个，反而降低可发现性；同时本项目顶栏已经承载状态胶囊与 4 个操作控件，横排下拉会把顶栏挤成两行。决策与备选方案见 §7 R5。

### 3.5 职责划分

| 文件 | 只负责 | 明确不碰 |
| --- | --- | --- |
| `public/index.html` | **骨架与模板**：`.shell` 布局、`#rail` 导航、`.topbar`、`.wrap` 内容区、`#dock`、`#cmd`；卡片模板与管理模板整块平移，id / class 一字不改 | 不写死平台清单、不写任何业务数据、不写内联样式 |
| `public/styles.css` | **只管视觉**：令牌、布局、组件外观、动效、响应式、减少动效降级 | 不含业务逻辑、不出现平台名、不依赖文案长度 |
| `public/app.js` | **只加外壳交互**：`els` 增量、`renderShell()`、scrollspy、高亮块测量、子菜单、命令面板、停靠栏、快捷键 | 不改 `load()` / `render()` 的既有 6 步 / `sortedProviders` / 卡片渲染 / 登录轮询 / 账号管理 / 密钥读取 |

---

## 4. 接口设计

**结论：本次无新增 / 变更 HTTP API，也无 `ProviderAdapter` 契约扩展、无新增 CLI 命令。** 全部改动落在前端：新增一组 DOM 契约（导航/徽标/命令面板/停靠栏）与一套键盘交互契约。

### 4.1 HTTP API

| 接口 | 变化 | 说明 |
| --- | --- | --- |
| `GET /api/summary` | 无变化 | 外壳与业务共用同一份数据；`force=1` 仍由「立即刷新」触发 |
| `GET /api/providers` | 无变化 | 仍用于管理表单字段声明 |
| `GET /api/config` | 无变化 | 未使用 |
| `GET /api/accounts` / `GET /api/accounts/overview` | 无变化 | 账号清单与平台总览 |
| `POST /api/accounts/add|remove` | 无变化 | 增删账号 |
| `POST /api/login/start|poll|cancel` | 无变化 | 扫码 / 链接 / 网页端登录 |
| `POST /api/reveal` | 无变化 | 仍走本机特权闸门 `privilegedGuard`（`src/server.ts:99-109`、`src/server.ts:265`） |
| `POST /api/refresh` | 无变化 | 前端仍只用 `/api/summary?force=1` |
| 新增接口 | **无** | — |

### 4.2 新增前端 DOM 契约

| 契约 | 选择器 / 属性 | 语义 | 消费方 |
| --- | --- | --- | --- |
| 导航容器 | `#nav` | 左侧导航根节点，高亮块以它为测量容器 | `positionNavPill()` |
| 高亮块 | `#nav .nav__pill` | 绝对定位的滑动底块；无激活项时加 `is-hidden` | `positionPill()` |
| 导航项 | `#nav .nav__item[data-nav-item][data-target]` | `data-target` = 目标区块的 `data-section` 值 | 点击 → `scrollToSection()`；`applyActiveTarget()` 加 `is-active` |
| 分组项 | `#nav-group-platforms`（`button`） | 「按平台」就地展开触发器，`aria-expanded` 反映展开态，`aria-controls="nav-platforms"` | `toggleNavPlatforms()` |
| 子菜单 | `#nav-platforms` | 就地展开容器，`hidden` 控制显隐；行由 JS 按 `providers[]` 生成 `.nav__sub-item`（内含 `.nav__sub-dot` / `.nav__sub-label` / `.nav__sub-amount`） | `renderNavPlatforms()` |
| 徽标 | `#nav-badge-platforms`、`#nav-badge-providers`、`#nav-badge-timeline`、`#nav-badge-diag` | 计数微标；热态加 `.nav__badge--hot`，零值加 `.is-zero` | `setBadge()` / `renderShell()` |
| 区块目标 | 任意元素 `[data-section]` | 取值：`summary` / `alert` / `toolbar` / `providers` / `timeline-panel` / `diagnostics-panel`；只有 4 个有导航项 | scrollspy 与跳转 |
| 状态胶囊 | `#status-chip` + `#status-text` | 顶部状态点与「N 正常 / M 异常」文案；异常时 `#status-chip` 加 `.status--warn` | `renderShell()` |
| 顶栏标题 | `#page-title` | 随激活区块切换文案 | `applyActiveTarget()` |
| 停靠栏 | `#dock` + `#dock .dock__pill` | ≤1024px 显示；高亮块按横轴测量；项 `.dock__item[data-target]` | `positionNavPill()` / `bindShell()` |
| 停靠栏搜索 | `#dock-cmd` | 打开命令面板 | `bindShell()` |
| 命令面板 | `#cmd`（`dialog`）+ `#cmd-input` + `#cmd-list` + `#cmd-empty` + `#cmd-hint` | 列表项 `.cmd__item`（`role="option"`、`aria-selected`、`id="cmd-item-N"`）；输入框用 `aria-activedescendant` 指向选中项 | `openCommandPalette()` / `rebuildCommandList()` / `syncCommandSelection()` |
| 两个搜索入口 | `#cmd-open`（导航底部）、`#cmd-btn`（顶栏） | 均打开命令面板 | `bindShell()` |
| 保留 id | 旧版 20 个 id（`#subtitle`、`#platforms-btn`、`#auto-refresh`、`#refresh-btn`、`#summary`、`#alert`、`#sort`、`#search`、`#meta`、`#grid`、`#timeline`、`#diagnostics`、`#footer-updated`、`#card-template`、`#manage-template`、`#platforms-dialog`、`#platforms-title`、`#platforms-subtitle`、`#platforms-close`、`#platforms-body`） | 骨架重写后仍全部存在，`els` 表原样可用 | 既有业务逻辑 |

### 4.3 键盘交互契约

| 按键 | 作用域 | 行为 | 边界条件 |
| --- | --- | --- | --- |
| `⌘K` / `Ctrl+K` | 全局（含输入框内） | 切换命令面板：已开则关，未开则开 | 只认「修饰键 + K」组合，且不叠加 `Alt`；普通字符输入不受影响 |
| `↑` / `↓` | 命令面板打开时 | 选中项上/下移动，越界循环 | 面板未打开时不拦截；无结果时索引为 -1 不报错 |
| `↵` | 命令面板打开时 | 执行当前选中命令 | **输入法组合态（`isComposing`）下不响应**，避免用回车确认候选词时误执行 |
| `Esc` | 命令面板打开时 | 关闭面板（走 `dialog` 原生 `cancel` → `close`） | 关闭时清空选中索引与 `aria-activedescendant` |
| `Tab` | 命令面板内 | 保持浏览器默认焦点顺序（不劫持） | — |
| 点击遮罩 | 命令面板 | 关闭 | 只在 `event.target === dialog` 时判定为遮罩 |
| 点击导航项 / 停靠栏项 | 全局 | `preventDefault` + 平滑滚动到目标 + 立即点亮 | 停留期间锁住 scrollspy，停稳 240ms 或 `scrollend` 后交还 |

---

## 5. 数据模型

**结论：无 `types.ts` 契约变更，`accounts.json` / `.env` 无新增配置项。** 外壳的全部数字都从 `/api/summary` 现有字段推导，不新增字段、不新增请求。

### 5.1 复用的现有字段

| 字段 | 来源 | 外壳用途 |
| --- | --- | --- |
| `providers[]` | `src/types.ts:234`（`ProviderSnapshot[]`） | 「按平台」子菜单的行、命令面板「查看 X」、卡片徽标计数口径 |
| `totals` | `src/types.ts:229`（`DashboardTotals`） | 平台总数徽标、状态胶囊的「N 正常 / M 异常」 |
| `expiryTimeline[]` | `src/types.ts:233`（`ExpiryHit[]`） | 到期批次徽标数量、徽标是否转暖色 |
| `expiryThresholds` | `src/types.ts:228`（`{ criticalDays, warningDays }`） | 状态点分级、徽标转暖色的阈值 |
| `nextExpiry` | `src/types.ts:231` | 业务侧告警条（外壳不读） |
| `warnings` / `missingEnvVars` | `src/server.ts:241-246` 在响应外层注入（未写进 `DashboardPayload`） | 诊断徽标计数 |
| `cacheTtlSeconds` / `generatedAt` | `src/types.ts:227`、`src/types.ts:226` | `#meta` 与 `#footer-updated`（业务侧写，外壳不读） |

### 5.2 徽标取数口径

| 徽标 | 数值口径 | 取值字段 / 函数 | 高亮规则 |
| --- | --- | --- | --- |
| `#nav-badge-platforms` | 平台总数 | `totals.providers`，缺失时回退 `providers.length` | 无 |
| `#nav-badge-providers` | **当前排序与筛选后**的卡片数 | `renderGrid` 计算出 `providers.length` 后回填（与网格实际张数严格一致） | 无 |
| `#nav-badge-timeline` | 到期批次数 | `expiryTimeline.length` | 存在 `hit.inDays <= criticalDays` 时加 `.nav__badge--hot`（暖色） |
| `#nav-badge-diag` | 待处理诊断条数 | `missingEnvVars.length + warnings.length + 状态 ∉ {ok, demo, notConfigured} 的平台数`（与 `renderDiagnostics` 同口径） | `> 0` 加 `.nav__badge--hot`；`= 0` 加 `.is-zero` 降透明度 |
| 子菜单行余额 | 账户余额，缺失时回退批次求和 | `balanceFor(provider)` + `fmtNumber` | 取数失败平台显示占位符并弱化，不显示 0 |
| 子菜单行状态点 | 平台紧急度 | `urgencyOf(provider, expiryThresholds)` → `critical` / `warning` / `safe` | 由 `data-level` 属性驱动颜色 |

### 5.3 配置与契约变更

| 项 | 结论 |
| --- | --- |
| `src/types.ts` 的 `ProviderSnapshot` / `QuotaWindow` / `CreditLot` / `DashboardPayload` 等 | **无变更** |
| `accounts.json` 字段 | **无新增**；不读写新字段 |
| `.env` 键名 | **无新增**；不引入任何新环境变量 |
| 已有配置迁移 | **不涉及** |
| 前端持久化（localStorage / cookie） | **不新增**；展开态、命令索引只存内存 `state` |

---

## 6. 影响面评估

**结论**：影响范围严格限定在 `public/` 三个文件；后端、CLI、适配器、聚合、缓存、既有 API 消费方零影响。

### 6.1 文件级影响

| 文件 | 改动性质 | 必须保住的东西 | 风险点 |
| --- | --- | --- | --- |
| `public/index.html` | **骨架重写**（`HEAD` 228 行 → 现行约 320 行）：新增 `.shell` 两列布局、`#rail` 导航、`.topbar` 拆分、`#dock`、`#cmd` | `#card-template`、`#manage-template` 两块模板**整块保留**，内部全部 id/class 不变；旧版 20 个 id 全部保留（已逐项核对存在） | 模板里少一个 class，卡片就少一块内容 |
| `public/styles.css` | **整体重写**（1685 行 → 约 2.5k 行）：令牌体系、全部组件外观、新增导航/停靠栏/命令面板样式、1024px 与 720px 两档断点 | `app.js` 依赖的全部 class（静态模板类 + 动态生成类）、`[hidden] { display: none !important }` 兜底、`@media (prefers-reduced-motion)` 降级 | 漏掉动态 class → 元素「有内容但没样式」 |
| `public/app.js` | **仅增量**（1911 行 → 约 2.5k 行）：`els` 新增 21 项、`state` 新增 5 项、新增外壳函数、`render()` 末尾加一行 `renderShell(data)`、末尾加 `bindShell()` | `load()` / `render()` 的既有 6 步顺序、`sortedProviders` 排序、`buildCard` 及卡片内全部渲染、登录轮询、账号管理、密钥读取 | 改动业务函数会牵动取数与凭证逻辑 |

> 行数为实施快照（2026-10-20 12:39）的观测量；前端仍在实施中，最终行数以收口时为准。

### 6.2 功能面影响

| 功能 | 影响 | 说明 |
| --- | --- | --- |
| 明暗双主题 | **不受影响** | 仍是 `@media (prefers-color-scheme: light)` 单点切换，只是令牌值换成石墨黑 / 纸感 off-white |
| ≤1024px 移动端 | **改善** | 旧版只有 640px 一档；新版 1024px 收起左侧导航换停靠栏，720px 再收内边距与时间线列 |
| `prefers-reduced-motion` | **不受影响** | 旧版 `styles.css:401-408` 只关卡片脉冲；新版统一关掉呼吸动画与全部位移/过渡 |
| 现有 8 张平台卡片渲染 | **不受影响** | 卡片数量由 `/api/summary` 的 `providers[]` 决定。当前配置下为 8 张：A1etherOpenAi 7 个账号按 `groupAccounts` 合并成 1 张（`src/providers/a1etheropenai.ts:75`）+ CodeBuddy 2 + Qoder 2 + CodeArts 1 + Trae 1 + MiniMax 1。外壳只读取数量，不参与构建 |
| 「最快到期」排序 | **不受影响** | 仍在 `sortedProviders`（`public/app.js:349-398`）里；`state.sort` 默认 `expiry`，外壳不写 `state.sort` |
| 筛选与命令面板联动 | **改善** | 子菜单行 / 「查看 X」命令写的是同一个 `state.query`，与实际筛选框完全一致 |
| 时间线 / 诊断渲染 | **不受影响** | `renderTimeline` / `renderDiagnostics` 未改动；诊断徽标与诊断区共用同一口径 |
| 登录、账号增删、密钥显示 | **不受影响** | 全部在模板 + 业务函数里，外壳不触碰 `state.login` / `state.revealed` |
| 后端 / CLI / 适配器 / 缓存 | **零影响** | 未改动任何 `src/**` 文件 |

---

## 7. 风险与备选方案

**结论**：主要风险集中在「CSS 重写漏 class」「scrollspy 抖动」「快捷键冲突」三处，均已给出可执行的缓解手段；同时记录一个明确被否决的方向（顶部横排下落面板）与两个被否决的轻量备选。

| 编号 | 风险 | 影响 | 缓解 / 降级 |
| --- | --- | --- | --- |
| R1 | **CSS 重写漏掉 `app.js` 动态生成的 class**，元素有 DOM 无样式 | 卡片、子菜单、命令项、徽标「裸奔」 | 重写前先做**class 白名单**：用脚本从 `app.js` 抽取 `classList.*` 实参、`element(tag,'cls')` 的 cls、模板字符串里的 class、`querySelector` 选择器、`getElementById` id（本次实测约 **92 个 class token / 63 个 querySelector / 39 个 getElementById**）；重写后用同一脚本反查样式表覆盖情况。CSS 头注释里也写明「删改 class 前先 grep app.js」 |
| R2 | **scrollspy 抖动**：平滑滚动途中把途经区块逐个点亮，或高频 scroll 事件里反复重排 | 高亮块来回跳、掉帧 | ① 滚动计算一律走 `requestAnimationFrame` 单帧合并（重复调用只保留一帧）；② 点击导航后**锁住 scrollspy**，每次滚动事件只续期，停稳 240ms 或 `scrollend` 后交还；③ 用户主动滚轮/触摸立即交还控制权 |
| R3 | **命令面板与输入框快捷键冲突**：`#search`、凭证输入框里的按键被劫持 | 无法输入 / 误执行命令 | ① 全局只拦「`⌘/Ctrl + K`」这一组组合键，且不叠 `Alt`；② 方向键与回车**只在面板打开时**接管；③ 输入法组合态（`isComposing`）直接放行，中文候选词确认不会触发执行 |
| R4 | **中文宽度导致高亮块尺寸计算偏差**：字体未加载完时量到 fallback 字体的宽度 | 高亮块与文字错位 | ① 尺寸一律实地测量激活项的 `offsetWidth/offsetHeight`（不估算、不写死）；② `resize`、`load`、字体加载后重新测量；③ `offsetParent` 不是容器时改用 `getBoundingClientRect()` 差值兜底；④ 徽标数字变化会改宽度，徽标更新后主动重量一次 |
| R5 | **未采用「顶部横排加下落面板」** | 少一个入口形式 | **决策：不实现。** 本项目已有左侧导航（区块级）与命令面板（键盘级）两个入口，再加顶栏横排下拉 = 三重重复入口，用户每次切区块都要在三处中选择，可发现性反而下降；且顶栏已承载状态胶囊 + 4 个操作控件，横排下拉会挤成两行。该模式适合「没有侧栏、顶栏就是唯一导航」的布局，与本项目不匹配 |
| R5-a | 备选方案（已否决）：**只换调色板、不改布局** | 成本最低 | 否决原因：P1（无导航）、P4（层级扁平）、P6（无 1024 断点）三个结构性问题一个都解决不了，用户「太丑」的感受里有一半来自结构而非配色 |
| R5-b | 备选方案（已否决）：**只微调 CSS 变量（改 `--accent` / `--radius`）** | 改动量最小、回归面最小 | 否决原因：旧令牌里没有主色梯度、没有表面层级、没有小号圆角，微调变量无法产出菜单范式，也无法落地任何一个交互模式；且 6 种模式全是结构级改动 |
| R6 | `<dialog>` 浏览器支持边界（`showModal` / `::backdrop`） | 老浏览器下命令面板打不开 | 现代 Safari / Chrome / Edge 均已支持；不可用时不阻断主流程（左侧导航与停靠栏仍可用）。是否需要降级方案（`hidden` + 遮罩层）标记为 **待确认** |
| R7 | **圆角梯度与实现快照不一致**（已关闭） | 视觉细节偏差 | 设计口径为 20/14/10；实施快照中 `--r-lg` 曾取 `22px`，收口时已统一回 `20px`，实现与设计口径一致 |
| R8 | 移动端停靠栏被 iOS 底部手势条遮挡 | 点击命中率下降 | 停靠栏固定底部并留出安全区内边距（`env(safe-area-inset-bottom)`）；具体取值在真机验证时确认，标记 **待确认** |
| R9 | 深色下发丝描边在低亮度屏上「看不见」 | 卡片边界消失 | 描边用 `color-mix(text 8%)` 跟随主题，浅色下单独覆盖为更实的 `rgba(20,23,29,.1)`；hover 提亮到 16% |

---

## 8. 实施步骤

**结论**：按 `index.html` → `styles.css` → `app.js` 分层推进，每层都可独立静态检查；不允许三层同时改到一半，否则无法判断「样式没生效」是骨架还是样式表的问题。

| 步骤 | 层 | 任务 | 产出物 |
| --- | --- | --- | --- |
| S0 | 预处理 | 从 `app.js` 抽取 class / selector / id 白名单并存档，作为后续两层的验收基准 | 白名单清单（约 92 class / 63 selector / 39 id） |
| S1 | `public/index.html` | 引入 `.shell` 两列布局容器，把原 `main.wrap` 与 `footer` 挪进 `.main` 列 | 骨架就位，业务区块位置不变 |
| S2 | `public/index.html` | 新增 `#rail`（品牌 + `#nav` + `.nav__pill` + 5 个导航项 + 4 个徽标 + 「按平台」分组项 + `#nav-platforms` + 命令入口） | 导航骨架 |
| S3 | `public/index.html` | 重写 `.topbar`：`#page-title` / `#subtitle` / `#status-chip`（含 `.status__dot`）/ `#cmd-btn` / 平台与账号 / 自动刷新 / 立即刷新 | 顶栏骨架 |
| S4 | `public/index.html` | 给 6 个内容区块补 `data-section`；给两个面板补 `#timeline-panel` / `#diagnostics-panel` | 可被导航定位的锚点 |
| S5 | `public/index.html` | 新增 `#dock`（`.dock__pill` + 4 项）与 `#cmd`（输入框 / 列表 / 空态 / 底部提示） | 移动端停靠栏 + 命令面板骨架 |
| S6 | `public/index.html` | 校验：旧版 20 个 id 全部存在；`#card-template` / `#manage-template` 内部结构零改动 | 骨架验收通过 |
| S7 | `public/styles.css` | 重写令牌层：深色石墨黑 + 薄荷绿语义色 + 圆角梯度 + 分层阴影；浅色改纸感 off-white 并补更实的描边 | 令牌体系 |
| S8 | `public/styles.css` | 布局层：`.shell` 两列网格、`.rail` 吸顶、`.main` 纵向、`.wrap` 内容宽度 | 布局就位 |
| S9 | `public/styles.css` | 导航组件：`.nav__pill` 滑动块（`transition`）、`.nav__item` / `.is-active`、`.nav__badge`（含 `.is-zero` / `.nav__badge--hot`）、`.nav__sub` 就地展开（含展开动画）、`.nav__sub-dot` 状态点 | 导航视觉 |
| S10 | `public/styles.css` | 内容组件重画：汇总卡（等高）、告警（左侧色条 + 细分隔线，取消整块铺底）、工具栏、卡片网格（等高）、面板、时间线、诊断 | 主体视觉 |
| S11 | `public/styles.css` | 浮层组件：命令面板（`dialog` + backdrop + 搜索行 + 列表项 + 选中态）、停靠栏（浮动胶囊 + 横轴高亮块） | 浮层视觉 |
| S12 | `public/styles.css` | 响应式与降级：`max-width: 1024px`（隐藏 rail、显示 dock）、`max-width: 720px`（内边距/时间线列）、`prefers-reduced-motion`（关动画与位移） | 响应式与无障碍 |
| S13 | `public/styles.css` | 白名单反查：逐个确认 S0 清单里的 class / 选择器在样式表中有对应规则，补齐漏项 | 样式覆盖验收 |
| S14 | `public/app.js` | `els` 增量（导航 / 徽标 / 状态条 / 停靠栏 / 命令面板共 24 项），全部按「可能不存在」处理 | 元素引用 |
| S15 | `public/app.js` | `state` 增量：`navPlatformsOpen` / `activeTarget` / `commands` / `cmdIndex` / `navScrollLock` | 外壳状态 |
| S16 | `public/app.js` | 导航常量与徽标：`NAV_SECTION_TITLES` / `NAV_TARGETS` / 探针比例 / 滚动锁定阈值；`renderShell()` 写 4 个徽标 + 状态胶囊；`render()` 末尾调用一次 | 徽标与状态条 |
| S17 | `public/app.js` | 「按平台」就地展开：`renderNavPlatforms()` 生成行（状态点 + 名称 + 余额）、`syncNavGroupState()` 落 `hidden`/`aria-expanded`/`is-open`、`filterByProvider()` 复用 `state.query` | 子菜单 |
| S18 | `public/app.js` | 滑动高亮块与 scrollspy：`positionPill()`（支持纵/横两轴）、`scheduleNavPill()`、`currentSectionTarget()`、`scheduleScrollSpy()`、滚动锁与释放 | 高亮块 + scrollspy |
| S19 | `public/app.js` | 命令面板：`buildCommands()`（7 条固定 + 每个平台 1 条）、`rebuildCommandList()`、`setCommandIndex()` 循环、`runCommand()` 先关后执行、`open/closeCommandPalette()`、快捷键判定 | 命令面板 |
| S20 | `public/app.js` | `bindShell()` 一次性绑定：导航/停靠栏点击、分组项、两个搜索入口、命令面板输入与键位、scroll/resize/load/scrollend/wheel/touchstart、首屏点亮 | 事件绑定 |
| S21 | 收口 | 静态检查（见 §9）+ 文档状态更新为「已完成」+ 变更记录追加 | 交付 |

---

## 9. 验证方式

**结论**：本次**只做静态检查**——`npm run typecheck` 与 `node --check`，外加 class 白名单的只读核对；按 AGENTS.md 第 6 节**不执行** `npm run dev` / `npm start` / `npm run probe`，也不启动开发服务器、不请求任何上游平台。页面效果由人工刷新浏览器确认。

### 9.1 静态检查项

| 检查 | 命令 / 方式 | 说明 | 本次结果 |
| --- | --- | --- | --- |
| TypeScript 类型检查 | `npm run typecheck`（`tsc --noEmit`） | 只证明 TS 侧（`src/**`）未被牵连；**覆盖不到前端 JS** | 通过（exit 0） |
| 前端语法检查 | `node --check public/app.js` | ESM 语法解析，能查出括号/引号/模板字符串断裂 | 通过 |
| class 白名单核对 | 用脚本比对 `app.js` 抽取的 class / selector / id 与 `styles.css` / `index.html` | 防 R1 的「有 DOM 无样式」 | 见 §7 R1，随实施逐步收敛 |
| id 保留核对 | 逐项确认旧版 20 个 id 在 `index.html` 中仍存在 | 防业务渲染拿到 `null` | 20/20 存在 |
| 只读核对 | `git status` / `git diff` | 确认改动范围只在 `public/` 与本文件 | 见收口汇报 |

**未执行**（按 AGENTS.md 第 6 节）：`npm run dev`、`npm start`、`npm run serve`、`npm run build`、`npm run probe`、`npm run capture`、启动 dev server、真实调用上游平台接口。因此**不能声称**「已验证页面显示」「已验证取数正常」。

### 9.2 人工验证清单

生效条件：`public/` 是静态托管且服务端对静态资源返回 `Cache-Control: no-cache`（`src/server.ts:199-217`，路由在 `src/server.ts:628`），因此**刷新浏览器即可生效**；因为改了 `index.html` 骨架，首次建议强刷（⌘⇧R / Ctrl+F5）。

| # | 验证项 | 操作 | 预期 |
| --- | --- | --- | --- |
| 1 | 深色主题 | 系统外观切深色后刷新 | 石墨黑底 + 近黑卡片 + 薄荷绿强调；卡片有 1px 极细描边与分层阴影 |
| 2 | 浅色主题 | 系统外观切浅色后刷新 | 纸感 off-white 底；描边更实、卡片边界清晰；薄荷绿压暗后正文对比度正常 |
| 3 | 1024px 断点 | 窗口从 >1024px 拖到 ≤1024px | 左侧导航消失，底部出现浮动停靠栏；再拖回大屏，导航恢复、停靠栏隐藏 |
| 4 | 滑动高亮块 | 依次点击左侧 4 个导航项 | 高亮块宽度贴合文字、平滑滑动到当前项；无闪烁、无残留 |
| 5 | scrollspy | 手动滚到「额度卡片」「到期时间线」「诊断」 | 左侧导航与顶栏标题同步高亮/切换，滚到页面底部不卡住在中间项 |
| 6 | 就地展开子菜单 | 点「按平台」 | 就地展开一行一个平台（状态点 + 名称 + 余额）；再点收起；刷新数据后展开态保持 |
| 7 | 子菜单筛选联动 | 点子菜单里某个平台 | 筛选框自动填入该平台名，卡片网格只剩它，并平滑滚到卡片区 |
| 8 | 徽标计数 | 观察 4 个导航徽标 | 平台数 = 平台总数；卡片数随筛选/排序变化；到期批次 = 时间线条数；诊断数 = 诊断区待处理条数 |
| 9 | 徽标高亮 | 制造一个 critical 到期批次 / 一条诊断问题 | 「到期时间线」徽标转暖色；诊断徽标转暖色；无问题时诊断徽标降透明度 |
| 10 | 命令面板唤起 | 按 `⌘K`（Mac）/ `Ctrl+K`（Win），以及点顶栏「搜索」、导航底部「命令面板」、停靠栏「搜索」 | 四种方式都能打开；输入框自动聚焦并清空 |
| 11 | 命令面板键盘 | 输入关键词后用 `↑↓` 选择、`↵` 执行、`Esc` 关闭 | 选中项循环移动且只高亮一处；执行后先关面板再动作；`Esc` 正常关闭 |
| 12 | 命令与筛选一致 | 在命令面板执行「查看 某平台」 | 与点子菜单行等价：筛选框被写入、网格被过滤、滚到卡片区 |
| 13 | 快捷键不冲突 | 在筛选框、凭证输入框里正常打字，并用输入法确认候选词 | 普通字符正常输入；`⌘K` 仍能唤起面板；回车确认候选词不会误执行命令 |
| 14 | 减少动效偏好 | 系统开启「减少动态效果」后刷新 | 呼吸/展开动画与位移全部关闭，布局不错乱 |
| 15 | 业务回归 | 检查汇总卡、告警条、卡片网格、时间线、诊断 | 数字与改动前一致；告警条不再整块铺粉底；卡片不再高低不齐；「最快到期」排序不变 |
| 16 | 账号操作回归（只读观察） | 打开「平台与账号」弹窗、展开某张卡片的「添加账号」 | 表单与登录块正常渲染（**不要粘贴任何凭证明文**） |

> 凭证相关的人工验证只走 `npm run probe` 与看板「诊断」区（由人工执行，AI 不代跑），且不要粘贴凭证明文。

---

## 变更记录

| 时间 | 变更内容 | 触发原因 | 影响章节 |
| --- | --- | --- | --- |
| 2026-10-20 | 初版：确立石墨+薄荷绿视觉语言与五种菜单模式落地范围 | 任务启动 | 全篇 |
| 2026-10-20 | 状态改为「已完成」；`--r-lg` 由 22px 统一为 20px，§7 R7 关闭；补齐实施与验收结论 | 收口 | 元信息、§3、§7 R7、§9、变更记录 |
| 2026-10-21 | 「按平台」子菜单行模型下移到 `docs/plans/前端看板-按平台筛选按站点拆分-开发设计方案.md`（一个平台一行、跨站点平台按站点拆行）；本文 §3 第 2 行加指引 | 用户反馈 Qoder 国内站/国际版筛选不区分 | §3 |
