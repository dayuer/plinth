# Plinth 立项决策记录(2026-08-16)

本文记录 Plinth 从调研到立项的完整决策链:每一步选了什么、放弃了什么、为什么。技术细节见 `../specs/2026-08-16-plinth-design.md`,调研证据见 `../research/`。

## 转向记录:2026-08-16 晚,v1 四件套 → v2 SQL BFF

用户在 Plan 1 v1 派工前澄清真实需求:**Claude 经 Lovrabet CLI 获取数据集的 AI 化语义 → 据此写对本地库的访问 SQL → 存入自己的 BFF 服务供真实业务调用,使 SQL 不再由程序员手写维护、可随业务变化自动调整、全程可审计**。据此转向:

| # | 问题 | 决定 | 放弃/说明 |
|---|------|------|----------|
| 转向-1 | 目标数据库 | **SilkLine 生产 PG**(旁挂只读) | Lovrabet 连的库;多源 |
| 转向-2 | 读写边界 | **只读起步留扩展**(格式预留 mode 位,write 拒绝加载) | 直接含写 |
| 转向-3 | 调用方鉴权 | **服务间静态 token + 内网隔离** | JWT 用户级(纯网关模式随之作废) |
| 转向-4 | 只读保证 | **A1a 双保险**:简化静态检查(独立函数可换装)+ 独立只读 DB 角色 | A1b 完整 parser(pg_query_go 引 CGO,毁单二进制),留 v0.2 |

随之作废的 v1 设计:表格 CRUD 网关、行级权限表达式语言、行列权限流水线、JWT 验签。全部保留的底盘哲学:文件优先(git 即审计)、零 DDL 客人姿态、Go 单二进制、Apache-2.0、agent 原生工作循环。v1 的 Plan 1(11 任务)已废弃未派工。

以下为 v1 立项时的原始决策(背景部分仍然有效,前半段五问答与路线 A 的「自研 Go 单二进制」结论继续适用):

## 背景:为什么不用现成的

起点是评估 Directus 能否替代 Lovrabet 并复刻成自己的平台。深调研(见 `../research/directus-deep-dive.md`)结论:

- Directus 功能最贴合需求,但协议两年两度收紧(GPLv3 → BSL 1.1 → v12 的 MSCL + 注册密钥强制),自托管免费档有硬门槛(Core 档 Flows 仅 5 条、无 SSO),且明文禁止拿它当内核做对外平台。
- 砍掉页面生成需求后,数据底座四件套(内省现有 PG、即时 REST/GraphQL、字段级+行级权限、自动化)在开源界横评(见 `../research/data-foundation-alternatives.md`):Hasura CE v2(Apache-2.0)最接近但 REST 弱一档、v3 已闭源;PostgREST+pg_graphql 积木路线零 UI;Supabase 想当 PG 的主人而非客人;NocoDB/Mathesar/Budibase/PocketBase/Appwrite 各缺一块。
- **四件套 + 宽协议 + 可声明式配权限的完全体不存在。** 这个空档就是 Plinth 的位置。

## 五个立项问答

| # | 问题 | 决定 | 放弃了什么 |
|---|------|------|-----------|
| 1 | 开源目的 | **SilkLine 自用为主,开源顺带** | 不为社区/商业化养用不上的功能(PostgREST/Supabase 起家的路径) |
| 2 | MVP 范围 | **基线版**:内省 + REST + 行列权限 + 事件/cron + MCP,零 UI 零 GraphQL | GraphQL、管理台 UI(消费端全是 REST;两者后补不伤架构) |
| 3 | 身份对接 | **纯网关模式**:无自有用户表,验 SilkLine JWT,权限规则引用 token 声明 | 自有用户表+映射(Directus/Hasura 模式)——双身份源迟早打架 |
| 4 | 技术栈 | **Go** | Node/TS(与 SilkLine 同构但无单二进制)、Rust(开发速度不匹配) |
| 5 | 开源协议 | **Apache-2.0** | MIT(无专利条款)、AGPL(传播面窄,与顺带开源的开放姿态相悖)、先私有(依赖选型容易堵死后路) |

## 技术路线:为什么纯自研

- **A. 纯 Go 单二进制自研(选定)**:内省器/SQL 生成器/权限编译器/事件引擎/MCP 全在一个进程。理由:单栈与 Go 选择自洽;纯网关模式必须把 token 声明织进每条查询,查询改写绕不开;PostgREST(MIT)语义可当规范,其测试用例可当验收锚。
- **B. 复用 PostgREST 本体 + 自写周边(弃)**:省约一半工作量,但两进程混栈、RLS 路线要在存量库装策略函数(客人变半个主人)、能力边界被 PostgREST 定义。省下的时间会在第一次撞墙时加倍还回去。
- **C. fork 现有 Go 项目(不成立)**:Go 生态没有挂外部 PG 的基座,PocketBase 是 SQLite 心脏,换 PG 等于重写。

## 设计承重墙(分节评审已通过)

1. **零 DDL 客人姿态**:对目标库零迁移、零系统表、零触发器(Directus 要建 ~20 张 `directus_*` 表,Plinth 一张不建);自带状态放本地嵌入存储。
2. **内省与注解分层**:启动时读真实 schema,YAML 只声明「在这之上暴露什么」;`plinth diff` 检测库结构漂移并大声报错。
3. **表达式编译而非裸 SQL**:行过滤是小型表达式语言(and/or/eq/in/gt,操作数为列名或 `$token.*` 声明),编译成参数化 SQL 片段;metadata 里永不允许出现 SQL 字符串。
4. **写操作同一条谓词**:UPDATE/DELETE 带策略谓词,受影响行为 0 时与「不存在」同形返回(防存在性探测)。
5. **事件捕获用 logical replication**:不在目标库建触发器;cron 只投 webhook 不执行命令。
6. **MCP 与 REST 同一条权限流水线**:Claude 的 token 绑定窄角色,默认非上帝模式。

## 成功标准(自用优先)

1. SilkLine 生产真实流量走 Plinth;
2. Claude 改 YAML → `plinth diff` → apply 的 agent 工作流日常可用;
3. 三个月内替换掉一个现有手写 CRUD 端点。

外部用户/贡献者是 stretch goal,不计入。

## 命名

工作名 Plinth(底座)。发布前可换,不构成技术约束。
