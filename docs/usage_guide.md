# Genesis 使用指南

## 快速启动
1. **准备环境**
   - Go 1.22+
   - Docker & Docker Compose（用于本地依赖）
2. **安装依赖**
   ```bash
   go mod tidy
   ```
3. **运行示例服务**（完成骨架后）
   ```bash
   go run ./cmd/server
   ```
4. **查看日志与指标**
   - 日志：标准输出或 `logs/` 目录
   - 指标：`http://localhost:9090/metrics`

## 目录速览
- `cmd/server/main.go`：服务入口，装配 `fx` 容器。
- `configs/config.yaml`：默认配置。
- `pkg/`：接口契约，业务层仅依赖此目录。
- `internal/`：组件实现，不直接暴露给外部。

## 依赖注入示例
```go
func main() {
    app := fx.New(
        config.Module(),
        log.Module(),
        cache.Module(),
        db.Module(),
        mq.Module(),
        uid.Module(),
        metrics.Module(),
        fx.Invoke(registerHooks),
    )

    app.Run()
}
```

## 配置加载
```go
func registerHooks(lc fx.Lifecycle, cfg config.Provider) {
    lc.Append(fx.Hook{
        OnStart: func(ctx context.Context) error {
            return cfg.UnmarshalKey("server", &serverCfg)
        },
    })
}
```

## 常用命令（建议后续加入 Makefile）
- `go test ./...`：运行单测。
- `go test -run Example ./pkg/...`：生成文档示例输出。
- `docker compose -f deployments/local/docker-compose.yml up -d`：启动本地依赖。

## 组件集成提示
- 日志：使用 `log.Logger` 注入 `context.Context`，避免全局变量。
- 缓存：优先使用 `cache.Cache` 接口，结合 `once` 保证幂等。
- 数据库：`db.Transaction` 提供 `ExecTx`，事务内部禁止阻塞操作。
- 限流/熔断/幂等：以中间件形式接入 HTTP/gRPC，保持业务函数干净。
- UID：服务启动时自动获取 WorkerID，关闭时释放。

## 开发习惯
- 新增组件后更新 `docs/` 与 `configs/` 示例。
- 使用 `internal/testing` 存放集成测试工具与假依赖。
- 重要设计决策记录在 `docs/ADR/<id>-<title>.md`（后续建立）。

## 调试技巧
- 在 `configs/config.yaml` 设置日志级别为 `debug`，观察组件输出。
- 使用 `pprof` 进行性能分析：`go tool pprof http://localhost:6060/debug/pprof/profile`。
- 对外部依赖提供本地 docker-compose 清单，保证可复现。
