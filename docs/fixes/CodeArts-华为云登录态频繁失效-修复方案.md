# CodeArts（华为云码道）登录态频繁失效 - 修复方案

> 文档类型：修复方案
> 关联模块：平台适配器（`codearts`）/ 凭证抓取
> 创建时间：2026-10-11
> 最后更新：2026-10-23
> 当前状态：Phase 1 + Phase 2（混合增强）已实施，待人工验证
> 关联改动文件：`src/lib/huaweicloud.ts`（新增，AK/SK 签名）、`src/cli/codearts-aksk-probe.ts`（新增，只读探测）、`src/providers/codearts-aksk.ts`（新增，AK/SK 数据源）、`src/providers/codearts.ts`（接入 authMode）、`package.json`、`.env.example`、`accounts.example.json`、`README.md`、`docs/凭证配置指南.md`

---

## 1. 问题现象

**结论：CodeArts 卡片的数据源完全依赖 `codearts.huaweicloud.com` 的控制台会话 Cookie，而华为云控制台会话受「会话超时策略」管控（默认 1 小时、默认开启且不可关闭），过期后必须人工重新登录浏览器并重跑 `npm run capture`，无法像 Trae 那样自动续签。**

- 典型表现：
  1. 看板 CodeArts 卡片间歇性变成失败态（`unauthorized` / 解析报错「响应缺少 all_credit_package」），需要重新抓取 Cookie 才恢复；
  2. `npm run probe` 报 401/403，或返回 HTTP 200 但内容是 SPA HTML（网关把请求当成了前端路由）；
  3. 重新抓取的完整人工链路是：`npm run chrome:debug` → 浏览器里重新登录华为云 → 停在用量页 → `npm run capture`（`src/cli/capture.ts:1-18` 明确要求这三个前置步骤）。
- 影响范围：仅 CodeArts 单卡片的取数，不波及其它平台；但「最快到期」排序与顶部汇总会因该卡片失败而缺一块。
- 触发环境：华为云账号（国内站），适配器走控制台 portal 接口，凭证类型为 Cookie（`src/providers/codearts.ts:229-233`）。

## 2. 根因分析

### 2.1 结论

问题不是「适配器写错了」，而是**凭证介质选型与控制台会话生命周期不匹配**：

1. 华为云 IAM 的「登录验证策略 → 会话超时」**默认开启、不能关闭**，超时时长默认 **1 小时**，可在 15 分钟 ~ 24 小时之间调整（来源：[IAM 用户指南 - 设置登录验证策略](https://support.huaweicloud.com/usermanual-iam/iam_01_0704.html)）。超过时长未操作界面，服务端会话失效；
2. 适配器只认 Cookie（`requiredCredentialKeys: ['cookie']`，`src/providers/codearts.ts:229`），且请求头由 Cookie 推导 CSRF `cftk`（`src/providers/codearts.ts:61-69`、`245`、`330`）；
3. 该 Cookie（`devclouddevuibjJ_SESSION_ID` / `devclouddevuibjtcftk`）是**服务端会话态凭证，没有文档化的刷新接口**，程序侧无法自行续期——只有浏览器登录这一条路；
4. 失效时的错误语义依赖上游返回形态：401/403 会被映射为 `unauthorized`/`forbidden`（`src/lib/http.ts:35-45`），返回 HTML 则会在 `parseCodeArtsOverview` 抛「响应缺少 all_credit_package（Cookie 可能已失效）」（`src/providers/codearts.ts:101`）。

### 2.2 证据链

| # | 事实 | 证据 |
| --- | --- | --- |
| 1 | 适配器唯一凭证是 Cookie | `src/providers/codearts.ts:229-233`（`requiredCredentialKeys: ['cookie']`、credentialHint 要求含 `devclouddevuibjJ_SESSION_ID` 与 `devclouddevuibjtcftk`） |
| 2 | 所有上游请求都靠 Cookie + 推导 cftk，没有其它鉴权 | `src/providers/codearts.ts:242-255`、`325-341` |
| 3 | 抓取工具对 CodeArts 同样是「Cookie 域收集」模式，无自动续期 | `src/cli/capture.ts:160-167`（`cookieSites: ['codearts.huaweicloud.com','huaweicloud.com']`、`apiHost: 'codearts.huaweicloud.com'`） |
| 4 | 抓取必须人工在已登录的调试 Chrome 内完成 | `src/cli/capture.ts:1-18`（前置三步）、`capture.ts:573-592`（先校验 CDP 连接） |
| 5 | 控制台会话超时默认 1 小时、默认开启不可关闭 | 华为云 IAM 文档（见 2.1 链接） |
| 6 | 项目已有「Cookie 会失效」的隐式预期（把失效文案写进了解析错误） | `src/providers/codearts.ts:101` |
| 7 | 项目早期调研已注意到 CodeArts 存在 AK/SK 体系但当时未采用 | `README.md:561-562` |

### 2.3 为什么其它平台没这么痛

| 平台 | 凭证形态 | 是否会自动续期 | 证据 |
| --- | --- | --- | --- |
| Trae | Cookie → 换取 JWT | ✅ 适配器内自动续签，JWT 按 Cookie 哈希缓存 | `docs/modules/项目整体-代码Wiki.md:232`、`src/providers/trae.ts:43-320` |
| A1etherOpenAi | API 密钥 | ✅ 长期有效（仅轮换时更新） | `docs/modules/项目整体-代码Wiki.md:235` |
| CodeBuddy / Qoder | Cookie | ❌ 同样会过期，但会话由各自平台策略决定 | `docs/modules/项目整体-代码Wiki.md:231-233` |
| **CodeArts** | **控制台会话 Cookie** | ❌ **且华为云会话超时策略默认 1 小时** | 见 2.1 |

即：CodeArts 是本项目里**唯一一个「凭证必须人工续、且上游明确限制会话时长」**的平台，所以体感最差。

## 3. 修复方案

### 3.1 选项对比（含「CLI 授权」评估）

**结论：可以走 AK/SK 授权，「CLI（KooCLI）」只是调用载体，真正解决过期问题的是 AK/SK 长期凭证；但 AK/SK 能覆盖多少数据（套餐 / 积分包 / 活动福利逐笔）取决于费用中心开放接口的实际覆盖范围，必须先用 Phase 0 实测确认。**

| 方案 | 可行性 | 能否根治过期 | 数据完整度风险 | 结论 |
| --- | --- | --- | --- | --- |
| **A. AK/SK + 费用中心（BSS/OCE）开放接口** | 高（接口已公开文档化，仅需实测覆盖范围） | ✅ 根治（AK/SK 长期有效） | 中：`活动福利`逐笔明细可能拿不到，退化为包级口径 | **推荐（主路径）** |
| **A'. KooCLI（`hcloud`）调用同一批接口** | 高（KooCLI 支持 `--cli-access-key/--cli-secret-key` 免配置调用 API Explorer 上的 API） | ✅ 同上 | 同上 | 仅作 Phase 0 的**验证工具**；不引入为运行时依赖（本项目运行时零第三方依赖，且外挂 CLI 会带来安装/版本/子进程解析成本） |
| **B. 维持 Cookie + 缓解措施**（IAM 会话超时调至 24h、失效文案可操作化） | 立即可做 | ❌ 仍会过期，只是变慢 | 无 | **必做的兜底与降级路径**，与 A 并存 |
| **C. IAM Token 自动换发（账号/密码调 `POST /v3/auth/tokens`）** | 低 | 部分（Token 最长 24h） | 需存账号密码，且**开了 MFA 就不可用** | ❌ 否决（安全红线） |
| **D. STS 临时凭证 / 委托** | 低 | ❌ 仍依赖父凭证，且更短命 | — | ❌ 否决 |
| **E. 浏览器自动化定时重登** | 低 | 不稳定 | 需要脚本保管登录凭据、涉及验证码/风控 | ❌ 否决；浏览器只保留人工 `capture` 这一条路 |

关于「CLI 授权」的准确表述：**推荐做法是「AK/SK 授权 + 在 Node 进程内自行签名（`SDK-HMAC-SHA256`）」，而不是让本项目去调 `hcloud`。** KooCLI 用于 Phase 0 手工验证一次即可；验证通过后，项目内用 Node 内置 `crypto` 实现同一签名（约 60~80 行，零依赖，符合本仓库「运行时零第三方依赖」约束）。

### 3.2 可用的 AK/SK 接口面（已查到文档，待实测）

费用中心「客户运营能力（OCE）」公开了三个与资源包相关的接口，**均明确支持 AK/SK 或 Token 调用，权限点 `billing:resourcePackages:view`**：

| 用途 | 接口 | 关键字段 | 备注 |
| --- | --- | --- | --- |
| **资源包列表**（余额 + 到期时间的主体） | `POST https://bss.myhuaweicloud.com/v3/payments/free-resources/query`（ListFreeResourceInfos） | 包级：`product_name`、`service_type_code`、`effective_time`、`expire_time`、`status`（0未生效/1生效中/2已用完/3已失效/4已退订）、`source_type`（1=软开云赠送）<br>资源项级：`free_resources[].amount`（**剩余额度**）、`original_amount`（原始额度）、`free_resource_id` | 支持 `service_type_code_list` 过滤；[文档](https://support.huaweicloud.com/api-oce/api_order_00027.html) |
| 资源包用量（按资源项查余量/周期） | `POST /v2/payments/free-resources/usages/details/query`（ListFreeResourceUsages） | `amount`、`original_amount`、`end_time`、`start_time`、`quota_reuse_cycle` | 入参 `free_resource_ids`（1~100）来自列表接口；[文档](https://support.huaweicloud.com/api-oce/api_order_00028.html) |
| **抵扣明细**（日级「用量」数据源） | `GET /v2/bills/customer-bills/free-resources-usage-records`（ListFreeResourcesUsageRecords） | `deduct_time`、`available_amount`、`remaining_amount`、`used_amount`、`resource_type_code` | `deduct_time_begin/end` 必填且跨度 ≤ 90 天；[文档](https://support.huaweicloud.com/api-oce/api_order_00029.html) |

签名方式为华为云标准 `SDK-HMAC-SHA256`（必选头 `host`、`x-sdk-date`；空 body 也要参与哈希），[签名指南](https://support.huaweicloud.com/devg-apisign/api-sign-algorithm.html)；KooCLI 无配置调用方式见 [KooCLI 选项说明](https://support.huaweicloud.com/intl/zh-cn/usermanual-hcli/hcli_05_06.html)。

与现状的对照（业务口径）：`src/providers/codearts.ts:56` 已在使用资源类型 `hws.resource.type.codearts.snap`、服务类型 `hws.service.type.devcloud`（`codearts.ts:372`），说明该账号在计费系统里的码道资源确实挂在 `devcloud` 服务类型下——这是「费用中心资源包接口可能覆盖码道积分」的间接证据，但**必须实测确认**（见 3.3 与第 8 节）。

### 3.3 Phase 0：实测确认（实施前必做，人工在浏览器完成）

**目标：用最小成本回答「BSS 资源包接口到底能拿到码道的哪些数据」，决定 Phase 1 采用「全量迁移」还是「混合模式」。**

用 API Explorer 在线调试即可：调试面板支持**用当前登录账号自动认证**，因此 Phase 0 连 AK/SK 都不需要创建（不需要写代码，也不要把任何凭证明文贴进对话）：

1. 打开 API Explorer → 费用中心 → `ListFreeResourceInfos`，用 AK/SK 认证调试；
2. 请求体 `{"service_type_code_list":["hws.service.type.devcloud"],"status":1,"limit":100,"offset":0}`；
3. 记录并核对：
   - 是否返回码道（码道/CodeArts/CodeArts Snap）相关资源包；`service_type_code` 实际值；
   - 每个包 `product_name`、`expire_time`、`free_resources[].amount` 与页面「套餐与用量」数字是否对得上；
   - **活动福利**（每日签到等逐笔奖励）是否单列为资源包（`source_type=1` 软开云赠送）——这决定逐笔批次能否保留；
4. 再用 `ListFreeResourcesUsageRecords`（时间范围取最近 2 天）确认抵扣明细能否还原日级用量。

判定规则：

| Phase 0 结果 | Phase 1 形态 |
| --- | --- |
| 码道积分包 + 活动福利都能在资源包列表看到 | **全量迁移**：AK/SK 为主，Cookie 可选 |
| 只有套餐/积分包（聚合），无活动逐笔 | **混合模式**：AK/SK 提供余额与到期；活动逐笔仅在 Cookie 新鲜时补充（现状能力保留） |
| 什么都查不到（积分不计入资源包） | 放弃 A，只做方案 B（超时策略 24h + 失效可操作化文案 + 默认提醒），并在文档中明确记录否决原因 |

#### Phase 0 首轮结果（2026-10-11）

**结论：首次调试返回空列表（`total_count=0`），但该结果目前不能作为否决依据——过滤条件是「devcloud 服务类型 + status=1」，还需做「无过滤对照」与「抵扣明细」两项检查才能定性。**

已在 API Explorer 完成：`ListFreeResourceInfos`，body `{"service_type_code_list":["hws.service.type.devcloud"],"status":1,"limit":100,"offset":0}`，结果 `{"free_resource_packages":[],"total_count":0}`，HTTP 200（认证与权限正常，非 403）。

可能性拆解与对应检查：

| 可能 | 特征 | 检查动作 |
| --- | --- | --- |
| ① 过滤条件不匹配（服务类型编码不是 devcloud） | 无过滤时能列出包，且出现别的 `service_type_code` | 去掉 `service_type_code_list` 与 `status`，只留 `{"limit":100,"offset":0}`，抄下返回的 `service_type_code` 值再按实际值重查 |
| ② 资源包存在但状态非「生效中」 | 无过滤时能列出包但 `status` ≠ 1 | 去掉 `status`（或分别试 0~4） |
| ③ 本账号确实没有购买型积分包（8500 积分来自套餐额度 + 活动福利，两者都不走「资源包」体系） | 无过滤列表里也没有码道相关包；费用中心「资源包」页面筛「码道/CodeArts」为空 | 与下面的抵扣明细检查交叉验证 |
| ④ 码道积分完全不计入资源包体系 | 抵扣明细也无 `codearts` 记录 | 判定方案 A 不可行，转方案 B |

补充检查（各 1 分钟）：

1. **无过滤列表**：`ListFreeResourceInfos` body 改为 `{"limit":100,"offset":0}`，看是否存在任何码道/CodeArts/Snap 相关 `product_name`；
2. **抵扣明细**：`ListFreeResourcesUsageRecords`，`deduct_time_begin`=昨天、`deduct_time_end`=今天（可选再加 `resource_type_code=hws.resource.type.codearts.snap`），看最近是否有抵扣记录及实际 `resource_type_code`；
3. **页面目视**：华为云控制台 → 费用中心 → 资源包 → 资源包列表，按名称筛选「码道 / CodeArts」。

> 注意：`hws.resource.type.codearts.snap` 是控制台 countdown 接口使用的资源类型（`src/providers/codearts.ts:56`、`372`），可作为抵扣明细的过滤候选；`hws.service.type.devcloud` 则是 countdown 请求体里的**服务类型**（`codearts.ts:372`）——两者若在 BSS 中对应不到，说明计费系统里该产品用了别的编码。

#### Phase 0 第二轮结论（2026-10-11：浏览器实测 + CLI 静态取证）

**结论：费用中心（BSS）路径被实测否决；同时确认码道官方 CLI 存在 AK/SK 直连链路，主路径改为「snap-access 网关 + AK/SK」，字段覆盖面待探测脚本确认。**

已完成的实测与取证：

| # | 事实 | 证据 |
| --- | --- | --- |
| 1 | `ListFreeResourceInfos` **去掉全部过滤条件**仍返回 `{"free_resource_packages":[],"total_count":0}` | API Explorer 在线调试（2026-10-11）；说明该账号在费用中心**没有任何资源包**，码道积分不在资源包体系内 |
| 2 | 费用中心页面路由 `/billing/` 一律重定向到控制台首页，账号为 agency（委托）上下文 | 浏览器实测（带 `agencyId` 参数仍重定向） |
| 3 | 码道 **官方 CLI 基于华为云 AK/SK 鉴权**（`CODEARTS_CLI_AK` / `CODEARTS_CLI_SK`），授权页明确给出环境变量用法 | `https://codearts.huaweicloud.com/portal/settings/cli-auth`；官方文档 `usermanual-cli/codeartsagent_cli_0026.html` |
| 4 | CLI 二进制（`agentkernel` 26.9.12）内置华为 SDK 签名器：`SDK-HMAC-SHA256`、`X-Sdk-Date`、`X-Security-Token`、IAM/STS 客户端（`OS-CREDENTIAL/securitytokens`） | CLI 安装包静态取证（只读，未执行） |
| 5 | CLI 的 AK/SK 业务网关为 `https://snap-access.cn-north-4.myhuaweicloud.com/snap-manager` | 同上：`TW0="https://snap-access.cn-north-4.myhuaweicloud.com/snap-manager"` |
| 6 | CLI 的 `fetchSubscriptionInfo()` 用 AK/SK 签名调用 `GET /snap-manager/v1/statistics/plugin`；失败时静默跳过 | 同上代码片段 |
| 7 | 该接口返回 `package.status` / `package.package_name_en`、`metrics[]`（`usageTokenChatMessages` → `usage_token_num` / `package_token_amount`）、`model_metrics[]`、`is_teadmin`/`show`/`is_white_list` | 同上：`zo1()` / `Fo1()` 解析逻辑 |
| 8 | CLI 另有 `/snap-manager/v1/privacy`（AK/SK 签名）、`/snap-manager/v1/current/user`（需 `X-Security-Token`） | 同上 |

据此已落地的探测工具（本次新增，未接入适配器）：

| 文件/命令 | 作用 |
| --- | --- |
| `src/lib/huaweicloud.ts` | 与 CLI 内置签名器逐行对齐的零依赖 `SDK-HMAC-SHA256` 实现 |
| `src/cli/codearts-aksk-probe.ts` | 只读探测 4 个候选接口，终端输出脱敏；`npm run codearts:aksk-probe` |
| `package.json` | 新增脚本 `codearts:aksk-probe` |

新判定规则（探测脚本跑完后据此选择形态）：

| 探测结果 | Phase 1 形态 |
| --- | --- |
| `statistics/plugin` 或其他候选接口返回**积分余额/批次到期** | 全量迁移：AK/SK 取代 Cookie，彻底解决登录态失效 |
| 只返回套餐状态 / token 用量（无积分） | 混合模式：AK/SK 提供套餐状态与到期兜底 + 失效预警，Cookie 仍负责积分逐笔 |
| 两种签名变体均 401/403 | 补做「AK/SK → IAM/STS 临时凭证（`X-Security-Token`）」链路后再探测 |
| 全部 404 / 不可达 | 放弃 AK/SK 主路径，仅保留方案 B（会话超时 24h + 失效文案） |

#### Phase 0 第三轮：AK/SK 首测结果（2026-10-11）

**结论：`statistics/plugin` 路由存在但不是纯 AK/SK 可调（401）；另两个候选接口在该网关未发布（404）。已按 CLI 二进制里的第二种签名器补齐签名变体，待复测。**

| 探测接口 | 结果 | 解读 |
| --- | --- | --- |
| `/snap-manager/v1/statistics/plugin` | **401**（两种签名变体） | 路由存在（若不存在会像下面两个一样 404）；鉴权被拒，说明签名风格不对或还需临时凭证 |
| `/snap-manager/v1/package/overview` | **404** `APIG.0101`「API does not exist or has not been published」 | 该路径未在 snap-access 网关发布，控制台同名接口只在 portal 侧 |
| `/promptcenter/v1/ops/credit/overview` | **404** `APIG.0101` | 积分接口不在该网关；验证了「AK/SK 直接换网关查积分」的假设不成立 |
| `/snap-manager/v1/current/user` | **401** | 与 CLI 行为一致：CLI 此接口带 `X-Security-Token`（STS 临时凭证） |

本轮新增取证与本轮改动：

1. CLI 二进制里存在**两种签名器**，本仓库先前只实现了其中一种：
   - `legacy`（带日期）：`StringToSign = "SDK-HMAC-SHA256 <X-Sdk-Date> <sha256(CanonicalRequest)>"`；
   - `legacy-no-date`：`StringToSign = "SDK-HMAC-SHA256 <sha256(CanonicalRequest)>"`（老 SDK 的 `getSignature()` 只拼两段）。
   → `src/lib/huaweicloud.ts` 已支持 `signatureStyle: 'legacy' | 'legacy-no-date'`。
2. CLI 的凭证获取链（静态取证）：
   - **OAuth 模式**：浏览器授权 → 返回临时凭证 `{access_key_id, secret_access_key, security_token}` → 存本地 Auth store → 之后请求用临时 AK/SK 签名并带 `X-Security-Token`；
   - **环境变量模式**（`CODEARTS_CLI_AK/SK`）：直接写入 `accessKeyId/secretAccessKey`，**没有 security_token**。
3. 探测脚本升级：`npm run codearts:aksk-probe` 现在会依次尝试 4 种组合（带/不带日期 × UNSIGNED-PAYLOAD / 空 body 哈希），且**失败时打印响应体**（APIG 的 `error_code`/`error_msg` 会直接说明原因）。

> 下一步动作：用同一 AK/SK 重跑探测脚本。若「不带日期」组合返回 200 → 直接接适配器；若仍 401 且响应体指出 token 问题 → 实施「IAM 换取临时凭证（STS）+ X-Security-Token」链路（CLI 的 OAuth 分支已给出字段形态：`access_key_id` / `secret_access_key` / `security_token`）。

#### Phase 0 第四轮：APIG 签名校验失败（2026-10-11）

**结论：`APIG.0301 verify ak sk signature fail` —— AK 已被网关识别，属于签名规范不匹配（或 SK 与 AK 不配对），不是权限问题；已补齐第三种「换行式」签名规范，待用新密钥复测。**

- 实测（4 种组合全试）：`/snap-manager/v1/statistics/plugin` 与 `/snap-manager/v1/current/user` 均返回 **401 `APIG.0301`**；`package/overview`、`promptcenter/...credit/overview` 稳定 404 `APIG.0101`（路由已排除）。
- 华为签名规范在 SDK 历史上存在三代，本仓库已在 `src/lib/huaweicloud.ts` 全部实现（`signatureStyle`）：

| 风格 | 分隔符 | StringToSign |
| --- | --- | --- |
| `legacy`（新 SDK） | 空格 | `SDK-HMAC-SHA256 <date> <sha256(CR)>` |
| `legacy-no-date`（老 SDK `getSignature()`） | 空格 | `SDK-HMAC-SHA256 <sha256(CR)>` |
| `newline`（APIG 文档格式） | `\n` | `SDK-HMAC-SHA256\n<date>\n<sha256(CR)>` |

- 探测脚本现覆盖 **6 种组合**（3 风格 × UNSIGNED-PAYLOAD/空 body 哈希），失败同样打印响应体。
- 另一个待排除变量：**AK 与 SK 不配对**（SK 抄错、或取自另一把 AK）——130 类错误的典型表现也是「signature fail」。

> **安全事件记录（2026-10-11）**：用户曾将一把 AK/SK 明文粘贴进对话。处置：立即在 IAM 禁用并删除该密钥、新建密钥；后续凭证明文只允许出现在本机 `.env`。本文档与代码库一律不复述其值（符合 AGENTS.md 第 7 节）。

下一步动作：

1. 用**新密钥**重跑 `npm run codearts:aksk-probe`（同时排除「配对错误」）；
2. 若「换行式」组合返回 200 → 进入 3.4 的适配器接入；
3. 若仍 0301 → 装官方 CLI 做对照（`codearts models` 验证密钥本身可用性），并检查 `~/.codeartsdoer/cli-data` 下的日志核对 canonical 细节（必要时再补 V11 派生签名）。

#### Phase 0 第五轮：AK/SK 直连打通（2026-10-11，关键突破）

**结论：`换行式（\n 分隔）+ 带日期 + 空 body 哈希` 组合返回 HTTP 200——AK/SK 已可稳定取到码道积分，无需任何登录态 Cookie；且返回字段与看板口径几乎一一对应。**

实测结果（`GET https://snap-access.cn-north-4.myhuaweicloud.com/snap-manager/v1/statistics/plugin`）：

| 字段 | 值（本次实测） | 对应看板元素 |
| --- | --- | --- |
| `metrics[usageTotalPackageCredit]` | `package_credit_amount=20500`、`used=0`、`remain=20500`、`expiring=0` | 卡片余额 + 额度窗口（used/limit/remaining） |
| `metrics[usageBasicPackageCredit]` | 500 / 0 / 500 | 批次「个人订阅套餐」 |
| `metrics[usageBonusPackageCredit]` | 20000 / 0 / 20000 | 批次「活动福利积分」 |
| `metrics[usageOnDemandPackageCredit]` | 0 / 0 / 0 | 批次「按需积分包」（>0 时才展示） |
| `metrics[usageTokenChatMessages]` / `usageDataChatMessages` | token 用量口径 | 暂不接入（与积分不是同一计量） |
| 顶层 `package` / `show` / `is_teadmin` / `is_white_list` / `start_date` / `end_date` / `model_metrics` | 待完整 JSON 落盘后核对（`package` 可能含套餐名与到期） | 套餐名 / 到期时间兜底 |

`GET /snap-manager/v1/current/user` 同样 200（返回 `domain_id`/`user_id`，可作身份标识与健康检查）。

由此可以定案的部分：

1. **签名规范**：`newline`（CanonicalRequest/StringToSign 均以 `\n` 分隔、StringToSign 带 `X-Sdk-Date`、payload 用空 body 哈希）——其余五种组合在 APIG 侧全部 401 `APIG.0301`；
2. **数据源**：`statistics/plugin` 一个接口即可覆盖余额 + 套餐/福利/按需三类批次；`404` 的 `package/overview`、`promptcenter/...credit/overview` 不再需要；
3. **凭证**：纯 AK/SK（无需 STS 临时凭证、无需 `X-Security-Token`）。

> 落盘约定：探测脚本命中后把完整响应写到 `.run/codearts-statistics-plugin.json`（`.run/` 已 gitignore），用于核对 `package`、`model_metrics`、计费周期等剩余字段。

### 3.4 Phase 1：代码改造设计（依据 Phase 0 结果裁剪）

> 注：本节早先基于费用中心 BSS 接口的设计已被实测否决；当前方案以 §3.3「Phase 0 第五轮」验证通过的 snap-access `statistics/plugin` + `newline` 签名为准（authMode 解析、Cookie 兜底、契约零变更三点不变）。

**总体思路：给 codearts 适配器加一条「AK/SK 优先、Cookie 兜底」的凭证路径，契约层（`ProviderSnapshot` / `CreditLot` / `QuotaWindow`）零变更，前端与聚合层零改动。**

新增文件：

| 文件 | 职责 |
| --- | --- |
| `src/lib/huaweicloud.ts` | 零依赖 `SDK-HMAC-SHA256` 签名（Node 内置 `crypto`）+ `bssRequest()` 封装：注入 `X-Sdk-Date`、`Authorization`、`host`；统一错误映射到既有 `HttpError` |

改造 `src/providers/codearts.ts`：

```
authMode 解析（新增 options.authMode: 'auto' | 'aksk' | 'cookie'，默认 auto）
├─ aksk（credentials.accessKey + secretKey 均存在）
│   └─ GET snap-access…/snap-manager/v1/statistics/plugin（newline 签名，空 body 哈希）
│        ① metrics[usageTotalPackageCredit] → balance + QuotaWindow{ used, limit, remaining }
│        ② usageBasicPackageCredit / usageBonusPackageCredit / usageOnDemandPackageCredit
│             → CreditLot[]（id: codearts-basic / codearts-bonus / codearts-ondemand）
│        ③ package / end_date → 套餐名与到期时间（字段以 .run 落盘结果为准）
│        ④ 失败（401/网络）且存在 cookie → 回退 cookie 路径并在 note 标注
└─ cookie（现状五接口不变，作为兜底与逐笔来源；自动模式下与 aksk 组成混合增强，见 3.4.2）
```

> 计费周期字段 `start_date` / `end_date` 与 `package.*` 的语义待 `.run/codearts-statistics-plugin.json` 核对后回填；
> `model_metrics` 属 token 口径，本期不接入（避免与积分混淆）。

凭证与配置变更：

| 位置 | 变更 |
| --- | --- |
| `.env` | 新增 `CODEARTS_AK` / `CODEARTS_SK`（沿用手工填写 + `accounts.json` 用 `${VAR}` 引用，`src/config.ts:94-104`） |
| `accounts.json` | codearts 条目新增 `credentials.accessKey/secretKey`；可选 `options.authMode` |
| `src/cli/capture.ts` | 不变（Cookie 路径保留）；仅在 `credentialHint` 文案里补「推荐改用 AK/SK，见凭证配置指南」 |
| `docs/凭证配置指南.md` | 新增「华为云 AK/SK 配置」小节（含 IAM 最小权限步骤、AK/SK 创建入口即用户给的 `#/mine/accessKey` 页） |
| `README.md:265` 凭证表格 | CodeArts 行补充两种凭证形态 |

失败语义（保持既有前端可读性）：

| 场景 | status | message 方向 |
| --- | --- | --- |
| AK/SK 403（`CBC.0151`） | `forbidden` | 提示检查 IAM 权限 `billing:resourcePackages:view` 与 AK/SK 所属账号 |
| AK/SK 400（`CBC.0100`） | `error` | 参数/服务类型编码不匹配，附上原始 error_msg |
| AK/SK 正常但无 devcloud 资源包 | `ok` + Cookie 兜底 | note：「费用中心未返回码道资源包，已回退登录态口径」 |
| Cookie 失效（现状） | `unauthorized` | 文案可操作化：追加「请执行 npm run capture -- codearts」 |

### 3.5 方案 B：与 A 并行的即时缓解（不依赖 Phase 0 结果）

1. 在华为云 IAM 控制台把「登录验证策略 → 会话超时」调到 **24 小时**（上限），把失效频率从「1 小时级」降到「1 天级」；
2. 失效文案可操作化（见 3.4 失败语义表最后一行），让看板直接告诉人「怎么修」；
3. 保留 `npm run capture -- codearts` 单平台抓取（当前已支持只抓指定 target，`src/cli/capture.ts:594-595`），缩短修复动作。

### 3.6 安全约束（实现时必须遵守）

1. 推荐**独立 IAM 用户 + 自定义身份策略**（只含 `billing:resourcePackages:view`）：IAM 控制台 → 用户 → 创建用户 → 勾选「编程访问」（AK/SK 仅创建时展示一次，可下载 csv），权限选自定义身份策略，JSON 参考 `{"Version":"5.0","Statement":[{"Effect":"Allow","Action":["billing:resourcePackages:view"]}]}`（最终以控制台校验通过为准）。不建议使用账号级（根）AK/SK——账号级 AK/SK 入口即用户提供的 `console.huaweicloud.com/iam/#/mine/accessKey`，权限覆盖全部云服务，仅适合临时验证；
2. 若当前控制台是**委托/agency 身份**进入（用户提供的链接带 `agencyId` 参数），AK/SK 必须创建在「积分实际归属的账号」下，否则会 403 或查不到数据——此点列为待确认；
3. AK/SK 只写 `.env`（已 gitignore），`accounts.json` 用 `${CODEARTS_AK}` 引用；任何日志、报告、`/api/reveal`、快照都不得出现 SK；
4. SK 只在创建时展示一次，创建后立即填入 `.env`，不要把明文贴进对话/文档/issue。

### 3.5 降低登录态维护成本：一键刷新 + 可定时（2026-10-12）

**结论：余额已彻底免登录态（AK/SK）；逐笔受华为云会话上限（≤24 小时）约束，无法用程序绕过，因此把人工动作压缩为「一条命令」并支持无人值守定时。**

| 改动 | 内容 |
| --- | --- |
| capture 去全局依赖 | `src/cli/capture.ts` 解析 playwright CLI：优先全局 `playwright-cli`，缺失时回退 `npx --no-install playwright cli`（实测本机没有全局命令，`npm run capture` 原先实际跑不通） |
| 一键刷新 | `tools/codearts-refresh.sh` + `npm run codearts:refresh`：确保调试 Chrome → 抓取 codearts → 校验登录态（退出码 0 有效 / 3 需人工登录 / 2 Chrome 不可用 / 1 抓取失败），并打印需要登录的 URL |
| 可编程校验 | `codearts:console-probe` 增加结论行与退出码（空响应 → 3），可被定时任务/监控判定 |
| 无人值守（macOS launchd） | `tools/install-codearts-refresh-launchd.sh` + `npm run codearts:schedule`：安装 `~/Library/LaunchAgents/com.powermeter.codearts-refresh.plist`，每天 **09:30 / 21:30** 执行刷新；`--status` / `--run` / `--uninstall` 可选；登录态过期时退出码 3 并**弹系统通知**；日志 `.run/codearts-refresh.log`；睡眠错过后补跑 |
| 无人值守（其它环境） | cron 一行（凭证指南已给），逻辑相同 |
| 脚本加固（2026-10-12 反馈后） | `tools/codearts-refresh.sh`：CDP 检测改为 `--noproxy '*'` + 校验 `webSocketDebuggerUrl`（避免代理/瞬时端口误判）；**始终打印判定过程**；自动拉起后最多等 40s 并回显 Chrome 日志；抓取失败自动重试一次；退出码 3 时**自动打开登录页**并弹通知 |
| 踩坑与追加加固 | `set -u` 下 `$CHROME_LOG` 紧跟全角字符会报 `unbound variable`（多字节字符被并入变量名）→ 全仓库 `tools/*.sh` 变量统一 `${VAR}` 形式，并用扫描脚本确认 0 处同类写法；调试 Chrome 改 `nohup` 拉起，避免父进程退出时被 SIGHUP 带走；用「桩替换 npx」做端到端冒烟测试（零上游请求）通过 |
| 「每次都要重新登录」根因（2026-10-12 二轮反馈） | 读取 `~/.power-meter/chrome-profile/Default/Cookies`（只读、不打印值）：**`devclouddevuibjJ_SESSION_ID` 持久=0（会话级、不落盘）**，而 `auth.huaweicloud.com` 的 `HW_id`/`HW_refts` 刷新令牌有效期到 **2027 年** → 账号登录态其实在，真正的根因是**调试 Chrome 每次都被关闭/随终端退出**，会话级 Cookie 一丢就等于未登录 |
| 最终修复（用户否决「Chrome 常驻」后） | ① **`.env` 即会话 Cookie 的持久副本** → 关闭 Chrome 不影响看板，浏览器只在「需要重新取 Cookie」时才用；② `codearts:refresh` 改为「先校验 `.env` → 有效则零打扰；失效但浏览器有会话则抓取；都没有则先等 SSO 静默重登 → 仍失败才弹通知 + 打开登录页」；③ launchd 只保留刷新任务（`com.powermeter.chrome-debug` 已卸载，安装脚本会自动清理历史任务）；④ 四分支桩测（有效 / 有会话 / SSO 成功 / 全失败）退出码 0/0/0/3 符合预期 |

必须写清的现实边界（避免反复投入）：

1. **逐笔数据源只有登录态**：AK/SK 网关无活动接口（3 条候选全 404，见 3.4.5）；
2. **会话最长 24 小时**：到期必须人工登录一次，程序无法绕过（华为云登录含风控/验证码）；
3. **当前账号逐笔的信息增量有限**：活动福利（21,000）到期日与套餐同为 2026-10-26，逐笔与否对「最快到期」结论影响不大——是否维持定时任务由用户自行取舍。

## 4. 影响面评估

| 受影响面 | 判断 | 依据 |
| --- | --- | --- |
| `ProviderSnapshot` / 聚合 / 前端 | **无变更** | 仍通过 `buildSnapshot` 产出批次与窗口（`src/providers/base.ts:198-251`） |
| 「最快到期」排序 | 可能变粗 | AK/SK 若只能到「包级」，批次粒度从「活动逐笔」变为「资源包」，Phase 0 决定是否混合保留逐笔 |
| 日级用量 / 增量 | 「用量」可换 `ListFreeResourcesUsageRecords`；「增量」AK/SK 无来源 | `covers` 机制已支持单维度降级（`src/providers/base.ts:73`、`src/providers/codearts.ts:322`） |
| 缓存策略 | 无变更 | 凭证/接口变化对 `service.ts` 透明 |
| 其它适配器 | 无影响 | 新增签名工具类为独立文件，不改 `base.ts` / `lib/http.ts` 现有行为 |
| 文档 | `docs/凭证配置指南.md`、`README.md` 必改 | 见 3.1 记忆约定：配置类改动必须同步环境变量文档 |

## 5. 实施步骤

| 阶段 | 步骤 | 产出物 |
| --- | --- | --- |
| Phase 0 | ① API Explorer 调 3 个 BSS 接口，按 3.3 表格记录结果 | 实测结论（回填本文档第 8 节） |
| Phase 0 | ② 根据判定规则确定 Phase 1 形态（全量迁移 / 混合 / 放弃） | 本文档「当前状态」更新为「已确认」 |
| Phase 1 | ③ 新建 `src/lib/huaweicloud.ts`（签名 + 请求封装） | 单个新文件 + 类型检查通过 |
| Phase 1 | ④ 改造 `codearts.ts`：authMode 解析、BSS 取数、失败回退、note 文案 | `codearts.ts` |
| Phase 1 | ⑤ 凭证链路：`.env.example`、`accounts.example.json`、`README.md`、`docs/凭证配置指南.md` | 配置文件与文档 |
| Phase 2（可选） | ⑥ 若 Phase 0 发现活动逐笔缺失显著影响体验 → 设计「双源对账」：BSS 为主、Cookie 逐笔为辅，差额补调节项（复用现有对账逻辑 `codearts.ts:444-471`） | 适配器增强 |

### 3.4.1 Phase 1 实施记录（2026-10-11，代码已就绪）

| 改动 | 内容 |
| --- | --- |
| `src/lib/huaweicloud.ts` | 零依赖 `SDK-HMAC-SHA256` 签名（3 种规范），实测采用 `newline + 空 body 哈希` |
| `src/providers/codearts-aksk.ts`（新增） | `readCodeArtsAkSk()`（credentials → options → `CODEARTS_AK`/`CODEARTS_CLI_AK` 兜底）、`parseStatisticsPlugin()`、`probeCodeArtsViaAkSk()` |
| `src/providers/codearts.ts` | `probe()` 增加 `authMode` 分支：`auto`（默认，AK/SK 优先、失败回退 Cookie）/ `aksk`（不回退）/ `cookie`（旧行为）；`credentialHint` 与 notConfigured 文案更新 |
| 配置与文档 | `.env.example`（`CODEARTS_AK`/`CODEARTS_SK`）、`accounts.example.json`（accessKey/secretKey 引用 + 注释）、`README.md` 凭证表、`docs/凭证配置指南.md` 新增「CodeArts 改用 AK/SK（推荐）」小节 |

字段映射（AK/SK 路径）：`usageTotalPackageCredit` → 余额 + 窗口；`usageBasicPackageCredit`/`usageBonusPackageCredit`/`usageOnDemandPackageCredit` → 三个批次（id 复用 `codearts-basic`/`codearts-bonus`/`codearts-ondemand`）；`start_date ~ end_date` → 批次与窗口到期时间（计费周期）；`package.package_name_cn/status` → note 与告警。

**解析验证（本地，无上游请求）**：用 `.run/codearts-statistics-plugin.json` 回放，得到
`total 20500 / basic 500 / bonus 20000 / ondemand 0 / 体验版 / normal / 2026-09-25 ~ 2026-10-26`，与探测输出一致。

**契约影响**：`CreditLot` / `QuotaWindow` / `ProviderSnapshot` 零变更；前端与聚合层无改动；Cookie 路径原样保留，可作为回退与在线回归对照。

### 3.4.2 Phase 2 混合增强：AK/SK 余额 + 登录态逐笔（2026-10-11，已实施）

**结论：`auto` 模式下，余额与额度窗口始终取 AK/SK 的账户级口径；只要登录态可用，活动福利批次就改用控制台逐笔明细（每笔独立到期），逐笔之和与账户级 bonus 的差额自动补「未明细部分」调节项；登录态失效时只降级这一部分，余额不受影响。**

实现要点：

| 位置 | 改动 |
| --- | --- |
| `src/providers/codearts-aksk.ts` | 拆分为 `fetchCodeArtsSubscription()`（拉取订阅）与 `buildCodeArtsSnapshot(…, detail?)`（组装）；`detail` 非空时用逐笔替换 bonus 聚合桶，并按 `bonusRemain − benefitTotal ≥ 1` 补 `codearts-bonus-gap` |
| `src/providers/codearts.ts` | 抽出 `consoleHeaders()`（Cookie + cftk）与 `fetchCampaignBenefits()`（④⑤ 逐笔），Cookie 路径与混合路径共用；`auto` 分支顺序：AK/SK → 逐笔 → 组装，逐笔失败只降级活动批次 |
| 降级语义 | 无 Cookie / 明细为空 / 明细报错 → 活动福利退回聚合口径，并在 note 写明原因（如「登录态可能已失效」） |

本地回放验证（纯函数，无上游请求；用 `.run` 快照 + 模拟两笔逐笔）：

| 场景 | 结果 |
| --- | --- |
| 逐笔 19000 + 账户级 20000 | basic 500（10-26）+ 逐笔 9000（10-20）/10000（10-25）+ 缺口 1000（10-26）；最快到期 **10-20**（逐笔更细） |
| 逐笔刚好 20000 | 不产生缺口调节项 |
| 无逐笔（登录态失效） | basic + 活动福利 20000（聚合）；余额仍为 20500，note 标注降级原因 |

### 3.4.3 缺陷修复：AK/SK 路径漏做「过期批次过滤」（2026-10-12）

**问题现象（用户反馈）**：活动福利的逐笔到期显示异常——批次以「已过期 X 天」的形式直接列在卡片上。

**根因**：`buildCodeArtsSnapshot()` 首版把逐笔批次原样塞进 `credits`，**漏掉了 Cookie 路径既有的 `splitByExpiry()` + `hiddenLotsNote()`**（`codearts.ts` 的旧路径会把过期批次移出列表并只留一句汇总）。控制台逐笔里可能出现「到期日已过、但上游尚未结清」的记录，直接列出会被误读成可用额度。

**修复**：`src/providers/codearts-aksk.ts` 在组装前统一执行 `splitByExpiry(lots, ctx.now)`；`credits` 只传 `active`，`note` 追加 `hiddenLotsNote(expired)`；逐笔口径文案改为「取到 N 笔」。

**本地回放验证**（纯函数）：输入 1 笔已过期（2026-10-05）+ 1 笔未到期（2026-11-05）——
展示 `套餐 500（10-26）` 与 `推荐码上有礼 19000（11-05）`，note 追加「另有 1 个批次已过期（共 1000 积分作废），未在列表中展示」，最快到期回落为 2026-10-26。

**同时新增的探测项**（用于回答「逐笔能否不依赖登录态」）：

| 候选 | 目的 |
| --- | --- |
| `GET https://codearts.huaweicloud.com/portal/promptcenter/v1/ops/credit/overview`（AK/SK 直签） | 控制台原路径是否也接受 AK/SK 签名 |
| `GET snap-access…/snap-manager/v1/ops/credit/overview` | CLI 存在 `/v1/ops/credit/usage`，探测同族 overview |

> 若任一命中 200，则可把活动逐笔也迁到 AK/SK 口径，彻底摆脱登录态；否则维持混合模式（Cookie 失效时活动福利降级为聚合口径）。

### 3.4.4 症状分析：昨日增量为 0 + 活动逐笔消失（2026-10-12）

**结论：两个症状同一个共因——控制台活动列表返回 0 个活动（登录态本身可用）。逐笔因此没有来源而退回聚合；而「获取明细」维度仍被错误地声明为已覆盖，把本地采样已经看到的 +1000 压成了 0。**

证据链（全部来自本地，未发上游请求）：

| 证据 | 内容 |
| --- | --- |
| `.run/server.log` | `[codearts] CodeArts 活动逐笔：0 笔，合计 0 积分` |
| `.run/usage-history.json` 采样 | 10-10 16:52 → **20500**；10-11 17:11 → **21500**（+1000 发生在昨日）；10-12 08:35 → 21500 |
| 看板截图（08:59） | 余额 21,500（AK/SK 口径正确）；活动福利 21,000 为聚合批次；note「登录态可用但活动明细为空」 |

根因拆解：

1. **逐笔缺失**：`/portal/promptcenter/v1/ops/credit/overview` 返回 0 个活动 → `fetchCampaignBenefits` 无逐笔可产出 → 按设计退回聚合口径（余额不受影响，这部分行为正确）；
2. **增量被压成 0**：`fetchUsageLedger` 在「活动列表为空」时仍置 `grantCovered = true`，等于向聚合层声明「该来源完整、该日无获取 = 0」，于是本地采样记录的 +1000 被覆盖掉（见 `src/service.ts:282-290` 的 per-dimension 取值逻辑）。

修复：

| 位置 | 改动 |
| --- | --- |
| `codearts.ts` `fetchUsageLedger` | 活动列表为 0 时**不再声明 grant 覆盖**（降级本地采样），并写明日志；有活动时维持原语义 |
| `codearts.ts` `fetchCampaignBenefits` | 返回值新增 `campaignCount`；日志改为「活动 N 个，逐笔 M 笔」 |
| `codearts.ts` 混合分支 | 降级原因精确化：`登录态可用，但活动列表为空（当前没有进行中的活动）` / `活动列表有 N 个但无剩余逐笔` |
| 新增 `src/cli/codearts-console-probe.ts` + `npm run codearts:console-probe` | 只读诊断：用 .env 的登录态直接打印余额与活动列表的**原始响应**（脱敏），用于区分「确实没有活动」/「接口口径变了」/「登录态半失效」 |

预期效果：重启后「昨日增量」应显示 **1,000 CREDITS**（本地采样兜底），今日增量 0；活动福利仍是聚合批次（逐笔是否有来源待诊断结论）。

> 待确认（诊断命令输出后可定）：活动列表为空的真实原因。若为「接口口径变化」或「新积分体系换接口」，需按新的原始响应调整 `parseCampaignList`，或把候选接口补进 `npm run codearts:aksk-probe` 的 AK/SK 直签清单。

**2026-10-12 补充：CLI 常量反推（静态取证）**

CLI 二进制里 `bQ0`/`fQ0` 的实际定义是 `["https://snap-access.cn-north-4.myhuaweicloud.com", "https://snap-access.cn-north-4.myhuaweicloud.com"]`，而会话积分接口拼装为 **`${snap-access}/v1/ops/credit/usage`（无 `snap-manager` 前缀）**——说明该网关上确实挂着一族 `/v1/ops/credit/*` 接口。据此把 `npm run codearts:aksk-probe` 的候选修正为：

| 候选 | 依据 |
| --- | --- |
| `GET https://snap-access.cn-north-4.myhuaweicloud.com/v1/ops/credit/overview` | CLI 常量反推的同族路径 |
| `GET https://snap-access.cn-north-4.myhuaweicloud.com/promptcenter/v1/ops/credit/overview` | 控制台服务名 + 网关根路径组合 |
| `GET https://codearts.huaweicloud.com/portal/promptcenter/v1/ops/credit/overview`（AK/SK 直签） | 控制台原路径是否也接受 AK/SK 签名 |

若任一返回 200，即可把「活动逐笔」迁到免登录态口径（届时 `statistics/plugin` + 活动接口都走 AK/SK，混合模式可退化为纯 AK/SK）。

### 3.4.5 根因确认：会话失效被误判为「没有进行中的活动」（2026-10-12）

**结论：逐笔缺来源的真实原因是控制台登录态早已失效——华为云会话超时上限 24 小时，而 `.env` 里的 Cookie 是数天前抓的；控制台对失效会话返回「HTTP 200 + 空响应」而不是 401，适配器把空响应当成「0 个活动」，于是对外宣称「当前没有进行中的活动」（误导）。**

探测证据（`npm run codearts:console-probe` + `npm run codearts:aksk-probe`）：

| 请求 | 结果 | 解读 |
| --- | --- | --- |
| 控制台 `/portal/snap-manager/v1/package/overview`（Cookie 长度 1093） | 200 + **空响应（长度 0）** | 登录态失效；会话有效时该接口返回完整 JSON |
| 控制台 `/portal/promptcenter/v1/ops/credit/overview` | 200 + **空响应（长度 0）** | 同上，因此活动列表必然是「0 个」 |
| AK/SK `snap-manager/v1/statistics/plugin` | 200 | 余额 **21500** = 套餐 500 + 活动 21000，AK/SK 路径健康 |
| AK/SK `/v1/ops/credit/overview` | 404 `APIG.0101` | 网关未发布该路由 |
| AK/SK `/promptcenter/v1/ops/credit/overview` | 404 `APIG.0101` | 同上 |
| AK/SK `/snap-manager/v1/package/overview` | 404 `APIG.0101` | 同上 |

由此定案三条：

1. **逐笔只能来自登录态**：AK/SK 网关上不存在活动/积分明细接口（三条候选全部 404），混合模式是唯一可行形态，「彻底不依赖登录态」的设想在此网关不成立；
2. **登录态失效不能靠状态码发现**：必须判「响应不是 JSON 对象 / 长度为 0」，不能把它当作「0 条记录」；
3. 余额与增量不受影响（余额走 AK/SK；用量/增量在覆盖标记修正后走本地采样）。

修复：`fetchCampaignBenefits` 先判空响应再解析，空响应直接抛错（文案含操作指引）；`fetchUsageLedger` 的按天用量部分同样记录空响应日志。修复后 note 会明确显示「控制台返回空响应（登录态已失效），请执行 npm run capture -- codearts 重新抓取」，不再误报「没有进行中的活动」；`codearts:console-probe` 也会直接标注空响应与处置办法。

**恢复逐笔的操作（一次性）**：`npm run chrome:debug` → 在弹出的 Chrome 里登录 codearts.huaweicloud.com → `npm run capture -- codearts`。`.env` 热加载后刷新看板即恢复逐笔；期间余额始终来自 AK/SK，不会中断。

## 6. 验证方式（人工，AI 不代跑）

1. **静态**：`npm run typecheck`（实施后由 AI 执行，属允许范围）；
2. **取数**：`npm run probe -- codearts`，预期输出正常余额 + 至少一个带 `expiresAt` 的批次；
3. **对照**：与华为云控制台「我的码道 → 套餐与用量」页面的余额/到期逐项核对；
4. **失效路径**：临时把 `CODEARTS_AK` 改成非法值，确认返回 `forbidden`/`error` 且文案指向 IAM 权限（而不是静默失败）；
5. **回归**：其余 5 个平台 `npm run probe` 全绿（确保未动公共层）。

> 注：验证涉及真实凭证与上游请求，**不要**把 AK/SK 或 Cookie 明文贴进任何对话与文档。

## 7. 风险与回滚

| 风险 | 等级 | 应对 |
| --- | --- | --- |
| 费用中心接口不覆盖码道积分（Phase 0 否决） | 中 | 退回方案 B；本文档记录否决原因，避免二次投入 |
| 批次粒度变粗，到期时间线不如页面精细 | 中 | 混合模式保留 Cookie 逐笔；或接受包级并在卡片 note 标注口径 |
| 签名实现细节出错（时间偏差、空 body 哈希、canonical query 排序） | 中 | 单测覆盖；probe 手工对照；签名失败不影响 Cookie 路径（auto 模式自动回退） |
| IAM 权限点/接口被华为云调整 | 低 | 只影响 `src/lib/huaweicloud.ts` + `codearts.ts` 两个文件，错误以明确 message 暴露 |
| 凭证安全问题（SK 泄露） | 高（性质） | 最小权限 IAM 用户、`.env` 只读权限、日志脱敏、文档不出现明文 |

**回滚**：`options.authMode: 'cookie'` 一键切回现状；或移除 `accessKey/secretKey` 后 auto 模式自然回落 Cookie 路径（无数据迁移、无配置结构破坏）。

## 8. 未决问题（待确认）

1. ~~费用中心资源包接口是否覆盖码道积分包~~——**已否决**（无过滤全量查询为空，见 3.3 第二轮）；
2. ~~AK/SK 能否取到积分~~——**已解决**：`newline` 签名 + `statistics/plugin` 返回 200，余额/三类批次字段齐备（见 3.3 第五轮）；
3. **完整字段语义待核对**：`package` 对象内容、`start_date`/`end_date` 是否为计费周期、`package_credit_expiring_amount` 与 `usageTokenChatMessages.package_token_amount` 的口径——待 `.run/codearts-statistics-plugin.json` 落盘后回填；
4. ~~活动福利逐笔到期能否经 AK/SK 获取~~——**已用混合增强解决**（活动逐笔仍来自登录态，AK/SK 负责余额；见 3.4.2），但若登录态长期失效，活动福利仍会退回聚合口径（待确认是否需要为此再找 AK/SK 侧的活动明细接口）；
5. **AK/SK 所需的最小 IAM 权限点名称**（当前这把 AK 可用，但权限点未知，凭证配置指南需要写明）；委托/agency 场景下 AK 归属账号已由本次实测间接确认（可用）。

## 变更记录

| 时间 | 变更内容 | 触发原因 | 影响章节 |
| --- | --- | --- | --- |
| 2026-10-11 | 初版：根因分析（会话超时策略 + Cookie 介质）、AK/SK 方案与「CLI 授权」评估、Phase 0 实测清单、待确认项 | 用户反馈 CodeArts 登录态总是失效，询问能否走 CLI 授权 | 全篇 |
| 2026-10-11 | 补充 Phase 0 可由 API Explorer「当前登录账号自动认证」完成（无需先创建 AK/SK）；补充 IAM 用户「编程访问」创建路径与最小权限身份策略 JSON；补充操作步骤清单（见 3.3/3.6/5） | 用户要求给出具体操作步骤 | 3.3、3.6 |
| 2026-10-11 | 回填 Phase 0 首轮实测：devcloud + status=1 返回空列表（HTTP 200），补充四种可能性拆解与「无过滤列表 / 抵扣明细 / 页面目视」三项补充检查 | 用户执行 API Explorer 调试并反馈响应 | 3.3、8 |
| 2026-10-11 | Phase 0 第二轮：无过滤查询证实费用中心无资源包（BSS 路径否决）；浏览器 + CLI 安装包静态取证确认码道官方 CLI 的 AK/SK 链路（snap-access 网关、SDK-HMAC-SHA256）；落地 `src/lib/huaweicloud.ts` 与 `npm run codearts:aksk-probe` 探测脚本；主路径改判为 snap-access | 用户要求用 playwright 直接检查；据此完成浏览器实测与 CLI 取证 | 3.3、3.4、8；关联改动文件 |
| 2026-10-11 | Phase 0 第三轮：AK/SK 首测 401（路由存在、鉴权被拒），两个候选接口 404；取证 CLI 存在两种签名器并补齐 `legacy-no-date` 变体，探测脚本扩展为 4 组合 + 失败打印响应体 | 用户执行 `npm run codearts:aksk-probe` 并回传结果 | 3.3、8、关联改动文件 |
| 2026-10-11 | Phase 0 第四轮：4 组合均返回 `APIG.0301 verify ak sk signature fail`（AK 已识别、签名不匹配）；补齐第三代「换行式」签名（`newline`），探测扩展为 6 组合；记录 AK/SK 明文泄露于对话的安全事件与处置要求 | 用户回传第二次探测结果（含明文凭证，已要求轮换） | 3.3、8、关联改动文件 |
| 2026-10-11 | Phase 0 第五轮（关键突破）：`newline` + 空 body 哈希返回 200，AK/SK 打通；`statistics/plugin` 完整覆盖余额（20500）+ 基础/活动/按需三类批次；探测脚本改为换行式优先并把完整响应落盘 `.run/`；§3.4 设计同步为 snap-access 路径 | 用户第三次探测（新密钥）回传 200 结果 | 3.3、3.4、8、关联改动文件 |
| 2026-10-11 | Phase 1 实施：新增 `codearts-aksk.ts` 数据源与 `authMode` 分支（AK/SK 优先、Cookie 兜底）；同步 `.env.example`、`accounts.example.json`、`README.md`、`docs/凭证配置指南.md`；用落盘 JSON 回放验证解析结果一致 | 落盘响应字段核对完成，具备接入条件 | 3.4、3.4.1、6、关联改动文件 |
| 2026-10-11 | Phase 2 混合增强：活动福利逐笔优先（登录态）叠加在 AK/SK 权威余额之上，差额补调节项、失效只降级活动批次；抽出 `consoleHeaders()`/`fetchCampaignBenefits()` 供两条路径共用；本地回放验证三种形态；凭证指南行为表更新 | 用户要求完成活动福利逐笔到期的混合增强 | 3.4、3.4.2、8、关联改动文件 |
| 2026-10-12 | 缺陷修复：AK/SK 路径补上 `splitByExpiry()` + `hiddenLotsNote()`（此前过期逐笔会以「已过期」直接列出）；probe 新增两个「免登录态拿逐笔」候选路径；回放验证过期批次被隐藏并汇总 | 用户反馈「活动福利逐笔到期显示失效」 | 3.4.3、关联改动文件 |
| 2026-10-12 | 症状分析（截图 + server.log + 采样时间线）：活动列表返回 0 个活动 → 逐笔退回聚合；且 grant 维度被错误声明覆盖，把本应显示的昨日 +1000 压成 0。修复 `fetchUsageLedger` 覆盖语义、降级文案与日志，新增 `npm run codearts:console-probe` 只读诊断（查活动接口原始返回） | 用户反馈「昨日增量没显示、活动福利没逐笔而是合起来」 | 3.4.4、关联改动文件 |
| 2026-10-12 | 静态取证补充：CLI 常量 `bQ0` = snap-access 根地址，积分接口为 `${base}/v1/ops/credit/usage`（无 snap-manager 前缀）；探测候选修正为 `/v1/ops/credit/overview` 等 3 条 | 昨日增量已修复、逐笔仍缺，继续找免登录态的数据源 | 3.4.4、关联改动文件 |
| 2026-10-12 | 根因确认：控制台两条接口返回「200 + 空响应」= 登录态早已失效，空响应被误判成「0 个活动」；AK/SK 侧三条活动候选全 404 → 逐笔只能来自登录态。修复空响应判定与文案，诊断脚本标注处置办法 | 用户运行两个探测命令并回传结果 | 3.4.5、关联改动文件 |
| 2026-10-12 | 维护成本优化：capture 支持 `npx playwright cli` 回退（移除全局依赖）；新增 `npm run codearts:refresh` 一键「拉起调试 Chrome + 抓取 + 校验」并给出可编程退出码；console-probe 加结论与退出码；凭证指南补充 cron 定时示例与 24h 会话边界说明 | 用户反馈「还是需要经常 chrome:debug + capture」，要求解决反复操作 | 3.5、关联改动文件 |
| 2026-10-12 | 落地 launchd 定时任务：`tools/install-codearts-refresh-launchd.sh` + `npm run codearts:schedule`（安装/状态/立即运行/卸载），每天 09:30 / 21:30 自动刷新，失败弹系统通知；已在本机安装并确认加载（`state = not running`，等下一个时间点） | 用户要求生成 launchd 定时任务 | 3.5、关联改动文件 |
| 2026-10-12 | 修复 `codearts:refresh` 直接报 `CHROME_LOG: unbound variable`：变量后紧跟全角字符 + `set -u` 的解析坑，统一改 `${VAR}` 并扫描 `tools/*.sh` 确认 0 处；Chrome 改 `nohup` 拉起；桩替换 npx 端到端冒烟通过 | 用户重跑时报错退出 | 3.5、关联改动文件 |
| 2026-10-12 | 解决「每次运行都要重新登录」：取证确认 codearts 会话 Cookie 为会话级（不落盘），根因是调试 Chrome 被关闭；改为 Chrome 后台常驻 + 刷新脚本「先校验后抓取」+ 新增 Chrome 常驻 launchd 任务与 `AbandonProcessGroup`；三分支桩测退出码 0/3/3 | 用户反馈登录态根本无法保存 | 3.5、关联改动文件 |
| 2026-10-12 | **按用户要求废弃「Chrome 常驻」方案**（日常必然关闭浏览器）：改为「`.env` 持久副本 + SSO 静默重登」模型；卸载 `com.powermeter.chrome-debug` 任务、安装脚本安装时自动清理历史遗留；新增 SSO 等待窗口（20s）；四分支桩测退出码 0/0/0/3 通过 | 用户明确表示不会保持 Chrome 常开 | 3.5、关联改动文件 |
| 2026-10-23 | 状态更新：浏览器侧将新增「网页端登录」编排入口（人工在调试 Chrome 窗口登录 + CDP 自动抓取落库），设计见 `docs/plans/账号管理-CodeArts网页端增删账号-开发设计方案.md` §10；本方案 §3.1 选项 C/E（服务端代登录）的否决**维持不变**——新入口不含任何自动登录动作 | 用户新需求「点添加跳转登录自动回调」 | §3.1（结论不变，补交叉引用） |
| 2026-10-23 | §10 已实施：`src/providers/codearts.ts` 声明 `codeartsWebLogin`（`kind:'web'`），`npm run typecheck` 通过；登录产物落 `CODEARTS_COOKIE`，与本方案的 `codearts:refresh` 续期链路同名对齐 | 用户确认完成全部开发 | §3.5、关联改动文件 |
