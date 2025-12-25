# refactor(metrics): 优化 API 设计，对齐 clog 设计风格

**状态**: 已完成 ✅

## 问题描述

当前 `metrics` 组件的 API 设计存在以下问题：

### 1. Noop 模式设计与 clog 不一致

当前通过 `cfg.Enabled` 参数控制 noop 行为：

```go
cfg := &metrics.Config{
    Enabled: false,  // 禁用时返回 noop
    ServiceName: "my-service",  // 但仍需提供这些字段...
}
meter, _ := metrics.New(cfg)
```

而 `clog` 的设计是提供 `Discard()` 函数显式获取 noop Logger：

```go
logger := clog.Discard()  // 显式的 noop
```

**问题**：
- 用户在禁用 metrics 时仍需配置 `ServiceName`、`Version` 等无用字段
- API 表达力不足，无法快速表达"我要一个 noop meter"
- 与 clog 的 `Discard()` 风格不一致

### 2. 缺少默认配置工厂

`clog` 提供了 `NewDevDefaultConfig()` 和 `NewProdDefaultConfig()` 两个工厂函数，方便用户快速获取合理默认值。

`metrics` 没有类似的便捷函数，用户必须手动配置所有字段。

### 3. 注释过多

与 `clog` 相比，`metrics` 组件的注释过于冗长：

| 文件 | 问题 |
|------|------|
| `types.go` | 每个接口方法都有详细的参数/返回值注释（如 `// 参数：ctx - 上下文`），显得冗余 |
| `label.go` | 包含标签命名规范、使用场景等非必要文档 |
| `config.go` | 每个字段都有多行注释，说明过于详细 |
| `metrics.go` | `New` 和 `Must` 函数注释过于冗长，包含大量示例 |

### 4. 文件组织问题

- `types.go` 包含了完整的 package 文档注释，与 `metrics.go` 重复
- 接口定义与 package 文档混在一起

## 最终方案：方案 A

采用方案 A（添加 `Discard()` + 移除 `Enabled`），并添加默认配置工厂。

### API 变更

**Before**:
```go
cfg := &metrics.Config{
    Enabled:     true,
    ServiceName: "my-service",
    Version:     "v1.0.0",
    Port:        9090,
    Path:        "/metrics",
}
meter, _ := metrics.New(cfg)
```

**After**:
```go
// 启用 metrics
meter, _ := metrics.New(&metrics.Config{
    ServiceName: "my-service",
    Version:     "v1.0.0",
    Port:        9090,
    Path:        "/metrics",
})

// 或使用默认配置
meter, _ := metrics.New(metrics.NewDevDefaultConfig("my-service"))

// 禁用 metrics
meter := metrics.Discard()
```

## 已完成的改动

- [x] 添加 `Discard()` 函数
- [x] 移除 `Config.Enabled` 字段和相关逻辑
- [x] 添加 `NewDevDefaultConfig()` 和 `NewProdDefaultConfig()` 工厂函数
- [x] 精简 `types.go` 中的接口注释
- [x] 精简 `label.go` 中的注释
- [x] 精简 `config.go` 中的字段注释
- [x] 精简 `metrics.go` 中的函数注释
- [x] 移除 `types.go` 中重复的 package 文档
- [x] 更新示例代码
- [x] 更新测试代码
- [x] 运行测试确保改动正确

## 影响范围

- `metrics/config.go` - 移除 Enabled 字段，添加默认配置工厂
- `metrics/metrics.go` - 添加 `Discard()` 函数，简化 New 函数
- `metrics/types.go` - 接口定义注释精简，移除重复 package 文档
- `metrics/label.go` - Label 注释精简
- `metrics/metrics_test.go` - 更新测试用例
- `metrics/integration_test.go` - 更新集成测试用例
- `examples/metrics/main.go` - 使用默认配置工厂
- `examples/connector/main.go` - 移除 Enabled 字段
- `examples/breaker/main.go` - 移除 Enabled 字段
