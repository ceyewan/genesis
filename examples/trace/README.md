# trace 示例

演示 OpenTelemetry 链路追踪的用法。

## 运行

```bash
# 启动服务
cd examples/trace
go run main.go

# 访问
curl http://localhost:8081/ping
```

## 需要 Collector

需要运行 OTLP Collector（如 Tempo 或 Jaeger）才能看到链路数据：

```bash
# 使用 Tempo
docker run -d -p 4317:4317 -p 4318:4318 \
  -v $(pwd)/tempo.yaml:/etc/tempo.yaml \
  grafana/tempo:latest \
  -config.file=/etc/tempo.yaml
```

## 内容

- 自动 HTTP 追踪（通过 otelgin 中间件）
- 手动创建子 Span
- 日志与 Trace 关联
