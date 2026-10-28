# CodeArts 华为云 · 网页端增删账号 开发设计方案

> 文档类型：开发设计方案
> 关联模块：平台适配器（`src/providers/codearts.ts`、`codearts-aksk.ts`）/ 通用账号管理（`src/account-store.ts`、`public/app.js`、`public/index.html`）
> 创建时间：2026-10-23
> 最后更新：2026-10-23
> 当前状态：§1–§9 已实施；§10「网页端登录」已实施；§11 需求迭代（当前浏览器开标签页的边界分析 + 登录成本优化文案）已实施
> 关联改动文件：`src/providers/base.ts`、`src/providers/codearts.ts`、`src/providers/codearts-aksk.ts`、`src/account-store.ts`、`src/cli/account.ts`、`public/app.js`、`public/index.html`、`README.md`、`docs/凭证配置指南.md`、`docs/modules/项目整体-代码Wiki.md`；§10 已改动：`src/providers/codearts.ts`（新增 `codeartsWebLogin` + `login` 声明 + `manageHint`/`credentialHint` 文案）、`README.md` 凭证表、`docs/凭证配置指南.md`
> 关联文档：`docs/fixes/CodeArts-华为云登录态频繁失效-修复方案.md`（AK/SK 数据源来源）、`docs/plans/账号管理-网页端统一账号入口-开发设计方案.md`（通用增删实现来源）

---

## 1. 背景与目标

**结论：CodeArts 是看板上唯一一个「有稳定凭证形态（AK/SK），却必须去终端敲 `npm run account` 或手改 `.env` 才能增删账号」的平台。本次让它接入已有的「声明式通用增删」实现，在看板卡片 / 「平台与账号」抽屉里直接添加、删除账号，不改任何写盘与槽位逻辑。**

### 1.1 要解决的问题

| 编号 | 问题 | 现状证据 |
| --- | --- | --- |
| P1 | CodeArts 适配器没有声明 `manageAccounts` / `manageFields`，前端 `renderManage` 直接 `hidden`，卡片与抽屉里只有「暂不支持网页添加」 | `src/providers/codearts.ts:306-312`（适配器定义里只有 `credentialHint`）、`public/app.js:1414-1418`（`if (!provider.manageAccounts) box.hidden = true`）、`public/app.js:1946-1949`（`platform__unsupported` 文案） |
| P2 | CodeArts 有**两种凭证形态**（AK/SK 首选、Cookie 兼容），而通用表单此前假设「每组字段同进同退」：必填字段要么全填要么全缺 | `src/providers/base.ts:43-66`（`ManageFieldDescriptor` 无成组能力）、`src/account-store.ts:474-478`（逐字段 `missingFields.push`） |
| P3 | AK/SK 读取函数能看到 `process.env.CODEARTS_AK/SK`，但看不到 `probe` 传入的 `ctx` 凭证来源，验证语义与取数语义不完全一致 | `src/providers/codearts-aksk.ts:85-101`（`readCodeArtsAkSk` 只读 `account` + `process.env`）、`src/providers/base.ts:532-547`（`cred()` 的取值口径） |

### 1.2 目标

1. CodeArts 在「平台与账号」中可直接**添加**（AK/SK，或 AK/SK + Cookie）和**删除**账号；写入走 `.env` 新槽位 `CODEARTS_ACCESSKEY` / `CODEARTS_SECRETKEY` / `CODEARTS_COOKIE`（`_N` 后缀），写盘前自动备份。
2. 表单语义与适配器的凭证优先级一致：**AK/SK 为主路径（必填）**；Cookie 为可选附加项，填了就参与混合增强（活动福利逐笔明细）。
3. 添加前用适配器自己的 `probe` 做真实验证——AK/SK 签名不通就不会写入。
4. 不改 `account-store.ts` 的槽位分配、去重、备份、`.env` 清理逻辑；前端不认平台名。

### 1.3 Non-Goals（本次明确不做）

| 不做的事 | 原因 |
| --- | --- |
| ~~不做 CodeArts 的扫码 / 打开链接登录~~（2026-10-23 被 §10 取代：新增「网页端登录」入口——服务端只负责**编排**调试 Chrome 开页与抓取，登录动作仍由人工在该窗口完成） | 原判断实际针对的是「服务端代登录」（修复方案 §3.1 选项 C/E 的否决**维持不变**）；§10 采用与 Qoder 相同的 `kind:'web'` 形态：人工登录 + CDP 自动抓 Cookie 落库，不触碰风控 |
| 不允许「只填 Cookie」添加 CodeArts 账号 | Cookie 1 小时会话超时后账号立刻失效，会制造「只看板报错」的假账号；只填 Cookie 的旧账号仍可删除，取数逻辑不受影响 |
| 不把 Sheet2 私钥（`sk`）当作 `secretKey` | 华为云 AK/SK 的 SK 是 IAM「我的凭证 → 访问密钥」里与 AK 配对的那一串；错误凭证交给 probe 验证时直接失败 |
| 不改 `manageAccounts` 的开关含义 | `account-store.ts` 的 403 条件保持不变 |

---

## 2. 现状调研

### 2.1 相关代码入口

| 能力 | 位置 | 现状 |
| --- | --- | --- |
| 适配器增删声明 | `src/providers/base.ts:194-224`（`credentialHint` / `manageAccounts` / `manageFields` / `manageHint`） | CodeArts 未声明 |
| 声明透出给前端 | `src/providers/index.ts:79-130`（`describeRegistry`） | `manageFields` 原样透出 |
| 通用新增 | `src/account-store.ts:387-637`（`addManagedAccount`） | 字段声明驱动；probe 验证 → 去重 → 槽位 → 备份 → 落盘 |
| 通用删除 | `src/account-store.ts:941-1010`（`removeManagedAccount`） | 按 `displayName` 定位，清理不再被引用的 `.env` 变量 |
| 环境变量命名 | `src/account-store.ts:111-128`（`envNameFor` / `providerEnvPrefix`） | `codearts.accessKey` → `CODEARTS_ACCESSKEY` |
| 前端表单 | `public/app.js:1389-1660`（`renderManage`） | 字段 → input/textarea/select；`credential` 落 `.env`，`option` 落 `options` |
| 表单标记 | `public/index.html:283-301`（`#manage-template`） | 字段容器 `.manage__fields` |
| CodeArts 取数 | `src/providers/codearts.ts:412-585`（`probe`） | AK/SK → 混合增强（+Cookie）→ 纯 Cookie 回退 |
| AK/SK 读取 | `src/providers/codearts-aksk.ts:85-101` | `account.credentials` → `options` → `process.env.CODEARTS_AK/SK` / `CODEARTS_CLI_AK/SK` |
| 凭证完整度 | `src/providers/base.ts:187-193`（`isConfigured`） | CodeArts 未声明，回退 `requiredCredentialKeys: ['cookie']` |

### 2.2 可复用的现有能力

- **声明式增删**：Trae / Qoder / CodeBuddy 已经在用同一套 `manageAccounts + manageFields`，前端与 `src/cli/account.ts` 都是通用实现，CodeArts 只需补声明。
- **凭证优先级的单一出口**：`probe` 已经按 `authMode` 选了 AK/SK 与 Cookie 的组合路径（`src/providers/codearts.ts:412-470`），表单验证直接调用它即可，不需要新的验证接口。
- **`requiredCredentialKeys` 的局限**：它表达不了「AK/SK 或 Cookie」，这正是 `isConfigured` 存在的理由（`src/providers/base.ts:187-193`），CodeArts 与 Trae 一样在这里补判断。

---

## 3. 方案设计

### 3.1 整体思路

```
表单（前端，通用）                account-store.addManagedAccount
┌────────────────────────┐      ┌─────────────────────────────────────────┐
│ AccessKey  [必填]       │      │ ① 字段校验（新增：paired 字段联动）          │
│ SecretKey  [全为必填]   │ ───▶ │ ② adapter.probe(临时账号) ─ AK/SK 签名→snap  │
│ Cookie（可选）[全为可选] │      │      ↓ ok？失败直接 400，不写盘              │
└────────────────────────┘      │ ③ 去重（credentials + options 全等）        │
                                │ ④ 分配 .env 槽位 CODEARTS_ACCESSKEY[_N] …   │
                                │ ⑤ 备份 → 写 .env + accounts.json            │
                                └─────────────────────────────────────────┘
```

**数据流**：看板上点「＋ 添加账号」→ 表单按 `/api/providers` 的 `manageFields` 渲染 → 提交 `/api/accounts/add` → 通用实现用 `codeartsAdapter.probe()` 验证 → 写 `.env` / `accounts.json` → 前端重绘卡片与抽屉。删除沿用通用实现，无改动。

### 3.2 字段设计

| 字段 key | 标签 | 落盘 | 必填 | 说明 |
| --- | --- | --- | --- | --- |
| `accessKey` | AccessKey | `CODEARTS_ACCESSKEY[_N]` | 是 | IAM 访问密钥的 AK |
| `secretKey` | SecretKey | `CODEARTS_SECRETKEY[_N]` | `pairedWith: accessKey` | 与 AK 成对 |
| `cookie` | Cookie（可选） | `CODEARTS_COOKIE[_N]` | 否（`required: false`） | 填了就参与混合增强 |

> `secretKey` 不设 `flagGroup`：只要 AccessKey 有值，SecretKey 就必须有值；只填 SecretKey 不填 AccessKey 时 AccessKey 仍按必填报错。

### 3.3 凭证完整度与读取口径

| 场景 | `isConfigured` | probe 行为 |
| --- | --- | --- |
| 只有 AK/SK | true | 走 snap-access，长期有效 |
| AK/SK + Cookie | true | AK/SK 权威余额 + 登录态活动逐笔（混合增强） |
| 只有 Cookie（旧账号） | true | 纯登录态回退（行为不变） |
| 都没有 | false → `notConfigured` | — |

`readCodeArtsAkSk` 新增 `ctx` 参数：`account.credentials` → `cred()`（含 `ctx` 凭证来源）→ `process.env`。这样「表单验证时临时账号里的 AK/SK」与「正式取数时的凭证解析」走同一条路径。

---

## 4. 接口设计

### 4.1 HTTP API

无新增接口。复用：

| 接口 | 方法 | 说明 |
| --- | --- | --- |
| `/api/accounts/add` | POST | 入参 `{ provider: 'codearts', credentials: { accessKey, secretKey, cookie? }, name? }`；CodeArts 从 403「未开放」变为可成功 |
| `/api/accounts/remove` | POST | 入参 `{ provider: 'codearts', accountId: '<displayName>' }` |
| `/api/providers` | GET | `codearts.manageAccounts = true`，`manageFields` 为 §3.2 三字段 |

### 4.2 契约扩展：`ManageFieldDescriptor`

| 新增字段 | 类型 | 语义 |
| --- | --- | --- |
| `pairedWith?: string` | 字段 key | 与指定字段成对：配对字段全空时本字段可选，配对字段有值则本字段必填（本字段 `required: false` 时始终可选） |
| `flagGroup?: string` | 字段组名 | 「全为可选」的字段组：组内全空不报错；任一有值时组内其余非 `required: false` 字段变为必填（本次无用法，留作通用能力） |

必填判定统一走 `src/account-store.ts` 的 `isFieldRequired()`（服务端校验与 `npm run account` 共用），前端 `updateGroupRules()` 只做同样的联动提示。

错误文案：`请填写：SecretKey。`（AccessKey 已填但 SecretKey 为空时）

### 4.3 CLI

`npm run account -- --provider codearts --accessKey <AK> --secretKey <SK> [--cookie <值>] [--name ...]` 由 `manageFields` 自动生效（flag 名 = 字段 key）；`--list` 的「可管理平台」列表会包含 codearts。

---

## 5. 数据模型

### 5.1 `accounts.json`

```json
{
  "provider": "codearts",
  "displayName": "CodeArts 华为云 · 账号 1",
  "enabled": true,
  "credentials": {
    "accessKey": "${CODEARTS_ACCESSKEY}",
    "secretKey": "${CODEARTS_SECRETKEY}",
    "cookie": "${CODEARTS_COOKIE}"
  },
  "options": {}
}
```

未填 Cookie 时 `credentials` 里不出现 `cookie` 键（与通用实现「可选字段留空不写引用」一致，`src/account-store.ts:588-594`）。

### 5.2 配置迁移

无。既有账号（Cookie 或 AK/SK + 全局 `.env`）不受影响；`CODEARTS_AK` / `CODEARTS_CLI_AK` 的全局兜底仍保留。

---

## 6. 影响面评估

| 面 | 影响 |
| --- | --- |
| 适配器 | 仅 CodeArts；Qoder / Trae / CodeBuddy / A1etherOpenAi 的 `manageFields` 不含新字段，行为不变 |
| 通用新增 | 字段校验新增成组分支；无 `pairedWith` / `flagGroup` 的声明走原路径（逐字段必填） |
| 通用删除 | 无改动；旧 Cookie 账号可直接删除 |
| 前端 | 字段渲染新增同组提示与浏览器级联动（`required` 动态化）；样式复用现有 `.manage__field` |
| 缓存 / 聚合 / 排序 | 无影响（账号变更后沿用现有缓存失效逻辑） |
| 凭证安全 | 表单 `secret: true` → 密码框；写入只落 `.env` 新槽位；页面不回显明文 |

---

## 7. 风险与备选方案

| 风险 | 降级 |
| --- | --- |
| AK/SK 权限不足（缺 `billing:resourcePackages:view` 等）导致验证失败 | probe 返回 `forbidden`，表单提示原始错误；用户在 IAM 补权限后重试 |
| 用户把 Sheet2 私钥误当 SecretKey | 验证失败即止，不写盘；字段 hint 说明来源 |
| 华为云后续新增 `X-Security-Token` 类约束（临时凭证） | 当前只支持长期 AK/SK；临时凭证需要 STS 头，属后续需求 |
| 错误的 AK/SK 实测返回 `HTTP 503 + APIG.0613`（不是 401），原先会被报成「服务不可用」 | 已在 `codearts-aksk.ts` 把 APIG.0613 映射成 `unauthorized`，文案提示「确认密钥正确且未被停用」，添加账号时不会误判为稍后重试即可 |

| 备选方案 | 结论 |
| --- | --- |
| 只声明 `cookie` 单字段（照抄 Trae） | ❌ 与「AK/SK 为推荐凭证」相悖，且表单验证恰在 Cookie 失效时最容易失败 |
| 跳过验证直接写盘 | ❌ 违背通用实现「先验证后写入」的约定 |
| AK/SK 与 Cookie 拆成两次添加 | ❌ 一个账号应是一份凭证组合；拆开会产生两条同平台账号与两份卡片 |

---

## 8. 实施步骤

| # | 步骤 | 产出物 |
| --- | --- | --- |
| 1 | `ManageFieldDescriptor` 增加 `pairedWith` / `flagGroup` | `src/providers/base.ts` |
| 2 | CodeArts 适配器声明 `isConfigured` / `manageAccounts` / `manageFields` / `manageHint`，接入 `scripted verify` 所需的凭证口径 | `src/providers/codearts.ts` |
| 3 | `readCodeArtsAkSk(account, ctx)` 纳入 `cred()` 口径 | `src/providers/codearts-aksk.ts` |
| 4 | `addManagedAccount` 字段校验支持成组语义 | `src/account-store.ts` |
| 5 | `describeRegistry` 透出 `required` / `pairedWith` / `flagGroup` / `secret` / `multiline` 等渲染字段 | `src/providers/index.ts` |
| 6 | 前端表单按成组语义渲染提示与动态必填 | `public/app.js`、`public/index.html` |
| 7 | CLI 帮助文案补充成组字段说明 | `src/cli/account.ts` |

---

## 9. 验证方式

### 9.1 静态检查（AI 执行）

| 检查 | 命令 | 预期 |
| --- | --- | --- |
| 类型检查 | `npm run typecheck` | 通过 |
| 前端语法 | `node --check public/app.js` | 通过 |
| 可管理平台 | `npm run account -- --list` | codearts 出现在可管理清单，字段为 accessKey / secretKey / cookie |
| 契约回归 | 对比 `/api/providers` 中其它四平台的 `manageFields` | 仅新增空字段，无值变化 |

### 9.2 人工验收（用户执行，AI 不接触凭证明文）

| # | 场景 | 预期 |
| --- | --- | --- |
| 1 | 打开「平台与账号」→ CodeArts 行 | 出现「＋ 添加账号」，展开后三个字段（AK 必填、SK 必填、Cookie 可选） |
| 2 | 填 AK/SK 提交 | 显示「验证中…」→ 成功提示带额度与变量名 `CODEARTS_ACCESSKEY` / `CODEARTS_SECRETKEY` |
| 3 | 只填 AK 提交 | 前端提示填写 SecretKey，不发请求 |
| 4 | 重复添加同一份 AK/SK | 409「这份凭证已经配置过了」 |
| 5 | 加 AK/SK + Cookie | 成功后 `npm run probe -- --json` 中活动福利为逐笔明细 |
| 6 | 删除新账号 | 卡片消失；`.env` 中对应变量被清理 |
| 7 | 删除旧 Cookie 账号 | 正常删除；其余 CodeArts 账号取数不受影响 |

---

## 10. 增量设计：CodeArts「网页端登录」一键添加（2026-10-23 新增，评审中）

### 10.1 背景与诉求

**结论：AK/SK 表单路径太重（要去 IAM 控制台建密钥、复制两段长字符串、SK 只显示一次）；用户诉求「点添加 → 跳转登录 → 自动回调」。华为云没有可供第三方使用的 OAuth 回调协议，但仓库已有完全等价的「网页端登录」基建（Qoder 已上线）——CodeArts 只需补一段适配器声明，即可获得：点按钮 → 自动打开调试 Chrome 的华为云用量页 → 人工在该窗口登录（华为云账号级登录态长期有效，多数情况 SSO 静默完成、无需输密码）→ 服务端 CDP 旁听真实请求头自动抓 Cookie → probe 验证 → 自动写入 `.env` / `accounts.json` → 前端报 ready，全程零复制。**

| # | 事实 | 证据 |
| --- | --- | --- |
| N1 | AK/SK 表单路径重：IAM 控制台 → 访问密钥 → 创建 → 复制 AK/SK → 回看板粘贴 | §3.2 字段声明（`src/providers/codearts.ts:337-347`） |
| N2 | 本机当前 `accounts.json` 无 codearts 条目（取数靠全局 `CODEARTS_AK/SK` 兜底）、`.env` 无 `CODEARTS_COOKIE`——正是「第一次添加被表单拦住」的场景 | 本机两文件键名核对（2026-10-23，只看键名不看值） |
| N3 | 华为云登录 = 浏览器 Set-Cookie 会话，无授权码回调；AK/SK 也不可能由登录流程产出（SK 创建时仅展示一次） | `docs/fixes/CodeArts-华为云登录态频繁失效-修复方案.md` §2.1、§3.6 |
| N4 | 等价形态已存在：Qoder「网页端登录」（`kind: 'web'`）= 服务端拉起调试 Chrome + CDP 旁听 Cookie + 登录会话层落库，全链路平台无关 | `src/providers/qoder.ts:1263-1390`、`src/lib/cdp.ts:203/353`、`src/login-session.ts`、`src/account-store.ts:784` |
| N5 | 华为云账号级登录态（auth.huaweicloud.com 刷新令牌）长期有效，打开用量页多可 SSO 静默重登（免密码） | `tools/codearts-refresh.sh:135-152` 及修复方案 §3.5 取证 |

### 10.2 可行性边界：为什么不是「跳转链接 + 回调」

**结论：用户日常浏览器打不开「自动回调」这条路——服务端没有它的 CDP 通道，华为云也不给 OAuth 授权码；唯一能「自动回传凭证」的浏览器是本仓库的调试 Chrome（带 `--remote-debugging-port`、独立 profile）。因此交互形态为「点按钮 → 调试 Chrome 自动开页 → 登录 → 自动落库」，体验上等价于跳转回调。**

| 候选路径 | 可行性 | 结论 |
| --- | --- | --- |
| OAuth 授权码回调（Trae 式 `/authorize`） | 华为云无公开授权页协议；登录即 Set-Cookie（Trae 的回调之所以成立，是因为 Trae 平台提供授权页） | ❌ 协议不存在 |
| 用户日常浏览器打开链接 | 服务端拿不到该浏览器 Cookie（无调试端口） | ❌ 无采集通道 |
| CodeArts CLI 的 OAuth（cli-auth 页） | 产出 STS 临时凭证且协议需逆向 CLI 二进制 | ❌ 修复方案 §3.1 选项 D 已否决（更短命） |
| 服务端代填账密 / 过验证码 | 安全红线 | ❌ 修复方案 §3.1 选项 C/E 否决**维持不变** |
| **调试 Chrome + CDP 旁听（Qoder 形态）** | 基建现成（`src/lib/cdp.ts`），登录动作人工完成 | ✅ **本次方案** |

### 10.3 方案设计

一次「网页端登录」的完整时序：

```
看板点「网页端登录」
  → POST /api/login/start { provider:'codearts' }            （privilegedGuard 闸门，src/server.ts:513）
  → startLogin → codeartsWebLogin.start                       （src/login-session.ts:170）
      ① ensureDebugChrome：未跑则按 chrome-debug.sh 同参拉起   （src/lib/cdp.ts:203）
      ② openCookieCollector：新标签页打开用量页 + Network 旁听 （src/lib/cdp.ts:353）
  → 前端面板显示「请在打开的 Chrome 窗口登录…」
  → 用户在调试 Chrome 完成登录（多数情况华为云 SSO 静默完成，无需密码）
  → 后台轮询（2s）：codearts.huaweicloud.com 请求头的 Cookie 名
      含 devclouddevuibjJ_SESSION_ID → ready
  → landLoginAccount（src/account-store.ts:784）：probe 真实验证（本机=AK/SK 混合增强口径）
      → 去重 → 槽位 CODEARTS_COOKIE → 备份 → 写 .env / accounts.json
  → 前端 ready：卡片出现；此后 launchd codearts:refresh 自动续期同一变量
```

关键设计决策：

| 决策 | 内容 | 理由 |
| --- | --- | --- |
| 登录方法声明 | `codeartsWebLogin`（`id:'web'`、`kind:'web'`、`qr:false`）挂 `adapter.login`（唯一方法，无需 `loginMethods` 数组） | 与 Qoder `qoderWebLogin` 同构（`src/providers/qoder.ts:1263`）；会话层/前端声明驱动，零新概念 |
| 打开页面 | `https://codearts.huaweicloud.com/portal/settings/personal-usage?locale=zh-cn` | 与 `tools/codearts-refresh.sh:27` 的 LOGIN_URL 同址：会触发 SSO 重定向与 `/portal/` 接口请求，登录后立即产生可旁听的请求 |
| 登录判据 | `collector.cookieNamesFor(['codearts.huaweicloud.com'])` 含 `devclouddevuibjJ_SESSION_ID`（主判据）；判据过松也无害——落库前 probe 真实验证兜底，失败报 failed 不写盘 | 与 `credentialHint` 要求一致（`src/providers/codearts.ts:320`）；Qoder「访客 Cookie」教训见 `qoder.ts:1310-1325` |
| 页面刷新策略 | 复用 Qoder「空闲才刷新」参数（idle ≥20s、间隔 ≥15s、最多 2 次，`qoder.ts:102-106`） | 避免重置用户正在填写的登录表单（`docs/fixes/Qoder-网页端登录一直等待-修复方案.md` 的教训） |
| TTL / 轮询间隔 | 10 分钟 / 2s（对齐 `QODER_WEB_LOGIN_TTL_MS`，`qoder.ts:108/93`） | 华为云 MFA 登录耗时可控 |
| 去重与更新语义 | `account.id` 恒为 `'web'`（平台级单账号）：重新登录 → 按 `options.loginAccountId` 命中同一条目原地更新（`src/account-store.ts:839`），Cookie 值原地刷新 `CODEARTS_COOKIE` | ① 调试 Chrome 单 profile 同一时刻只有一个华为云登录态，多账号本就无法并发登录（多账号走 AK/SK 表单多槽位）；② 避免「Session ID 每次登录都变 → Cookie 指纹去重必新建卡片」的坑（Qoder 用指纹可行是因为其登录态 Cookie 相对稳定，华为云不行） |
| 凭证落点 | `credentials.cookie` → 槽位 1 `CODEARTS_COOKIE`（`envNameFor` 默认前缀推导，`src/account-store.ts:125-133`） | 与 `capture.ts:246`（envKey）、`codearts:refresh` / `codearts:console-probe`（读写 `CODEARTS_COOKIE`）整条工具链同名——登录落库后，既有 launchd 定时任务自动接管续期 |
| 昵称 | 首版用占位名「CodeArts 华为云 · 登录账号」；身份接口取证后再补（§10.6 待确认 1） | Qoder 有 `/api/v1/me` 可取昵称，CodeArts 的对应接口未取证；占位名不影响去重与功能（`isPlaceholderLoginName` 支持后补改名） |
| 与 AK/SK 的关系 | 登录产物只有 Cookie。本机有全局 `CODEARTS_AK/SK` → probe 走混合增强（余额 AK/SK 权威 + 活动逐笔 Cookie）；无全局 AK/SK 的机器 → 纯 Cookie 口径，会话过期后卡片 unauthorized | 余额主路径仍是 AK/SK（§3.3 不变）；登录入口的定位是「免复制快速上手 + 活动明细」，不是替代 AK/SK |

### 10.4 接口与数据模型（无新增）

| 面 | 结论 |
| --- | --- |
| HTTP API | 无新增：`/api/login/start` / `poll` / `cancel`（`src/server.ts:513-582`）与 `privilegedGuard` 原样复用；`/api/providers` 的 `login` / `loginMethods` 由 `describeRegistry` 自动透出（`src/providers/index.ts:132-142`） |
| 契约 | `ProviderLogin`（`src/providers/base.ts:127-160`）零扩展；`kind: 'web'` 语义已存在 |
| 前端 | **零改动**：`renderLogin` 按 `loginMethods` 渲染按钮组（`public/app.js:1147-1156`）；`kind==='web'` 自动隐藏链接/复制/二维码（`public/app.js:1185-1191`）；声明登录后手工表单自动收进「手工填写凭证（兜底）」`<details>`（`public/app.js:1445-1454`） |
| accounts.json 新条目 | `{ provider:'codearts', displayName:'CodeArts 华为云 · 登录账号', enabled:true, credentials:{ cookie:'${CODEARTS_COOKIE}' }, options:{ loginAccountId:'web' } }` |
| `.env` | 登录成功自动写入 `CODEARTS_COOKIE=<整段 Cookie 请求头>`（槽位 1） |

### 10.5 影响面评估

| 面 | 影响 |
| --- | --- |
| 改动范围 | 仅 `src/providers/codearts.ts`（新增 `login` 声明 + `codeartsWebLogin` 实现约 80 行）；`cdp.ts` / `login-session.ts` / `account-store.ts` / `server.ts` / `public/*` **零改动** |
| 旧 Non-Goal 修订 | §1.3 首行改为「不做**服务端代登录**」；表单的「不允许只填 Cookie」维持不变（表单无自愈能力），网页登录允许 Cookie-only 落库（probe 验证 + refresh 工具链 + 一键重登自愈，成本结构与表单裸 Cookie 不同） |
| AK/SK 表单 | 保留为兜底与多账号路径；前端从主入口退为折叠的「手工填写凭证（兜底）」 |
| 其它平台 | 声明驱动，零影响（`describeRegistry` 只透出声明） |
| 凭证安全 | Cookie 只经 CDP → 内存 → `.env`（0600），不经 HTTP 响应与日志（`login-session.ts:13-18` 设计要点 4）；`/api/login/*` 走 privilegedGuard（回环 Host + Origin + 意图头） |
| 工具链协同 | `codearts:refresh`（launchd）维护的正是 `CODEARTS_COOKIE`——登录产物与其对齐后，该定时任务从「无 Cookie 可刷、永远 exit 3」变为自动维护这个账号（SSO 静默续期） |

### 10.6 风险与待确认

| 风险 | 降级 |
| --- | --- |
| 匿名态也出现 `devclouddevuibjJ_SESSION_ID`（判据过松）→ 误报 ready | `landLoginAccount` 落库前 probe 真实验证（`src/account-store.ts:812-829`）必然失败 → 报 failed + 原因，不写盘 |
| 华为云登录风控 / 验证码 / MFA | 登录动作 100% 人工在调试 Chrome 窗口完成，服务端不触碰（维持修复方案 §3.1 选项 C/E 红线） |
| `.env` 残留旧 `CODEARTS_COOKIE`（历史 capture 留下且有值）→ 登录落到 `_2` 槽位，与 refresh 工具链错位 | 凭证指南与 `manageHint` 写明：登录前清理遗留变量（`allocateSlot` 会跳过有值槽位，`src/account-store.ts:150-159`）；本机 2026-10-23 已核对无此变量，首登即槽位 1 |
| 无全局 AK/SK 的机器上 Cookie 过期 → 卡片 unauthorized | ready 文案与凭证指南引导补 AK/SK；装了 launchd refresh 则 SSO 自动续期；否则重点一次「网页端登录」即自愈 |
| 昵称缺失（占位名不友好） | 待确认 1 解决后补；不影响去重与功能 |

待确认（不阻塞实施，首版均有兜底）：

1. **控制台身份接口**：是否有「当前用户」类 `/portal/` 接口可拿昵称（需一次人工 DevTools 取证，类似 Qoder `/api/v1/me` 的发现过程）；取证前用占位名。
2. **会话 Cookie 匿名态行为**：`devclouddevuibjJ_SESSION_ID` 是否匿名访客也会下发；若会，判据需加功能性证据（页面自己请求 `/portal/promptcenter/v1/ops/credit/overview` 得 2xx 且响应非空——注意华为云失效会话返回「200 + 空响应」，纯 2xx 不算证据，`src/providers/codearts.ts:93-97`）。

被否决的备选方案：见 §10.2 表（OAuth 回调 / 日常浏览器链接 / CLI OAuth / 服务端代登录）。

### 10.7 实施步骤

| # | 步骤 | 产出物 |
| --- | --- | --- |
| 1 | `src/providers/codearts.ts`：新增 `codeartsWebLogin`（`start` / `poll` / `cleanup`）并声明 `adapter.login`；删除「刻意不提供登录入口」注释（`src/providers/codearts.ts:329-330`）；`credentialHint` / `manageHint` 文案补「推荐网页端登录」 | `src/providers/codearts.ts` |
| 2 | 文档同步：`README.md` 凭证表 CodeArts 行、`docs/凭证配置指南.md` CodeArts 小节改为「网页端登录（推荐）→ AK/SK 表单（兜底 / 多账号）」；修复方案文档补变更记录 | 三处文档 |
| 3 | 静态检查：`npm run typecheck`；确认其它平台 `loginMethods` 输出不变 | 检查结论 |

> 刻意不改 `src/lib/cdp.ts`、`src/login-session.ts`、`src/account-store.ts`、`src/server.ts`、`public/*`——全部复用既有声明驱动链路，这是本方案改动面最小的证明。

### 10.8 验证方式

静态（AI 执行）：`npm run typecheck` 通过；`describeRegistry` 透出的 codearts 登录方法为 `[{ id:'web', kind:'web', qr:false }]`，其它平台输出不变。

#### 实施记录（2026-10-23，§10 已落地）

| 项 | 结果 |
| --- | --- |
| `npm run typecheck` | 通过（exit 0） |
| 注册表透出 | `codearts.loginMethods = [{ id:'web', label:'网页端登录', qr:false, kind:'web' }]`；`manageFields` 仍为 accessKey / secretKey / cookie 三字段 |
| 其它平台回归 | qoder=2（scan+web）、codebuddy=1（scan）、trae=1（link）——与实施前一致，零变化 |
| 改动面 | 与 §10.5 承诺一致：仅 `src/providers/codearts.ts`（`codeartsWebLogin` 约 100 行 + `login` 声明 + 常量区 + 文案）；`cdp.ts` / `login-session.ts` / `account-store.ts` / `server.ts` / `public/*` 零改动 |
| 实现与设计的偏差 | ① 昵称：新增 `codeartsCookieIdentity()`（`/portal/rest/me` 的 name/userName/projectName 兜底解析）——比设计的「首版占位名」更进一步，取证顺手做了，取不到仍回落占位名；② `manageHint` 追加「网页登录账号与全局 AK/SK 并存会重复计余额」提示（设计未列，实施时识别到的既有语义风险） |
| 未执行 | 浏览器端登录流（需真实凭证与人工操作，按规范留给用户 §10.8 验收） |

人工验收（用户执行，AI 不接触凭证明文）：

| # | 场景 | 预期 |
| --- | --- | --- |
| 1 | 「平台与账号」→ CodeArts → 点「网页端登录」 | 调试 Chrome 自动打开用量页；看板面板显示等待文案 |
| 2 | 在该窗口完成登录（有账号级登录态时应 SSO 静默完成） | 面板 ready；卡片出现且活动福利为逐笔明细（本机混合增强口径） |
| 3 | 核对 `.env` / `accounts.json` | 出现 `CODEARTS_COOKIE`；新条目引用 `${CODEARTS_COOKIE}` |
| 4 | 再点一次「网页端登录」并完成 | 更新同一条目（不新增卡片），Cookie 值刷新 |
| 5 | AK/SK 表单兜底 | 折叠在「手工填写凭证（兜底）」里，仍可用 |
| 6 | 到点或手动跑 `npm run codearts:refresh` | 自动校验 / SSO 续期该 Cookie，看板活动逐笔持续 |

---

## 11. 需求迭代：「改在当前浏览器打开标签页」（2026-10-23）

### 11.1 结论与边界

**结论：无法把「自动抓取登录态」搬进日常浏览器——Chrome 136+ 禁止对默认 user-data-dir 开调试端口（传了也会被静默忽略），且 CDP 端口必须在浏览器启动时传入、无法附加到已在运行的日常实例；没有调试端口服务端就拿不到 Cookie，「自动完成添加」就无从谈起。改为把调试 Chrome 的登录成本降到「一次」：引导保存密码 / App 扫码 / Touch ID 通行密钥，配合既有 SSO 静默续期与 launchd 定时任务。**

| # | 事实 | 证据 |
| --- | --- | --- |
| E1 | Chrome 136+ 对默认 user-data-dir 静默忽略 `--remote-debugging-port` | `tools/chrome-debug.sh:4-8`（本机实证注释）、`src/lib/cdp.ts:194-199`（同约束注释） |
| E2 | CDP 端口只能启动时传入；运行中的日常浏览器无法事后附加 | Chrome DevTools 协议约束（`cdp.ts` 的 `ensureDebugChrome` 只能「拉起」不能「附着」） |
| E3 | 调试 profile（`~/.power-meter/chrome-profile`）是持久目录：Chrome 密码管理器保存的密码、华为云账号级登录态（auth.huaweicloud.com 持久 Cookie）跨窗口关闭/重启存活 | `tools/chrome-debug.sh:17`（固定 PROFILE 路径）、修复方案 §3.5「HW_id/HW_refts 有效期到 2027」 |
| E4 | Touch ID / 通行密钥（WebAuthn）走 macOS 系统钥匙串，与浏览器 profile 无关——调试 Chrome 同样可用指纹快速填充 | macOS 系统级能力（iCloud 钥匙串同步） |
| E5 | 本机 launchd 续期任务已装载且上次退出码 0（会自动 SSO 静默续期，多数情况无需人工登录） | `launchctl list` 含 `com.powermeter.codearts-refresh`（2026-10-23 核对） |

### 11.2 被否决的备选方案

| 备选 | 否决原因 |
| --- | --- |
| 在日常浏览器打开登录页（`open` 默认浏览器） | 登录态落在日常浏览器，服务端无 CDP 通道拿不到 Cookie → 退回「手工复制 Cookie」路径（表单兜底已存在），失去自动添加；且会误导用户以为在该窗口登录有效 |
| 复制日常 profile 目录给调试 Chrome 用 | profile 体积大（GB 级）；日常 Chrome 运行期间 profile 被锁；双实例并发写同一 profile 数据会损坏 |
| symlink 指向默认 profile 绕过 E1 检测 | Chrome 按解析后路径判定，绕不过；且并发写损坏 profile 的风险同上 |
| 把日常浏览迁移进调试 profile（让它变成日常浏览器） | 调试端口常开 = 本机任意进程可经 CDP 完全控制该浏览器（读取任意页面登录态），安全面不可接受，不推荐 |

### 11.3 已实施的优化（仅文案，行为零改动）

| # | 改动 | 内容 |
| --- | --- | --- |
| 1 | `codeartsWebLogin.hint`（按钮提示） | 引导「首次登录让 Chrome 保存密码 / 华为云 App 扫码 / Touch ID 通行密钥——该窗口配置持久，之后基本无需再输密码」 |
| 2 | `start()` 返回的 `hint`（面板提示） | 同上口径：都只需一次，之后自动填充 / SSO 静默续期 |
| 3 | `docs/凭证配置指南.md` | CodeArts 小节补「首次登录小贴士」段 |

> 机制澄清（写给未来维护者）：用户体感「每次都要输密码」多发生在**首次**或账号级登录态过期后；正常路径下 `.env` 副本 + launchd SSO 续期（E5）+ 账号级持久 Cookie（E3）已把人工登录压到极低频。保存密码（E3）与 Touch ID 通行密钥（E4）再把「不得不登录时」的成本降到秒级。

### 11.4 已知问题：点已存账号填充时每次都弹 Touch ID（2026-10-23 实测反馈）

**结论：该弹窗来自 macOS 钥匙串而非华为云或本项目代码——Chrome 的已存密码由钥匙串条目「Chrome Safe Storage」的钥匙加密，其「访问控制」若勾了「访问前确认」，每次自动填充解密都要 Touch ID 授权一次（授权只放行当次，不写入信任列表）。解除属于**一次性系统设置**，代码侧无可改也不应改（程序代填/绕过钥匙串属安全红线，见 AGENTS.md §7）。**

| 修法 | 步骤 | 代价 / 边界 |
| --- | --- | --- |
| **钥匙串一次性解除（推荐）** | 打开「钥匙串访问」→ 登录钥匙串 →「密码」→ 搜索 `Chrome Safe Storage` → 双击 →「访问控制」→ 取消「访问前确认」（或改选「允许所有应用程序访问这项」）→ 存储更改（输一次登录密码） | 选「允许所有应用程序」会让本机任意应用都能解 Chrome 密码库，安全面收窄；优先用「取消确认 + 限定 Chrome」形态。系统找不到「钥匙串访问」App 时（新版 macOS 逐步用「密码」App 取代）退用扫码路径 |
| **华为云 App 扫码（零密码零指纹）** | 登录页左侧二维码直接用华为云 App 扫、手机上确认 | 与密码/钥匙串完全无关，最省事；需要装 App |
| 兜底：重建钥匙 | 钥匙串里删除 `Chrome Safe Storage` → 重启 Chrome（自动重建）→ 重新保存一次密码 | 已存密码全部失效，需重存；仅在前两法都不行时用 |

> 附带清理：Chrome 的密码管理器里若同一账号存了两条（如 `15170353201` 与 `008615170353201`），可在 `chrome://password-manager/passwords` 删掉重复项，避免选错。

---

## 变更记录

| 时间 | 变更内容 | 触发原因 | 影响章节 |
| --- | --- | --- | --- |
| 2026-10-23 | 初版：CodeArts 接入通用声明式增删，AK/SK 必填 + Cookie 可选；`ManageFieldDescriptor` 增加成组语义；`isConfigured` 与 `readCodeArtsAkSk(ctx)` 对齐凭证口径 | 用户要求「继续 CodeArts 网页端增删的开发」 | 全篇 |
| 2026-10-23 | 实施回填：① `probe` 在「有 AK/SK、无 Cookie」时直接以 AK/SK 结果返回（原逻辑会落到 `notConfigured`，导致纯 AK/SK 账号与添加预校验误判）；② `APIG.0613` 映射为 `unauthorized` 并补文案；③ `npm run account` 的必填判断改用 `isFieldRequired()`；④ 前端管理表单对成组字段联动「（必填）/（可选）」与 `input.required` | 静态联调发现上述三处语义不一致 | §3.3、§4.2、§7、§8、变更记录 |
| 2026-10-23 | 验证记录：`npm run typecheck`、`node --check public/app.js` 通过；`npm run account -- --list` 中 CodeArts 出现在可管理清单；用本机 `.env` 中已有 AK/SK 对 `probe` 与 `addManagedAccount({ dryRun: true })` 做了只读验证，返回 `status: ok`、预分配槽位 `CODEARTS_ACCESSKEY_2` / `CODEARTS_SECRETKEY_2`，未写盘 | 按 §9.1 执行静态检查 | §9 |
| 2026-10-23 | 增量设计 §10：CodeArts「网页端登录」一键添加——复用 Qoder `kind:'web'` 基建（ensureDebugChrome + openCookieCollector + 登录会话层落库），仅改 `codearts.ts` 声明；登录判据 SESSION_ID Cookie 名、去重用平台级常量 id `'web'`、槽位对齐 `CODEARTS_COOKIE` 工具链；修订 §1.3 首条 Non-Goal（「不做登录入口」→「不做服务端代登录」） | 用户反馈 AK/SK 添加太复杂，要求「点添加跳转登录自动回调」；调研确认华为云无 OAuth 回调协议，采用等价的调试 Chrome 编排形态 | §1.3、§10、元信息 |
| 2026-10-23 | §10 实施完成：`codearts.ts` 新增 `codeartsWebLogin`（start/poll/cleanup）并声明 `login`；新增 `codeartsCookieIdentity()`（昵称兜底解析，`/portal/rest/me`）；`credentialHint`/`manageHint` 文案更新（网页登录为推荐路径 + 全局 AK/SK 并存提示）；同步 `README.md` 凭证表与 `docs/凭证配置指南.md`（小节更名「网页端登录（推荐）与 AK/SK」）。`npm run typecheck` 通过、注册表回归无误。文档行号基线随之偏移（§10 各引用核对于实施前） | 用户确认「完成全部开发」 | §10.7、§10.8、元信息、变更记录 |
| 2026-10-23 | §11 新增：需求「改在当前浏览器打开标签页」的边界分析（Chrome 136+ 禁止日常 profile 开 CDP 端口，自动抓取无法迁移）与四个被否决备选；实施三处文案优化（登录方法 `hint` ×2、凭证指南「首次登录小贴士」），引导保存密码 / App 扫码 / Touch ID 通行密钥把登录成本降到一次；`npm run typecheck` 通过 | 用户反馈新开 Chrome 没有密码存储、每次要重新输入/指纹 | §11、元信息、变更记录 |
| 2026-10-23 | §11.4 新增：实测反馈「点已存账号填充每次弹 Touch ID」的根因（macOS 钥匙串对 Chrome Safe Storage 的「访问前确认」，非华为云/本项目问题）；给出一次性解除步骤、华为云 App 扫码零指纹路径与重建钥匙兜底；凭证指南小贴士同步补一段。纯文档改动 | 用户回传两张登录页截图（密码管理器弹层 + Touch ID 授权框）并要求「点击账号直接填充」 | §11.4、变更记录 |
