# Container 设计文档

## 1. 目标与职责

`container` 是 Genesis 应用的“骨架”和“胶水层”，负责：

1. 统一管理所有组件与连接器的**依赖注入**；
2. 编排各模块的**生命周期 (Lifecycle)** 与 **启动顺序 (Phase)**；
3. 将 `config.Manager` 产生的 `AppConfig`、`clog.Logger`、`telemetry` 能力组装成一个可直接使用的应用容器；
4. 为业务代码提供一个稳定的访问入口（如 `app.DB`、`app.DLock` 等）。

业务代码只需要：

1. 使用 `pkg/config` 读入配置并绑定到 `AppConfig`；
2. 创建应用级 Logger；
3. 调用 `container.New(AppConfig, ...Option)` 获取容器实例并从中取用组件。

## 2. 核心概念

### 2.1 Container 结构

Container 聚合了所有“可被业务直接使用”的依赖：

- 基础能力：`clog.Logger`、`telemetry`（Meter/Tracer）、`config.Manager`（可选）；
- 连接器：如 `MySQL`, `Redis`, `Etcd`, `NATS` 等；
- 业务组件：如 `db`, `dlock`, `cache`, `mq`, `idgen`, `ratelimit` 等。

Container 本身并不关心具体业务逻辑，它只负责：

- 基于 `AppConfig` 组装出依赖图；
- 按 Phase 启动/关闭实现了 `Lifecycle` 接口的对象；
- 对外暴露已就绪的组件接口。

### 2.2 Lifecycle 与 Phase

`pkg/container/lifecycle.go` 定义了统一的生命周期接口：

```go
type Lifecycle interface {
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    Phase() int // Phase 越小越先启动
}
```

推荐的 Phase 分配（仅示意，可按需要微调）：

- `0`：基础日志 / Telemetry Provider / Config Manager（如需托管）；
- `10`：连接器（MySQL / Redis / Etcd / NATS 等）；
- `20`：业务组件（db / dlock / cache / mq / idgen / ratelimit 等）；
- `30`：传输层（HTTP / gRPC 服务器等）。

容器在 `Start` 时按 Phase 升序启动，在 `Close` 时按 Phase 逆序关闭。

## 3. 初始化流程

Container 的初始化流程与应用启动流程保持一致：

1. **Config Bootstrapping（在 Container 之外）：**
   - 使用 `config.NewManager(...)` 创建 `Manager`；
   - 调用 `Load + Validate`；
   - 调用 `Unmarshal(&AppConfig)` 得到强类型配置。

2. **构建应用级 Logger：**
   - 使用 `AppConfig.Log` 创建基础 Logger；
   - 通过 `WithNamespace(AppConfig.App.Namespace)` 附加服务级命名空间，例如 `user-service`。

3. **初始化 Telemetry：**
   - 基于 `AppConfig.Telemetry` 初始化 OTel Provider；
   - 导出 `metrics.Meter` 和 `trace.Tracer` 供后续组件使用；
   - Telemetry 通常实现 `Lifecycle`，Phase 较小（如 0）。

4. **初始化 Connectors：**
   - 读取 `AppConfig.Connectors` 中的配置；
   - 通过对应的 `connector.Manager` / Factory 创建或复用连接器实例；
   - 为每个连接器注入派生好的 Logger，例如：
     - `logger.WithNamespace("connector.mysql.primary")`；
     - `logger.WithNamespace("connector.redis.default")`；
   - 将连接器实例注册为 `Lifecycle` 对象，Phase 通常为 10。

5. **初始化 Components：**
   - 读取 `AppConfig.Components` 中的各组件配置；
   - 为每个组件准备 Dep：
     - 例如 dlock 的 Dep 包含 Redis/Etcd Connector 接口；
     - db 的 Dep 包含 MySQL Connector、分库分表规则等；
   - 调用各组件的 `New(dep, cfg, ...Option)`：
     - `WithLogger(appLogger)` —— 组件内部会在 Option 中附加 `<component>` 命名空间；
     - `WithMeter(meter)` / `WithTracer(tracer)`（按需）；
   - 注册组件为 `Lifecycle` 对象（如果实现了该接口），Phase 通常为 20。

6. **启动与关闭：**
   - Container 在 `New` 内部或显式 `Start` 中：
     - 对所有 `Lifecycle` 对象按 Phase 排序并依次调用 `Start`；
   - 在 `Close` 或 `Stop` 时：
     - 按 Phase 逆序调用 `Stop`，确保上层依赖先关闭。

## 4. 与其他模块的关系

### 4.1 与 Config

- Config 模块负责在 Container 之外完成配置加载与校验，并输出 `AppConfig`；
- Container 只消费 `AppConfig`（以及可选托管的 `Manager`）来初始化各模块；
- 若需要托管 `Manager` 的 Watch 等后台任务，可将其注册为 `Lifecycle` 对象，Phase 较小。

### 4.2 与 clog

- Application 级别的 Logger 由业务或启动代码通过 `clog.New(AppConfig.Log)` 创建；
- Container 只在此基础上派生命名空间：
  - 组件级：`appLogger.WithNamespace("dlock")`；
  - 连接器级：`appLogger.WithNamespace("connector.redis.default")`；
- 组件内部不再 new Logger，而是使用通过 Option 注入的派生 Logger。

### 4.3 与 Telemetry

- Telemetry Provider 在 Container 初始化早期被创建，通常作为 Phase 最小的一类模块；
- Container 负责将 `metrics.Meter` 和 `trace.Tracer` 通过 Option 传递给组件；
- 组件内部只依赖抽象接口，不关心 OTel 的具体实现或 Exporter 类型。

### 4.4 与 Connector / Components

- Container 使用 `AppConfig.Connectors` 与 `AppConfig.Components` 构建依赖图：
  - 先通过 Connector Factory/Manager 构建连接器实例；
  - 再将这些连接器作为 Dep 传入组件的 `New` 函数；
- Container 是连接器与组件的“装配者”，但不参与它们的具体业务逻辑。

## 5. 使用示例（简版）

```go
// main.go
cfgMgr := config.NewManager(config.WithPaths("./config"))
_ = cfgMgr.Load(ctx)
var appCfg AppConfig
_ = cfgMgr.Unmarshal(&appCfg)

logger := clog.New(appCfg.Log).WithNamespace(appCfg.App.Namespace)

app, err := container.New(appCfg,
    container.WithLogger(logger),
    container.WithConfigManager(cfgMgr),
)
if err != nil {
    panic(err)
}
defer app.Close()

// 业务代码通过 app.DB / app.DLock 等组件接口工作
```

