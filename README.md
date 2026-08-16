# Plinth

**An agent-native, file-first SQL BFF gateway that guests on your existing PostgreSQL.**

单二进制旁挂、对目标库零 DDL、命名查询即文件(注释头元数据 + 参数化 SQL)、静态 token 鉴权、JSONL 执行审计;Claude/agent 经 CLI 五命令直接运维。

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
semantics:
  pull_command: ./scripts/pull-semantics.sh
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

- [设计文档 v2.0](docs/specs/2026-08-16-plinth-design.md) — 查询文件格式、只读双保险、审计、语义快照
- [立项决策记录](docs/decisions/2026-08-16-brainstorm-decisions.md) — 为什么自研、五个立项问答、放弃清单
- 调研:[Directus 深度调研](docs/research/directus-deep-dive.md) · [数据底座备选横评](docs/research/data-foundation-alternatives.md) · [来源清单](docs/research/sources.md)
- 变更:[CHANGELOG.md](CHANGELOG.md)

## 已知限制(v0.1)

- SQL 方言子集:标签美元引号(`$tag$…$tag$`)、嵌套块注释、数组切片(`arr[1:2]`)不支持;识别不了的结构一律「过拒不过放」——readcheck 拒绝或参数集校验报错,绝不放行。
- token 比较非常数时间(静态 token 内网模型)。
- 审计 JSONL 不轮转,先手动归档。
- 拒绝请求(401/403/404/400)审计已记录(status=`denied` / `bad-request`)。

## License

Apache-2.0(见 LICENSE)。
