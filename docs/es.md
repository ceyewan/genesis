# ES 组件实施手册

## 功能边界
- 提供面向 Elasticsearch/OpenSearch 的查询与写入封装。
- 支持索引管理、批量写入、查询 DSL 构建。
- 默认实现以 Elasticsearch v8 为基准，可扩展多后端。

## 代码位置
- 接口：`pkg/es/es.go`
- 实现：`internal/es/elasticsearch`
- 辅助工具：`internal/es/elasticsearch/indexer.go`

## 开发步骤
1. **接口设计**：定义索引管理（Create/Update/Exists）、文档 CRUD、滚动查询等能力。
2. **配置结构**：地址、认证、重试策略、批量大小。
3. **客户端封装**：基于官方 Go SDK，加入请求重试与超时控制。
4. **索引模板管理**：支持从 `configs/es` 读取模板并自动创建。
5. **DI Module**：提供 `fx` 注册，支持多索引场景的独立客户端。

## 测试要点
- 单测：DSL builder 输出、错误码转换、批量写入分片逻辑。
- 集成：结合 docker-compose 启动 ES，验证索引创建与滚动查询。
- 观测：记录请求耗时、失败类型、重试次数。

## 验收清单
- 默认实现具备幂等写入（基于文档 ID）。
- 清晰区分客户端错误（4xx）与服务端错误（5xx）。
- 错误返回时包含索引、文档 ID、trace_id。

## 后续演进
- 增加 OpenSearch 适配。
- 引入异步批量写入管道，提升写入吞吐。
