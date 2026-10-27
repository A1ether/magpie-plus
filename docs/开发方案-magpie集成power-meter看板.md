# Magpie × Power Meter 二次开发方案

> 目标：在 magpie（本地 AI 网关 + 菜单栏应用）之上，并入 power-meter 的两大看板能力——**多供应商余额看板** 与 **到期时间线看板**，形成一个独立 fork 项目。
>
> 编写日期：2026-10-25 · 工作区：`~/DevSpace/magpie+powermeter`

---

## 1. 背景与两个项目的现状

### 1.1 magpie（上游，Go）

- **定位**：本地 AI 网关（`127.0.0.1:3425`），为 60+ 编程代理统一切换模型，多协议互转（OpenAI / Anthropic / Gemini），多账号故障转移。
- **技术栈**：Go 后端（`internal/` 70+ 个包），GUI 为嵌入式静态资源（`internal/gui/assets/`，原生 JS + CSS，`//go:embed assets` 内嵌进二进制），有 Web / TUI / 菜单栏 App / CLI 四种形态。
- **与看板相关的已有能力**：
  - `internal/provider/balance.go`（48.7K）：已实现各供应商余额抓取，`Balance(ctx, p)` 返回字符串金额。
  - `internal/provider/subscription_usage.go`：`SubscriptionQuota` 结构（Provider / Name / Plan / Windows / Balance / BalanceParts / BalanceSplit / BalanceTrend / Until），`QuotaWindow` 已含 reset 语义。
  - `internal/gui/assets/plugins/usage/`：用量页插件（`page.js` 181K），是 GUI 的标准页面插件形态。
  - GUI 通过 `pluginPageRoutes(mux)`（`internal/gui/pluginpage.go:320`）注册插件页面路由。

### 1.2 power-meter（本机，TypeScript）

- **定位**：多平台 AI 账户余额/积分看板，聚焦「最快到期」优先消耗。
- **技术栈**：Node 20+ / TypeScript（`tsx` 运行），无框架（裸 `http` 服务 + 静态 `public/` 三件套：`index.html` / `styles.css` 53K / `app.js` 113K）。
- **核心资产**：
  - **数据契约**（`src/types.ts`）：`ProviderSnapshot` / `QuotaWindow`（session|daily|weekly|billing）/ **`CreditLot`（带过期时间的积分批次，`basis: remaining|granted`）** / `DailyUsage` / 8 种 `ProviderStatus`。
  - **供应商适配器框架**（`src/providers/`）：`base.ts` 定义 `ProviderAdapter`（`fetchUsageLedger`、`ProbeContext` 带凭证回写），已有 qoder / trae / codebuddy / codearts（AK/SK + cookie 双模）/ a1etheropenai（多 key 合并卡片）/ minimax（签到+购买积分批次）/ qoder 双站点 / demo / custom 共 9 个适配器。
  - **聚合层**（`src/aggregate.ts`）：`groupSnapshots` 多账号合并卡片、`expiryTimeline` 到期时间线数据、`expiryThresholds`（critical/warning 天数）。
  - **看板前端**：左侧导航 + 总览 / 按平台 / 额度卡片 / **到期时间线** / 诊断五个区块，命令面板（⌘K），到期状态点、批次列表（`lot__expiry`）。
  - **配套能力**：凭证管理（`.env` 引用 + `accounts.json` 热加载）、Chrome CDP 抓 Cookie（`npm run capture`）、扫码/链接登录、用量历史本地采样兜底。

### 1.3 关键差异（决定集成策略）

| 维度 | magpie | power-meter |
|---|---|---|
| 语言 | Go | TypeScript/Node |
| 余额粒度 | 字符串金额 + 配额窗口，**无批次级到期** | `CreditLot[]` 批次级到期（核心资产） |
| 供应商 | 20+ 主流厂商（余额查询较浅） | 9 个深度适配（签到积分、批次到期、双站点、AK/SK） |
| 凭证 | 网关侧统一管理 | `.env` + `accounts.json` + CDP 抓取 + 扫码登录 |
| 前端 | 嵌入式原生 JS（插件式页面） | 独立静态站（设计感强的定制 UI） |

**结论**：power-meter 的两大看板价值在「**数据深度**」（批次级到期、签到积分、日级用量/增量）和「**适配器实现**」（9 个平台的取数逻辑）。magpie 的价值在「**载体**」（常驻网关、菜单栏、GUI 框架、多供应商统一管理）。**两者语言不同，不能代码级合并，只能做进程级或服务级集成。**

---

## 2. 目标形态

在 magpie 的 GUI 中新增一个 **「余额看板」（Balance Board）** 页面插件（与 Usage 页平级），内含两个区块：

1. **多供应商余额看板**：卡片式展示各平台/各账号余额、配额窗口、日级用量/增量，多账号合并卡片，按余额升序排列（最该关注的排最前）。
2. **到期时间线看板**：把所有 `CreditLot` 按到期天数排成时间线，critical（≤N 天）/ warning 分级告警，折算人民币估值。

数据由嵌入/伴随 magpie 运行的 power-meter 服务提供，magpie GUI 页面通过 API 读取渲染。

---

## 3. 集成架构（三种方案对比与选型）

### 方案 A：sidecar 进程 + iframe/API 接入（推荐）

```
┌─────────────────────────────────────────────┐
│ magpie (Go, 127.0.0.1:3425)                  │
│  ┌───────────────────────────────────────┐  │
│  │ GUI 新增页面插件 plugins/balance/      │  │
│  │  page.js ──fetch──▶ /api/powermeter/* │  │
│  └──────────────┬────────────────────────┘  │
│                 │ 反向代理（Go httputil）     │
│  internal/powermeterproxy/ ──▶ 127.0.0.1:PORT│
│  internal/powermeterproc/（拉起/守护 sidecar）│
└─────────────────────────────────────────────┘
                  │ spawn / 守护
┌─────────────────▼───────────────────────────┐
│ power-meter (Node, tsx 编译产物 dist/)        │
│  /api/summary  /api/accounts/overview ...    │
│  accounts.json + .env（独立配置目录）          │
└─────────────────────────────────────────────┘
```

- **做法**：magpie 启动时拉起 power-meter 的编译产物（`node dist/server.js`，或打包成 `pkg`/`bun build --compile` 单文件），监听随机空闲端口（仅回环）；magpie 新增 `internal/powermeterproxy/` 包，把 `/api/powermeter/*` 反向代理过去；GUI 加一个插件页面调这些 API 渲染。
- **优点**：
  - power-meter 的 9 个适配器、聚合层、到期时间线逻辑**零改动复用**（这是最大资产）；
  - 两边独立演进，magpie 上游更新易合入（fork 只加新包、不改旧代码）；
  - power-meter 仍可独立运行（调试、CLI 抓凭证不受影响）。
- **缺点**：依赖 Node 运行时（或需预编译二进制）；两个进程。
- **选型理由**：power-meter 的取数逻辑（CDP 抓 cookie、AK/SK、签到积分、双站点回退）重写为 Go 的成本极高（估计 8K+ 行 TS），且会持续演进，**复用优于重写**。

### 方案 B：适配器移植到 Go（不推荐做全量）

把 power-meter 的 `CreditLot`、聚合逻辑、9 个适配器全部用 Go 重写进 `internal/provider/`。
- **优点**：单进程、单语言、无 Node 依赖；与 magpie 的 `SubscriptionQuota` 统一。
- **缺点**：重写工作量巨大且失去 power-meter 独立演进能力；CDP 抓取、扫码登录等配套也要移植。**只适合后续只对 1-2 个核心平台做「原生增强」时局部采用。**

### 方案 C：数据契约对齐 + magpie 原生看板（中期演进方向）

不动 power-meter 运行时，而是把它的**数据契约**（`CreditLot` / `QuotaWindow.boundary` / `DailyUsage` / `expiryTimeline`）作为 magpie `SubscriptionQuota` 的超集扩展，让 magpie 自己的 balance 抓取逐步输出批次级到期信息，GUI 原生渲染。
- 这是**长期目标**：A 方案先落地出价值，再按平台逐个把适配器能力沉淀进 magpie 原生（B 的局部化），C 是收敛态。

**决策：先做方案 A（sidecar），以方案 C 为演进方向，方案 B 仅按需局部进行。**

---

## 4. 实施计划（方案 A，分 5 个阶段）

### 阶段 0：工作区与构建基建（0.5 天）

- 在 `~/DevSpace/magpie+powermeter/` 下建独立 git 仓库（或两个子模块）：
  - magpie 仓库已 clone 为 fork 基底（当时在 `magpie-upstream/`，2026-10-27 上移到工作区根；`git remote add upstream` 指向官方库以便追新）。
  - `power-meter/` 用 git submodule 或 vendor 方式纳入。
- power-meter 出可部署产物：`npm run build` → `dist/`（验证 `node dist/server.js` 可跑、端口可配）。
- 定 fork 分支策略：`main` 跟上游，`feature/balance-board` 承载二开。

> 2026-10-27 更新：`magpie-upstream/` 已整体上移，`~/DevSpace/magpie+powermeter/` 本身就是 git 仓库根，
> 不再有二级目录；`docs/` 与 `FORK.md` 随之上移进入仓库工作区。

### 阶段 1：power-meter 服务化改造（1 天）

让 power-meter 从「独立站」变成「纯 API 服务」：
- `src/server.ts`：增加 `--port 0`（随机空闲端口）与 `--host 127.0.0.1` 参数；增加 `--no-static` 模式（看板前端由 magpie 渲染后，power-meter 自己的 `public/` 可不serve，或保留作独立访问）。
- 暴露给 magpie 消费的最小 API 面：
  - `GET /api/summary`（已有，含 `providers` + `totals` + `expiryTimeline` + `expiryThresholds`）——余额看板与时间线的主数据源；
  - `GET /api/accounts/overview`（已有）——账号级明细；
  - `POST /api/refresh`（已有）——手动刷新。
- 安全：默认只绑回环；可选共享密钥 header（`X-PowerMeter-Token`），由 magpie 拉起时注入。
- 打包：评估 `bun build --compile` 或 `pkg` 出单文件二进制，去掉对 Node 安装的依赖（magpie 本身已会管理 Bun 运行时，可复用其 `internal/pluginrt` 的 Bun 下载能力跑 power-meter——**这是首选，零额外运行时**）。

### 阶段 2：magpie 侧 sidecar 与代理（1–1.5 天）

新增两个 Go 包（fork 只增不改，保证可追上游）：
- `internal/powermeterproc/`：
  - 在 magpie 启动（GUI 模式）时拉起 power-meter（优先用 magpie 自带的 Bun 跑 `dist/server.js`，其次找系统 `node`）；
  - 监听退出、崩溃重启（退避）、随 magpie 退出而终止（`cmd.SysProcAttr` / 进程组）；
  - 把随机端口与 token 通过环境变量传给 GUI 层。
- `internal/powermeterproxy/`：
  - 在 GUI 的 mux 上挂 `/api/powermeter/`，用 `httputil.ReverseProxy` 转发到 sidecar，注入 token；
  - 健康检查端点 `/api/powermeter/status`（sidecar 是否在跑、版本、上次刷新时间）。
- 设置项：在 magpie 设置里加「余额看板」开关、power-meter 配置目录路径（默认 `~/.config/magpie/powermeter/`，把 `accounts.json` 与 `.env` 放这里，通过 power-meter 的 `--config-dir` 或 env 指定）。

### 阶段 3：GUI 页面插件——余额看板（1.5–2 天）

- 仿照 `internal/gui/assets/plugins/usage/` 的形态新建 `internal/gui/assets/plugins/balance/`（`page.js` + `client.js` + i18n）：
  - 路由 `#/balance`，左侧导航加「余额看板」项（参考 `pluginpage.go` 的注册方式与 `plugins.js` 的挂载）。
  - `page.js` 调 `/api/powermeter/summary` 渲染：
    - **余额卡片区**：按 power-meter 的 `groupSnapshots` 结果一平台一卡，多账号卡内条目升序；状态点（8 种 `ProviderStatus`）、配额窗口进度条、日级用量/增量。
    - **视觉**：尽量贴 magpie 现有设计令牌（看 `app.css` 的变量），不照搬 power-meter 的整套皮肤，保持 GUI 一致性；交互参考 power-meter 卡片信息架构即可。
- **到期时间线区块**（同页内第二个 section，或独立子路由 `#/balance/timeline`）：
  - 渲染 `expiryTimeline`：按到期天数排序的批次列表，critical/warning 阈值色阶，每条显示 `label / amount currency / expiresAt / 折算 CNY`；
  - 顶部放「≤N 天到期」汇总 chip（复用 power-meter `app.js:340-360` 的分级逻辑）。
- 手动刷新按钮 → `POST /api/powermeter/refresh`，轮询刷新状态。

### 阶段 4：账号管理联动与打磨（1 天）

- 看板卡片上的「＋ 添加账号」/「✕ 移除」接到 power-meter 的 `/api/accounts/add` / `/api/accounts/remove`（经代理），保持与 power-meter 独立站一致的交互。
- 凭证相关操作（reveal、扫码登录 `/api/login/start|poll|cancel`）在 magpie GUI 内做最小封装弹窗。
- 菜单栏（tray）入口：在 magpie 托盘菜单加「余额看板」项，直达 `#/balance`；可选地在托盘 tooltip 显示「最近到期」一条。
- 诊断区：sidecar 状态、取数失败的平台列表、配置错误提示。

### 阶段 5：测试、文档与发布（0.5–1 天）

- 测试：
  - Go 侧：`powermeterproc`（拉起/重启/终止）、`powermeterproxy`（转发、token、sidecar 不在时的降级）单测；
  - 端到端：脚本拉起 magpie → 断言 `/api/powermeter/status` 就绪 → 页面数据渲染（可用 magpie 现有 GUI 测试形态，参考 `internal/gui/tests/`）。
- 文档：fork 的 README 增补「Balance Board (powered by power-meter)」章节；配置示例；与上游的差异清单（`FORK.md`）。
- 发布：Makefile 加 `balance-board` 相关 target；确认 sidecar 产物（Bun 单文件或 dist 目录）随 magpie 分发的方式。

**总计预估：5.5–7 天。**

---

## 5. 风险与开放问题

| 风险 | 说明 | 缓解 |
|---|---|---|
| power-meter 依赖 Chrome CDP | 抓 Cookie 需要本机 Chrome 调试端口，sidecar 化后仍可用（同机），但要注意权限弹窗 | 文档说明；抓凭证仍走 power-meter CLI，看板只读展示 |
| magpie 不接受外部 PR | 上游明确只维护者提交 | fork 定位为独立分发；严格「只增不改」降低合并上游成本 |
| 单文件打包体积 | Bun 编译产物 + magpie 二进制 | sidecar 独立分发 + 首次运行下载（复用 magpie 的 Bun 下载机制） |
| 数据口径冲突 | magpie 自己的 balance 与 power-meter 的余额可能重复展示 | 看板页以 power-meter 为准；magpie 原生 quota 页保留，二者在导航上区分（「额度（网关）」vs「余额看板（账户）」） |
| `.env` 凭证安全 | power-meter 明文 env 引用 | 配置目录权限 0700；reveal 接口仅回环 + token；文档强调不提交 |

**开放问题（建议落地前确认）**：
1. sidecar 运行时首选 **magpie 自带 Bun**（零额外依赖）还是系统 Node（更稳）？——建议 Bun，与 magpie 插件运行时一致。
2. 看板是否需要 magpie 的局域网共享能力（`access/` 命名密钥）暴露给局域网？——默认否，仅本机。
3. 时间线的人民币折算汇率与开关，沿用 power-meter 配置还是并入 magpie 设置？——先沿用 power-meter 配置目录，阶段 4 再评估并入。

---

## 6. 验收标准

- `magpie` GUI 启动后，导航出现「余额看板」，页面正确渲染所有已配置平台的余额卡片与多账号合并视图；
- 「到期时间线」按天排序展示所有 `CreditLot`，critical/warning 分级与折算金额正确；
- 手动刷新、添加/移除账号在 GUI 内可用且与 power-meter 独立站行为一致；
- sidecar 随 magpie 启停，崩溃可恢复；sidecar 缺失时看板页给出明确降级提示而非白屏；
- fork 能干净地 `git merge upstream/main`（二开代码全部在新增目录/文件中）。
