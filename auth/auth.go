// Package auth 提供基于 JWT 的认证能力。
//
// 遵循 Genesis L3 治理层规范，支持：
//   - Token 生成、验证与刷新
//   - Gin 中间件集成
//   - 基于角色的访问控制 (RBAC)
//   - 多种 Token 提取方式 (Header, Cookie, Query)
//
// 基本使用：
//
//	authenticator, _ := auth.New(&auth.Config{SecretKey: "..."})
//	token, _ := authenticator.GenerateToken(ctx, &auth.Claims{
//	    RegisteredClaims: jwt.RegisteredClaims{Subject: "user-123"},
//	})
package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
	"github.com/ceyewan/genesis/xerrors"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// Authenticator 认证器接口
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

// jwtAuth JWT 认证实现
type jwtAuth struct {
	config         *Config
	options        *options
	validatedCount metrics.Counter // Token 验证计数
	refreshedCount metrics.Counter // Token 刷新计数
}

// New 创建 Authenticator
func New(cfg *Config, opts ...Option) (Authenticator, error) {
	if cfg == nil {
		return nil, ErrInvalidConfig
	}

	cfg.setDefaults()

	o := defaultOptions()
	for _, opt := range opts {
		opt(o)
	}

	auth := &jwtAuth{
		config:  cfg,
		options: o,
	}

	if err := auth.validate(); err != nil {
		return nil, err
	}

	// 初始化指标（Discard() 返回的 noopMeter 永远返回有效的 Counter，不需要判空）
	auth.validatedCount, _ = o.meter.Counter(
		MetricTokensValidated,
		"Total number of tokens validated",
	)
	auth.refreshedCount, _ = o.meter.Counter(
		MetricTokensRefreshed,
		"Total number of tokens refreshed",
	)

	return auth, nil
}

// validate 验证配置
func (a *jwtAuth) validate() error {
	if a.config.SecretKey == "" {
		return ErrInvalidConfig
	}

	if len(a.config.SecretKey) < 32 {
		return xerrors.Wrapf(ErrInvalidConfig, "secret_key must be at least 32 characters")
	}

	return nil
}

// GenerateToken 生成 Token
func (a *jwtAuth) GenerateToken(ctx context.Context, claims *Claims) (string, error) {
	if claims == nil {
		return "", ErrInvalidClaims
	}

	// 设置标准声明
	if claims.ExpiresAt == nil {
		claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(a.config.AccessTokenTTL))
	}
	if claims.IssuedAt == nil {
		claims.IssuedAt = jwt.NewNumericDate(time.Now())
	}
	if claims.Issuer == "" && a.config.Issuer != "" {
		claims.Issuer = a.config.Issuer
	}

	// 选择签名方法
	method := jwt.GetSigningMethod(a.config.SigningMethod)
	if method == nil {
		// 默认使用 HS256
		method = jwt.SigningMethodHS256
	}

	token := jwt.NewWithClaims(method, claims)
	tokenString, err := token.SignedString([]byte(a.config.SecretKey))
	if err != nil {
		return "", xerrors.Wrap(err, "failed to sign token")
	}

	a.options.logger.Info("token generated", clog.String("user_id", claims.Subject))

	return tokenString, nil
}

// ValidateToken 验证 Token
func (a *jwtAuth) ValidateToken(ctx context.Context, tokenString string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		// 验证签名算法
		if token.Method.Alg() != a.config.SigningMethod {
			// 如果配置中未指定或不匹配，尝试默认 HS256
			if a.config.SigningMethod == "" {
				if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, ErrInvalidSignature
				}
			} else {
				return nil, ErrInvalidSignature
			}
		}
		return []byte(a.config.SecretKey), nil
	})

	if err != nil {
		var errType string
		if errors.Is(err, jwt.ErrTokenExpired) {
			errType = "expired"
			err = ErrExpiredToken
		} else if errors.Is(err, jwt.ErrTokenSignatureInvalid) {
			errType = "invalid_signature"
			err = ErrInvalidSignature
		} else {
			errType = "invalid_token"
			err = ErrInvalidToken
		}

		// Metrics: 验证失败
		a.validatedCount.Add(ctx, 1, metrics.L("status", "error"), metrics.L("error_type", errType))
		return nil, err
	}

	if !token.Valid {
		a.validatedCount.Add(ctx, 1, metrics.L("status", "error"), metrics.L("error_type", "invalid_token"))
		return nil, ErrInvalidToken
	}

	a.options.logger.Info("token validated", clog.String("user_id", claims.Subject))

	// Metrics: 验证成功
	a.validatedCount.Add(ctx, 1, metrics.L("status", "success"))

	return claims, nil
}

// RefreshToken 刷新 Token
func (a *jwtAuth) RefreshToken(ctx context.Context, token string) (string, error) {
	claims, err := a.ValidateToken(ctx, token)
	if err != nil {
		// Metrics: 刷新失败（验证失败已经在 ValidateToken 中计数，这里只记录刷新失败）
		a.refreshedCount.Add(ctx, 1, metrics.L("status", "error"))
		return "", err
	}

	// 更新过期时间和签发时间
	claims.ExpiresAt = nil
	claims.IssuedAt = nil

	// 使用相同的 claims，重新生成 token
	newToken, err := a.GenerateToken(ctx, claims)
	if err != nil {
		a.refreshedCount.Add(ctx, 1, metrics.L("status", "error"))
		return "", err
	}

	a.options.logger.Info("token refreshed", clog.String("user_id", claims.Subject))

	// Metrics: 刷新成功
	a.refreshedCount.Add(ctx, 1, metrics.L("status", "success"))

	return newToken, nil
}

// ExtractToken 从请求中提取 token（导出用于中间件）
//
// 查找顺序（如果 TokenLookup 未配置）:
// 1. header:Authorization (Bearer token)
// 2. query:token
// 3. cookie:jwt
//
// 如果配置了 TokenLookup，则只按指定方式提取。
func (a *jwtAuth) ExtractToken(r *http.Request) (string, error) {
	// 如果用户配置了特定的 lookup 方式，只使用该方式
	if a.config.TokenLookup != "" {
		parts := strings.Split(a.config.TokenLookup, ":")
		if len(parts) != 2 {
			return "", ErrMissingToken
		}
		source, key := parts[0], parts[1]
		token, ok := a.extractFromSource(r, source, key)
		if !ok {
			return "", ErrMissingToken
		}
		return token, nil
	}

	// 默认多源查找：header -> query -> cookie
	// 1. 尝试从 header 提取
	if token, ok := a.extractFromSource(r, "header", "Authorization"); ok {
		return token, nil
	}
	// 2. 尝试从 query 提取
	if token, ok := a.extractFromSource(r, "query", "token"); ok {
		return token, nil
	}
	// 3. 尝试从 cookie 提取
	if token, ok := a.extractFromSource(r, "cookie", "jwt"); ok {
		return token, nil
	}

	return "", ErrMissingToken
}

// extractFromSource 从指定来源提取 token
// 返回 token 和是否成功找到（注意：找到但格式错误时也返回 ok=false）
func (a *jwtAuth) extractFromSource(r *http.Request, source, key string) (string, bool) {
	switch source {
	case "header":
		authHeader := r.Header.Get(key)
		if authHeader == "" {
			return "", false
		}
		tokenParts := strings.SplitN(authHeader, " ", 2)
		if len(tokenParts) != 2 || tokenParts[0] != a.config.TokenHeadName {
			return "", false
		}
		return tokenParts[1], true

	case "query":
		token := r.URL.Query().Get(key)
		if token == "" {
			return "", false
		}
		return token, true

	case "cookie":
		cookie, err := r.Cookie(key)
		if err != nil {
			return "", false
		}
		return cookie.Value, true

	default:
		return "", false
	}
}

const ClaimsKey = "auth:claims"
