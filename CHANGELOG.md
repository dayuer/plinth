# Changelog

## v0.1.0 — 2026-08-16

First usable cut: the read-only SQL BFF foundation, wired end to end.

- **CLI 五命令全接线**:`validate`(离线全量检查)、`test`(单查询真库冒烟,行数封顶 5)、`pull` / `semantics pull`(刷新语义快照)、`serve`(HTTP BFF)、`status`(查询清单 + 审计尾部)。退出码契约:2 = 元数据/查询定义错,3 = 数据库/运行时错。
- **只读双保险**:load-time readcheck 关键词扫描 —— DML/DDL 关键字之外还按函数族前缀禁用(如 `pg_*` 咨询锁/统计重置、`lo_*` 大对象写入),连接串指向只读角色由部署方保证。
- **命名参数**:`:name` 重写为 `$N`;声明式类型强制(int/float/bool/str)、默认值、缺省可选参数绑定 SQL NULL;SQL 参数与声明必须严格相等集。
- **JSONL 执行审计 + 参数脱敏**:每请求一行(`audit/executions.jsonl`),`mask_params` 列出的参数记为 `***`;审计写失败只记日志,不打断请求。
- **语义快照与 drift 警告**:`pull` 把拉取内容的 sha256 前 12 位写为快照版本;`validate` 对 pinned 旧快照的查询打警告,提醒按新语义复审 SQL。
- **SIGHUP 热加载**:serve 中重载查询注册表走原子指针交换;加载失败保留旧注册表继续服务,绝不部分降级。
- **CI**:gofmt / vet / test(-race) 之外新增独立 integration job(Docker + testcontainers,`-tags integration`)。

此前(Tasks 1–9):plinth.yml 配置加载(${VAR} 展开、严格键)、查询文件头解析、注释/字符串感知的 SQL 扫描与重写、readcheck 语料固定、注册表加载、执行引擎(超时 + 行上限)、审计器、POST /q/{name}(静态 token、RFC 7807、审计钩子)。
