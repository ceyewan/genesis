# 配置中心抽象接口设计

## 设计目标
- 提供统一的配置中心抽象接口，支持Redis和etcd两种实现
- 支持配置的动态更新和热加载
- 提供配置版本管理和回滚功能
- 支持多环境、多租户配置隔离
- 提供配置变更审计和权限控制
- 支持配置加密和敏感信息保护

## 核心接口设计

### 基础配置中心接口
```go
// pkg/common/config/config.go
package config

import (
    "context"
    "time"
)

// ConfigCenter 定义了配置中心的通用接口
type ConfigCenter interface {
    // 基本操作
    Get(ctx context.Context, key string) (string, error)
    Set(ctx context.Context, key string, value string) error
    Delete(ctx context.Context, key string) error
    Exists(ctx context.Context, key string) (bool, error)
    
    // 批量操作
    GetMulti(ctx context.Context, keys []string) (map[string]string, error)
    SetMulti(ctx context.Context, items map[string]string) error
    DeleteMulti(ctx context.Context, keys []string) error
    
    // 配置监听
    Watch(ctx context.Context, key string) (<-chan ConfigEvent, error)
    WatchPrefix(ctx context.Context, prefix string) (<-chan ConfigEvent, error)
    WatchMulti(ctx context.Context, keys []string) (<-chan ConfigEvent, error)
    
    // 配置列表
    List(ctx context.Context, prefix string) (map[string]string, error)
    ListKeys(ctx context.Context, prefix string) ([]string, error)
    
    // 版本管理
    GetVersion(ctx context.Context, key string) ([]ConfigVersion, error)
    GetConfigByVersion(ctx context.Context, key string, version int64) (*ConfigItem, error)
    Rollback(ctx context.Context, key string, version int64) error
    
    // 配置元数据
    GetMetadata(ctx context.Context, key string) (*ConfigMetadata, error)
    SetMetadata(ctx context.Context, key string, metadata *ConfigMetadata) error
    
    // 配置锁定
    Lock(ctx context.Context, key string, ttl time.Duration) error
    Unlock(ctx context.Context, key string) error
    IsLocked(ctx context.Context, key string) (bool, error)
    
    // 配置发布
    Publish(ctx context.Context, key string, env string) error
    GetPublished(ctx context.Context, key string, env string) (*ConfigItem, error)
    
    // 统计信息
    Stats(ctx context.Context) (*Stats, error)
    
    // 健康检查
    HealthCheck(ctx context.Context) error
    
    // 关闭
    Close() error
}

// ConfigItem 配置项
type ConfigItem struct {
    Key         string            `json:"key"`
    Value       string            `json:"value"`
    Version     int64             `json:"version"`
    CreatedAt   time.Time         `json:"created_at"`
    UpdatedAt   time.Time         `json:"updated_at"`
    CreatedBy   string            `json:"created_by"`
    UpdatedBy   string            `json:"updated_by"`
    Environment string            `json:"environment"`
    Metadata    map[string]string `json:"metadata"`
    Tags        []string          `json:"tags"`
    Status      ConfigStatus      `json:"status"`
    Checksum    string            `json:"checksum"`
}

// ConfigStatus 配置状态
type ConfigStatus string

const (
    ConfigStatusDraft     ConfigStatus = "draft"     // 草稿
    ConfigStatusPublished ConfigStatus = "published" // 已发布
    ConfigStatusArchived  ConfigStatus = "archived"  // 已归档
    ConfigStatusDeleted   ConfigStatus = "deleted"   // 已删除
)

// ConfigVersion 配置版本
type ConfigVersion struct {
    Version   int64     `json:"version"`
    Value     string    `json:"value"`
    CreatedAt time.Time `json:"created_at"`
    CreatedBy string    `json:"created_by"`
    Comment   string    `json:"comment"`
    Checksum  string    `json:"checksum"`
}

// ConfigMetadata 配置元数据
type ConfigMetadata struct {
    Description   string            `json:"description"`   // 配置描述
    Type          ConfigType        `json:"type"`          // 配置类型
    Format        string            `json:"format"`        // 配置格式
    Validation    *ValidationRule   `json:"validation"`    // 验证规则
    Encryption    *EncryptionInfo   `json:"encryption"`    // 加密信息
    Dependencies  []string          `json:"dependencies"`  // 依赖配置
    Tags          []string          `json:"tags"`          // 标签
    Custom        map[string]string `json:"custom"`        // 自定义字段
}

// ConfigType 配置类型
type ConfigType string

const (
    ConfigTypeString  ConfigType = "string"  // 字符串
    ConfigTypeJSON    ConfigType = "json"    // JSON
    ConfigTypeYAML    ConfigType = "yaml"    // YAML
    ConfigTypeXML     ConfigType = "xml"     // XML
    ConfigTypeNumber  ConfigType = "number"  // 数字
    ConfigTypeBoolean ConfigType = "boolean" // 布尔值
    ConfigTypeArray   ConfigType = "array"   // 数组
    ConfigTypeObject  ConfigType = "object"  // 对象
)

// ValidationRule 验证规则
type ValidationRule struct {
    Required    bool        `json:"required"`    // 是否必填
    MinLength   int         `json:"min_length"`  // 最小长度
    MaxLength   int         `json:"max_length"`  // 最大长度
    Pattern     string      `json:"pattern"`     // 正则表达式
    MinValue    interface{} `json:"min_value"`   // 最小值
    MaxValue    interface{} `json:"max_value"`   // 最大值
    Enum        []string    `json:"enum"`        // 枚举值
    CustomRule  string      `json:"custom_rule"` // 自定义验证规则
}

// EncryptionInfo 加密信息
type EncryptionInfo struct {
    Enabled    bool   `json:"enabled"`     // 是否启用加密
    Algorithm  string `json:"algorithm"`   // 加密算法
    KeyID      string `json:"key_id"`      // 密钥ID
    KeyVersion string `json:"key_version"` // 密钥版本
}

// ConfigEvent 配置事件
type ConfigEvent struct {
    Type        EventType         `json:"type"`         // 事件类型
    Key         string            `json:"key"`          // 配置键
    Value       string            `json:"value"`        // 配置值
    OldValue    string            `json:"old_value"`    // 旧值
    Version     int64             `json:"version"`      // 版本号
    Environment string            `json:"environment"`  // 环境
    Timestamp   time.Time         `json:"timestamp"`    // 事件时间
    User        string            `json:"user"`         // 操作用户
    Metadata    map[string]string `json:"metadata"`     // 事件元数据
}

// EventType 事件类型
type EventType string

const (
    EventConfigCreated   EventType = "config_created"   // 配置创建
    EventConfigUpdated   EventType = "config_updated"   // 配置更新
    EventConfigDeleted   EventType = "config_deleted"   // 配置删除
    EventConfigPublished EventType = "config_published" // 配置发布
    EventConfigRolledBack EventType = "config_rolled_back" // 配置回滚
    EventConfigLocked    EventType = "config_locked"    // 配置锁定
    EventConfigUnlocked  EventType = "config_unlocked"  // 配置解锁
)

// Stats 统计信息
type Stats struct {
    TotalConfigs    int64            `json:"total_configs"`    // 总配置数
    PublishedConfigs int64           `json:"published_configs"` // 已发布配置数
    DraftConfigs    int64            `json:"draft_configs"`    // 草稿配置数
    TotalVersions   int64            `json:"total_versions"`   // 总版本数
    ConfigSizes     map[string]int64 `json:"config_sizes"`     // 各类型配置大小
    LastUpdateTime  time.Time        `json:"last_update_time"` // 最后更新时间
}
```

## 高级特性接口

### 配置模板
```go
// TemplateEngine 配置模板引擎
type TemplateEngine interface {
    // 渲染配置模板
    Render(ctx context.Context, template string, data map[string]interface{}) (string, error)
    
    // 验证模板语法
    ValidateTemplate(ctx context.Context, template string) error
    
    // 获取模板变量
    GetTemplateVariables(ctx context.Context, template string) ([]string, error)
}

// TemplateConfigCenter 支持模板的配置中心
type TemplateConfigCenter interface {
    ConfigCenter
    
    // 模板管理
    CreateTemplate(ctx context.Context, name string, template string, variables []string) error
    UpdateTemplate(ctx context.Context, name string, template string) error
    DeleteTemplate(ctx context.Context, name string) error
    GetTemplate(ctx context.Context, name string) (*Template, error)
    ListTemplates(ctx context.Context) ([]Template, error)
    
    // 基于模板生成配置
    GenerateFromTemplate(ctx context.Context, templateName string, configName string, data map[string]interface{}) error
}

// Template 配置模板
type Template struct {
    Name       string   `json:"name"`
    Content    string   `json:"content"`
    Variables  []string `json:"variables"`
    CreatedAt  time.Time `json:"created_at"`
    UpdatedAt  time.Time `json:"updated_at"`
}
```

### 配置对比和合并
```go
// ConfigComparator 配置对比器
type ConfigComparator interface {
    // 对比两个配置
    Compare(ctx context.Context, key1 string, key2 string) (*ConfigDiff, error)
    
    // 对比配置版本
    CompareVersions(ctx context.Context, key string, version1 int64, version2 int64) (*ConfigDiff, error)
}

// ConfigDiff 配置差异
type ConfigDiff struct {
    Key        string       `json:"key"`
    Type       DiffType     `json:"type"`
    OldValue   string       `json:"old_value"`
    NewValue   string       `json:"new_value"`
    Changes    []ConfigChange `json:"changes"`
    CreatedAt  time.Time    `json:"created_at"`
}

// DiffType 差异类型
type DiffType string

const (
    DiffTypeAdded    DiffType = "added"    // 新增
    DiffTypeModified DiffType = "modified" // 修改
    DiffTypeDeleted  DiffType = "deleted"  // 删除
)

// ConfigChange 配置变更详情
type ConfigChange struct {
    Path      string      `json:"path"`      // 变更路径
    Type      ChangeType  `json:"type"`      // 变更类型
    OldValue  interface{} `json:"old_value"` // 旧值
    NewValue  interface{} `json:"new_value"` // 新值
}

// ChangeType 变更类型
type ChangeType string

const (
    ChangeTypeAdd     ChangeType = "add"     // 添加
    ChangeTypeRemove  ChangeType = "remove"  // 删除
    ChangeTypeUpdate  ChangeType = "update"  // 更新
    ChangeTypeReplace ChangeType = "replace" // 替换
)
```

### 配置审批流程
```go
// ApprovalWorkflow 配置审批流程
type ApprovalWorkflow interface {
    // 提交配置审批
    SubmitForApproval(ctx context.Context, key string, approvers []string) error
    
    // 审批配置
    Approve(ctx context.Context, key string, comment string) error
    
    // 拒绝配置
    Reject(ctx context.Context, key string, comment string) error
    
    // 获取审批状态
    GetApprovalStatus(ctx context.Context, key string) (*ApprovalStatus, error)
    
    // 获取审批历史
    GetApprovalHistory(ctx context.Context, key string) ([]ApprovalRecord, error)
}

// ApprovalStatus 审批状态
type ApprovalStatus struct {
    Key         string            `json:"key"`
    Status      ApprovalState     `json:"status"`
    Submitter   string            `json:"submitter"`
    Approvers   []string          `json:"approvers"`
    CurrentStep int               `json:"current_step"`
    TotalSteps  int               `json:"total_steps"`
    SubmittedAt time.Time         `json:"submitted_at"`
    UpdatedAt   time.Time         `json:"updated_at"`
    Metadata    map[string]string `json:"metadata"`
}

// ApprovalState 审批状态
type ApprovalState string

const (
    ApprovalStatePending  ApprovalState = "pending"  // 待审批
    ApprovalStateApproved ApprovalState = "approved" // 已批准
    ApprovalStateRejected ApprovalState = "rejected" // 已拒绝
    ApprovalStateCanceled ApprovalState = "canceled" // 已取消
)

// ApprovalRecord 审批记录
type ApprovalRecord struct {
    Step      int               `json:"step"`
    Approver  string            `json:"approver"`
    Action    ApprovalAction    `json:"action"`
    Comment   string            `json:"comment"`
    Timestamp time.Time         `json:"timestamp"`
    Metadata  map[string]string `json:"metadata"`
}

// ApprovalAction 审批动作
type ApprovalAction string

const (
    ApprovalActionApprove ApprovalAction = "approve" // 批准
    ApprovalActionReject  ApprovalAction = "reject"  // 拒绝
    ApprovalActionComment ApprovalAction = "comment" // 评论
)
```

## Redis实现设计

### 配置结构
```go
// pkg/redis/config/config.go
package config

import (
    "time"
    "github.com/ceyewan/genesis/pkg/common/config"
)

// RedisConfig Redis配置中心配置
type RedisConfig struct {
    // Redis连接配置
    Addr     string `json:"addr"`
    Password string `json:"password"`
    DB       int    `json:"db"`
    
    // 配置中心配置
    KeyPrefix       string        `json:"key_prefix"`        // 键前缀
    VersionTTL      time.Duration `json:"version_ttl"`       // 版本TTL
    LockTimeout     time.Duration `json:"lock_timeout"`      // 锁超时时间
    WatchBufferSize int           `json:"watch_buffer_size"` // 监听缓冲区大小
    
    // 发布配置
    PublishChannel  string        `json:"publish_channel"`   // 发布频道
    HistoryLimit    int           `json:"history_limit"`     // 历史版本限制
}
```

### 核心实现
```go
// pkg/redis/config/config.go
package config

import (
    "context"
    "encoding/json"
    "fmt"
    "strconv"
    "time"
    
    "github.com/ceyewan/genesis/pkg/common/config"
    "github.com/redis/go-redis/v9"
)

// RedisConfigCenter Redis配置中心实现
type RedisConfigCenter struct {
    client   *redis.Client
    config   *RedisConfig
    versions map[string][]config.ConfigVersion
    mu       sync.RWMutex
    watchers map[string]chan config.ConfigEvent
}

// Get 获取配置
func (c *RedisConfigCenter) Get(ctx context.Context, key string) (string, error) {
    fullKey := c.getConfigKey(key)
    
    value, err := c.client.Get(ctx, fullKey).Result()
    if err != nil {
        if err == redis.Nil {
            return "", fmt.Errorf("config key %s not found", key)
        }
        return "", fmt.Errorf("failed to get config: %w", err)
    }
    
    return value, nil
}

// Set 设置配置
func (c *RedisConfigCenter) Set(ctx context.Context, key string, value string) error {
    fullKey := c.getConfigKey(key)
    
    // 获取当前版本
    currentVersion, err := c.getCurrentVersion(ctx, key)
    if err != nil {
        currentVersion = 0
    }
    
    // 新版本号
    newVersion := currentVersion + 1
    
    // 保存配置值
    err = c.client.Set(ctx, fullKey, value, 0).Err()
    if err != nil {
        return fmt.Errorf("failed to set config: %w", err)
    }
    
    // 保存版本信息
    versionKey := c.getVersionKey(key, newVersion)
    versionInfo := config.ConfigVersion{
        Version:   newVersion,
        Value:     value,
        CreatedAt: time.Now(),
        CreatedBy: c.getCurrentUser(ctx),
        Checksum:  c.calculateChecksum(value),
    }
    
    versionData, err := json.Marshal(versionInfo)
    if err != nil {
        return fmt.Errorf("failed to marshal version info: %w", err)
    }
    
    err = c.client.Set(ctx, versionKey, versionData, c.config.VersionTTL).Err()
    if err != nil {
        return fmt.Errorf("failed to save version info: %w", err)
    }
    
    // 更新当前版本号
    err = c.setCurrentVersion(ctx, key, newVersion)
    if err != nil {
        return fmt.Errorf("failed to update current version: %w", err)
    }
    
    // 发送配置更新事件
    c.emitEvent(config.ConfigEvent{
        Type:      config.EventConfigUpdated,
        Key:       key,
        Value:     value,
        Version:   newVersion,
        Timestamp: time.Now(),
        User:      c.getCurrentUser(ctx),
    })
    
    return nil
}

// Watch 监听配置变化
func (c *RedisConfigCenter) Watch(ctx context.Context, key string) (<-chan config.ConfigEvent, error) {
    eventCh := make(chan config.ConfigEvent, c.config.WatchBufferSize)
    
    // 启动goroutine监听Redis键空间通知
    go c.watchKey(ctx, key, eventCh)
    
    return eventCh, nil
}

// watchKey 监听特定键的变化
func (c *RedisConfigCenter) watchKey(ctx context.Context, key string, eventCh chan<- config.ConfigEvent) {
    // 订阅键空间事件
    pubsub := c.client.Subscribe(ctx, fmt.Sprintf("__keyspace@*__:%s", c.getConfigKey(key)))
    defer pubsub.Close()
    
    for {
        select {
        case <-ctx.Done():
            return
        case msg := <-pubsub.Channel():
            if msg == nil {
                return
            }
            
            // 获取当前配置值
            value, err := c.Get(ctx, key)
            if err != nil {
                continue
            }
            
            // 发送事件
            event := config.ConfigEvent{
                Type:      config.EventConfigUpdated,
                Key:       key,
                Value:     value,
                Timestamp: time.Now(),
            }
            
            select {
            case eventCh <- event:
            default:
                // 通道满，丢弃事件
            }
        }
    }
}
```

## etcd实现设计

### 配置结构
```go
// pkg/etcd/config/config.go
package config

import (
    "time"
    "github.com/ceyewan/genesis/pkg/common/config"
)

// EtcdConfig etcd配置中心配置
type EtcdConfig struct {
    // etcd连接配置
    Endpoints   []string      `json:"endpoints"`
    DialTimeout time.Duration `json:"dial_timeout"`
    Username    string        `json:"username"`
    Password    string        `json:"password"`
    
    // 配置中心配置
    KeyPrefix       string        `json:"key_prefix"`        // 键前缀
    LeaseTTL        time.Duration `json:"lease_ttl"`         // 租约TTL
    WatchBufferSize int           `json:"watch_buffer_size"` // 监听缓冲区大小
    
    // 历史版本配置
    HistoryLimit    int           `json:"history_limit"`     // 历史版本限制
    CompactInterval time.Duration `json:"compact_interval"`  // 压缩间隔
}
```

### 核心实现
```go
// pkg/etcd/config/config.go
package config

import (
    "context"
    "encoding/json"
    "fmt"
    "path"
    "strconv"
    "time"
    
    "github.com/ceyewan/genesis/pkg/common/config"
    clientv3 "go.etcd.io/etcd/client/v3"
)

// EtcdConfigCenter etcd配置中心实现
type EtcdConfigCenter struct {
    client   *clientv3.Client
    config   *EtcdConfig
    watchers map[string]chan config.ConfigEvent
    mu       sync.RWMutex
}

// Get 获取配置
func (c *EtcdConfigCenter) Get(ctx context.Context, key string) (string, error) {
    fullKey := c.getConfigKey(key)
    
    resp, err := c.client.Get(ctx, fullKey)
    if err != nil {
        return "", fmt.Errorf("failed to get config: %w", err)
    }
    
    if len(resp.Kvs) == 0 {
        return "", fmt.Errorf("config key %s not found", key)
    }
    
    return string(resp.Kvs[0].Value), nil
}

// Set 设置配置
func (c *EtcdConfigCenter) Set(ctx context.Context, key string, value string) error {
    fullKey := c.getConfigKey(key)
    
    // 获取当前版本
    currentVersion, err := c.getCurrentVersion(ctx, key)
    if err != nil {
        currentVersion = 0
    }
    
    // 新版本号
    newVersion := currentVersion + 1
    
    // 保存配置值
    _, err = c.client.Put(ctx, fullKey, value)
    if err != nil {
        return fmt.Errorf("failed to set config: %w", err)
    }
    
    // 保存版本信息
    versionKey := c.getVersionKey(key, newVersion)
    versionInfo := config.ConfigVersion{
        Version:   newVersion,
        Value:     value,
        CreatedAt: time.Now(),
        CreatedBy: c.getCurrentUser(ctx),
        Checksum:  c.calculateChecksum(value),
    }
    
    versionData, err := json.Marshal(versionInfo)
    if err != nil {
        return fmt.Errorf("failed to marshal version info: %w", err)
    }
    
    _, err = c.client.Put(ctx, versionKey, string(versionData))
    if err != nil {
        return fmt.Errorf("failed to save version info: %w", err)
    }
    
    // 更新当前版本号
    err = c.setCurrentVersion(ctx, key, newVersion)
    if err != nil {
        return fmt.Errorf("failed to update current version: %w", err)
    }
    
    // 发送配置更新事件
    c.emitEvent(config.ConfigEvent{
        Type:      config.EventConfigUpdated,
        Key:       key,
        Value:     value,
        Version:   newVersion,
        Timestamp: time.Now(),
        User:      c.getCurrentUser(ctx),
    })
    
    return nil
}

// Watch 监听配置变化
func (c *EtcdConfigCenter) Watch(ctx context.Context, key string) (<-chan config.ConfigEvent, error) {
    eventCh := make(chan config.ConfigEvent, c.config.WatchBufferSize)
    
    // 启动etcd监听
    go c.watchEtcd(ctx, key, eventCh)
    
    return eventCh, nil
}

// watchEtcd 监听etcd变化
func (c *EtcdConfigCenter) watchEtcd(ctx context.Context, key string, eventCh chan<- config.ConfigEvent) {
    fullKey := c.getConfigKey(key)
    
    // 创建监听器
    watchCh := c.client.Watch(ctx, fullKey)
    
    for {
        select {
        case <-ctx.Done():
            return
        case resp := <-watchCh:
            if resp.Err() != nil {
                continue
            }
            
            for _, event := range resp.Events {
                var configEvent config.ConfigEvent
                
                switch event.Type {
                case clientv3.EventTypePut:
                    configEvent = config.ConfigEvent{
                        Type:      config.EventConfigUpdated,
                        Key:       key,
                        Value:     string(event.Kv.Value),
                        Version:   event.Kv.Version,
                        Timestamp: time.Now(),
                    }
                    
                case clientv3.EventTypeDelete:
                    configEvent = config.ConfigEvent{
                        Type:      config.EventConfigDeleted,
                        Key:       key,
                        Timestamp: time.Now(),
                    }
                }
                
                // 发送事件
                select {
                case eventCh <- configEvent:
                default:
                    // 通道满，丢弃事件
                }
            }
        }
    }
}
```

这个配置中心设计提供了完整的抽象接口和丰富的功能特性，支持配置版本管理、动态更新、权限控制等高级功能，同时提供了Redis和etcd两种实现方案。