# 架构设计经验总结

## 项目背景
当前处于开发阶段，不需要考虑API向后兼容性，优先保证易用性、简洁性，避免用户困扰。

## 关键设计决策

### 1. 避免循环依赖

**问题**：原设计存在潜在的循环引用风险
- `pkg/lock/simple` → `internal/lock` 
- `internal/lock` → `pkg/lock` (接口定义)

**解决方案**：
```
清晰的单向依赖层次：
pkg/lock/simple/          (新API层)
  ↓
pkg/lock/                 (接口定义)
  ↓
internal/lock/            (实现层)
  ↓
internal/connector/       (连接管理层)
```

**验证方法**：
```bash
go list -json ./pkg/lock/simple | grep Imports
# 确认只有单向依赖，无循环引用
```

### 2. API设计遵循Go规范

**设计原则**：参考Go标准库模式
- `New(config, option)` - config必需，option可选
- 类似 `database/sql.Open(driver, dsn)` 的设计哲学

**职责划分**：
- **Config**：连接相关（backend, endpoints, auth, timeout）
- **Option**：行为相关（ttl, retry, autoRenew, maxRetries）

### 3. 配置分离策略

**连接配置 vs 行为配置**：
```go
// Config - 连接相关（必需）
type Config struct {
    Backend   string   // 后端类型
    Endpoints []string // 连接地址
    Username  string   // 认证信息
    Password  string
    Timeout   time.Duration // 连接超时
}

// Option - 行为相关（可选）
type Option struct {
    TTL           time.Duration // 锁超时
    RetryInterval time.Duration // 重试间隔
    AutoRenew     bool          // 自动续期
    MaxRetries    int           // 最大重试
}
```

### 4. 连接复用机制

**核心设计**：
- 配置哈希计算 → 相同配置复用连接
- 线程安全的连接池管理
- 懒加载创建，双重检查避免并发重复创建

**实现要点**：
```go
func (m *Manager) GetEtcdClient(config ConnectionConfig) (*clientv3.Client, error) {
    configHash := m.hashConfig(config)  // SHA256哈希
    
    // 读写锁优化并发性能
    m.mu.RLock()
    if client, exists := m.etcdClients[configHash]; exists {
        m.mu.RUnlock()
        return client, nil
    }
    m.mu.RUnlock()
    
    // 双重检查避免并发重复创建
    // ... 创建并缓存连接
}
```

## 实施经验

### 1. 渐进式重构
1. 先重构连接管理器（`internal/connector`）
2. 扩展内部锁实现（`internal/lock`）
3. 最后创建新API层（`pkg/lock/simple`）
4. 保持现有API工作（开发阶段可移除）

### 2. 零值友好设计
- 两个参数都可以为 `nil`
- 合理的默认值自动应用
- 一行初始化：`simple.New(nil, nil)`

### 3. 错误处理策略
- 早期参数验证（backend, endpoints必需）
- 清晰的错误信息链
- 资源清理保证（defer Close）

## 性能优化

### 1. 连接复用效果
- 相同配置自动复用连接
- 减少etcd服务器连接压力
- 降低连接建立开销

### 2. 并发安全
- 读写锁（`sync.RWMutex`）优化读多写少场景
- 双重检查模式避免重复创建
- 无锁化设计在关键路径

## 测试验证

### 1. 架构验证
- 构建成功（`go build ./...`）
- 无循环依赖（`go list` 验证）
- 清晰的包依赖层次

### 2. 功能验证
- 基本CRUD操作（Lock/Unlock/TryLock）
- 连接复用验证
- 配置自定义验证
- 并发安全性验证

## 后续开发指导

### 1. API演进原则
- 保持 `New(config, option)` 签名稳定
- Config和Option职责清晰分离
- 新增功能优先用Option扩展

### 2. 扩展性考虑
- 支持多后端（etcd、redis等）
- 插件化行为配置
- 监控和指标集成

### 3. 性能持续优化
- 连接池大小可调
- 批量操作支持
- 异步化改进

## 关键收获

1. **架构清晰比代码复杂更重要**：清晰的层次结构避免了循环依赖
2. **遵循语言规范**：Go的config+option模式用户更易理解
3. **渐进式重构**：分步骤重构降低了风险和复杂度
4. **测试驱动**：每个阶段都有对应的验证测试

这个架构设计为后续功能扩展打下了良好基础，同时保持了API的简洁性和易用性。