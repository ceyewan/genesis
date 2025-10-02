# Breaker 组件实施手册

## 功能边界
- 为外部依赖（HTTP、RPC、数据库等）提供熔断、降级与自恢复策略。
- 支持同步调用链的请求级别保护。
- 可插入到客户端中间件链路，无业务侵入。

## 代码位置
- 接口：`pkg/middleware/breaker.go`
- 默认实现：`internal/middleware/breaker/gobreaker`
- 监控指标：`internal/middleware/breaker/metrics.go`

## 开发步骤
1. **接口定义**：描述状态监听、执行包装、事件订阅等核心方法。
2. **策略配置**：支持基于错误率/超时的多种触发条件，含默认阈值。
3. **Gobreaker 封装**：基于 `sony/gobreaker` 提供标准实现，同时注入日志与指标。
4. **DI 模块**：提供 `fx` Module，允许针对不同下游创建独立实例。
5. **示例集成**：在 `usage_guide.md` 中演示 HTTP 客户端包装方式。

## 测试要点
- 单测：验证半开/打开/关闭状态转换、熔断后降级函数执行。
- 集成：结合假服务注入错误，确保熔断恢复时间符合预期。
- 指标：上报状态切换次数、失败率、熔断持续时间。

## 验收清单
- 默认配置能在 5 分钟内恢复健康连接。
- 降级逻辑自带超时保护，避免卡死。
- 支持通过配置调整不同服务的阈值。

## 后续演进
- 引入滑动窗口指标聚合以降低抖动。
- 支持 gRPC `UnaryClientInterceptor` 与 `StreamClientInterceptor`。
