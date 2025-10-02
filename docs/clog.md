# Clog 组件实施手册

## 功能边界
- 提供结构化日志接口，支持分级输出与上下文字段。
- 默认实现基于 `zap`，支持 JSON/console 编码。
- 集成 TraceID、RequestID 等跨服务字段。

## 代码位置
- 接口：`pkg/log/logger.go`
- 配置：`pkg/log/config.go`
- 默认实现：`internal/log/zap`

## 开发步骤
1. **接口确认**：定义 `Debug/Info/Warn/Error`、`With`、`Sync` 等方法，并支持 `context.Context`。
2. **配置**：支持最小配置（级别、输出、编码），并留出扩展字段（钩子、采样率）。
3. **Zap 实现**：封装全局字段注入、error stack 展开、选项链式组合。
4. **DI Module**：在 `internal/log/zap/module.go` 提供 `fx` 注入函数。
5. **全局使用示例**：编写 `pkg/log/example_test.go`，展示基础用法和字段注入。

## 测试要点
- 单测：验证日志级别过滤、生效字段、Sync 行为。
- 集成：与 `config` 组件联动，确认配置热更新后日志级别实时生效。
- 性能：简单基准测试，记录在文档中。

## 验收清单
- 默认输出具备时间戳、级别、消息、trace_id。
- 错误日志自动展开 error stack。
- 支持将日志导向多输出（文件、STDERR）。

## 后续演进
- 引入 `zerolog` 或 `slog` 适配器，验证多实现并存的可行性。
- 与指标组件联动记录日志速率。
