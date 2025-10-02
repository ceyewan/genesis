# Metrics 组件实施手册

## 功能边界
- 为框架提供统一的指标采集入口，支持 Prometheus 暴露。
- 提供简单的计数、Gauge、直方图封装，统一标签风格。
- 集成请求链路与组件级指标。

## 代码位置
- 接口：`pkg/metrics/metrics.go`
- 默认实现：`internal/metrics/prometheus`
- 公共标签定义：`pkg/metrics/tags.go`

## 开发步骤
1. **接口确认**：提供 `Counter`, `Gauge`, `Histogram`, `Timer` 等基础 API。
2. **标签规范**：定义核心标签（service, module, instance, trace_id）。
3. **Prometheus 实现**：封装注册与暴露 HTTP Handler，支持自定义采样桶。
4. **集成钩子**：
   - 日志：记录指标注册错误。
   - 中间件：默认追踪请求耗时与状态码分布。
5. **DI Module**：暴露 `fx` 模块，提供 `StartServer` 选项。

## 测试要点
- 单测：指标注册、重复注册处理、标签数量校验。
- 集成：启动内置 HTTP 端点，确认 `/metrics` 输出符合 Prometheus 格式。
- 性能：验证高频调用下的分配次数。

## 验收清单
- 框架组件调用 metrics 时只需依赖 `pkg/metrics`。
- 标签必须控制在 4-6 个以内，提供默认缺省值。
- 文档示例展示自定义指标与 HTTP 暴露方式。

## 后续演进
- 支持 OpenTelemetry exporter。
- 引入采样与聚合策略应对高吞吐场景。
