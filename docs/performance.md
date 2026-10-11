# 输入模糊测试与性能基线

这些测试用于发现解析边界和记录优化前的成本，不设置硬件无关的绝对门槛。
普通 `go test` 运行 Fuzz seed；探索性 fuzz 使用独立命令，不需要 Docker。

```bash
go test ./cache -run '^Fuzz' -fuzz '^FuzzCacheSliceDestination$' -fuzztime=30s -parallel=2
go test ./idgen -run '^Fuzz' -fuzz '^FuzzParseGeneratorID$' -fuzztime=30s -parallel=2
go test ./mq -run '^Fuzz' -fuzz '^FuzzRedisHeaders$' -fuzztime=30s -parallel=2
```

覆盖缓存 slice 反射目标与畸形 JSON、Snowflake ID 解析/重建、Redis Headers 解码和
副本隔离。序列号 int64 精度、上限与 TTL 使用真实 Redis 边界测试保护。

## 可复跑的 microbenchmark

```bash
go test ./cache ./idem ./mq -run '^$' \
  -bench 'Benchmark(LocalKV|MultiLocalHit|IdemMemoryHit|MQMiddleware)' \
  -benchmem -benchtime=100ms -count=3
```

首次基线：Go `1.26.1`、macOS `darwin/arm64`、Apple M3、GOMAXPROCS=8，三轮范围：

| 路径 | ns/op | B/op | allocs/op |
| :--- | :--- | :--- | :--- |
| Local JSON Set | 482–510 | 278–279 | 7 |
| Local JSON Get | 738–742 | 601 | 14 |
| Multi 本地命中（并发） | 115–142 | 165 | 3 |
| Memory 幂等结果命中（并发） | 311–322 | 536 | 9 |
| MQ Recover/Logging/Retry 成功路径 | 86–94 | 128 | 1 |

Local 使用小型 map，Multi 使用字符串，因此两行不能直接比较。并发数字是 Go
benchmark 吞吐换算，不是单请求延迟。Multi 远端是测试替身，基线不包括 Redis、
网络、冷回填或协调锁写争用；Memory 命中不代表首次业务执行的锁竞争成本；MQ
成功路径不包括消息传输或重试等待。

修改实现前后在同硬件、Go 版本、输入与并发下采集多个样本，再用 benchstat 分析。
应用容量应在真实后端和代表性负载下测量，不据本表承诺生产吞吐。
