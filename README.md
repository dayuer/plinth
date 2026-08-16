# Plinth

**An agent-native, file-first data foundation that guests on your existing PostgreSQL.**

单二进制旁挂、对目标库零 DDL、metadata 全在 YAML、权限编译成参数化 SQL,内置 MCP server 供 AI agent 直接运维。

状态:**设计阶段**(尚无代码)。路线与取舍见决策记录,完整设计见 spec,立项依据的 Directus/开源平台调研见 research。

## 文档

- [设计文档 v1.0](docs/specs/2026-08-16-plinth-design.md) — 架构、metadata 格式、权限流水线、事件引擎、测试策略
- [立项决策记录](docs/decisions/2026-08-16-brainstorm-decisions.md) — 为什么自研、五个立项问答、放弃清单
- 调研:[Directus 深度调研](docs/research/directus-deep-dive.md) · [数据底座备选横评](docs/research/data-foundation-alternatives.md) · [来源清单](docs/research/sources.md)

## License

Apache-2.0(见 LICENSE)。查询语义以 [PostgREST](https://github.com/PostgREST/postgrest) 为规范锚,特此致谢。
