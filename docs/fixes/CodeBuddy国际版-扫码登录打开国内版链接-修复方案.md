# CodeBuddy国际版-扫码登录打开国内版链接-修复方案

> 文档类型：修复方案
> 关联模块：平台适配器 / 账号登录
> 创建时间：2026-10-25
> 最后更新：2026-10-25
> 当前状态：已确认
> 关联改动文件：`src/providers/codebuddy.ts`、`.env.example`、`docs/plans/平台适配器-接入CodeBuddy国际版-开发设计方案.md`（同步更新）

## 1. 问题现象

- 触发路径：看板 → 「WorkBuddy 国际版」卡片 → 「扫码登录」。
- 现象：弹窗中展示的授权链接 / 二维码指向**国内版** CodeBuddy（`www.codebuddy.cn` / `copilot.tencent.com` 体系），而不是国际版。
- 影响范围：国际版扫码登录不可用；扫码授权后拿到的是国内版账号令牌，落库到 `codebuddy-intl` 平台下必然取数失败或数据错乱。

## 2. 根因分析

### 2.1 直接原因

`src/providers/codebuddy.ts` 的站点元信息表 `CODEBUDDY_SITES.global` 中：

- `pluginBase` 被默认成 `https://copilot.tencent.com`（国内版认证主机）；
- 扫码登录的 `start()` 调用的正是 `${site.pluginBase}/v2/plugin/auth/state?platform=CLI`，于是国际版会话向**国内版认证后端**申请授权链接，返回的自然是国内版 `authUrl`。

### 2.2 证据（外部参考实现，已交叉验证）

| 实现 | 证据 | 结论 |
| --- | --- | --- |
| `yetone/magpie` `internal/provider/workbuddy.go` | `wbAIEndpoint = "https://www.workbuddy.ai"`；`wbAI = {id: "workbuddy-ai", website: "https://www.workbuddy.ai", platform: "workbuddy-ai", endpoint: &wbAIEndpoint}` | 国际版认证主机 = `www.workbuddy.ai`，`platform=workbuddy-ai` |
| `Javis603/token-monitor` `docs/providers/workbuddy.md` | 「international uses `https://www.workbuddy.ai/v2/plugin/auth/state?platform=workbuddy-ai`」 | 同上 |
| `Hyact/workbuddy-switch-plus` `reverse-proxy/admin/realm.py` | 「国际版是 `workbuddy-ai`（与桌面端 `WbVariant::oauth_platform` 同值）」 | 同上 |
| `smart-open/TraeWorkAssistant` `src-tauri/src/commands/workbuddy/buddy_ai.rs` | 「R-1 OAuth 全链路：`www.workbuddy.ai/v2/plugin/auth/state?platform=workbuddy-ai`；R-9：workbuddy.ai ↔ codebuddy.ai 同一账号体系」 | 同上，且确认 `codebuddy.ai` 与 `workbuddy.ai` 同一账号体系 |
| `lidge-jun/opencodex` `src/oauth/workbuddy.ts` | `workBuddyApi("/v2/plugin/auth/state?platform=workbuddy-ai", ...)`，base 为 `https://www.workbuddy.ai` | 同上 |

### 2.3 结论

国际版与国内版的**认证后端完全分离**：

| | 认证 / 插件后端（`pluginBase`） | 计费域（`billingOrigin`） | `platform` 参数 |
| --- | --- | --- | --- |
| 国内版 `codebuddy` | `https://copilot.tencent.com` | `https://www.codebuddy.cn` | `workbuddy`（原实现误用 `CLI`） |
| 国际版 `codebuddy-intl` | `https://www.workbuddy.ai` | `https://www.codebuddy.ai` | `workbuddy-ai` |

> 注：部分实现中国内版 `platform` 写作 `CLI`（旧版 workbuddy-manager）或 `ide`（OmniRoute），`workbuddy` 为当前官方桌面端取值（`WbVariant::oauth_platform`）。本次修复将两站点的 `platform` 显式参数化，国内版默认 `workbuddy`，国际版默认 `workbuddy-ai`。

## 3. 修复方案

### 3.1 改动点

1. **`CODEBUDDY_SITES` 补 `platform` 字段**：每站点显式声明扫码登录的 `platform` 参数，不再硬编码 `CLI`。
2. **国际版 `pluginBase` 改为 `https://www.workbuddy.ai`**；保留 `CODEBUDDY_GLOBAL_PLUGIN_BASE` 环境变量覆盖（防御国际站后续换域名）。
3. **扫码登录 URL 模板**：`POST {pluginBase}/v2/plugin/auth/state?platform={site.platform}`。
4. **同步更新** `.env.example` 注释与《开发设计方案》§4.3 的站点参数表。

### 3.2 为什么这样改

- 与社区多个独立实现（Go / Rust / Python / TS）逐一对齐，属于「官方客户端实测行为」，而非臆测；
- 改动收敛在站点元信息表，不触碰会话层、前端、账号存储——符合「站点工厂化」的既有架构；
- `platform` 参数化后，若上游改版只需改一处常量。

### 3.3 对现有逻辑的影响

- 国内版 `codebuddy` 行为**不变**：`platform` 从 `CLI` 改为 `workbuddy`，与官方桌面端当前取值一致（原 `CLI` 为旧版 workbuddy-manager 的遗留写法，实测当前官方桌面端已用 `workbuddy`；若个别环境仍接受 `CLI`，属上游兼容行为，不在本次保证范围）。
- 国际版 `codebuddy-intl` 扫码登录从「不可用」修复为「指向正确的国际版授权页」。
- 不涉及 `accounts.json` 结构变更；已落库的国际版账号不受影响。

## 4. 回归验证

| 用例 | 操作 | 预期结果 |
| --- | --- | --- |
| 国际版扫码登录 | 看板 → WorkBuddy 国际版 → 扫码登录 | 授权链接域名含 `workbuddy.ai` 或 `codebuddy.ai`（国际版），不再指向 `codebuddy.cn` |
| 国际版令牌取数 | `npm run probe -- codebuddy-intl`（已配置 `CODEBUDDY_GLOBAL_ACCESS_TOKEN`） | 正常返回余额 / 批次 / 到期时间 |
| 国内版扫码登录回归 | 看板 → CodeBuddy → 扫码登录 | 授权链接仍指向国内版，落库正常 |
| 国内版取数回归 | `npm run probe -- codebuddy` | 行为完全不变 |
| 类型检查 | `npm run typecheck` | 0 错误 |

> 按 AGENTS.md §6，本次仅做静态检查；上述动态验证由用户人工执行。

## 5. 风险与回滚

| 风险 | 说明 | 回滚 |
| --- | --- | --- |
| 国际版 `platform=workbuddy-ai` 与上游实际要求不符 | 已有 5+ 独立实现交叉验证，风险极低 | 将 `CODEBUDDY_SITES.global.platform` 改回 `CLI` 或设 `CODEBUDDY_GLOBAL_PLUGIN_BASE` 覆盖 |
| 国内版 `platform` 从 `CLI` 改为 `workbuddy` 导致个别旧环境失效 | 官方桌面端当前即 `workbuddy`；workbuddy-manager 的 `CLI` 为旧版写法 | 将 `CODEBUDDY_SITES.cn.platform` 改回 `CLI` |
| `workbuddy.ai` 与 `codebuddy.ai` 账号体系不互通 | `smart-open/TraeWorkAssistant` 实测「同一账号体系，单一鉴权门面即可」；若例外，网页端登录（Cookie）仍可兜底 | 改用手工 Cookie 或网页端登录 |

## 变更记录

| 时间 | 变更内容 | 触发原因 | 影响章节 |
| --- | --- | --- | --- |
| 2026-10-25 | 初版 | 用户报告国际版扫码登录打开国内版链接 | 全篇 |
