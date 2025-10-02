# UID 组件实施手册

## 功能边界
- 生成全局唯一、自增趋势 ID，满足排序与分库需求。
- 默认实现 Snowflake，可扩展 UUID、Segment 等策略。
- 支持 WorkerID 自动分配与失效回收。

## 代码位置
- 接口：`pkg/uid/generator.go`
- 配置：`pkg/uid/config.go`
- 默认实现：`internal/uid/snowflake`
- WorkerID 管理：`internal/uid/snowflake/allocator.go`

## 开发步骤
1. **接口确认**：提供 `Generate()`、`Batch(n)` 等方法，支持错误返回。
2. **配置**：包含 epoch、worker bits、sequence bits、防时钟回拨策略。
3. **Snowflake 实现**：
   - 保证线程安全，记录上一毫秒与序列值。
   - 时钟回拨时触发等待或使用备用 worker。
4. **WorkerID 分配**：
   - 依赖 `coord` 组件，使用带 TTL 的租约注册。
   - 提供随机化抢占策略，防止热点。
5. **DI Module**：封装启动与关闭逻辑，集成健康检查。

## 测试要点
- 单测：唯一性、顺序性、批量分配、回拨处理。
- 集成：多实例并发启动，验证 WorkerID 唯一。
- 性能：基准测试单实例生成速度，记录在文档。

## 验收清单
- WorkerID 释放后可被其他实例重新获取。
- 暴露指标：生成速率、冲突次数、回拨次数。
- 使用示例写入 `usage_guide.md`。

## 后续演进
- 提供可配置的 Segment（DB 自增）实现。
- 暴露 REST/RPC 接口，供非 Go 服务调用。
