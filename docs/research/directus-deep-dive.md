# Directus 深度调研:能否对标 Lovrabet,能否复刻成自己的平台

调研日期:2026-08-16 · 方法:一手来源优先(官方协议页/公告/文档/GitHub 仓库实测),社区信源仅作交叉验证
来源编号(S1–S18)见同目录 `sources.md`

---

## TL;DR

1. **对标 Lovrabet:能对标它的「数据底座层」,对标不了它的「低代码应用层」。** Directus 给你:数据集(Collections,可直接内省现有 PG)、即时 REST/GraphQL CRUD、字段级+行级权限、Flows 自动化、官方 MCP(agent 友好,这点比 Lovrabet 强)。它没有:智能列表页/页面生成、多 dblink 外部库联邦、多应用门户、菜单事实源治理、自由 BFF 脚本运行时。约等于 Lovrabet 后端能力的一半,前端低代码能力为零。
2. **复刻成自己的平台:分两种问法,答案相反。**
   - 「用 Directus 当内核,上面做我自己的业务产品(如 SilkLine 类财税 SaaS)」:**MSCL 明确允许**(build products on top / host / offer services),符合 Grant 条件(<$5M 营收且 <50 员工)时免费。
   - 「用 Directus 内核做一个对外售卖的通用低代码/数据平台(= 做自己的 Lovrabet)」:**这就是 MSCL 定义的 Competing Use,被禁止**,除非买商业授权。
   - 硬 fork 有合法窗口(已满期转 GPLv3 的代码),但代价是接手一个 ~37k stars、数十万行 TS 的巨型 monorepo 的永久分叉,且只拿到 2023 年水平的功能。不推荐。
3. **你插的 SilkLine 三点方案成立,但要修四处**:系统表会混进现有库、snapshot 不含权限/Flows、SSO 和 Flows 数量在免费 Core 档有硬门槛、Run Script 是 10 秒沙箱不是 BFF 运行时。详见 §5。

---

## 1. Directus 是什么

- **定位**:官方自我描述已从「headless CMS」改为「backend platform / The flexible backend for all your projects」——包一层在任何 SQL 库上,即时给出 API + 管理台 + 权限 + 文件 + 自动化(S4)。
- **架构**(S4/S12):Node.js/TypeScript monorepo。三个面:API server(REST + GraphQL + WebSocket + 原生 MCP)、Vue 3 管理台(Studio)、数据库内省层(读 `information_schema` 映射你的既有表,不改你的结构)。支持 Postgres/MySQL/SQLite/MS SQL/OracleDB/CockroachDB 等。
- **体量与活跃度**(S12,gh api 实测 2026-08-16):37,414 stars,4,900 forks,TS 源码 11.3MB + Vue 2.7MB(数十万行量级),仓库 448MB,最近推送 2026-08-14。**维护非常活跃,这不是弃子项目;反过来说,fork 它等于追着一列高速火车跑。**
- **能力面**:Collections/字段/关系建模(可内省既有库)、policies 化 RBAC(字段级 + 行级 filter,`$CURRENT_USER` 等动态变量,App 访问与 API 访问分权)、Flows 自动化(事件/webhook/cron 触发 + 编排操作)、Insights 仪表盘、Marketplace 扩展市场、文件与资产管线、内容翻译/i18n、原生 remote MCP(S4/S9)。
- **扩展模型**:app 扩展(界面组件/布局/面板/模块)、API 扩展(hooks/endpoints)、hybrid 扩展,TypeScript 写,走 Marketplace 分发。**模块(module)扩展可以给 Studio 加全新页面**,这是补页面生成缺口的官方姿势;但那是自己写 React/Vue 页面,不是 Lovrabet 式的配置生成。

## 2. 协议:两年两次收紧(本案最重的信息)

时间线(全部一手信源):

| 时期 | 协议 | 商用条件 | 转 GPL 期限 |
|------|------|---------|------------|
| ≤ v9 | GPLv3 | 完全开源 | 已是 |
| v10–v11(2023-05 起) | BSL 1.1 | 生产环境免费,除非法人年总财力 > $5M | 每版本发布满 **3 年** |
| v12(2026 年年中起,现行) | MSCL-1.0-GPL(源自 Fair Core License) | 见下 | 每版本发布满 **4 年** |

**现行 MSCL 的许可/禁止**(S1/S2/S5/S6):

- 允许:看代码、改代码、**在 Directus 上构建产品**、托管实例、围绕它做服务;内部商用、教育、科研。
- 禁止(Competing Use):**把它做成主要目的与 Directus 竞争的产品对外售卖**,即拿它当内核卖通用后端/数据平台。
- 免费商用 = Open Innovation Grant:法人 **年营收 <$5M 且员工 <50**(按「谁能进 Studio」的法人算),自助申请,许可 key 支持 5 个环境激活(本机/开发/预发/生产),**在线周期校验,key 绑定 PUBLIC_URL**。
- 不注册的自托管默认 = **Core 档**:可以跑,但有硬限制——**Flows 仅 5 条、SSO(SAML/OIDC)不可用且不可加购**(S6/S7/S15);席位只计有 App-Access 的 Studio 用户,**API-only 用户(后端服务、集成、终端 App 用户)不限量**(S5)。
- 超额/失效机制:宽限期 → 锁定态(非管理员的 API 被封、GraphQL/WebSocket 关闭),「不删数据」;且 v12 引入的 license key 校验**协议明文禁止移除/绕过**(S2/S14)。

**收紧的动机与效果**(S3 一周年复盘):改 BSL 后两周,企业自托管 license 成为其新业务主体,ARR +200%,下载量与 star 反而上涨,<1% 用户受门槛影响。→ 对 Directus 公司,收紧是成功的;对依赖它的你,**方向只会更紧,不会更松**。v12 加主动强制后社区已出现「找替代品」声浪(S14),也有客户因许可模式弃用的案例。

**合规 fork 的合法窗口**:BSL/MSCL 的转 GPL 是**按版本**计的。今天(2026-08)已自动变成 GPLv3 的代码 = v9 全部 + v10/v11 中发布满 3 年的部分(2023-08 之前的 v10.x)。**现行 v12 主干要 2030 年才转 GPL。** 已有先例:d9(Directus v9 的社区 GPL fork,含 project-as-code 工具),但社区规模很小(S13/S17)。

## 3. 对标 Lovrabet:能力对照

Lovrabet 侧事实源:本环境 `rabetbase`(开发工作流 CLI)与 `lovrabet`(运行态 CLI)暴露的完整能力面。

| Lovrabet 能力 | Directus 对应 | 判定 |
|---|---|---|
| 数据集(表/字段对象/doType/options/关联/业务组/relation-audit/软删恢复/rename) | Collections/fields/relations;**可直接内省既有 PG 表** | ≈ 平手;Directus 加分在内省,Lovrabet 加分在数据集治理审计工具 |
| 外部数据库连接 dblink(db list/test/diff/analyze,一个平台挂多库) | **无。一个实例连一个库** | ✗ 硬缺口(只能多实例拼) |
| 自定义 SQL 保存/校验/执行(sql.execute,SQL 即 API) | 无原生 SQL 端点;Flows Run Script 或自定义 endpoint 扩展可绕 | ✗ 弱 |
| BFF 脚本(bff.execute + 通知配置) | Flows(触发器+编排+email/webhook/notification+Run Script);重逻辑写 TS 扩展 | △ Flows 是**编排器**不是脚本运行时:Run Script 默认 **10 秒超时**、沙箱隔离(历史上 vm2 有逃逸 CVE)、环境变量白名单制(S10) |
| 智能列表页生成(page generate/sync/pull/push + codegen) | **无页面生成器**。Studio 是数据浏览台;module 扩展=手写页面;或自建前端 | ✗ 硬缺口(Lovrabet 的核心卖点) |
| 菜单事实源(menu list/sync/异常审计/手动删除清单) | 无对应产品概念,Studio 导航固定 | ✗ |
| 角色/用户组/permit/role-menus/role-apis/行级权限(SELF/ALL/row-roles) | policies/roles/permissions:字段级+行级 filter,App 与 API 分权,动态变量 | ✓ 粒度不输甚至更强(但注意 §2 的档位争议) |
| 多应用 workspace(app list/appcode/多应用切换/默认应用) | 一个 project = 一个应用;多应用=多实例,无统一门户 | ✗ |
| (衍生)多租户 SaaS 化 | 非产品化功能。官方给三式模式:行级(tenant_id+权限过滤,单库称可撑 ~5000 租户)/schema 级/库级(S8),全靠自己搭 | △ DIY |
| 文件上传/长期链接/压缩 | files/assets,本地/S3/GCS/Azure 存储,token 链接 | ✓ |
| 通知配置(EMAIL configCode/notification.send) | Flows 的 email/webhook/notification 操作 | △ |
| 项目管理(project create/upgrade) | CLI + schema apply | △ |
| —(Directus 独有) | GraphQL、WebSocket 实时、Insights 仪表盘、Marketplace、内容 i18n、**原生 MCP(agent 直接管 schema/数据/flows)** | ✓ 反超项,尤其 MCP |

**结论**:Directus ≈ Lovrabet 的「数据 + API + 权限 + 运维台」那一半,且在 API 面、权限粒度、agent 化(MCP)上更强;Lovrabet 的「低代码应用工厂」那一半(页面生成、多库联邦、多应用门户、菜单治理、自由 BFF)在 Directus 里**要么没有,要么要你用扩展/自建前端补**。所以「能不能平台 Lovrabet」:如果你要的是「给团队一个数据底座 + agent 自动维护的 API 层」,能;如果你要的是「复现 Lovrabet 式的完整低代码开发平台」,Directus 单独做不到。

## 4. 「复刻成自己的平台」三条路径

### 路径 A:包壳不 fork(推荐给几乎所有场景)
Directus 自托管(Grant 内免费或买授权)+ 你的业务代码全部走扩展与外部服务,不改 Directus 主干,保持升级能力。
- **合规**:MSCL 明文允许「build products on top、host、offer services」;只有当你卖的产品的**主要目的**是「替代 Directus 本身」时才触线。
- **边界判定一句话**:你的平台对客户收钱时,客户买的是「财税/企服/某某业务系统」→ 合规;客户买的是「低代码数据平台/BaaS」且内核是 Directus → Competing Use,需要商业授权。
- **工程量**:小。你的代码在自己的仓库,Directus 当基础设施。
- **风险**:协议继续收紧(已两次);license key 在线校验依赖厂商存活与仁慈;Core 档功能天花板(5 Flows、无 SSO)逼你注册 Grant 或付费。**Grant 门槛(<$5M 且 <50 员工)今日满足、明日公司长大了就不满足**,这是个随你成功而失效的免费。

### 路径 B:fork 已转 GPLv3 的代码
合法(满期代码就是 GPLv3),但:
- 只能拿到 v9(2021 水平,Vue 2)或 ≤2023-08 的 v10.x(无 v11 policies、无 Marketplace、无原生 MCP、无 v12 全部新特性)。
- 接手数十万行 TS 的永久分叉,脱离上游,安全补丁自己背。
- 若把平台**分发**给客户(而非纯 SaaS 自用),GPLv3 传染:必须向客户提供你的修改源码。
- 先例 d9 证明可行,也证明社区接不住,它没长起来。
- **适用**:想要一个永不失效的底座,且愿意自己养它。对 SilkLine 这种业务线,养平台的成本大概率压过收益。

### 路径 C:商业授权
Team 档 ~$499/月起(Cloud 价;自托管企业授权社区口径 ~$999/月)(S6/S18)。买断不确定性,换 SSO、Flows 放开、离线 token(Enterprise)。**若平台承载核心营收,这笔钱是保险费。**

**附带一提**:若「永久可控的宽协议」是第一诉求,可评估宽协议基座:已核实 MIT 的 Payload(S17),另 PocketBase、Supabase、Appsmith 亦以宽松开源见称(协议以各自仓库为准,本案未逐一核验)。功能面与 Directus 各有取舍,它们没有 Directus 这套 Studio+内省+MCP 组合,但不存在协议再收紧的风险。本案未深评,只立此存照。

## 5. 你的 SilkLine 三点方案:逐条验证与修正

> 原方案:① caddy vhost + 连现有 PG;② schema snapshot YAML 进仓库,Claude 改 YAML→apply;③ 官方 MCP 进 Claude Code。

**总判定:方向成立,这正是 Directus 当前最强的使用姿势(agent 驱动、schema 即代码、不动存量服务)。修四处:**

1. **混库问题**(①的修正):Directus 首次启动会在**同一个库/schema** 里建约 20 张 `directus_*` 系统表,且需要 DDL 权限(S19)。SilkLine 的 Java migration 若是「重建式」(记忆:迁移可重建库重跑),重跑时会撞上这些表——要么迁移脚本排除 `directus_*`,要么给 Directus 独立 schema/独立库。另外 Studio 管理员可绕过 Java/Node 业务逻辑直改业务表,起步建议把业务 collections 配成只读,把 Studio 当内部运维台用。SilkLine「生产=开发系统」的授权降低了试错成本,但别让 directus_ 表悄悄进 Java 的迁移基线。
2. **snapshot 范围**(②的修正):`npx directus schema snapshot` 只含数据模型(collections/fields/relations);**roles/policies/permissions/flows 不在 snapshot 里**(S11,官方迁移教程与 GH discussions #10496/#16988 一致)。你原话「角色权限、Flows 也走 API/MCP 管理」是对的,但要意识到这是**两条管理通道**:YAML 管 schema,API/MCP 管权限与 Flows。建议在仓库里再放一个权限/Flows 的导出脚本(走 `/schema`、`/policies`、`/flows` 端点),CI 里做 drift 检查,否则环境间会漂移。
3. **MCP**(③成立):remote MCP 在实例内开启(Settings → AI),工具面 system-prompt/items/schema/collections/fields/relations/files/assets/**flows/operations/trigger-flow**/folders;local MCP(`npx @directus/content-mcp`)22 个更细工具(S9)。**schema 建/改、Flows 编排、数据 CRUD 全覆盖,「Claude 改 → apply → API 变」的闭环真实存在。** 安全姿势:给 Claude 建专用受限 MCP 用户(token 最小权限),`DISABLE_TOOLS` 关掉 delete/update-field 类高危工具。
4. **档位现实**(你没提但会咬人的):免费 Core 档 **Flows 只有 5 条、SSO 无**。若把 Flows 当「Lovrabet BFF 脚本」用,5 条根本不够——立刻申请 Grant(<$5M 且 <50 员工时=全功能,需在线激活、绑 PUBLIC_URL,注意换域名/内网地址会吃激活位,共 5 个)。Run Script 是 10s 沙箱,重逻辑(报表、对账、批量)写 TS 扩展或留在 SilkLine 自己的 Node 服务里,Flows 只做轻编排。

## 6. 建议

- **场景一(最可能):给 SilkLine/自用加一个 agent 可维护的数据底座** → 走路径 A。今天就能跑:caddy vhost + 独立 schema + Grant 注册 + snapshot 入库 + MCP 接 Claude Code。成本几乎为零,收益是「API 层零代码化」。
- **场景二:做自己的 Lovrabet(对外低代码平台)** → Directus 不是你的内核,是你的竞品。要么买授权(路径 C,且要谈),要么换基座(Payload/PocketBase/Supabase 一类宽协议,或干脆像 SilkLine 一样自研,你已经有一套了)。
- **场景三:想要「永久属于自己的 Directus」** → 路径 B 合法但重。先问自己能否供养一个数十万行 TS 的分叉;答案是「不能」就放弃这个念头,用 A 的「扩展即资产」替代「fork 即资产」。

## 7. 矛盾与未决(保留,不裁决)

1. Grant 档是否真「全功能无限制」:创始人说 every feature/unlimited(S16),定价矩阵显示 flows 分档(S7)。合理解释是 Core(未注册)与 Grant(注册后)是两回事,但**注册前无法确证**,落地时拿 Grant key 实测 flows 数与 SSO。
2. BSL 时代门槛「$5M 总财力」 vs MSCL 时代「$5M 年营收 + <50 员工」:两次协议的真实差异,引用时别混用。
3. v12 上线时间:官方公告「5 月」(S2)与第三方「2026-06」(S15)差一个月,不影响结论。
4. 「自定义权限规则被锁进付费档」仅一家第三方信源(S15),官方文档各档 RBAC 描述无此差异,**未证实**,存疑待实测。

## 8. 主要来源

官方:协议页、v12 协议公告、BSL 一周年复盘、licensing docs、pricing、Flows 配置、MCP 工具文档、多租户指南、迁移教程;仓库:directus/directus(gh api 实测);社区:HN 35730861、r/selfhosted 与 r/Directus v12 声浪、d9 fork、dxpscorecard/contensu 第三方分析。完整清单与矛盾记录:`sources.md`。
