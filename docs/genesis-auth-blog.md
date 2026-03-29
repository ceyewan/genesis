# Genesis auth：双 JWT 令牌认证组件的设计与实现

Genesis `auth` 是治理层（L3）的认证组件，负责应用自建认证体系中的令牌签发、校验与 Gin 接入。它解决的问题不是"如何做统一身份中心"，而是"当应用决定自己管理用户登录态时，如何用尽量低的复杂度提供一套语义清晰、边界明确、可直接接入业务 API 的双令牌认证能力"。这篇文章聚焦它为什么采用双 JWT 令牌模型、为什么保留 Gin 集成、为什么当前阶段故意不引入外部存储，以及这种设计适合什么场景、不适合什么场景。

---

## 1 背景

在很多中小型服务和内部系统里，认证并不一定需要 OAuth2、OIDC 或统一身份中心。更常见的需求是：应用自己完成登录校验，在本服务或同一业务系统内签发令牌，让前端携带令牌访问后续接口。这个场景的核心矛盾在于，系统既希望 access token 足够短，降低泄露后的风险，又希望用户不必频繁重新登录。因此单 access token 模型通常很快就会走向双令牌模型：短期 access token 负责访问业务接口，长期 refresh token 负责在窗口内换发新的 token。

如果没有统一组件，业务代码很容易重复发明同样的轮子：手写 JWT claims、手写签名与验证、手写过期判断、手写 Gin middleware、手写角色检查。更麻烦的是，不同服务会在令牌刷新、错误处理、Header 提取顺序这些细节上各做一套，最后形成不一致的认证语义。Genesis `auth` 的价值就在这里：它不是完整身份平台，而是把"应用自建双令牌认证"这条最常见主路径组件化。

---

## 2 基础原理

JWT 适合承载自包含的访问令牌。服务端签发后，后续校验只需要同一份签名密钥，不需要额外查库。这带来两个直接好处：一是多副本部署下可以天然水平扩展；二是业务服务在认证主链路上没有额外依赖。

但 JWT 并不自动等于"完整认证体系"。它默认解决的是令牌结构、签名与标准声明问题，并不解决会话撤销、单设备登录、refresh token 重放检测、黑名单等问题。因此 Genesis `auth` 的一个核心设计前提是：先把双 JWT 令牌这条主路径做正确，把 access token 和 refresh token 的职责彻底分开；更强的会话语义以后再通过引入存储扩展。

在这套模型里，业务接口只接受 access token。refresh token 不参与普通业务请求，只在 access token 过期、前端需要续期时，拿来调用刷新接口。这个分工非常关键，因为它决定了后续接口设计、中间件设计和前端交互方式。

---

## 3 设计目标

`auth` 当前版本围绕以下几个目标展开：

1. **双令牌语义明确**：access token 与 refresh token 在 claims、校验路径和使用方式上都必须区分。
2. **无状态主路径**：签发、校验和换发不依赖 Redis 或数据库。
3. **Gin 接入低成本**：保留 `GinMiddleware` 和 `RequireRoles`，让项目接入足够直接。
4. **错误语义稳定**：业务方只需要处理清晰的哨兵错误，不依赖第三方库的原始文案。
5. **先收敛边界，再谈扩展**：当前故意不做撤销、黑名单和 OAuth/OIDC，避免把组件做成"什么都想管一点"的中间态。

这些目标共同决定了 `auth` 的接口形状：它要足够小，足够明确，但不能继续假装单令牌接口就能表达双令牌语义。

---

## 4 核心接口与配置

`auth` 对外暴露的核心接口如下：

```go
type Authenticator interface {
    GenerateTokenPair(ctx context.Context, claims *Claims) (*TokenPair, error)
    ValidateAccessToken(ctx context.Context, token string) (*Claims, error)
    ValidateRefreshToken(ctx context.Context, token string) (*Claims, error)
    RefreshToken(ctx context.Context, refreshToken string) (*TokenPair, error)
    GinMiddleware() gin.HandlerFunc
}
```

接口设计的重点不在方法数量，而在语义明确。旧的单令牌接口 `GenerateToken / ValidateToken / RefreshToken(token string)` 最大的问题不是代码不好写，而是它无法准确表达 refresh token 的角色。现在的接口直接把 access token 校验和 refresh token 校验拆开，让调用方在方法名层面就不会混淆。

配置项方面，当前仍然保持克制：

| 字段 | 默认值 | 说明 |
| --- | --- | --- |
| `SecretKey` | 必填 | HMAC 签名密钥，至少 32 字符 |
| `SigningMethod` | `HS256` | 当前只支持 HS256 |
| `Issuer` | 空 | 可选签发者约束 |
| `Audience` | 空 | 可选受众约束 |
| `AccessTokenTTL` | `15m` | access token 有效期 |
| `RefreshTokenTTL` | `7d` | refresh token 有效期 |
| `TokenLookup` | 空 | access token 提取方式 |
| `TokenHeadName` | `Bearer` | Authorization header 前缀 |

这里有一个刻意的设计选择：`TokenLookup` 只用于 access token 提取，因为普通请求只应该携带 access token。refresh token 的提取通常由业务刷新接口显式决定，而不是混进全局 middleware。

---

## 5 核心概念与数据模型

这套组件有两个最重要的概念：`Claims` 和 `TokenPair`。

`Claims` 仍然基于 `jwt.RegisteredClaims` 扩展，但现在增加了 `TokenType` 字段，用来标识令牌到底是 `access` 还是 `refresh`。这一点看起来只是多了一个字段，实际意义非常大，因为它把"令牌用途"从隐式约定变成了显式声明。后续 `ValidateAccessToken` 和 `ValidateRefreshToken` 会同时校验签名、标准声明和 `TokenType`，从而保证 refresh token 不能拿去访问业务接口，access token 也不能伪装成 refresh token。

`TokenPair` 是登录和刷新接口的统一返回结构，它同时返回 access token、refresh token、两者的过期时间，以及 HTTP `Authorization` 头使用的认证方案。这么设计的目的是让前端逻辑简单而稳定。登录成功时前端拿到一对 token，普通业务请求只发送 access token；当接口因为 access token 过期返回 401 时，前端再用 refresh token 调刷新接口，拿到一对新的 token 并覆盖本地状态。

这里还需要刻意区分两个容易混淆的概念：`Claims.TokenType` 表示 JWT 的业务用途，值是 `access` 或 `refresh`；`TokenPair.AuthorizationScheme` 表示 HTTP 头里的认证方案，默认是 `Bearer`。前者参与签发与校验，后者只是帮助客户端正确组装 `Authorization` 头，不能混为一谈。

从这个模型可以看出，`auth` 当前更接近"应用内认证组件"而不是"认证平台"。它管理的是令牌本身，而不是更完整的会话生命周期。

---

## 6 关键实现思路

双令牌签发的主链路很直接：`GenerateTokenPair` 接收一份业务 claims，先深拷贝成 access claims，再深拷贝成 refresh claims。两份 claims 共享 `sub`、`iss`、`aud`、用户名、角色等业务信息，但分别写入不同的 `TokenType` 和不同的过期时间。access token 使用 `AccessTokenTTL`，refresh token 使用 `RefreshTokenTTL`。签发时还会写入 `iat` 和 `jti`，其中 `jti` 的存在保证即使在同一秒内刷新，得到的新 token 也不会与旧 token 完全相同。

校验链路同样围绕类型分离展开。`ValidateAccessToken` 和 `ValidateRefreshToken` 最终都会走统一的 JWT 解析逻辑：先校验算法、签名、`iss`、`aud`、`exp` 等标准声明，再校验 `TokenType` 是否符合预期。如果类型不匹配，即使签名和过期时间都合法，也会返回 `ErrInvalidToken`。这让 access token 和 refresh token 的语义边界真正落到了实现里，而不是停留在调用方约定。

刷新链路的核心是"refresh token 只做换发，不做业务访问"。`RefreshToken` 先调用 `ValidateRefreshToken`，只有 refresh token 能通过。通过后，它会复制这份 refresh token 中的业务 claims，清空 `typ`、`exp`、`iat`、`jti` 等由系统生成的字段，再重新走一遍 `GenerateTokenPair`。这样拿到的是一对新的 token，而不是沿用旧 refresh token 重新签一个 access token。

Gin 中间件则只服务于 access token 主链路。它按配置提取 access token，调用 `ValidateAccessToken`，校验通过后把 claims 放进 `gin.Context`。这个设计看起来保守，但它避免了很多后续混乱：一旦 middleware 接受 refresh token，前端或调用方很容易偷懒把 refresh token 直接用于业务访问，双令牌模型就会被破坏。

---

## 7 工程取舍与设计权衡

这里最核心的权衡是：为什么先做双 JWT 令牌，而不是一步引入 refresh token 存储。

原因很简单。当前 `auth` 的主要目标是把 access token / refresh token 的语义先做正确，而不是立刻承诺完整会话体系。只要一引入存储，设计复杂度会马上上升：需要 `jti` 持久化、需要 refresh token 轮换状态、需要撤销标记、需要处理并发刷新和重放检测、需要讨论 Redis 还是数据库、需要补更多运维与容灾策略。这些都不是不该做，而是应该放在下一阶段，在边界清晰的前提下做。

另一个取舍是为什么继续保留 Gin 集成。严格说，把 `gin.HandlerFunc` 放进核心接口会增加框架耦合；但当前 Genesis 的实际使用场景里，你明确需要 Gin，而且 `RequireRoles` 在 RBAC 场景下确实能显著降低接入成本。既然这个组件首先服务于实际项目，而不是抽象洁癖，那么保留 Gin 适配层就是合理的工程取舍。相应地，我们做的不是删除它，而是把它的语义收敛得更安全：默认错误响应更保守，middleware 只认 access token。

还有一个值得强调的权衡是算法支持。当前只支持 HS256，会让一些人觉得"不够通用"。但在 Genesis 这种组件库里，过早支持多算法、多密钥来源、`kid`、JWKS 往往会先带来复杂性，而不是带来真实收益。只支持 HS256 的代价是暂时不适合对接外部 IdP，收益是接口和实现都更收敛，主路径更容易做对。

---

## 8 适用场景与实践建议

当前 `auth` 适合以下场景：应用自己控制登录流程，用户规模和安全复杂度尚未逼到必须上统一身份中心；系统希望使用双令牌模型降低 access token 风险，同时又不想引入 Redis 或数据库来管理 refresh token；项目主要运行在 Gin 栈上，认证接入成本要足够低。

不适合的场景同样明确：你需要强制下线、单设备登录、refresh token 撤销、重放检测、统一身份中心、第三方登录、企业 SSO 或 OAuth2/OIDC 互操作时，当前 `auth` 都不是终点方案。此时更合理的做法是要么在下一阶段为 refresh token 引入存储，要么直接转向更完整的身份系统。

前端接入时的推荐实践也很重要。普通业务请求只发送 access token，不要把 refresh token 带进每个 API。refresh token 只在 access token 失效后调用刷新接口使用。刷新成功后，前端应同时替换 access token 和 refresh token，而不是只替换 access token。否则虽然当前版本没有重放检测，但前端行为会和未来更严格的轮换模型产生冲突。

在 claims 设计上也建议保持克制。JWT 不是加密容器，业务方不应该把敏感明文塞进 `Extra`。更适合放进去的是稳定的身份字段、角色、少量授权上下文，而不是高敏感信息或频繁变化的信息。

---

## 9 常见误区

最常见的误区是把 refresh token 当成"更长寿命的 access token"。如果 refresh token 也能直接访问业务接口，那它和 access token 的区别就只剩过期时间，双令牌模型就失去了意义。Genesis `auth` 通过 `TokenType` 校验和 `GinMiddleware` 的 access-only 设计，明确阻止了这种用法。

第二个误区是把"无存储双 JWT 令牌"误解成"完整登录体系"。当前版本没有撤销能力，也没有重放检测，意味着一旦 refresh token 泄露，在其有效期内仍可能被使用。因此它适合主路径认证，不适合需要强会话控制的高安全场景。

第三个误区是把 README、blog 和 `go doc` 混为一谈。README 解决的是快速接入，`go doc` 解决的是 API 怎么调用，而这篇 blog 解决的是为什么要做成双 JWT、为什么先不引入存储、为什么继续保留 Gin 集成。三者各有角色，不能互相替代。

---

## 10 总结

Genesis `auth` 当前阶段的核心价值，不是把认证问题一口气做完，而是把应用自建认证里最常见、最容易写乱的那部分先做正确：双 JWT 令牌、类型分离校验、统一刷新语义、统一 Gin 接入和简单 RBAC。它故意不去承诺完整会话体系，也正因为这种克制，才让当前接口、实现和使用方式保持一致。

后续如果需要更强的会话控制，最自然的演进方向不是继续往单纯 JWT 里堆逻辑，而是在当前双令牌语义稳定的基础上，为 refresh token 引入持久化、撤销和重放检测能力。那会是下一阶段的问题，而不是这一阶段就应该混进来的复杂度。
