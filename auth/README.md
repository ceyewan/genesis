# Auth 组件

Auth 组件为 Genesis 框架提供统一的认证能力，基于 JWT (JSON Web Token) 实现。

## 特性

- **无状态认证**：JWT 自包含用户信息，易于横向扩展。
- **统一接口**：HTTP 和 WebSocket 使用相同的验证逻辑。
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
├── middleware.go   # Gin 中间件
└── ws.go           # WebSocket 认证
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
  token_lookup: "header:Authorization"
  token_head_name: "Bearer"
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

r.GET("/profile", func(c *gin.Context) {
    claims, _ := auth.GetClaims(c)
    c.JSON(200, gin.H{"user_id": claims.Subject})
})
```

## 监控指标

| 指标名 | 类型 | 描述 |
|--------|------|------|
| `auth_tokens_generated_total` | Counter | 生成的 Token 总数 |
| `auth_tokens_validated_total` | Counter | 验证的 Token 总数 |
| `auth_tokens_refreshed_total` | Counter | 刷新的 Token 总数 |
| `auth_token_validation_duration_seconds` | Histogram | 验证耗时分布 |
