# Auth 组件

`auth` 是 Genesis 的 L3 治理层组件，提供**基于 JWT 的双令牌认证能力**。它适合"应用自己签发 access token / refresh token，并在服务端本地完成验签"的场景，重点解决统一签发、统一校验、统一 Gin 接入的问题。

如果你需要的是：

- 轻量、无存储的认证方案；
- 业务接口只接收 access token；
- access token 过期后用 refresh token 换发新 token；
- 在 Gin 项目里快速接入认证和简单 RBAC；

那么当前 `auth` 组件是合适的。

如果你需要的是：

- token 撤销、黑名单、单设备登录；
- refresh token 重放检测；
- OAuth2 / OIDC / SSO；
- 统一身份中心或外部 IdP 联动；

那么当前 `auth` 不覆盖这些能力。

---

## 快速开始

### 1. 初始化认证器

```go
authenticator, err := auth.New(&auth.Config{
    SecretKey:       "your-secret-key-at-least-32-chars",
    SigningMethod:   "HS256",
    Issuer:          "my-service",
    Audience:        []string{"frontend"},
    AccessTokenTTL:  15 * time.Minute,
    RefreshTokenTTL: 7 * 24 * time.Hour,
    TokenHeadName:   "Bearer",
}, auth.WithLogger(logger))
```

### 2. 登录时签发双令牌

```go
claims := &auth.Claims{
    RegisteredClaims: jwt.RegisteredClaims{
        Subject: "user-123",
    },
    Username: "alice",
    Roles:    []string{"admin"},
}

pair, err := authenticator.GenerateTokenPair(ctx, claims)
if err != nil {
    return err
}

// pair.AccessToken     用于业务接口访问
// pair.RefreshToken    用于刷新
// pair.AccessTokenExpiresAt / RefreshTokenExpiresAt 可返回给前端做过期管理
```

### 3. 业务接口使用 access token

```go
r := gin.Default()
r.Use(authenticator.GinMiddleware())

r.GET("/profile", func(c *gin.Context) {
    claims, _ := auth.GetClaims(c)
    c.JSON(200, gin.H{
        "user_id": claims.Subject,
        "roles":   claims.Roles,
    })
})
```

### 4. 刷新接口使用 refresh token

```go
r.POST("/refresh", func(c *gin.Context) {
    var req struct {
        RefreshToken string `json:"refresh_token"`
    }
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(400, gin.H{"error": "bad request"})
        return
    }

    pair, err := authenticator.RefreshToken(c.Request.Context(), req.RefreshToken)
    if err != nil {
        c.JSON(401, gin.H{"error": "unauthorized"})
        return
    }

    c.JSON(200, pair)
})
```

---

## 核心接口

```go
type Authenticator interface {
    GenerateTokenPair(ctx context.Context, claims *Claims) (*TokenPair, error)
    ValidateAccessToken(ctx context.Context, token string) (*Claims, error)
    ValidateRefreshToken(ctx context.Context, token string) (*Claims, error)
    RefreshToken(ctx context.Context, refreshToken string) (*TokenPair, error)
    GinMiddleware() gin.HandlerFunc
}
```

### Claims

```go
type Claims struct {
    jwt.RegisteredClaims

    TokenType TokenType      `json:"typ,omitempty"`
    Username  string         `json:"uname,omitempty"`
    Roles     []string       `json:"roles,omitempty"`
    Extra     map[string]any `json:"extra,omitempty"`
}
```

说明：

- `TokenType` 由组件内部写入，业务方通常不需要手动设置。
- `Username`、`Roles`、`Extra` 用于承载业务身份信息。
- `GenerateTokenPair` 会复制输入 claims，不会修改原对象。

### TokenPair

```go
type TokenPair struct {
    AccessToken           string
    RefreshToken          string
    AccessTokenExpiresAt  time.Time
    RefreshTokenExpiresAt time.Time
    TokenType             string
}
```

---

## 配置

| 字段 | 默认值 | 说明 |
| --- | --- | --- |
| `SecretKey` | 必填 | HMAC 签名密钥，至少 32 字符 |
| `SigningMethod` | `HS256` | 当前仅支持 HS256 |
| `Issuer` | 空 | 可选签发者约束 |
| `Audience` | 空 | 可选受众约束 |
| `AccessTokenTTL` | `15m` | access token 有效期 |
| `RefreshTokenTTL` | `7d` | refresh token 有效期 |
| `TokenLookup` | 空 | access token 提取方式，留空使用默认多源查找 |
| `TokenHeadName` | `Bearer` | Authorization header 前缀 |

### Access Token 提取方式

`GinMiddleware()` 内部只负责提取和校验 **access token**。

当 `TokenLookup` 留空时，提取顺序为：

1. `Authorization: Bearer <token>`
2. `?token=<token>`
3. `jwt=<token>` cookie

如果希望只从固定来源提取，可配置：

```go
&auth.Config{
    SecretKey:   "...",
    TokenLookup: "header:Authorization",
}
```

---

## Gin 集成

### 认证中间件

`GinMiddleware()` 的语义是：

- 自动提取 access token；
- 自动校验签名、过期时间、issuer、audience；
- 拒绝 refresh token 直接访问业务接口；
- 验证成功后把 claims 放进 `gin.Context`。

### 角色校验

`RequireRoles` 采用 **OR 逻辑**：

```go
r.GET("/admin", auth.RequireRoles("admin"), handler)
r.GET("/moderate", auth.RequireRoles("admin", "moderator"), handler)
```

这表示用户只要拥有任意一个指定角色即可通过。

---

## 前端交互模型

推荐的前端使用方式是：

1. 登录成功后保存 `access_token` 与 `refresh_token`。
2. 普通业务请求只携带 `access_token`。
3. 当业务接口因 access token 过期返回 401 时，前端调用刷新接口。
4. 刷新接口使用 `refresh_token` 换发新的 `TokenPair`。
5. 刷新失败则清理本地登录态并跳转登录页。

也就是说，**普通业务请求不需要同时携带两个 token**。

---

## 指标

当前组件导出两个指标常量：

| 指标名 | 类型 | 标签 | 说明 |
| --- | --- | --- | --- |
| `auth_tokens_validated_total` | Counter | `status`, `error_type` | token 校验次数 |
| `auth_tokens_refreshed_total` | Counter | `status` | token 刷新次数 |

注意：

- token 缺失不计入校验失败指标；
- 当前指标没有区分 access / refresh 类型；
- 若未来需要更细的观测维度，可在后续版本扩展。

---

## 边界与限制

当前 `auth` 组件明确**不提供**以下能力：

- token 撤销；
- 黑名单；
- 单设备登录；
- refresh token 持久化；
- refresh token 重放检测；
- OAuth2 / OIDC / SSO。

因此，它更适合作为**应用自建认证层**，而不是完整身份系统。
