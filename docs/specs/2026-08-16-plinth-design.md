# Plinth 设计文档 v1.0

日期:2026-08-16 · 状态:待用户终审
决策链见 `../decisions/2026-08-16-brainstorm-decisions.md`,调研证据见 `../research/`。

一句话定位:**agent 原生、文件优先、给存量 PostgreSQL 当客人的数据底座网关**——单二进制旁挂,对目标库零 DDL,metadata 全在 YAML,权限编译成参数化 SQL,Claude 通过内置 MCP 直接运维。

## 0. 背景与动机

Directus 调研(`../research/directus-deep-dive.md`)证明「内省现有 PG + 即时 API + 声明式行列权限 + 自动化」这个组合有真实价值,但其协议两年两度收紧(GPLv3 → BSL 1.1 → MSCL + 注册密钥),不适合作为自用底座的长期地基。开源横评(`../research/data-foundation-alternatives.md`)证明四件套加宽协议加声明式权限的完全体不存在:Hasura CE 最接近但 REST 弱且 v3 闭源;PostgREST 是积木无权限台;Supabase 想当库的主人。Plinth 补这个空档,首要服务 SilkLine,顺手以 Apache-2.0 开源。

## 1. 目标与非目标

**目标(MVP 五件基线)**

1. 内省现有 PG,零迁移零系统表零触发器;
2. 全自动 CRUD REST(PostgREST 查询语义子集);
3. 字段级 + 行级权限,metadata 声明式,编译进 SQL;
4. 表事件(webhook)+ cron 自动化,at-least-once 投递;
5. 内置 MCP server,与 REST 同一条权限流水线。

**非目标(明确不做)**

GraphQL(留扩展点)、任何 UI、多数据库(PG only)、HA 多实例、可视化 Flows、文件存储、自有认证体系、分布式锁、任意代码执行。关系嵌套深度 MVP = 1。

## 2. 总体架构

单二进制 Go 进程(CGO-free 静态编译),旁挂部署:caddy vhost → Plinth → 现有 PG。

```
caddy vhost ─→ Plinth 单二进制
                ├─ REST 引擎(查询编译流水线)
                ├─ 内省器(启动读 information_schema → 内存目录)
                ├─ 权限编译器(metadata → 参数化 SQL 片段)
                ├─ 事件引擎(pgoutput 复制流 → webhook 投递)
                ├─ cron 调度器(状态存本地嵌入库)
                ├─ MCP server(streamable HTTP + stdio)
                └─ CLI 子命令(validate / diff / apply / serve / status)
```

硬约束:**对目标库零 DDL**。Plinth 自带状态(调度水位、死信队列)存本地嵌入存储 bbolt(纯 Go、单文件、零运维),不在目标库建任何对象。单实例部署(SilkLine 的真实形态),请求路径完全无状态,进程可随时重启。

## 3. metadata 即代码

全部配置是仓库里的 YAML 文件,没有任何一层状态存数据库。

```
plinth.yml          # 连接串、JWKS、事件引擎、嵌入存储路径
models/*.yml        # 内省注解:暴露哪些表、隐藏列、别名、关系覆写
policies/*.yml      # 角色→表:列白名单 + 行过滤表达式
events/*.yml        # 表事件→webhook;cron 任务
```

`plinth.yml`:

```yaml
database:
  url: ${DATABASE_URL}
auth:
  jwks_url: https://api.silkline.id/.well-known/jwks.json
  roles_claim: role
storage:
  path: /var/lib/plinth/state.db
events:
  mode: logical          # logical | polling(降级)
  slot_name: plinth_slot
```

`models/invoices.yml`(注解只写增量,不复制 schema):

```yaml
schema: public
table: invoices
expose: true
columns:
  hide: [internal_note]
relations:
  - name: buyer
    type: many-to-one
    on: buyer_id
    references: { table: buyers, column: id }
    expose: true
```

`policies/invoices.yml`:

```yaml
table: invoices
rules:
  - role: accountant
    columns: { allow: "*", deny: [internal_note] }
    row: org_id == $token.org and status != 'VOID'
  - role: ops
    columns:
      allow: [id, status, buyer_id, amount_total, currency]
    row: true
  - role: "*"
    columns: { allow: [] }
    row: false
```

`events/invoices.yml`:

```yaml
triggers:
  - table: invoices
    operations: [insert, update]
    deliver:
      url: https://api.silkline.id/hooks/invoice-changed
      secret_env: EVENT_SECRET
      retry: { backoff: exponential, base: 5s, max: 1h, attempts: 8 }
crons:
  - name: nightly-reconcile
    schedule: "17 2 * * *"
    deliver: { url: https://api.silkline.id/hooks/reconcile, secret_env: EVENT_SECRET }
```

**内省与注解分层**是与 Directus snapshot 的本质区别:启动时内省器读真实 schema,YAML 只声明「在这之上暴露什么」;`plinth diff` 比对线上内省与 YAML 引用的列/关系,库结构漂移时启动失败或显式报错,不静默。修改后 `SIGHUP` 或文件监听热加载;加载失败保留旧目录并告警,绝不半载。

## 4. 请求流水线(REST 引擎 + 权限)

查询语法采用 **PostgREST 语义子集**:过滤操作符 `eq/ne/gt/gte/lt/lte/in/like/is`,逻辑 `and/or`,`select=` 列选择与单层关系嵌套,`order/limit/offset`,响应头返回行数。不自创语法;PostgREST 文档为规范,其公开测试用例为验收锚。

```
请求 → JWT 验签(JWKS)
     → claims 解出 role/org/sub
     → 查已编译策略:列投影 + 行谓词
     → SQL 生成器:白名单 SELECT + 行谓词 AND 用户过滤,全参数化
     → pgx 连接池执行 → JSON
```

安全设计:

- **行过滤表达式语言**:and/or/not/eq/ne/in/gt/gte/lt/lte/is null,操作数为本表列名或 `$token.<claim>`;启动时编译为参数化 SQL 片段树,token 声明按请求绑定为参数值。metadata 中禁止出现 SQL 字符串,校验器强制执行。
- **写操作同谓词**:POST 插入前列校验;UPDATE/DELETE 生成 `WHERE <策略谓词> AND <用户过滤>`,受影响行数为 0 时返回 404,与记录不存在同形,防存在性探测。
- 列拒绝不在错误中区分「无权」与「不存在」;错误响应不回显 SQL。

## 5. 事件与自动化

- **捕获:logical replication(pgoutput)**。零表级 DDL、零触发器;要求 `wal_level=logical`(实例配置)与一个复制槽。不用 LISTEN/NOTIFY(需在目标库建触发器),不用轮询作主路径(滞后且丢 delete)。目标库给不了 logical 时降级 `polling` 模式(xmin/updated_at 水位),文档明标局限(丢 delete、秒级滞后)。
- **投递**:at-least-once;HMAC 签名头(secret_env 提供);指数退避重试(次数/上限可配);超限进死信。死信持久于本地嵌入库,经 CLI/MCP 查看与重放。重启后从复制槽确认位点续投,不丢不重(至少一次语义)。
- **cron**:5 字段表达式,到点投递 webhook(与表事件同一投递器、同一死信机制)。不执行命令、不跑脚本——无任意代码执行面。

## 6. MCP server

双传输:streamable HTTP(同进程,路径 `/mcp`)与 stdio(本地调试)。

工具集:`schema-read`(内省目录+策略摘要)、`items-query/create/update/delete`、`metadata-validate`、`metadata-diff`、`deadletter-list`、`deadletter-replay`。

铁律:MCP 与 REST 走同一条权限流水线;MCP 令牌绑定可配置角色,默认窄角色;`items-delete`、`deadletter-replay` 等高危工具可在配置中逐个禁用。Claude 的日常循环:读 `schema-read` → 改 YAML → `metadata-validate/diff` → 提交 → 热加载。

## 7. 错误处理与可观测

- 错误体统一 RFC 7807 problem-details;策略拒绝与 404 同形;SQLSTATE 映射(如 23505→409);参数校验错 400 带字段路径。
- 启动失败三分类退出码:metadata 校验错(2)/数据库不可达或漂移(3)/JWKS 不可达(4)。
- 请求路径无状态;事件投递状态持久;优雅停机:停止收新请求 → 排空在途投递 → 确认复制位点 → 退出。
- 可观测:slog 结构化日志;`/metrics` Prometheus;请求 ID 贯穿 REST 处理与事件投递日志;慢查询阈值日志;`plinth status` 输出健康摘要(连接、槽位、死信数、最近投递延迟)。

## 8. 测试策略

- **单元**:SQL 生成器 golden 测试(用 PostgREST 语义用例集);权限编译器属性测试;表达式解析器 fuzzing(go-fuzz 常驻 CI)。
- **集成**:testcontainers-go 起临时 PG,fixture 为 SilkLine 形态多租户 schema(organizations/users/invoices/buyers);**权限矩阵表驱动测试**:角色×表×列×行 的期望可见性全枚举,一格一测;假 webhook 接收器验证投递/重试/死信/重放闭环;漂移检测测试(改表结构后 diff 必须报)。
- **安全门禁**(发布阻塞项,非普通测试):注入攻击语料集(每个过滤操作符喂 hostile 输入,断言永远参数化);越权矩阵回归;JWKS 轮换/过期 token 用例。
- **CI**:gosec + staticcheck + lint;goreleaser 构建单二进制与 docker 镜像;vegeta 压测冒烟出基线 RPS 报告。

## 9. 工程化与发布

Apache-2.0;README 英文,文档英文+中文;README 致谢 PostgREST(语义规范与测试锚)与 Directus 调研启发的架构取舍,无代码复制。发版走 goreleaser + CHANGELOG + 语义化版本(v0.x 起)。仓库:`github.com/dayuer/plinth`。

## 10. 成功标准

1. SilkLine 生产真实流量经 Plinth 服务(替换至少一个手写 CRUD 端点,三个月内);
2. Claude 经 MCP 改 YAML→validate→diff→热加载的工作流日常可用;
3. 权限矩阵测试全绿且注入门禁在 CI 强制。

外部用户为 stretch goal,不计入验收。

## 11. 风险与对策

| 风险 | 对策 |
|---|---|
| SQL 生成器是最难件(关系嵌套、防注入) | PostgREST 语义当规范、其测试用例当验收;MVP 嵌套深度锁 1 |
| logical replication 依赖实例配置 | 降级 polling 模式文档化;SilkLine 自有机器可直接开 |
| 单实例无 HA | 明示边界;无状态请求路径 + 位点续投,重启即恢复 |
| 表达式语言表达力不够 | 只服务行过滤场景;复杂需求引导写 PG 视图(Plinth 内省视图如同表) |
| 自用项目维护精力有限 | 成功标准控制范围;非目标清单挡需求蔓延 |
