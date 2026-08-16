# 数据底座四件套:GitHub 备选平台对照(Directus 调研续篇)

日期:2026-08-16 · 前篇:`directus-deep-dive.md`
需求收窄:去掉页面生成/低代码应用层,只要数据底座,四件套:

① **内省现有 PG**(平台是客人,不是 PG 的主人) ② **即时 REST + GraphQL** ③ **字段级 + 行级权限** ④ **自动化**(Flows 级)
基线:Directus 功能最贴,但协议最紧(MSCL + key 强制,见前篇 §2)。

GitHub 数据均为 2026-08-16 gh api 实测。

---

## 对照矩阵

| 平台 | ①内省现有 PG | ②REST/GraphQL | ③字段+行权限 | ④自动化 | 协议 | Stars/活跃 |
|---|---|---|---|---|---|---|
| [Hasura CE v2](https://github.com/hasura/graphql-engine) | ✓ track 既有表 | GraphQL ✓✓ / REST △(RESTified 端点,按查询定义,非全自动 CRUD) | ✓✓ 行+列,控制台配,**权限写进 metadata 文件** | ✓ Event Triggers + Scheduled Triggers(cron) | **Apache-2.0** | 32.1k,活跃 |
| [Supabase 自托管](https://github.com/supabase/supabase) | △ 想当 PG 主人(全家桶注入 auth/storage 等 schema;可指外部 PG 但非一等公民) | ✓ PostgREST / ✓ pg_graphql + realtime | 行 ✓(Postgres RLS)/ 列 △(grant,全 SQL 无 UI) | △ webhook UI + pg_cron + Edge Functions,无可视化 flows | **Apache-2.0** | 108k,最活跃 |
| [PostgREST](https://github.com/PostgREST/postgrest) + [pg_graphql](https://github.com/supabase/pg_graphql) / [PostGraphile](https://github.com/graphile/crystal) | ✓ 天生就是给既有 PG 挂 API | ✓✓ / ✓(二选一拼装) | 行 ✓ 列 ✓(全部 SQL:RLS + grant,零 UI) | △ PG 触发器 + pg_cron | **MIT / Apache-2.0 / MIT** | 27.6k + 3.3k / 12.9k,均活跃 |
| [NocoDB](https://github.com/nocodb/nocodb) | ✓ 外连 PG 一等公民 | ✓ 自动 REST + Swagger / **✗ 无 GraphQL** | 行级(记录级)= **付费**;OSS 只有组织/工作区/库三级角色 | △ webhook(条件 webhook 付费) | ⚠️ AGPL 双授权(公司留 CLA) | 64.5k,活跃 |
| [Budibase](https://github.com/Budibase/budibase) | ✓ 外部数据源 | ✓ 自动 CRUD 端点 + Public API / ✗ | 应用级 RBAC(屏幕/角色),不是 API 层的字段/行权限 | ✓ automations + AI agent | GPLv3 + 商业版 | 28.2k,活跃 |
| [PocketBase](https://github.com/pocketbase/pocketbase) | ✗ 仅 SQLite | ✓ / ✗ | 行 ✓(规则)/ 列 ✗ | △ JS hooks | **MIT** | 60.7k,活跃 |
| [Mathesar](https://github.com/mathesar-foundation/mathesar) | ✓(定位就是给既有 PG 套表格 UI) | △ API 能力弱,非 API 平台 | ✗(用 PG 自身角色) | ✗ | GPL-3.0 | 5.1k,活跃 |
| [Appwrite](https://github.com/appwrite/appwrite) | ✗ 自有数据模型 | ✓ / ✗ | 集合+属性级 ✓ / 行 △ | ✓ functions(写代码) | BSD-3 | 57k,活跃 |
| (基线)Directus | ✓ | ✓ / ✓ | ✓✓ | ✓✓ 可视化 Flows | ⚠️ MSCL(v12) | 37.4k,活跃 |

## 分平台要点

**Hasura CE v2** 四件套命中度最高:连上既有 PG、track 表即得 GraphQL;行/列权限在控制台配,且**权限和 Event Trigger 都存在 metadata YAML 里,hasura CLI apply 即生效**,这跟你「schema 即代码 + Claude 改文件」的工作流天然同构(比 Directus 还顺,snapshot 和权限是两套,Hasura metadata 是一套)。两个代价:REST 是 GraphQL 的 RESTified 投影,不是 PostgREST 那种全自动 CRUD REST;**v3(DDN)已闭源且是公司主推方向,开源线只剩 v2**(v2 仍在维护,pushed 2026-08-11)。法律上无悬顶之剑:已发布的 Apache-2.0 代码不能被收回授权,哪天 v2 停更,fork 无负担。EE 才有的:SSO、缓存、监控、限流、Mongo/Oracle 等企业库连接。

**Supabase 自托管** 生态最大(108k)、协议最宽,API 面最全(REST+GraphQL+realtime+storage+auth)。但它对 PG 的姿态是「我是平台,库归我管」:自托管全家桶会把 auth/storage/realtime 一堆 schema 注进目标库,给 SilkLine 现有 PG 挂 Supabase 等于请一个新住户进屋。权限和自动化都回到写 SQL(RLS/webhook/pg_cron),没有 Directus 那种可视化权限台和 Flows 画布。适合的场景是「新项目从 PG 起步」,不是「给存量 PG 加盖」。

**PostgREST + pg_graphql/PostGraphile 积木路线** 最轻最稳:单二进制挂上既有 PG,REST 全自动,RLS+grant 管行列权限,自动化用 PG 触发器/pg_cron。没有控制台,一切权限编排都是 SQL 和配置文件。对「Claude 即运维」反而可能是优点:全是文本,全可进 git,没有 UI 状态漂移问题。缺点是可视化、仪表盘、内容台一个没有,非技术同事用不了。

**NocoDB/Mathesar** 是「表格 UI」路线,对外连 PG 友好,但一个把行级权限放付费档且没有 GraphQL(AGPL),一个压根不是 API 平台(GPL)。按四件套标准都不合格,除非你的真实需求其实是「给运营一个 Airtable 式界面」。

**Budibase/PocketBase/Appwrite** 各缺一块:Budibase 本质是页面构建器(你已砍掉)+权限在应用层不在 API 层;PocketBase 不支持 PG;Appwrite 不吃存量库。

## 结论与推荐

**四件套 + 宽协议 + 可视化权限台的完全体,在开源界不存在;Directus 的组合就是它的护城河,这也是它敢连改两次协议的底气。** 按你的场景排序:

1. **Hasura CE v2(Apache-2.0)**:最接近四件套,metadata 即代码与你既有工作流同构,行列权限齐,事件/定时触发器齐。接受 REST 弱一档、盯住 v2 停更风险(停了就 fork,法律无险)。
2. **PostgREST + pg_graphql 积木(MIT/Apache)**:协议与长期风险最优,全 SQL 全文本,agent 友好;放弃 UI,权限靠 RLS/grant,自动化回到数据库层。
3. **Supabase 自托管(Apache)**:若将来起新库新项目,它是生态之王;给存量 PG 加盖不是它的姿势。
4. 仍要 Directus 的完整体验,就回到前篇结论:Grant/付费,或接受 MSCL 约束。

SilkLine 语境一句话:给现有 PG 加一个 agent 可维护的 API 层,首选试 **Hasura CE**(caddy vhost 同款姿势,metadata YAML 进仓库,Claude 改文件 + hasura CLI apply);对可视化 Flows 有硬需求再回 Directus。

## 来源

GitHub 实测(gh api, 2026-08-16):supabase/supabase 108,036★ Apache-2.0;hasura/graphql-engine 32,083★ Apache-2.0;PostgREST/postgrest 27,601★ MIT;nocodb/nocodb 64,549★(NOASSERTION,AGPL 双授权);Budibase/budibase 28,202★(GPLv3+商业);teableio/teable 21,658★;pocketbase/pocketbase 60,690★ MIT;mathesar-foundation/mathesar 5,095★ GPL-3.0;supabase/pg_graphql 3,345★ Apache-2.0;graphile/crystal 12,929★ MIT;appwrite/appwrite 57,016★ BSD-3;directus/mcp 83★ MIT。

功能与许可信源:[Hasura EE vs CE 文档](https://hasura.io/docs/2.0/enterprise/overview/)、[Hasura Event Triggers](https://hasura.io/docs/2.0/event-triggers/overview/)、[Scheduled Triggers](https://hasura.io/docs/2.0/scheduled-triggers/how-it-works/)、[CE→EE 升级说明(开源部分 Apache-2.0)](https://hasura.io/docs/2.0/enterprise/upgrade-ce-to-ee/)、[v3 不开源的社区讨论](https://www.reddit.com/r/Hasura/comments/1k1rzd8/hasura_v3_is_not_open_source_and_worse_the_build/)、[NocoDB 角色/权限文档](https://nocodb.com/docs/product-docs/roles-and-permissions)(行级权限付费见 [pricing](https://nocodb.com/pricing))、[NocoDB REST 文档](https://nocodb.com/docs/product-docs/developer-resources/rest-apis)、[NocoDB GraphQL 缺席讨论](https://community.nocodb.com/t/graphql-support-jul-25/621)、[NocoDB license 讨论 #6741](https://github.com/nocodb/nocodb/discussions/6741)、[Budibase 许可说明](https://github.com/Budibase/budibase/discussions/12377)、[Budibase data 文档](https://docs.budibase.com/docs/data)、[Mathesar 官网](https://mathesar.org/)。
