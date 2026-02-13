# Genesis Auth：JWT 认证组件的设计与实现

Genesis `auth` 是治理层（L3）的认证组件，基于 JWT（JSON Web Token）提供统一的令牌签发、验证、刷新与 Gin 中间件集成能力。它遵循 Genesis 基础规范，使用 `clog`、`metrics`、`xerrors` 构建可观测、可注入、可维护的认证基础设施。

---

## 0 摘要

- `auth` 将认证能力抽象为 `Authenticator` 接口，统一 `GenerateToken`、`ValidateToken`、`RefreshToken`、`GinMiddleware` 四类核心操作
- 签名算法限制为 HS256，通过 `jwt.WithValidMethods` 阻断算法混淆攻击
- Token 提取支持"可配置单源"与"默认多源回退"两种模式：`header:Authorization → query:token → cookie:jwt`
- 刷新策略采用"忽略时间声明校验的解析 + Issuer/Audience/NBF/IAT/RefreshTTL 二次判定"的分段验证
- Claims 采用深拷贝机制，确保 `GenerateToken` 不修改原始对象
- RBAC 通过 `RequireRoles` 中间件实现，采用 OR 逻辑（满足任一角色即可通过）

---

## 1 背景：微服务认证的核心问题

在微服务架构中，认证系统需要同时满足以下要求：

- **无状态扩展**：服务多副本部署时不依赖本地会话存储
- **跨入口一致性**：HTTP API、WebSocket、浏览器请求使用统一的认证语义
- **安全可控**：签名验证、过期检查、签发方校验、受众限制等规则必须明确
- **可观测性**：区分验证失败类型（过期、签名错误、格式错误），便于监控与排障
- **可集成性**：中间件接入后，业务 Handler 能直接获取身份声明

JWT 适合承载"自包含、可验签"的访问令牌，而 `auth` 的目标是将这套能力组件化，避免业务重复实现。

---

## 2 核心设计

### 2.1 接口抽象

`auth` 对外暴露 `Authenticator` 接口：

```go
type Authenticator interface {
    GenerateToken(ctx context.Context, claims *Claims) (string, error)
    ValidateToken(ctx context.Context, token string) (*Claims, error)
    RefreshToken(ctx context.Context, token string) (string, error)
    GinMiddleware() gin.HandlerFunc
}
```

这种抽象让业务代码依赖能力而非实现，后续替换认证方案时无需修改调用侧。

### 2.2 配置模型

组件配置集中在 `auth.Config`：

| 字段              | 类型     | 默认值         | 说明           |
| ----------------- | -------- | -------------- | -------------- |
| `SecretKey`       | string   | 必填，≥32 字符 | HMAC 签名密钥  |
| `SigningMethod`   | string   | `HS256`        | 签名算法       |
| `Issuer`          | string   | -              | 签发者标识     |
| `Audience`        | []string | -              | 受众列表       |
| `AccessTokenTTL`  | duration | `15m`          | 访问令牌有效期 |
| `RefreshTokenTTL` | duration | `7d`           | 刷新令牌有效期 |
| `TokenLookup`     | string   | -              | Token 提取位置 |
| `TokenHeadName`   | string   | `Bearer`       | Header 前缀    |

配置验证在构造阶段执行：密钥长度不足、算法不支持、TTL 非正数等错误在初始化时暴露，避免运行时才发现配置问题。

### 2.3 Claims 结构

```go
type Claims struct {
    jwt.RegisteredClaims           // 标准声明
    Username string                `json:"uname,omitempty"`
    Roles    []string              `json:"roles,omitempty"`
    Extra    map[string]any        `json:"extra,omitempty"`
}
```

标准声明包含：`iss`（签发者）、`sub`（主体）、`aud`（受众）、`exp`（过期时间）、`nbf`（生效时间）、`iat`（签发时间）。业务字段使用短 key（如 `uname`）减少 Token 体积。

---

## 3 令牌签发（GenerateToken）

### 3.1 签发流程

签发过程包含四个步骤：首先深拷贝 Claims 以避免修改原对象，然后回填标准声明（exp、iat、iss、aud 缺省时注入），接着使用 HS256 算法签名，最后记录日志并返回 Token。

### 3.2 Claims 不可变性

`GenerateToken` 首先执行 `cloneClaims(claims)`，对 `Roles`、`Extra`、`Audience` 等引用类型执行深拷贝。这确保：

- 调用方传入的 Claims 对象不会被修改
- 多次调用 `GenerateToken` 不会产生副作用
- 并发场景下数据安全性

### 3.3 默认值注入

标准声明的回填逻辑：

- `ExpiresAt`：当前时间 + `AccessTokenTTL`
- `IssuedAt`：当前时间
- `Issuer`：使用配置值（若 Claims 未设置）
- `Audience`：使用配置值（若 Claims 未设置）

---

## 4 令牌验证（ValidateToken）

### 4.1 验证流程

验证过程首先使用 `jwt.ParseWithClaims` 解析 Token，通过 `jwt.WithValidMethods` 限定算法为 HS256，然后根据配置附加 Issuer、Audience 校验。最后将错误归类为 `expired`、`invalid_signature`、`invalid_token` 三类，并记录 `auth_tokens_validated_total` 指标。

### 4.2 错误语义映射

| 底层 JWT 错误                  | 映射后错误            | error_type 标签     |
| ------------------------------ | --------------------- | ------------------- |
| `jwt.ErrTokenExpired`          | `ErrExpiredToken`     | `expired`           |
| `jwt.ErrTokenSignatureInvalid` | `ErrInvalidSignature` | `invalid_signature` |
| 其他                           | `ErrInvalidToken`     | `invalid_token`     |

稳定的错误语义让业务侧可以做精确的错误处理，而不依赖第三方库的原始错误文案。

### 4.3 算法混淆防护

通过 `jwt.WithValidMethods([]string{"HS256"})` 强制限定签名算法。这防止了攻击者将 Token Header 中的 `alg` 修改为 `none` 而绕过验证的攻击手法。

---

## 5 令牌刷新（RefreshToken）

### 5.1 分段验证策略

刷新不是盲目重签，而是分两段校验。

第一阶段使用 `jwt.WithoutClaimsValidation` 跳过时间校验，仅验证签名与结构合法性。第二阶段执行自定义的刷新策略校验：iss 必须匹配配置（若配置了），aud 必须与配置有交集（若配置了），nbf 不能在未来，iat 必须存在且满足 `now <= iat + RefreshTokenTTL`。

### 5.2 刷新窗口设计

刷新窗口独立于访问令牌有效期。从签发时间开始，AccessTokenTTL 决定访问令牌的生命周期，RefreshTokenTTL 决定整个刷新窗口的长度。这种设计允许访问令牌已过期但仍在刷新窗口内的令牌续期，同时阻断超窗刷新。

### 5.3 重签机制

通过后清空 `exp` 和 `iat`，重新调用 `GenerateToken`。这确保新令牌的时间戳反映当前时间，而非原始签发时间。

---

## 6 Token 提取与 Gin 集成

### 6.1 提取策略

单源模式通过配置 `TokenLookup` 指定唯一来源，如 `header:Authorization`、`query:token` 或 `cookie:jwt`。

多源回退模式在 `TokenLookup` 留空时生效，按顺序尝试三个来源：Header（`Authorization: Bearer <token>`）、Query（`?token=<token>`）、Cookie（`jwt=<token>`）。这种设计让同一配置可以同时支持 REST API（Header）、WebSocket（Query）、前端应用（Cookie）。

### 6.2 Gin 中间件行为

中间件首先从请求提取 Token，然后调用 `ValidateToken` 验证。验证成功后将 Claims 存入 `gin.Context`（键：`auth:claims`）并调用 `c.Next()`，失败则返回 401。

注意：Token 缺失不计入验证失败指标，因为用户未提供 Token 属于正常业务场景，而非验证错误。

### 6.3 角色鉴权（RequireRoles）

`RequireRoles(...)` 提供 RBAC 能力：

```go
// 用户必须拥有 admin 或 editor 任一角色
r.GET("/admin", auth.RequireRoles("admin", "editor"), handler)
```

采用 OR 逻辑：用户拥有任意一个指定角色即可通过。若没有任何匹配角色，返回 403 Forbidden。

---

## 7 JWT 底层原理

### 7.1 JWT 结构

JWT（JWS 格式）由三部分组成：`base64url(header).base64url(payload).base64url(signature)`。Header 包含算法类型（`{"alg":"HS256","typ":"JWT"}`），Payload 包含标准声明与业务声明，Signature 是 HMAC-SHA256 签名结果。

注意：JWT 默认不是加密格式，Payload 可被 Base64 解码查看，不应存放敏感明文。

### 7.2 标准声明的语义

| 声明  | 含义       | 使用场景                         |
| ----- | ---------- | -------------------------------- |
| `sub` | Subject    | 用户唯一标识                     |
| `exp` | Expiration | 硬截止时间，过期即拒绝           |
| `iat` | Issued At  | 签发时间，用于刷新窗口与时钟校验 |
| `nbf` | Not Before | 生效时间，防止提前使用           |
| `iss` | Issuer     | 签发者，防止跨系统串用           |
| `aud` | Audience   | 受众，限制令牌可使用的服务范围   |

### 7.3 常见误区

- **JWT 自带加密**：默认只有签名保护完整性，不保护机密性
- **只校验签名就够**：`iss/aud/exp/nbf` 都是必要约束
- **允许客户端传入任意 alg**：必须在服务端白名单固定算法

---

## 8 可观测性与错误语义

### 8.1 指标设计

| 指标名                        | 类型    | 标签                   | 描述           |
| ----------------------------- | ------- | ---------------------- | -------------- |
| `auth_tokens_validated_total` | Counter | `status`, `error_type` | Token 验证计数 |
| `auth_tokens_refreshed_total` | Counter | `status`               | Token 刷新计数 |

### 8.2 Prometheus 查询示例

```promql
# 验证成功率
rate(auth_tokens_validated_total{status="success"}[5m])
/ rate(auth_tokens_validated_total[5m])

# 验证失败分布（按错误类型）
sum by (error_type) (
    rate(auth_tokens_validated_total{status="error"}[5m])
)
```

---

## 9 认证体系之外的补充方式

JWT 适合无状态访问令牌场景，但并非所有场景的唯一解。

### 9.1 Session + Cookie（有状态）

- 服务端保存会话（Redis/DB），客户端持有 Session ID Cookie
- 优点：可服务端即时失效（登出、风控封禁）
- 代价：需要中心化会话存储与粘性治理

### 9.2 Opaque Token + Introspection

- 令牌本身无业务信息，只是随机串
- 资源服务通过鉴权服务做在线查询
- 优点：撤销与权限变更即时生效
- 代价：增加鉴权网络开销与可用性依赖

### 9.3 mTLS（双向证书）

- 用于高安全内网调用或零信任网络
- 以证书身份替代共享密钥，天然抗重放与中间人风险更强

### 9.4 OAuth2/OIDC（第三方身份体系）

- 当系统需要接入企业 SSO、社交登录、多租户 IdP 时更合适
- 可将 Genesis `auth` 作为资源服务本地校验层，与外部 IdP 联动

---

## 10 最佳实践与常见坑

### 10.1 推荐配置

生产环境推荐配置如下：secret_key 从环境变量读取，signing_method 使用 HS256，设置合理的 issuer 和 audience，access_token_ttl 建议 15 分钟，refresh_token_ttl 建议 7 天，token_lookup 留空使用默认多源提取。

### 10.2 密钥管理

生产环境密钥必须从环境变量或密钥管理系统读取，不应硬编码。密钥长度至少 32 字符（256 位），推荐使用随机生成的高熵字符串。密钥轮换需要平滑过渡策略（双密钥验证期）。

### 10.3 TTL 选择建议

| 场景          | AccessTokenTTL | RefreshTokenTTL |
| ------------- | -------------- | --------------- |
| 普通 Web 应用 | 15m - 1h       | 7d - 30d        |
| 移动应用      | 1h - 24h       | 30d - 90d       |
| 高安全系统    | 5m - 15m       | 1h - 24h        |

### 10.4 常见误区

在 Token 中存储大量用户信息是常见误区，正确做法是只存必要的标识（如 user_id），详细信息从数据库查询。认为 RefreshToken 可以无限期使用也是错误的，RefreshToken 也应有合理的有效期，超期需重新登录。对于登出场景，JWT 本身不支持主动失效，登出可通过客户端删除 Token 实现，高安全场景需要维护黑名单。

---

## 11 总结

Genesis `auth` 提供了一套生产可用的 JWT 认证组件，其核心价值在于：通过接口抽象实现可替换性，通过构造时校验确保配置安全下限，通过分段验证实现可控的刷新策略，通过可观测性支持生产监控。

认证体系通常是组合拳：`auth` 解决的是本地令牌能力的核心拼图，而非替代全部身份基础设施。实际系统中，JWT 常与 Session、OAuth2、mTLS 等机制按场景组合使用。
