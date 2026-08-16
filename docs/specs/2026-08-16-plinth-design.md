# Plinth 设计文档 v2.0

日期:2026-08-16 · 状态:已批准(v2 转向版;v1.0「数据底座四件套」见 git 历史,未实现即被替代)
决策链见 `../decisions/2026-08-16-brainstorm-decisions.md`(含转向记录)。

一句话定位:**AI 维护、文件优先、给存量 PostgreSQL 当客人的 SQL BFF 网关**——Claude 依据 Lovrabet 数据集语义写命名 SQL,SQL 以文件入库(git 即变更审计),业务服务带 token 调用,执行全留痕。

## 0. 背景与转向

v1 调研(Directus/开源横评,见 `../research/`)确立「文件优先、零 DDL、agent 原生、Go、Apache-2.0」的底盘哲学。v2 把目标收窄到用户真实要的闭环:**SQL 不再由程序员手写维护,由 Claude 依据语义撰写、随业务变化自动调整,全程可审计**。v1 的表格 CRUD、行级权限表达式语言等设计随转向作废;底盘哲学与工程框架全部保留。

## 1. 目标与非目标

**目标**

1. 命名查询注册中心:`queries/*.sql` 一个文件一个查询,注释头声明元数据;
2. HTTP BFF:业务服务带静态 token 调 `POST /q/{name}`,参数化只读执行,返回 JSON;
3. 双轨审计:变更审计=git 历史;执行审计=JSONL(调用方/查询/参数/行数/耗时/状态,支持按参数名脱敏);
4. 语义同步:`plinth semantics pull` 从 Lovrabet CLI 导出数据集语义快照入库,查询声明所依据的快照版本,漂移即告警;
5. agent 工作循环:pull → 写/改 SQL → validate(离线)→ test(真库试跑)→ commit → 热加载。

**非目标**

写操作(格式预留 `mode` 位,非 `read` 拒绝加载)、表格 CRUD、用户级鉴权(JWT)、GraphQL、UI、多数据库、多实例 HA、MCP server(留 v0.3,CLI+文件循环已可闭环)、SQL 结果缓存。

## 2. 总体架构

单二进制 Go 进程(CGO-free),内网部署:caddy vhost → Plinth → SilkLine 生产 PG(**独立只读数据库角色**,只有 SELECT 授权)。

```
queries/*.sql ─→ 加载器(注释头解析)─→ 注册表 ─┐
                                              ├→ POST /q/{name}
plinth.yml(token/审计/超时/语义命令)──────────┤    → token 鉴权 + allow-tokens
                                              │    → 参数校验(类型/必填)
semantics/*.yml(Lovrabet 快照)←─ pull ←──────┤    → :name 重写为 $N
                                              │    → pgx 只读执行(超时/行上限)
audit/executions.jsonl ←──────────────────────┘    → JSON + X-Plinth-Rows/Duration
```

组件:`queryfile`(注释头解析)、`registry`(加载与静态校验)、`sqlscan`(注释/字符串感知扫描:剥离与参数发现)、`readcheck`(只读闸)、`exec`(命名参数重写+执行)、`server`(HTTP+鉴权)、`audit`(JSONL 写入)、`cli`(validate/test/pull/serve/status)。

硬约束:对目标库零 DDL(只读角色连查询都改不了结构);自带状态仅审计 JSONL,不进业务库。

## 3. 查询文件格式

```sql
-- plinth: name: invoice-list
-- params: org_id:int:required | status:str:optional | limit:int:50
-- allow-tokens: web-bff, report-worker
-- semantics: dataset=invoices snapshot=2026-08-16a
-- timeout-ms: 5000
-- desc: 按机构列发票;依据 Lovrabet invoices 数据集语义
SELECT id, buyer_id, status, amount_total, currency
FROM invoices
WHERE org_id = :org_id
  AND (:status::text IS NULL OR status = :status)
ORDER BY id DESC
LIMIT :limit
```

规范:头部为文件起始的连续 `-- key: value` 注释行;`name` 必填且必须等于文件名(去 `.sql`);`params` 用 `|` 分隔,每项 `name:type:required|optional|default 值`,type ∈ int|str|bool|float;`allow-tokens` 必填(逗号分隔的调用方名);`semantics` 格式 `dataset=X snapshot=Y`;`timeout-ms` 可选(默认 5000);`mode` 未声明视为 `read`,声明为 `write` 则**拒绝加载**(扩展位,防抢跑)。正文为 `:name` 命名参数 SQL;`::` 转型语法不受影响;不支持 `E''` 字符串与字符串内反斜杠(加载即报错)。

## 4. 运行时流水线

`POST /q/{name}`,头 `X-Plinth-Token: <token>`:

1. token 匹配配置表得调用方名(`plinth.yml` 的 `auth.tokens`,name→token 环境变量展开);
2. 查询存在且 `allow-tokens` 含该调用方,否则 404/403;
3. JSON body 参数校验:未知参数 400;required 缺失 400;类型按声明强校验(int 接受整值 JSON number→int64;float→float64;bool;str);optional 缺失取 default,无 default 则绑 NULL;
4. `:name` 重写为 `$N`,按首现顺序绑定参数;
5. 执行:ctx 语句超时(查询级或默认),行数上限默认 10000,超限报错不截断;
6. 响应:`{"rows":[...]}` + `X-Plinth-Rows` + `X-Plinth-Duration-Ms`;错误统一 RFC 7807 problem-details;404 与「查询不存在」「无权」区分(内网服务间语义明确优先,不做人造模糊)。

热加载:SIGHUP 或文件监听重载注册表;重载失败保留旧表并告警,绝不半载。

## 5. 只读与安全(双保险)

- **静态检查(加载闸)**:基于 `sqlscan` 剥离注释与字符串内容后的文本判定——必须以 `SELECT`/`WITH` 开头;禁止一切 `;`(多语句);禁词表全词匹配(INSERT/UPDATE/DELETE/MERGE/CREATE/ALTER/DROP/TRUNCATE/GRANT/REVOKE/COPY/CALL/DO/SET/RESET/VACUUM/REINDEX/INTO/LOCK/LISTEN/NOTIFY/PREPARE/EXECUTE/DISCARD/IMPORT/LOAD/CHECKPOINT/REASSIGN 及危险函数 PG_READ_FILE/PG_SLEEP/LO_IMPORT 等)。字符串内容已剥离,关键词藏在字符串里不会误判也不会漏判;引号标识符保守保留(内容含禁词即拒)。检查器为独立函数 `readcheck.Check(sql) error`,v0.2 可换完整 parser 而不动调用方。
- **运行时兜底**:连接串使用只授予 SELECT 的独立数据库角色;每查询语句超时;行数上限。两道闸任一道独立成立。
- 注入面:参数永远走 pgx 绑定,SQL 文本中不出现任何运行时拼接的值。

## 6. 审计

- **变更审计 = git**:每次 Claude 改 SQL 即 commit,`semantics` 头声明所依据快照,diff 可审。
- **执行审计 = JSONL 追加**(`audit.path`):`{ts, caller, query, params, rows, ms, status, err}`;`audit.mask_params` 列出的参数名记录为 `"***"`;文件按天滚动(v0.2 轮转,先手动归档);写入失败记日志、不阻断请求(可用性优先,丢一条审计可容忍,写入器持锁串行)。

## 7. 语义同步(Lovrabet)

`plinth.yml` 配 `semantics.pull_command`(外部命令,默认指向 lovrabet CLI 的数据集导出);`plinth semantics pull` 执行该命令,产物落 `semantics/*.yml`,并写 `semantics/snapshot.txt`(内容哈希作快照版本)。查询头部 `semantics: dataset=X snapshot=Y`;`plinth validate` 校验:引用的 dataset 在快照中存在(缺→错误),snapshot ≠ 当前快照版本(**→ 告警不失败**,提示 Claude 复查 SQL)。运行时零依赖 Lovrabet;pull 命令可替换为任意脚本(测试用 fixture 脚本)。

## 8. agent 工作循环

**CLI 是第一 agent 接口**:`validate / test / pull / serve / status` 五个子命令即 Claude Code 的操作面,经 Bash 直接调用;输出为可直接阅读的文本,退出码 0/2/3 可判读(成功/元数据错/数据库错),无隐藏状态。MCP server 是 v0.3 的可选第二封面,agent 闭环不依赖它。

```
Claude: plinth semantics pull            # 语义快照更新
      → 读 semantics/,发现受影响查询(validate 告警)
      → 写/改 queries/*.sql
      → plinth validate                  # 格式/参数/只读/语义引用,离线
      → plinth test --query invoice-list # 真库、只读角色、默认参数试跑
      → git commit                       # 变更审计
      → SIGHUP                           # 热加载,业务即刻可用
```

## 9. 测试策略

- 单元:`queryfile` golden 解析(含畸形);`sqlscan` 状态机语料(注释/字符串/$$/::/E'');`readcheck` 攻击语料(多语句、CTE 内 DELETE、SELECT INTO、字符串藏关键词);`exec` 参数强转表驱动;`audit` 脱敏断言。
- 集成(testcontainers,fixture 同 SilkLine 形态):注册表全链加载;HTTP 鉴权矩阵(未知 token/无权/ok);参数校验矩阵;执行与行上限;审计落盘内容;热加载。
- 安全门禁(CI 阻塞):readcheck 语料全过;`sqlscan` native fuzz(字符串/注释状态机不炸、参数发现不漏)。

## 10. 工程化与发布

同 v1:Apache-2.0、goreleaser 单二进制 + docker、README 英文 + 中文文档、CHANGELOG、语义化版本。仓库 `github.com/dayuer/plinth`。

## 11. 成功标准

1. 一个真实业务查询(如 SilkLine 发票列表)经 Plinth 服务、被真实调用方调用;
2. Claude 改 SQL→validate→test→commit→热加载 全循环无人工编辑 SQL;
3. 执行审计能回答「哪个服务、何时、用什么参数、跑了哪个查询、返回多少行」。

## 12. 风险与对策

| 风险 | 对策 |
|---|---|
| SQL 质量依赖 Claude | validate(静态)+ test(真库试跑)双闸;git 审计可回滚 |
| 简化静态检查存在绕过面 | 只读角色独立兜底;v0.2 换完整 parser 的接口已留 |
| Lovrabet CLI 不可用/变动 | 快照化解耦,运行时零依赖;pull_command 可配置可替换 |
| 审计文件无限增长 | 按天滚动 v0.2;先手动归档 |
| 查询拖垮库 | 语句超时 + 行数上限 + 只读角色;慢查询进执行审计(ms 字段) |
