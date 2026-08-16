# Directus 深度调研 · 来源清单(2026-08-16 收集)

## 一手来源(官方/构建者)

| # | 来源 | 内容 | 要点提取 |
|---|------|------|---------|
| S1 | https://directus.com/license | 当前协议全文页 | 现行协议 = Monospace Sustainable Core License (MSCL-1.0-GPL);允许:内部商用/教育/科研/为持证客户部署托管;禁止 Competing Use(向与官方商业产品构成竞争的方提供并收费);发布满 4 周年自动转 GPL-3.0;再分发不得加收许可费 |
| S2 | https://directus.com/resources/directus-v12-license-change | v12 协议公告「Evolving Our License for Long-Term Sustainability」 | MSCL 源自 Fair Core License;明确允许「在 Directus 上构建产品、托管实例、围绕它提供服务」;唯一限制是 competing use;注册密钥替代荣誉制;每版本 4 年后转 GPLv3 |
| S3 | https://directus.com/resources/changing-our-license-one-year-later | 2023 BSL 改协议一周年复盘 | BSL 时代条款:生产免费除非年总财力>$5M;3 年转 GPL;改后企业自托管license两周内成新业务主体、ARR +200%;下载与 star 反增;<1% 用户受门槛影响 |
| S4 | https://directus.com/docs/ | 官方文档首页 | 定位「backend platform」:API server(REST/GraphQL/WS)+ Studio 管理界面 + 数据库内省;原生 MCP server 支持 |
| S5 | https://directus.com/docs/licensing/overview | 自托管许可规则 | 无 license 自托管 = Core 档;Open Innovation Grant:<$5M 年营收 且 <50 员工 → 免费商用(以「访问 Studio 的法人」计),Grant key 支持 5 个激活环境;席位只计拥有 App-Access/Admin 策略的 Studio 用户,API-only 用户不限量;key 绑定 PUBLIC_URL、在线周期校验;超额:宽限期 → 锁定态(非管理员 API 封锁、GraphQL/WS 关闭);「执行不删数据」 |
| S6 | https://directus.com/pricing | 定价页 | Cloud:Core $0(3 席位/25 collections/5 flows)、Team $499–599/月(SSO、20 flows、granular RBAC)、Enterprise 定制(社区口径 ~$15k+/年);「SSO 不在 Core 档且不可加购」;自托管各档均可 |
| S7 | https://directus.com/airtable | 官方对比页 | Flows 计数分档:Core 5 / Team 20 / Enterprise 无限;flow 运行次数不限;自托管 flows 跑自己基础设施 |
| S8 | https://directus.com/resources/stop-overengineering-your-multitenant-architecture | 官方多租户指南 | 三式:行级(tenant_id+权限过滤,单库~5000 租户)/schema 级/库级隔离;均为模式而非产品化多租户功能 |
| S9 | https://directus.com/docs/guides/ai/mcp/tools + /local-mcp/tools | 官方 MCP 工具文档 | Remote MCP(实例内开启):system-prompt/items/schema/collections/fields/relations/files/assets/flows/operations/trigger-flow/folders;Local MCP(npx @directus/content-mcp)22 个更细粒度工具;建议建专用受限 MCP 用户 |
| S10 | https://directus.com/docs/configuration/flows | Flows 配置文档 | Run Script 默认 10s 超时(FLOWS_RUN_SCRIPT_TIMEOUT)、FLOWS_ENV_ALLOW_LIST 环境白名单;历史上用 vm2 沙箱且有逃逸 CVE(见 advisories) |
| S11 | https://directus.com/docs/tutorials/migration/promoting-changes-between-environments-in-directus + GH discussions #10496/#16988 | schema snapshot/apply 生态 | snapshot 覆盖数据模型(collections/fields/relations);roles/permissions/policies/flows 不在 snapshot 内,需走 API/Template CLI 单独管理 |

## 仓库/社区事实

| # | 来源 | 要点 |
|---|------|------|
| S12 | GitHub directus/directus(gh api,2026-08-16) | 37,414 stars / 4,900 forks / TypeScript 主导(TS 11.3MB + Vue 2.7MB 源码字节 ≈ 数十万行)/ 仓库 448MB / 最近推送 2026-08-14(活跃)/ license 字段 NOASSERTION(自定义) |
| S13 | HN 35730861「Directus is no longer open source」(2023-05,BSL 切换时) | 社区主张「fork 才符合开源精神」;有 v9 GPL fork(d9)先例 |
| S14 | Reddit r/selfhosted「Directus now has active license enforcement…any good alternatives?」+ r/Directus「New Directus license model」(v12 后) | v12 引入主动 license 强制;SSO 移出免费档;有客户因 5–50 许可模式弃用;社区寻找替代品 |
| S15 | dxpscorecard.com/platform/directus + contensu.com(SSO 分析) | 第三方口径:SSO 需 Team/Enterprise;「v12 没改 SSO 工作方式,改的是谁有权用」;亦称「自定义权限规则移入更高档」(单一来源,未获官方文档证实,存疑) |
| S16 | Ben Haynes(Directus 创始人)LinkedIn + Grant 相关帖 | Grant 档=「every feature, unlimited users, no artificial limits」,在线表单自助申请(约 <50 员工公司) |
| S17 | dev.to d9 fork 文 + Sliplane/UnfoldCMS 2026 替代品清单 | d9 = Directus v9 的 GPL fork(含 project-as-code 工具);替代品:Strapi、Payload(MIT)、Sanity、PocketBase 等 |
| S18 | nayankyada.com Directus pricing 2026 + Reddit agency 帖 | 自托管超门槛商业授权社区口径 ~$999/月;自托管基础设施 ~$20–50/月 |
| S19 | directus.io/features/existing-database(官方特性页)+ Medium 迁移管理文 + GH discussion #6832 + Cloudron/StackOverflow 排障帖 | 「带库接入」是官方卖点:内省不改表结构;首启会在同库创建 directus_* 系统表(directus_users/roles/collections/fields 等),需 DDL 权限;社区最佳实践:先备份,更严隔离可用独立 schema/库 |

## 矛盾记录(按 learn 规则保留,不静默裁决)

1. **Grant 是否「全功能」**:S16 称 Grant=全功能无限制;S6/S7 定价矩阵显示 flows 5/20 分档、SSO 不在 Core。两者可能都真:Core(默认无 key)受硬限制,Grant(注册后)= 全功能。报告按此表述并标注「待注册验证」。
2. **门槛口径变化**:BSL 时代「$5M 总财力」(S3);MSCL 时代「<$5M 年营收 且 <50 员工」(S5/S6)。是两次不同的协议,不是笔误。
3. **v12 时间**:S2 公告「v12 于 5 月上线」;S15 记录 2026-06。取「2026 年年中」并注明年份来源不一。
4. **「自定义权限规则被锁进高档」**:仅 S15 一家,官方文档(S5/S6)显示 RBAC 粒度描述在各档均有。标记为未证实。
