# Telemetry 模块审查报告

**审查时间:** 2025-11-23
**审查对象:** `pkg/telemetry`, `internal/telemetry`, `examples/telemetry`
**参考文档:** `docs/telemetry-design.md`

## 1. 总体评价

`telemetry` 模块实现了基于 OpenTelemetry (OTel) 的指标 (Metrics) 和链路追踪 (Tracing) 能力。
实现质量**很高**，不仅提供了简洁的统一接口，还完整实现了 OTel Provider 的初始化、Exporter 配置以及 HTTP/gRPC 的自动拦截器。它已经具备了被集成到 `container` 和其他组件中的所有条件。

| 维度 | 评分 | 说明 |
|---|---|---|
| **设计一致性** | ✅ 高 | 核心功能与设计文档一致，结构上有轻微（但合理的）调整 |
| **功能完备性** | ✅ 高 | 支持 Counter/Gauge/Histogram, Span Start/End, 多种 Exporter (Prometheus, OTLP, Zipkin, Stdout) |
| **代码质量** | ✅ 高 | 接口抽象清晰，底层 OTel 封装得当，隐藏了 SDK 复杂性 |
| **Example 覆盖** | ✅ 高 | 示例非常详尽，涵盖了基础用法、自定义指标和完整的 HTTP/gRPC 服务集成 |

---

## 2. 详细发现

### 2.1 优点 (Strengths)

1. **接口抽象 (Abstraction):**
    * 通过 `pkg/telemetry/types` 定义了与 OTel 无关的纯 Go 接口 (`Meter`, `Tracer`, `Span`)。
    * 这使得业务代码与具体的监控实现解耦，符合 Genesis "接口驱动" 的设计哲学。

2. **开箱即用 (Out-of-the-box):**
    * `telemetry.New(cfg)` 一站式完成了 MeterProvider 和 TracerProvider 的初始化。
    * 自动配置了全局 Propagator (`TraceContext`, `Baggage`)，确保上下文传播正常工作。

3. **中间件集成 (Middleware):**
    * 提供了 `HTTPMiddleware` (Gin) 和 `GRPCServer/ClientInterceptor`，大大降低了业务接入成本。

4. **示例丰富 (Documentation):**
    * `examples/telemetry/main.go` 是一个教科书级的示例，展示了如何在复杂的真实场景（HTTP 调用 gRPC）中使用遥测。

### 2.2 偏差 (Deviations)

1. **包结构调整:**
    * **设计:** 建议 `pkg/metrics` 和 `pkg/trace` 作为独立包。
    * **实现:** 采用了 `pkg/telemetry` 作为统一入口，`pkg/telemetry/types` 定义接口。
    * **评价:** 这种调整是合理的。统一入口 (`telemetry.New`) 简化了初始化流程，且避免了用户需要分别初始化 Metrics 和 Tracing 的麻烦。

### 2.3 待办与建议 (Recommendations)

1. **集成到 Container (High Priority):**
    * 正如 `container` 审查报告中指出的，`container` 模块目前尚未集成 `telemetry`。
    * **建议:** 修改 `container.New`，在初始化早期调用 `telemetry.New`，并将获取到的 `Meter` 和 `Tracer` 注入到后续初始化的组件中。

2. **Clog 自动关联 (Log Correlation):**
    * 设计文档提到 `clog` 应自动检测 Context 中的 TraceID。
    * **建议:** 验证 `clog` 是否已通过 `ContextField` 机制实现了这一点，或者是否需要专门的适配器来从 OTel 的 `SpanContext` 中提取 TraceID。

3. **Sampler 配置:**
    * 目前 `Config` 中只支持 `trace_id_ratio` 和 `always_on/off`。
    * **建议:** 未来可以考虑支持 `ParentBased` 采样策略，这是生产环境中更常用的策略（即如果上游已经采样，下游也跟随采样）。

## 3. 结论

`telemetry` 模块是一个**成熟且高质量**的组件。它已经准备好承担 Genesis 框架的可观测性基座角色。
接下来的核心工作是将其**组装**进 `container` 中，打通 "Telemetry -> Container -> Component" 的依赖链路。
