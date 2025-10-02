# Ratelimit 组件实施手册

## 功能边界
- 提供限流能力，支持单机令牌桶与分布式限流。
- 可作为 HTTP/gRPC 中间件使用。
- 支持动态调节阈值与监控采集。

## 代码位置
- 接口：`pkg/middleware/ratelimit.go`
- 单机场景：`internal/middleware/ratelimiter/local`
- 分布式：`internal/middleware/ratelimiter/redis`

## 开发步骤
1. **接口设计**：定义 `Allow`, `Wait`, `WithContext` 等方法。
2. **配置结构**：速率、容量、冷却时间、分布式标识。
3. **本地实现**：基于 `golang.org/x/time/rate` 封装。
4. **分布式实现**：使用 Redis + Lua 脚本/令牌桶算法，支持滑动窗口统计。
5. **中间件集成**：提供 HTTP/Gin 与 gRPC 示例。
6. **动态调节**：结合 `config` 或 `coord` Watch 实现阈值热更新。

## 测试要点
- 单测：边界速率、突发流量、阻塞等待行为。
- 集成：结合 Redis 验证分布式限流一致性。
- 指标：记录拒绝率、等待时间、当前速率。

## 验收清单
- 支持白名单/黑名单扩展接口。
- 限流失败时返回可读错误并记录日志。
- 提供默认监控指标与告警建议。

## 后续演进
- 增加基于令牌补偿的公平调度策略。
- 支持多区域配额同步。
