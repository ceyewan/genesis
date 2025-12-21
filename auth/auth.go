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
	config  *Config
	options *options
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
	start := time.Now()

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

	// Metrics: Token 生成成功
	if counter := a.options.GetCounter("auth_tokens_generated_total", "Total number of tokens generated"); counter != nil {
		counter.Add(ctx, 1)
	}

	// Metrics: Token 生成耗时
	if histogram := a.options.GetHistogram("auth_token_generation_duration_seconds", "Token generation duration in seconds"); histogram != nil {
		histogram.Record(ctx, time.Since(start).Seconds())
	}

	return tokenString, nil
}

// ValidateToken 验证 Token
func (a *jwtAuth) ValidateToken(ctx context.Context, tokenString string) (*Claims, error) {
	start := time.Now()

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
		if counter := a.options.GetCounter("auth_tokens_validated_total", "Total number of tokens validated"); counter != nil {
			counter.Add(ctx, 1, metrics.L("status", "error"), metrics.L("error_type", errType))
		}
		return nil, err
	}

	if !token.Valid {
		return nil, ErrInvalidToken
	}

	a.options.logger.Info("token validated", clog.String("user_id", claims.Subject))

	// Metrics: 验证成功
	if counter := a.options.GetCounter("auth_tokens_validated_total", "Total number of tokens validated"); counter != nil {
		counter.Add(ctx, 1, metrics.L("status", "success"))
	}

	// Metrics: 验证耗时
	if histogram := a.options.GetHistogram("auth_token_validation_duration_seconds", "Token validation duration in seconds"); histogram != nil {
		histogram.Record(ctx, time.Since(start).Seconds())
	}

	return claims, nil
}

// RefreshToken 刷新 Token
func (a *jwtAuth) RefreshToken(ctx context.Context, token string) (string, error) {
	claims, err := a.ValidateToken(ctx, token)
	if err != nil {
		// Metrics: 刷新失败 - 验证失败
		if counter := a.options.GetCounter("auth_tokens_refreshed_total", "Total number of tokens refreshed"); counter != nil {
			counter.Add(ctx, 1, metrics.L("status", "error"), metrics.L("error_type", "validation_failed"))
		}
		return "", err
	}

	// 更新过期时间和签发时间
	claims.ExpiresAt = nil
	claims.IssuedAt = nil

	// 使用相同的 claims，重新生成 token
	newToken, err := a.GenerateToken(ctx, claims)
	if err != nil {
		// Metrics: 刷新失败 - 生成失败
		if counter := a.options.GetCounter("auth_tokens_refreshed_total", "Total number of tokens refreshed"); counter != nil {
			counter.Add(ctx, 1, metrics.L("status", "error"), metrics.L("error_type", "generation_failed"))
		}
		return "", err
	}

	a.options.logger.Info("token refreshed", clog.String("user_id", claims.Subject))

	// Metrics: 刷新成功
	if counter := a.options.GetCounter("auth_tokens_refreshed_total", "Total number of tokens refreshed"); counter != nil {
		counter.Add(ctx, 1, metrics.L("status", "success"))
	}

	return newToken, nil
}

// ExtractToken 从请求中提取 token（导出用于中间件）
func (a *jwtAuth) ExtractToken(r *http.Request) (string, error) {
	parts := strings.Split(a.config.TokenLookup, ":")
	if len(parts) != 2 {
		return "", ErrMissingToken
	}

	source, key := parts[0], parts[1]

	switch source {
	case "header":
		authHeader := r.Header.Get(key)
		if authHeader == "" {
			return "", ErrMissingToken
		}
		tokenParts := strings.SplitN(authHeader, " ", 2)
		if len(tokenParts) != 2 || tokenParts[0] != a.config.TokenHeadName {
			return "", ErrInvalidToken
		}
		return tokenParts[1], nil

	case "query":
		token := r.URL.Query().Get(key)
		if token == "" {
			return "", ErrMissingToken
		}
		return token, nil

	case "cookie":
		cookie, err := r.Cookie(key)
		if err != nil {
			return "", ErrMissingToken
		}
		return cookie.Value, nil

	default:
		return "", ErrMissingToken
	}
}

const ClaimsKey = "auth:claims"
