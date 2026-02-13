# Auth 组件

Auth 组件为 Genesis 框架提供统一的认证能力，基于 JWT (JSON Web Token) 实现。

## 特性

- **安全性保障**：
    - 严格的签名方法验证（仅支持 HS256）
    - Claims 不可变性（GenerateToken 不会修改原始 Claims）
    - 完善的刷新令牌验证（Issuer、Audience、时间窗口检查）
- **无状态认证**：JWT 自包含用户信息，易于横向扩展。
- **多源 Token 提取**：自动从 Header、Query、Cookie 中提取 Token，开箱即用。
- **Gin 集成**：提供开箱即用的中间件。
- **遵循 L0 规范**：集成 `clog`、`metrics`、`xerrors`。
- **标准协议**：使用 `github.com/golang-jwt/jwt/v5` 标准库。

## 目录结构

```text
auth/
├── auth.go         # 接口定义 + 实现
├── config.go       # 配置结构
├── errors.go       # 哨兵错误
├── options.go      # 函数式选项
├── claims.go       # Claims 定义
├── metrics.go      # 指标常量
└── middleware.go   # Gin 中间件
```

## 核心接口

### Authenticator

```go
type Authenticator interface {
    // GenerateToken 生成 Token
    GenerateToken(ctx context.Context, claims *Claims) (string, error)

    // ValidateToken 验证 Token，返回 Claims
    ValidateToken(ctx context.Context, token string) (*Claims, error)

    // RefreshToken 刷新 Token
    RefreshToken(ctx context.Context, token string) (string, error)

    // GinMiddleware 返回 Gin 认证中间件
    GinMiddleware() gin.HandlerFunc
}
```

### Claims

```go
type Claims struct {
    // 标准声明 (使用 jwt.RegisteredClaims)
    jwt.RegisteredClaims

    // 自定义声明
    Username string         `json:"uname,omitempty"`
    Roles    []string       `json:"roles,omitempty"`
    Extra    map[string]any `json:"extra,omitempty"`
}
```

## 配置

```yaml
auth:
    secret_key: "your-secret-key-min-32-chars-long"
    signing_method: "HS256"
    issuer: "my-service"
    access_token_ttl: 15m
    refresh_token_ttl: 168h
    # token_lookup: 可选，留空则使用默认多源查找
    # 默认查找顺序: header:Authorization -> query:token -> cookie:jwt
    token_lookup: "header:Authorization" # 可指定单一来源
    token_head_name: "Bearer"
```

### Token 提取方式

默认情况下（`token_lookup` 留空），组件会按以下顺序尝试提取 Token：

1. **Header**: `Authorization: Bearer <token>`
2. **Query**: `?token=<token>`
3. **Cookie**: `jwt=<token>`

这种设计使得同一份配置可以同时支持：

- REST API（使用 Header）
- WebSocket 连接（使用 Query）
- 前端应用（使用 Cookie）

如果需要限制只从特定来源提取，可配置 `token_lookup`：

```go
&auth.Config{
    SecretKey: "...",
    TokenLookup: "query:token",  // 只从 query 提取
}
```

## 使用示例

### 初始化

```go
// 创建认证器
authenticator, err := auth.New(&auth.Config{
    SecretKey: "your-secret-key-at-least-32-chars",
}, auth.WithLogger(logger))
```

### 登录并生成 Token

```go
claims := &auth.Claims{
    RegisteredClaims: jwt.RegisteredClaims{
        Subject: user.ID,
    },
    Username: user.Username,
    Roles:    []string{"admin"},
}

token, err := authenticator.GenerateToken(ctx, claims)
```

### 在中间件中使用

```go
r := gin.Default()
r.Use(authenticator.GinMiddleware())

// 需要认证的路由
r.GET("/profile", func(c *gin.Context) {
    claims, _ := auth.GetClaims(c)
    c.JSON(200, gin.H{"user_id": claims.Subject})
})

// 要求特定角色的路由（OR 逻辑）
r.GET("/admin", auth.RequireRoles("admin"), handler)
r.GET("/moderate", auth.RequireRoles("admin", "moderator"), handler)  // 拥有任一角色即可
```

### RequireRoles

`RequireRoles` 是一个角色检查中间件，采用 **OR 逻辑**：

- 用户只需拥有 **任意一个** 指定角色即可通过
- `RequireRoles("admin", "editor")` 表示用户必须有 `admin` 或 `editor` 角色
- 如果没有任何匹配角色，返回 403 Forbidden

## 监控指标

Auth 组件提供以下可观测性指标，业务方可通过导出的常量引用指标名称：

| 指标名                        | 类型    | 标签                                                                                | 描述           |
| ----------------------------- | ------- | ----------------------------------------------------------------------------------- | -------------- |
| `auth_tokens_validated_total` | Counter | `status="success\|error"`, `error_type="expired\|invalid_signature\|invalid_token"` | Token 验证计数 |
| `auth_tokens_refreshed_total` | Counter | `status="success\|error"`                                                           | Token 刷新计数 |

### 指标常量引用

```go
import "github.com/ceyewan/genesis/auth"

// 使用导出的常量引用指标名
metricName := auth.MetricTokensValidated
```

### Prometheus 查询示例

```promql
# 验证成功率
rate(auth_tokens_validated_total{status="success"}[5m]) / rate(auth_tokens_validated_total[5m])

# 验证失败数（按错误类型分组）
sum by (error_type) (rate(auth_tokens_validated_total{status="error"}[5m]))

# 刷新成功率
rate(auth_tokens_refreshed_total{status="success"}[5m]) / rate(auth_tokens_refreshed_total[5m])

# 刷新失败数
sum(rate(auth_tokens_refreshed_total{status="error"}[5m]))
```

### 指标说明

- **Token 缺失**（客户端未提供 token）不计入验证失败指标
- **验证失败** 仅统计提供了 token 但验证失败的情况（过期、签名错误、格式错误）
