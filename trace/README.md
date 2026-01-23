# trace - OpenTelemetry 链路追踪封装

[![Go Reference](https://pkg.go.dev/badge/github.com/ceyewan/genesis/trace.svg)](https://pkg.go.dev/github.com/ceyewan/genesis/trace)

trace 初始化全局 TracerProvider，连接 Tempo/Jaeger 等 OTLP 后端。

## 快速开始

```go
import "github.com/ceyewan/genesis/trace"

// 初始化，返回 shutdown 函数
shutdown, err := trace.Init(&trace.Config{
    ServiceName: "my-service",
    Endpoint:    "localhost:4317",
    Sampler:     1.0,
})
defer shutdown(context.Background())
```

## API

```go
// 初始化 TracerProvider
func Init(cfg *Config) (func(context.Context) error, error)

// 创建不导出的 Provider（仅生成 TraceID）
func Discard(serviceName string) (func(context.Context) error, error)

// 默认配置
func DefaultConfig(serviceName string) *Config
```

## 使用

```go
tracer := otel.Tracer("my-component")
ctx, span := tracer.Start(ctx, "operation")
defer span.End()

// 添加属性
span.SetAttributes(attribute.String("key", "value"))
```

## License

[MIT License](../../LICENSE)
