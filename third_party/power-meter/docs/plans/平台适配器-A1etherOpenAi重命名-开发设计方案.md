# 平台适配器 - A1etherOpenAi 重命名 - 开发设计方案

> 文档类型：开发设计方案
> 关联模块：平台适配器（供应商 A1etherOpenAi，旧名 D1V）
> 创建时间：2026-10-11
> 最后更新：2026-10-11
> 当前状态：已完成
> 关联改动文件：`src/providers/d1v.ts` → `src/providers/a1etheropenai.ts`、`src/d1v-store.ts` → `src/a1etheropenai-store.ts`、`src/cli/d1v-add.ts` → `src/cli/a1etheropenai-add.ts`、`src/providers/index.ts`、`src/server.ts`、`src/types.ts`、`src/aggregate.ts`、`src/providers/base.ts`、`src/cli/probe.ts`、`public/app.js`、`package.json`、`accounts.example.json`、`.env.example`、`README.md`、`docs/凭证配置指南.md`、`docs/modules/项目整体-代码Wiki.md`、`docs/plans/供应商卡片-*.md`、`docs/fixes/CodeArts-华为云登录态频繁失效-修复方案.md`、`AGENTS.md`

> 说明：**本文件是全仓库唯一保留旧名 `D1V` 的地方**（作为重命名的变更记录与迁移映射表），其余代码、配置样例、文档一律不再出现旧名（上游真实域名 `*.d1v.ai` 除外，见 3.3）。

---

## 1. 背景与目标

把看板中「D1V」这一供应商的全套命名统一改为 **A1etherOpenAi**：展示名、provider id、环境变量名、CLI 命令、HTTP 路由、源文件名、日志/文档文案一次改齐，不留混用状态。

| 目标 | 说明 |
| --- | --- |
| 展示名统一 | 卡片标题、托管密钥标签、删除确认文案、CLI 输出中的 `D1V` → `A1etherOpenAi` |
| 标识统一 | provider id `d1v` → `a1etheropenai`；`provider: "d1v"` 写法在示例配置与文档中同步更换 |
| 变量名统一 | `.env` 键名前缀 `D1V_` → `A1etherOpenAi_`（用户确认：与品牌名逐字一致，保留混合大小写） |
| 入口统一 | `npm run a1etheropenai:add`、`/api/a1etheropenai/accounts*`、`src/providers/a1etheropenai.ts` 等 |
| 不遗漏 | 结束后全仓库检索旧名，仅剩上游域名与本文档 |

**Non-Goals（本次明确不做）**：

1. **不改上游域名**：`api.d1v.ai` / `pai.d1v.ai` / `www.d1v.ai` 是平台真实服务地址，替换后取数会全部失败；用户已确认保留（变量名、常量名照改，值不变）。
2. **不改取数逻辑与数据契约**：请求路径、字段映射、`QUOTA` 单位、`groupAccounts` / `neverExpires` / `manageAccounts` 语义全部不变。
3. **不做旧名兼容层**：不保留 `d1v` provider 别名回退、不保留 `D1V_*` 环境变量回退（用户确认要彻底改净；本机 `.env` / `accounts.json` 由本次迁移脚本一并处理）。
4. **不改 git 历史、不改 `.run/` 内的历史备份内容**（备份是凭证快照，不属于命名空间）。

---

## 2. 现状调研

### 2.1 引用面清单（改前的真实分布）

确认方式：全仓库内容检索（含被 gitignore 但属于本机配置的 `.env.example`）+ 逐个文件核对。

| 层 | 文件 | 关键行 | 内容 |
| --- | --- | --- | --- |
| 适配器 | `src/providers/d1v.ts` | 64-84 | `D1V_API_BASE`、`D1V_TIMEOUT_FALLBACK`、`d1vAdapter`（id/displayName/credentialHint 含 `npm run d1v:add`） |
| 适配器 | `src/providers/d1v.ts` | 89-92、113-118、181-228 | 密钥标签 `D1V_PAI_API_KEY` / `D1V_API_KEY`、`parseD1vQuota`、`D1vQuotaSummary`、`parseD1vProfile` |
| 注册表 | `src/providers/index.ts` | 5、23、67-71 | 导入、注册、别名 `d1v.ai` / `www.d1v.ai` / `api.d1v.ai` / `pai.d1v.ai` / `d1v-ai` |
| 存储层 | `src/d1v-store.ts` | 50、60、100-101、218、354-355 | `.env` 槽位正则、写入键名、`${D1V_A_API_KEY}` 反解、备份目录 `.run/d1v-*` |
| 存储层 | `src/d1v-store.ts` | 156-220、261-365、430-495 | `D1vAccountView`、`listD1vAccounts`、`addD1vAccount`、`removeD1vAccount`、默认名 `D1V · <邮箱前缀>` |
| CLI | `src/cli/d1v-add.ts` | 全文 | 用法文案、`npm run d1v:add`、`D1V · sb4hnm` 示例 |
| HTTP | `src/server.ts` | 6、70、263-341 | 导入、`/api/d1v/accounts`（GET/POST）、`/api/d1v/accounts/remove` |
| 前端 | `public/app.js` | 77、390、682-711、785 | 接口路径、删除确认与诊断文案中的 `D1V` |
| 契约注释 | `src/types.ts`、`src/providers/base.ts`、`src/aggregate.ts`、`src/cli/probe.ts` | 各 1-3 处 | 注释里以 `D1V` 举例（`QUOTA` 单位、`groupAccounts`、永不过期等） |
| 脚本/样例 | `package.json:14`、`accounts.example.json`、`.env.example:54-69` | — | `d1v:add` 脚本、示例账号条目、空槽位键名 |
| 文档 | `README.md`、`docs/凭证配置指南.md`、`docs/modules/项目整体-代码Wiki.md`、`docs/plans/供应商卡片-*.md`、`docs/fixes/CodeArts-华为云登录态频繁失效-修复方案.md`、`AGENTS.md` | — | 平台表、配置步骤、目录树、示例、文档命名规范举例 |

### 2.2 关键可行性证据

| 结论 | 证据 |
| --- | --- |
| `${VAR}` 展开支持混合大小写键名，`A1etherOpenAi_A_API_KEY` 可直接用 | `src/config.ts:96` `/\$\{([A-Za-z0-9_]+)\}/g` |
| `.env` 解析不限制键名大小写（取第一个 `=` 前的内容为键） | `src/config.ts:52-70` |
| 槽位正则只含 `[A-Za-z0-9]`，混合大小写前缀安全 | `src/d1v-store.ts:50,60,218` |
| provider 解析前会 `toLowerCase()`，id 用小写 `a1etheropenai` 可命中 | `src/providers/index.ts:41-42` |
| 前端不硬编码 provider id，账号管理按钮由 `manageAccounts` 开关驱动 | `src/providers/base.ts:52-57`、`public/app.js:700-716` |

---

## 3. 方案设计

### 3.1 总体思路

一次性、全量、可机械校验的替换，规则只有三条（顺序敏感，等价于"逐字直译"）：

| 序 | 匹配 | 替换为 | 覆盖对象 |
| --- | --- | --- | --- |
| 1 | `D1V` | `A1etherOpenAi` | 品牌名、`D1V_API_KEY` 一类环境变量名与密钥标签、`D1V · <邮箱>` 显示名 |
| 2 | `D1v` | `A1etherOpenAi` | `parseD1vQuota`、`D1vQuotaSummary` 等混合大小写标识符 |
| 3 | `d1v`（其后不是 `.ai`） | `a1etheropenai` | provider id、文件名 `d1v.ts` / `d1v-store.ts` / `d1v-add.ts`、路由 `/api/d1v/*`、`d1v:add` 脚本、`.run/d1v-*` |

数据流不变：**上游接口 → `a1etheropenaiAdapter.probe()` → `service.ts` 并发与缓存 → `aggregate.ts` 合并卡片 → `/api/summary` → `public/app.js` 渲染**。本次只改名字，不改任何一跳的行为。

### 3.2 命名映射表（实施基准）

| 类别 | 旧 | 新 |
| --- | --- | --- |
| 展示名 | `D1V` | `A1etherOpenAi` |
| provider id | `d1v` | `a1etheropenai` |
| 适配器导出 | `d1vAdapter` | `a1etheropenaiAdapter` |
| 适配器文件 | `src/providers/d1v.ts` | `src/providers/a1etheropenai.ts` |
| 存储层文件/函数 | `src/d1v-store.ts`、`listD1vAccounts`、`addD1vAccount`、`removeD1vAccount`、`D1vAccountView` | `src/a1etheropenai-store.ts`、`listA1etherOpenAiAccounts`、`addA1etherOpenAiAccount`、`removeA1etherOpenAiAccount`、`A1etherOpenAiAccountView` |
| CLI | `src/cli/d1v-add.ts`、`npm run d1v:add` | `src/cli/a1etheropenai-add.ts`、`npm run a1etheropenai:add` |
| 解析函数 | `parseD1vQuota` / `D1vQuotaSummary` / `parseD1vProfile` | `parseA1etherOpenAiQuota` / `A1etherOpenAiQuotaSummary` / `parseA1etherOpenAiProfile` |
| 常量 | `D1V_API_BASE` / `D1V_TIMEOUT_FALLBACK` | `A1etherOpenAi_API_BASE` / `A1etherOpenAi_TIMEOUT_FALLBACK` |
| 环境变量 | `D1V_A_API_KEY` / `D1V_A_PAI_API_KEY` | `A1etherOpenAi_A_API_KEY` / `A1etherOpenAi_A_PAI_API_KEY` |
| 密钥展示标签 | `D1V_API_KEY` / `D1V_PAI_API_KEY` | `A1etherOpenAi_API_KEY` / `A1etherOpenAi_PAI_API_KEY` |
| HTTP 路由 | `/api/d1v/accounts`（GET/POST）、`/api/d1v/accounts/remove` | `/api/a1etheropenai/accounts`（GET/POST）、`/api/a1etheropenai/accounts/remove` |
| 备份目录 | `.run/d1v-<op>-<时间戳>/` | `.run/a1etheropenai-<op>-<时间戳>/` |
| 默认账号名 | `D1V · sb4hnm` | `A1etherOpenAi · sb4hnm` |
| 别名 slug | `d1v-ai` | `a1ether-openai` |
| 域名别名键 | `d1v.ai` / `www.d1v.ai` / `api.d1v.ai` / `pai.d1v.ai` | **保留键名**（用户会粘贴域名），值改为 `a1etheropenai` |

### 3.3 保留项（刻意不改）

| 保留 | 原因 |
| --- | --- |
| `https://api.d1v.ai`、`https://pai.d1v.ai/v1`、`https://www.d1v.ai/settings?tab=api-key` | 上游真实地址，改动即取数失败；用户已确认保留 |
| `src/providers/index.ts` 中四个域名别名键 | 它们是域名本身，用于把"粘贴域名"解析到新 provider id |
| `options.baseUrl` 覆盖能力 | 若将来有自建网关，仍可用它覆盖上游地址，无需改代码 |
| 本文档中的旧名 | 重命名记录与迁移映射表，刻意保留 |

---

## 4. 接口设计

接口语义、入参、出参、错误码全部不变，仅路径更名。

| 方法 | 路径 | 入参 | 出参 | 变化 |
| --- | --- | --- | --- | --- |
| GET | `/api/a1etheropenai/accounts` | — | `{ accounts, missingEnvVars }` | 仅路径 |
| POST | `/api/a1etheropenai/accounts` | `{ apiKey, paiKey, name?, force? }` 或 `{ text }` | `{ account, modelCount, ... }` | 仅路径 |
| POST | `/api/a1etheropenai/accounts/remove` | `{ accountId }` | `{ ok, name, removedEnvLines, backupDir }` | 仅路径 |

三者仍受 `privilegedGuard`（回环来源 + `X-Power-Meter-Intent`）保护（`src/server.ts:69-100`），行为不变。

CLI：`npm run a1etheropenai:add -- <账号密钥> <项目密钥> [--name] [--text] [--list] [--remove "<账号名>"] [--dry-run] [--force] [--pai-base]`，参数集合与输出结构不变。

---

## 5. 数据模型

| 对象 | 变化 |
| --- | --- |
| `ProviderSnapshot` / `QuotaWindow` / `CreditLot` / `ProviderEntry` / `SecretRef`（`src/types.ts`） | **字段与契约零变化**，仅注释文案与 `SecretRef.label` 的取值文案改名 |
| `.env` | 键名前缀 `D1V_` → `A1etherOpenAi_`；槽位字母规则（A/B/C…）与"先验证再写入""写入前备份"策略不变 |
| `accounts.json` | `provider: "a1etheropenai"`；`credentials.apiKey` / `paiApiKey` 内的 `${A1etherOpenAi_<X>_API_KEY}`；`displayName` 中的品牌词改名；`options.revealApiKey` 等不变 |
| `accounts.example.json` / `.env.example` | 同步改名（两者都只放占位符，不含任何凭证明文） |
| 汇总/排序 | `neverExpires: true` + `groupAccounts: true` 语义不变：仍不进"最快到期"，仍合并为一张卡片，`QUOTA` 仍不与 USD 相加 |

**本机既有配置迁移**：`.env` 与 `accounts.json` 属本机特权数据（已 gitignore）。本次由用户显式授权，用一次性脚本完成：**只改键名 / provider 字段 / `${...}` 引用与 displayName 品牌词，明文值一律不动**，写入前备份到 `.run/migrate-brand-<时间戳>/`，脚本只打印键名与计数。

---

## 6. 影响面评估

| 受影响面 | 影响 | 处理 |
| --- | --- | --- |
| 适配器注册与解析 | id 更名；旧 `provider: "d1v"` 配置将解析失败（预期行为） | 本机 `accounts.json` 由迁移脚本改名 |
| `.env` 凭证 | 键名更名；旧键名将不再被读取 | 迁移脚本改名 + 备份；`.env.example` 同步 |
| 本机特权接口 | 前端与后端路径同时改，一处不留旧路径 | `src/server.ts` 与 `public/app.js` 同轮修改 |
| `npm run d1v:add` 使用者习惯 | 命令更名 | README / 凭证指南 / AGENTS.md 同步；`package.json` 只留新名 |
| 缓存 / 聚合 / 前端渲染 | 无逻辑影响（缓存键基于 provider id，改后自然重建） | 无需处理 |
| 文档 | 19 处文件引用 | 一次性替换；`docs/fixes/*` 中作为"凭证是否自动续期"表格的一行同步改名 |
| `dist/`（`npm run build` 产物） | 仍含旧名，若以 `npm start`/`npm run serve` 运行需重建 | 在收口"生效前提"中提示 |
| `.run/` 历史备份 | 保持原样（凭证快照，不属于命名空间） | 不处理 |

---

## 7. 风险与备选方案

| 风险 | 等级 | 缓解 |
| --- | --- | --- |
| 本机配置迁移失败导致取数全空 | 中 | 迁移前整份备份到 `.run/`；脚本幂等（无可改内容时明确"无需迁移"退出） |
| 旧键名残留导致"变量缺失" | 低 | 迁移脚本同时输出改名后的键名清单；看板"诊断"区与 `npm run probe` 会明确指出缺失变量名 |
| 上游域名被误改 | 低（已防） | 替换规则显式排除 `d1v.ai`；收口做一次全仓库残留检索核对 |
| 文档中混用新旧名 | 低 | 同一轮内完成文档替换；唯一保留旧名处为本文档，且已声明 |

**被否决的备选方案**：

| 方案 | 否决原因 |
| --- | --- |
| 只改 `displayName` 文案 | 不满足"标识、配置项、变量名、常量一并替换"的要求，会长期新旧混用 |
| 保留 `d1v` provider 别名与 `D1V_*` 环境变量回退 | 与"不遗漏任何一处旧引用、命名一致"直接冲突；用户已确认彻底改净 |
| 一并替换上游域名 | 上游无 `A1etherOpenAi` 域名，替换后取数 100% 失败，用户已确认保留真实域名 |
| 新建第二套适配器、新旧并存 | 两套代码长期分叉，与本任务目标相反 |

---

## 8. 实施步骤

| 步骤 | 内容 | 产出物 |
| --- | --- | --- |
| 1 | 产出本方案文档 | 本文档 |
| 2 | 文本替换（三条规则，覆盖 `src/**`、`public/app.js`、`package.json`、两个 example 文件与全部文档） | 各文件改名后的内容 |
| 3 | 文件重命名：`src/providers/d1v.ts`、`src/d1v-store.ts`、`src/cli/d1v-add.ts` | 三个新文件路径（`git mv` 保留历史） |
| 4 | 人工复核别名表与路由：`d1v-ai` → `a1ether-openai`；域名别名键保留 | `src/providers/index.ts` 定稿 |
| 5 | 本机配置迁移脚本（`.env` + `accounts.json`，先备份、只改键名）并运行（脚本用完即删，避免仓库残留旧名） | `.run/migrate-brand-<时间戳>/` 备份 + 改名后的配置 |
| 6 | 静态检查：`npm run typecheck` + 全仓库残留检索 | 检查结论（写入本文档第 9 节与变更记录） |

---

## 9. 验证方式

| 项 | 命令 / 方式 | 预期 |
| --- | --- | --- |
| 类型检查 | `npm run typecheck` | 无错误 |
| 残留检索 | `grep -rIl -e D1V -e d1v <仓库，排除凭据/构建产物>` | 仅剩上游域名（`*.d1v.ai`）与本文档 |
| 路由一致性 | 检索 `/api/d1v` | 零命中（前后端都在同一轮改完） |
| 变量名一致性 | 检索 `D1V_`（含 `.env.example`） | 零命中 |
| 人工取数验证（AI 不代跑） | `npm run probe -- a1etheropenai` | 迁移后能正常取到 quota（需人工执行） |

---

## 变更记录

| 时间 | 变更内容 | 触发原因 | 影响章节 |
| --- | --- | --- | --- |
| 2026-10-11 | 初版：确定三条替换规则、命名映射表、保留项（上游域名）、本机配置迁移方式 | 用户要求全仓库把 D1V 命名统一改为 A1etherOpenAi，并就环境变量命名 / 域名 / 配置迁移三项做出决策 | 全篇 |
| 2026-10-11 | 实施完成：代码、文件、脚本、路由、样例与全部文档完成改名；本机 `.env` / `accounts.json` 已迁移（备份在 `.run/`）；静态检查通过 | 按第 8 节步骤执行 | 8、9、状态 |
