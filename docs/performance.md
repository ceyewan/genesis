# Fuzz 与性能测试

普通 `go test` 运行 Fuzz seed。探索性 Fuzz 使用独立命令，不需要 Docker：

```bash
go test ./cache -run '^Fuzz' -fuzz '^FuzzCacheSliceDestination$' -fuzztime=30s -parallel=2
go test ./idgen -run '^Fuzz' -fuzz '^FuzzParseGeneratorID$' -fuzztime=30s -parallel=2
go test ./mq -run '^Fuzz' -fuzz '^FuzzRedisHeaders$' -fuzztime=30s -parallel=2
```

## Microbenchmark

```bash
go test ./cache ./idem ./mq -run '^$' \
  -bench 'Benchmark(LocalKV|MultiLocalHit|IdemMemoryHit|MQMiddleware)' \
  -benchmem -benchtime=100ms -count=3
```

比较实现前后结果时固定提交、Go 版本、硬件、输入和并发度，采集多个样本，再用
benchstat 分析。具体测量结果随 PR 记录，不在主线长期追加历史性能表。

这些测试不设置硬件无关的绝对门槛，也不能用于承诺生产吞吐：

- Local 与 Multi 的输入类型不同，数字不能直接横向比较。
- 并发 benchmark 是吞吐换算，不是单请求延迟。
- Multi 使用远端替身，不包含 Redis、网络、冷回填或读写协调竞争。
- Memory 幂等命中不包含首次业务执行；MQ 成功路径不包含传输或重试等待。

应用容量仍需在真实后端和代表性负载下测量。
