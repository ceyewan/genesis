# Genesis auth：JWT 认证组件的设计与实现

Genesis `auth` 是治理层（L3）的认证组件，基于 JWT（JSON Web Token）提供统一的令牌签发、验证、刷新与 Gin 中间件集成能力。它遵循 Genesis 的基础规范，使用 `clog`、`metrics`、`xerrors` 构建可观测、可注入、可维护的认证能力。

---

## 0. 摘要

- `auth` 把认证能力抽象为 `Authenticator` 接口，统一了 `GenerateToken`、`ValidateToken`、`RefreshToken` 和 `GinMiddleware` 四类核心能力。
- 当前实现明确限制签名算法为 `HS256`，并通过 `jwt.WithValidMethods` 阻断算法混淆风险。
- Token 提取采用“可配置单源 + 默认多源回退”模式：`header:Authorization -> query:token -> cookie:jwt`。
- 刷新逻辑采用“签名有效但忽略时间声明校验”的解析策略，再叠加 `Issuer/Audience/NBF/IAT/RefreshTTL` 二次判定，确保刷新行为可控。
- 在 JWT 之外，生产系统通常还会结合 Session、Opaque Token、API Key、mTLS、OIDC 等机制，按场景组合使用。

---

## 1. 背景：微服务认证要解决的“真实问题”

在微服务场景中，认证系统通常要同时满足：

- 无状态扩展：服务多副本部署时，不依赖本地会话。
- 跨入口一致性：HTTP API、WebSocket、浏览器请求要有统一认证语义。
- 安全可控：过期、签名、签发方、受众等校验规则必须明确。
- 可观测：要知道“验证失败是过期、签名错误还是格式错误”。
- 可集成：中间件接入后，业务 Handler 能直接拿到身份声明。

JWT 适合承载“自包含、可验签”的访问令牌，而 `auth` 的目标是把这套能力组件化，避免业务重复实现。

---

## 2. 核心设计：统一接口与显式约束

### 2.1 接口抽象

`auth` 对外暴露 `Authenticator`：

- `GenerateToken(ctx, claims)`：签发访问令牌。
- `ValidateToken(ctx, token)`：验证并解析声明。
- `RefreshToken(ctx, token)`：在刷新窗口内重签令牌。
- `GinMiddleware()`：HTTP 鉴权中间件。

这种抽象让业务代码依赖能力而非实现，后续替换认证实现时不需要改调用侧。

### 2.2 配置模型

组件配置集中在 `auth.Config`，核心字段包括：

- 签名与声明：`secret_key`、`signing_method`、`issuer`、`audience`
- 生命周期：`access_token_ttl`、`refresh_token_ttl`
- 提取策略：`token_lookup`、`token_head_name`

默认值策略：

- `signing_method` 默认 `HS256`
- `access_token_ttl` 默认 `15m`
- `refresh_token_ttl` 默认 `7d`
- `token_head_name` 默认 `Bearer`
- `token_lookup` 留空时启用默认多源提取

### 2.3 安全约束（实现层）

当前实现在构造阶段进行强校验：

- `secret_key` 必须存在且长度至少 32 字符。
- 仅允许 `HS256`（其他算法直接返回配置错误）。
- `access_token_ttl`、`refresh_token_ttl` 必须为正数。

这保证了组件“默认安全下限”，避免弱配置进入运行态。

---

## 3. 鉴权流程与组件行为

### 3.1 令牌签发（GenerateToken）

签发流程：

1. 复制 `Claims`（避免修改调用方原对象）。
2. 回填标准声明：`exp`、`iat`、`iss`、`aud`（缺省时由配置注入）。
3. 使用配置签名算法创建 Token 并签名。
4. 记录日志（命名空间 `auth`）。

关键点是“声明不可变 + 默认值注入”，既减少调用方样板代码，也避免共享对象被隐式修改。

### 3.2 令牌验证（ValidateToken）

验证流程：

1. 使用 `jwt.ParseWithClaims` 解析并验签。
2. 通过 `jwt.WithValidMethods` 限定允许算法。
3. 根据配置附加 `issuer`、`audience` 校验选项。
4. 错误归类为：`expired`、`invalid_signature`、`invalid_token`。
5. 打点 `auth_tokens_validated_total` 并携带状态标签。

组件会把底层 JWT 错误映射为稳定的业务错误（`ErrExpiredToken` 等），便于上层统一处理。

### 3.3 令牌刷新（RefreshToken）

刷新不是“盲目重签”，而是分两段校验：

1. 先做“签名与结构合法”校验，时间声明暂不参与（`WithoutClaimsValidation`）。
2. 再做刷新策略校验：
   - `iss` 必须匹配（若配置了）
   - `aud` 必须有交集（若配置了）
   - `nbf` 不能在未来
   - `iat` 必须存在，且 `now <= iat + refresh_token_ttl`
3. 通过后清空 `exp/iat`，重新调用 `GenerateToken` 签发新令牌。

该策略允许“已过访问期但仍在刷新窗口”的令牌续期，同时阻断超窗刷新。

### 3.4 Token 提取与 Gin 集成

提取策略：

- 若配置 `token_lookup`，仅按该来源提取。
- 否则按默认顺序：Header -> Query -> Cookie。

Gin 中间件行为：

1. 从请求提取 Token。
2. 验证 Token 并解析 `Claims`。
3. 将 `Claims` 写入 `gin.Context`（键：`auth:claims`）。
4. 失败时返回 `401`。

`RequireRoles(...)` 提供 RBAC 角色校验中间件，缺少任一角色返回 `403`。

---

## 4. JWT 底层原理（面向工程实现）

### 4.1 JWT 是什么

JWT 是一种紧凑令牌格式（RFC 7519），常见形态是 JWS（已签名、未加密）：

`base64url(header).base64url(payload).base64url(signature)`

注意：JWT 默认不是加密格式，Payload 可被解码查看，不应放敏感明文。

### 4.2 签名与验签的核心过程

以 `HS256` 为例（对称密钥）：

1. 构造 Header：`{"alg":"HS256","typ":"JWT"}`
2. 构造 Payload：标准声明 + 业务声明（如 `sub`、`exp`、`roles`）
3. 对 `base64url(header)+"."+base64url(payload)` 做 HMAC-SHA256
4. 生成 Signature 并拼接成最终 Token

验证时重复计算签名并比对，同时校验时间与语义声明（`exp/nbf/iss/aud`）。

### 4.3 标准声明的工程意义

- `sub`：主体标识（通常是用户 ID）
- `exp`：过期时间（硬截止）
- `iat`：签发时间（用于刷新窗口与时钟校验）
- `nbf`：生效时间（防止提前使用）
- `iss`：签发者（防止跨系统串用）
- `aud`：受众（限制令牌可使用的服务范围）

### 4.4 常见误区

- 误区 1：JWT 自带加密。  
  实际：默认只有签名保护完整性，不保护机密性。
- 误区 2：只校验签名就够。  
  实际：`iss/aud/exp/nbf` 都是必要约束。
- 误区 3：允许客户端传入任意 `alg`。  
  实际：必须在服务端白名单固定可接受算法。

---

## 5. 可观测性与错误语义

`auth` 内建两个计数指标：

- `auth_tokens_validated_total`
- `auth_tokens_refreshed_total`

标签策略：

- 验证：`status=success|error`，失败时附带 `error_type`
- 刷新：`status=success|error`

同时，组件使用 `xerrors` 统一错误语义，业务侧可以稳定匹配错误类别，而不依赖第三方库原始错误文案。

---

## 6. JWT 之外的补充认证方式

JWT 适合“无状态访问令牌”，但不是所有场景的唯一解。常见补充方式：

### 6.1 Session + Cookie（有状态）

- 服务端保存会话（Redis/DB），客户端持有 Session ID Cookie。
- 优点：可服务端即时失效（登出、风控封禁）。
- 代价：需要中心化会话存储与粘性治理。

### 6.2 Opaque Token + Introspection

- 令牌本身无业务信息，只是随机串。
- 资源服务通过鉴权服务做在线查询（introspection）。
- 优点：撤销与权限变更即时生效。
- 代价：增加鉴权网络开销与可用性依赖。

### 6.3 API Key（服务到服务）

- 常用于内部系统或低复杂度 Open API。
- 适合“调用方身份识别 + 配额控制”，不适合表达复杂用户上下文。

### 6.4 mTLS（双向证书）

- 用于高安全内网调用或零信任网络。
- 以证书身份替代共享密钥，天然抗重放与中间人风险更强。

### 6.5 OAuth2/OIDC（第三方身份体系）

- 当系统需要接入企业 SSO、社交登录、多租户 IdP 时更合适。
- 可将 Genesis `auth` 作为资源服务本地校验层，与外部 IdP 联动。

---

## 7. 组合建议（实践导向）

- 面向 BFF/网关：`JWT Access Token + 短 TTL + RefreshToken`。
- 面向高安全后台：`JWT + Token 黑名单（登出撤销）`。
- 面向内部服务网格：`mTLS` 负责服务身份，JWT 负责终端用户身份透传。
- 面向开放平台：`OAuth2/OIDC` 负责授权，资源侧仍可用本地 JWT 验签加速。

结论：认证体系通常是组合拳，`auth` 解决的是“本地令牌能力”的核心拼图，而不是替代全部身份基础设施。
