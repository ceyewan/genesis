# Auth 模块设计文档

## 1. 概述

Auth 模块为 Genesis 框架提供统一的认证能力，基于 JWT (JSON Web Token) 实现。

* **所属层级**：L3 (Governance) — 治理能力，切面组件
* **核心职责**：Token 生成、验证、刷新，支持 HTTP 和 WebSocket 认证
* **设计原则**：
  * 无状态认证：JWT 自包含用户信息
  * 统一接口：HTTP 和 WebSocket 使用相同的验证逻辑
  * 与 Gin 深度集成：提供开箱即用的中间件
  * 遵循 Genesis L0 规范：使用 `clog`、`metrics`、`xerrors`

## 2. 目录结构

```text
pkg/auth/
├── auth.go         # 接口定义 + 工厂函数
├── config.go       # Config 结构体
├── errors.go       # Sentinel Errors
├── options.go      # Option 函数
├── jwt.go          # JWT 实现
├── claims.go       # Claims 定义
├── middleware.go   # Gin 中间件
└── ws.go           # WebSocket 认证
```

## 3. 核心接口

### 3.1 Authenticator 接口

```go
// pkg/auth/auth.go

// Authenticator 认证器接口
type Authenticator interface {
    // GenerateToken 生成 Token
    GenerateToken(ctx context.Context, claims *Claims) (string, error)
    
    // ValidateToken 验证 Token，返回 Claims
    ValidateToken(ctx context.Context, token string) (*Claims, error)
    
    // RefreshToken 刷新 Token
    RefreshToken(ctx context.Context, token string) (string, error)
    
    // RevokeToken 撤销 Token（可选，需要 Redis 支持）
    RevokeToken(ctx context.Context, token string) error
    
    // IsRevoked 检查 Token 是否已撤销
    IsRevoked(ctx context.Context, token string) (bool, error)
}
```

### 3.2 Claims 结构

```go
// pkg/auth/claims.go

// Claims JWT 声明
type Claims struct {
    // 标准声明
    jwt.RegisteredClaims
    
    // 自定义声明
    UserID   string            `json:"uid"`
    Username string            `json:"uname,omitempty"`
    Roles    []string          `json:"roles,omitempty"`
    Extra    map[string]any    `json:"extra,omitempty"`
}

// NewClaims 创建 Claims
func NewClaims(userID string, opts ...ClaimsOption) *Claims
```

### 3.3 ClaimsOption

```go
// ClaimsOption Claims 配置选项
type ClaimsOption func(*Claims)

func WithUsername(username string) ClaimsOption
func WithRoles(roles ...string) ClaimsOption
func WithExtra(key string, value any) ClaimsOption
func WithExpiration(d time.Duration) ClaimsOption
func WithIssuer(issuer string) ClaimsOption
func WithAudience(audience ...string) ClaimsOption
```

## 4. 配置结构

```go
// pkg/auth/config.go

// Config Auth 配置
type Config struct {
    // JWT 配置
    SecretKey     string        `mapstructure:"secret_key"`      // 签名密钥
    SigningMethod string        `mapstructure:"signing_method"`  // 签名方法: HS256, HS384, HS512, RS256
    Issuer        string        `mapstructure:"issuer"`          // 签发者
    Audience      []string      `mapstructure:"audience"`        // 接收者
    
    // Token 有效期
    AccessTokenTTL  time.Duration `mapstructure:"access_token_ttl"`  // 默认 15m
    RefreshTokenTTL time.Duration `mapstructure:"refresh_token_ttl"` // 默认 7d
    
    // Token 黑名单（可选，需要 Redis）
    EnableBlacklist bool `mapstructure:"enable_blacklist"`
    
    // Token 提取配置
    TokenLookup   string `mapstructure:"token_lookup"`    // header:Authorization, query:token, cookie:jwt
    TokenHeadName string `mapstructure:"token_head_name"` // Bearer
}

func (c *Config) SetDefaults() {
    if c.SigningMethod == "" {
        c.SigningMethod = "HS256"
    }
    if c.AccessTokenTTL == 0 {
        c.AccessTokenTTL = 15 * time.Minute
    }
    if c.RefreshTokenTTL == 0 {
        c.RefreshTokenTTL = 7 * 24 * time.Hour
    }
    if c.TokenLookup == "" {
        c.TokenLookup = "header:Authorization"
    }
    if c.TokenHeadName == "" {
        c.TokenHeadName = "Bearer"
    }
}
```

## 5. 错误处理

使用 `xerrors` 定义错误：

```go
// pkg/auth/errors.go
import "github.com/ceyewan/genesis/xerrors"

var (
    ErrInvalidToken     = xerrors.New("auth: invalid token")
    ErrExpiredToken     = xerrors.New("auth: token expired")
    ErrRevokedToken     = xerrors.New("auth: token revoked")
    ErrMissingToken     = xerrors.New("auth: missing token")
    ErrInvalidClaims    = xerrors.New("auth: invalid claims")
    ErrInvalidSignature = xerrors.New("auth: invalid signature")
    ErrTokenNotActive   = xerrors.New("auth: token not yet active")
    ErrBlacklistError   = xerrors.New("auth: blacklist operation failed")
)
```

## 6. 工厂函数

```go
// pkg/auth/auth.go

// New 创建 Authenticator
func New(cfg *Config, opts ...Option) (Authenticator, error)

// Must 类似 New，但出错时 panic
func Must(cfg *Config, opts ...Option) Authenticator
```

## 7. Option 模式

```go
// pkg/auth/options.go
import (
    "github.com/ceyewan/genesis/clog"
    "github.com/ceyewan/genesis/metrics"
    "github.com/ceyewan/genesis/connector"
)

type options struct {
    logger    clog.Logger
    meter     metrics.Meter
    redisConn connector.RedisConnector // 用于 Token 黑名单
}

type Option func(*options)

func WithLogger(l clog.Logger) Option {
    return func(o *options) {
        o.logger = l.WithNamespace("auth")
    }
}

func WithMeter(m metrics.Meter) Option {
    return func(o *options) {
        o.meter = m
    }
}

// WithRedis 启用 Token 黑名单功能
func WithRedis(conn connector.RedisConnector) Option {
    return func(o *options) {
        o.redisConn = conn
    }
}
```

## 8. Gin 中间件

### 8.1 HTTP 认证中间件

```go
// pkg/auth/middleware.go

// GinMiddleware 返回 Gin 认证中间件
func (a *jwtAuth) GinMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        token, err := a.extractToken(c.Request)
        if err != nil {
            c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
                "error": err.Error(),
            })
            return
        }
        
        claims, err := a.ValidateToken(c.Request.Context(), token)
        if err != nil {
            c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
                "error": err.Error(),
            })
            return
        }
        
        // 将 Claims 存入 Context
        c.Set(ClaimsKey, claims)
        c.Next()
    }
}

// GetClaims 从 Gin Context 获取 Claims
func GetClaims(c *gin.Context) (*Claims, bool) {
    claims, exists := c.Get(ClaimsKey)
    if !exists {
        return nil, false
    }
    return claims.(*Claims), true
}

// MustGetClaims 从 Gin Context 获取 Claims，不存在时 panic
func MustGetClaims(c *gin.Context) *Claims {
    claims, ok := GetClaims(c)
    if !ok {
        panic("auth: claims not found in context")
    }
    return claims
}
```

### 8.2 可选认证中间件

```go
// OptionalMiddleware 可选认证，不强制要求 Token
func (a *jwtAuth) OptionalMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        token, err := a.extractToken(c.Request)
        if err != nil {
            // Token 不存在或格式错误，继续处理
            c.Next()
            return
        }
        
        claims, err := a.ValidateToken(c.Request.Context(), token)
        if err != nil {
            // Token 无效，继续处理（但不设置 Claims）
            c.Next()
            return
        }
        
        c.Set(ClaimsKey, claims)
        c.Next()
    }
}
```

### 8.3 角色授权中间件

```go
// RequireRoles 要求特定角色
func RequireRoles(roles ...string) gin.HandlerFunc {
    return func(c *gin.Context) {
        claims, ok := GetClaims(c)
        if !ok {
            c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
                "error": "unauthorized",
            })
            return
        }
        
        for _, required := range roles {
            found := false
            for _, role := range claims.Roles {
                if role == required {
                    found = true
                    break
                }
            }
            if !found {
                c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
                    "error": "forbidden: missing role " + required,
                })
                return
            }
        }
        
        c.Next()
    }
}
```

## 9. WebSocket 认证

### 9.1 Token 提取

WebSocket 不支持自定义 Header，Token 通常通过以下方式传递：

1. **URL Query 参数**：`ws://host/path?token=xxx`
2. **协议头**：`Sec-WebSocket-Protocol: token, xxx`
3. **首条消息**：连接后发送包含 Token 的消息

### 9.2 WebSocket 认证实现

```go
// pkg/auth/ws.go

// WSAuthenticator WebSocket 认证器
type WSAuthenticator interface {
    // AuthenticateRequest 从 HTTP 请求中认证（升级前）
    AuthenticateRequest(r *http.Request) (*Claims, error)
    
    // AuthenticateMessage 从消息中认证（连接后首条消息）
    AuthenticateMessage(data []byte) (*Claims, error)
}

// AuthenticateRequest 从请求中提取并验证 Token
func (a *jwtAuth) AuthenticateRequest(r *http.Request) (*Claims, error) {
    // 1. 尝试从 Query 参数获取
    token := r.URL.Query().Get("token")
    if token == "" {
        // 2. 尝试从 Sec-WebSocket-Protocol 获取
        protocols := r.Header.Get("Sec-WebSocket-Protocol")
        token = a.extractFromProtocol(protocols)
    }
    
    if token == "" {
        return nil, ErrMissingToken
    }
    
    return a.ValidateToken(r.Context(), token)
}

// WSMiddleware Gin WebSocket 认证中间件
func (a *jwtAuth) WSMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        claims, err := a.AuthenticateRequest(c.Request)
        if err != nil {
            c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
                "error": err.Error(),
            })
            return
        }
        
        c.Set(ClaimsKey, claims)
        c.Next()
    }
}
```

### 9.3 Gorilla WebSocket 集成

```go
// WSUpgrader 带认证的 WebSocket 升级器
type WSUpgrader struct {
    auth     Authenticator
    upgrader *websocket.Upgrader
}

func NewWSUpgrader(auth Authenticator, upgrader *websocket.Upgrader) *WSUpgrader {
    return &WSUpgrader{auth: auth, upgrader: upgrader}
}

// Upgrade 升级并认证 WebSocket 连接
func (u *WSUpgrader) Upgrade(w http.ResponseWriter, r *http.Request) (*websocket.Conn, *Claims, error) {
    claims, err := u.auth.AuthenticateRequest(r)
    if err != nil {
        http.Error(w, err.Error(), http.StatusUnauthorized)
        return nil, nil, err
    }
    
    conn, err := u.upgrader.Upgrade(w, r, nil)
    if err != nil {
        return nil, nil, err
    }
    
    return conn, claims, nil
}
```

## 10. 使用示例

### 10.1 基础配置

```yaml
# config.yaml
auth:
  secret_key: "your-secret-key-min-32-chars-long"
  signing_method: "HS256"
  issuer: "my-service"
  access_token_ttl: 15m
  refresh_token_ttl: 168h  # 7 days
  token_lookup: "header:Authorization"
  token_head_name: "Bearer"
  enable_blacklist: false
```

### 10.2 初始化

```go
func main() {
    // 加载配置
    cfg := config.MustLoad("config.yaml")
    
    // 初始化 Logger
    logger := clog.Must(&cfg.Log)
    
    // 创建认证器
    auth, err := auth.New(&cfg.Auth,
        auth.WithLogger(logger),
    )
    if err != nil {
        log.Fatal(err)
    }
    
    // 如果需要 Token 黑名单
    // redisConn, _ := connector.NewRedis(&cfg.Redis)
    // auth, _ := auth.New(&cfg.Auth,
    //     auth.WithLogger(logger),
    //     auth.WithRedis(redisConn),
    // )
    
    // 创建 Gin 路由
    r := gin.Default()
    
    // 公开路由
    r.POST("/login", loginHandler(auth))
    r.POST("/refresh", refreshHandler(auth))
    
    // 需要认证的路由
    authorized := r.Group("/api")
    authorized.Use(auth.GinMiddleware())
    {
        authorized.GET("/profile", profileHandler)
        authorized.GET("/admin", auth.RequireRoles("admin"), adminHandler)
    }
    
    // WebSocket 路由
    r.GET("/ws", auth.WSMiddleware(), wsHandler)
    
    r.Run(":8080")
}
```

### 10.3 登录处理

```go
func loginHandler(a auth.Authenticator) gin.HandlerFunc {
    return func(c *gin.Context) {
        var req LoginRequest
        if err := c.ShouldBindJSON(&req); err != nil {
            c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
            return
        }
        
        // 验证用户名密码（示例）
        user, err := validateCredentials(req.Username, req.Password)
        if err != nil {
            c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
            return
        }
        
        // 生成 Token
        claims := auth.NewClaims(user.ID,
            auth.WithUsername(user.Username),
            auth.WithRoles(user.Roles...),
        )
        
        token, err := a.GenerateToken(c.Request.Context(), claims)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
            return
        }
        
        c.JSON(http.StatusOK, gin.H{
            "token": token,
            "expires_in": int(cfg.Auth.AccessTokenTTL.Seconds()),
        })
    }
}
```

### 10.4 获取当前用户

```go
func profileHandler(c *gin.Context) {
    claims := auth.MustGetClaims(c)
    
    c.JSON(http.StatusOK, gin.H{
        "user_id":  claims.UserID,
        "username": claims.Username,
        "roles":    claims.Roles,
    })
}
```

### 10.5 WebSocket 认证

```go
var upgrader = websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool { return true },
}

func wsHandler(c *gin.Context) {
    claims := auth.MustGetClaims(c)
    
    conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
    if err != nil {
        return
    }
    defer conn.Close()
    
    // 使用 claims.UserID 处理消息
    for {
        _, msg, err := conn.ReadMessage()
        if err != nil {
            break
        }
        // 处理消息，可使用 claims.UserID 标识用户
        log.Printf("User %s: %s", claims.UserID, msg)
    }
}
```

### 10.6 客户端 WebSocket 连接

```javascript
// 方式 1: URL Query 参数
const ws = new WebSocket('ws://localhost:8080/ws?token=' + token);

// 方式 2: Sec-WebSocket-Protocol（需要服务端支持）
const ws = new WebSocket('ws://localhost:8080/ws', ['token', yourToken]);
```

## 11. 内置指标

| 指标名 | 类型 | 描述 |
|--------|------|------|
| `auth_tokens_generated_total` | Counter | 生成的 Token 数 |
| `auth_tokens_validated_total` | Counter | 验证的 Token 数 |
| `auth_tokens_refreshed_total` | Counter | 刷新的 Token 数 |
| `auth_tokens_revoked_total` | Counter | 撤销的 Token 数 |
| `auth_validation_errors_total` | Counter | 验证失败数 |
| `auth_validation_duration_seconds` | Histogram | 验证耗时 |

**Label 维度**：`status` (success/error), `error_type` (expired/invalid/revoked)

## 12. 安全建议

1. **密钥管理**：
   * 生产环境使用强密钥（至少 32 字符）
   * 通过环境变量注入，不要硬编码

2. **Token 有效期**：
   * Access Token：15 分钟 ~ 1 小时
   * Refresh Token：7 天 ~ 30 天

3. **HTTPS**：
   * 生产环境必须使用 HTTPS
   * WebSocket 使用 WSS

4. **Token 黑名单**：
   * 关键操作（登出、密码修改）后撤销 Token
   * 使用 Redis 存储黑名单

5. **Claims 最小化**：
   * 只存储必要信息
   * 敏感信息不要放入 Token

## 13. L0 组件集成

| 能力 | L0 组件 | Option |
|------|---------|--------|
| 日志 | `clog` | `WithLogger(logger)` |
| 指标 | `metrics` | `WithMeter(meter)` |
| 错误 | `xerrors` | Sentinel Errors |
| 配置 | `config` | `auth.Config` |
