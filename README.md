# Plinth

**An agent-native, file-first data foundation that guests on your existing PostgreSQL.**

单二进制旁挂、对目标库零 DDL、metadata 全在 YAML、权限编译成参数化 SQL,内置 MCP server 供 AI agent 直接运维。

状态:**v0.1.0 地基完成**(只读 SQL BFF 五命令全接线,全层测试含 Docker 集成);Plans 2+(写入端、MCP、事件引擎)待启动。路线与取舍见决策记录,完整设计见 spec,立项依据的 Directus/开源平台调研见 research。

## 快速上手

```bash
go build -o bin/plinth ./cmd/plinth

# plinth.yml —— database.url 必须指向只读角色
cat > plinth.yml <<'YAML'
database:
  url: postgres://ro_user:ro_pass@host:5432/appdb
auth:
  tokens:
    web-bff: ${WEB_BFF_TOKEN}
audit:
  path: audit/executions.jsonl
  mask_params: [status]
YAML

# queries/invoice-list.sql —— 头部声明元数据,正文是命名参数 SQL
cat > queries/invoice-list.sql <<'SQL'
-- plinth: name: invoice-list
-- params: org_id:int:required | status:str:optional
-- allow-tokens: web-bff
SELECT id, status FROM invoices WHERE org_id = :org_id AND (:status::text IS NULL OR status = :status) ORDER BY id
SQL

./bin/plinth validate                                # 离线:加载并检查全部查询(exit 2 = 元数据错)
./bin/plinth test --query invoice-list --param org_id=1   # 真库单查询冒烟,行数封顶 5
./bin/plinth semantics pull                          # 拉取 datasets.yml,sha256 前 12 位作快照版本
./bin/plinth serve --addr :8080                      # POST /q/invoice-list,头 X-Plinth-Token;SIGHUP 热加载查询
./bin/plinth status                                  # 查询清单 + 最近审计尾部
```

退出码:`0` 成功,`2` 元数据/查询定义错,`3` 数据库/运行时错。`validate` 会对 pinned 旧语义快照的查询打 drift 警告(不失败),提醒人工按新语义复审 SQL。

## 文档

- [设计文档 v1.0](docs/specs/2026-08-16-plinth-design.md) — 架构、metadata 格式、权限流水线、事件引擎、测试策略
- [立项决策记录](docs/decisions/2026-08-16-brainstorm-decisions.md) — 为什么自研、五个立项问答、放弃清单
- 调研:[Directus 深度调研](docs/research/directus-deep-dive.md) · [数据底座备选横评](docs/research/data-foundation-alternatives.md) · [来源清单](docs/research/sources.md)
- 变更:[CHANGELOG.md](CHANGELOG.md)

## License

Apache-2.0(见 LICENSE)。查询语义以 [PostgREST](https://github.com/PostgREST/postgrest) 为规范锚,特此致谢。
