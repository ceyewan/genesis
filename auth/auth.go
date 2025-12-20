package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
	"github.com/ceyewan/genesis/xerrors"
	"github.com/gin-gonic/gin"
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

	cfg.SetDefaults()

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

// Must 类似 New，但出错时 panic
func Must(cfg *Config, opts ...Option) Authenticator {
	auth, err := New(cfg, opts...)
	if err != nil {
		panic(err)
	}
	return auth
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
		expiresAt := time.Now().Add(a.config.AccessTokenTTL)
		claims.ExpiresAt = &expiresAt
	}
	if claims.IssuedAt == nil {
		issuedAt := time.Now()
		claims.IssuedAt = &issuedAt
	}
	if claims.Issuer == "" && a.config.Issuer != "" {
		claims.Issuer = a.config.Issuer
	}

	// 编码 header
	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	headerBytes, _ := json.Marshal(header)
	headerEncoded := base64.RawURLEncoding.EncodeToString(headerBytes)

	// 编码 payload
	payloadBytes, _ := json.Marshal(claims)
	payloadEncoded := base64.RawURLEncoding.EncodeToString(payloadBytes)

	// 生成签名
	message := headerEncoded + "." + payloadEncoded
	signature := a.sign([]byte(message))

	token := message + "." + signature

	a.options.logger.Info("token generated", clog.String("user_id", claims.UserID))

	// Metrics: Token 生成成功
	if counter := a.options.GetCounter("auth_tokens_generated_total", "Total number of tokens generated"); counter != nil {
		counter.Add(ctx, 1)
	}

	// Metrics: Token 生成耗时
	if histogram := a.options.GetHistogram("auth_token_generation_duration_seconds", "Token generation duration in seconds"); histogram != nil {
		histogram.Record(ctx, time.Since(start).Seconds())
	}

	return token, nil
}

// ValidateToken 验证 Token
func (a *jwtAuth) ValidateToken(ctx context.Context, token string) (*Claims, error) {
	start := time.Now()

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		// Metrics: 验证失败 - 格式错误
		if counter := a.options.GetCounter("auth_tokens_validated_total", "Total number of tokens validated"); counter != nil {
			counter.Add(ctx, 1, metrics.L("status", "error"), metrics.L("error_type", "invalid_format"))
		}
		return nil, ErrInvalidToken
	}

	headerEncoded, payloadEncoded, signatureEncoded := parts[0], parts[1], parts[2]

	// 验证签名
	expectedSignature := a.sign([]byte(headerEncoded + "." + payloadEncoded))
	if !hmac.Equal([]byte(signatureEncoded), []byte(expectedSignature)) {
		// Metrics: 验证失败 - 签名无效
		if counter := a.options.GetCounter("auth_tokens_validated_total", "Total number of tokens validated"); counter != nil {
			counter.Add(ctx, 1, metrics.L("status", "error"), metrics.L("error_type", "invalid_signature"))
		}
		return nil, ErrInvalidSignature
	}

	// 解码 payload
	payloadBytes, err := base64.RawURLEncoding.DecodeString(payloadEncoded)
	if err != nil {
		// Metrics: 验证失败 - 解码错误
		if counter := a.options.GetCounter("auth_tokens_validated_total", "Total number of tokens validated"); counter != nil {
			counter.Add(ctx, 1, metrics.L("status", "error"), metrics.L("error_type", "decode_error"))
		}
		return nil, ErrInvalidToken
	}

	claims := &Claims{}
	if err := json.Unmarshal(payloadBytes, claims); err != nil {
		// Metrics: 验证失败 - JSON 解析错误
		if counter := a.options.GetCounter("auth_tokens_validated_total", "Total number of tokens validated"); counter != nil {
			counter.Add(ctx, 1, metrics.L("status", "error"), metrics.L("error_type", "json_error"))
		}
		return nil, ErrInvalidClaims
	}

	// 检查过期时间
	if claims.ExpiresAt != nil && time.Now().After(*claims.ExpiresAt) {
		// Metrics: 验证失败 - Token 过期
		if counter := a.options.GetCounter("auth_tokens_validated_total", "Total number of tokens validated"); counter != nil {
			counter.Add(ctx, 1, metrics.L("status", "error"), metrics.L("error_type", "expired"))
		}
		return nil, ErrExpiredToken
	}

	a.options.logger.Info("token validated", clog.String("user_id", claims.UserID))

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

	// 使用相同的 claims，重新生成 token
	newToken, err := a.GenerateToken(ctx, claims)
	if err != nil {
		// Metrics: 刷新失败 - 生成失败
		if counter := a.options.GetCounter("auth_tokens_refreshed_total", "Total number of tokens refreshed"); counter != nil {
			counter.Add(ctx, 1, metrics.L("status", "error"), metrics.L("error_type", "generation_failed"))
		}
		return "", err
	}

	a.options.logger.Info("token refreshed", clog.String("user_id", claims.UserID))

	// Metrics: 刷新成功
	if counter := a.options.GetCounter("auth_tokens_refreshed_total", "Total number of tokens refreshed"); counter != nil {
		counter.Add(ctx, 1, metrics.L("status", "success"))
	}

	return newToken, nil
}

// sign 生成签名
func (a *jwtAuth) sign(message []byte) string {
	h := hmac.New(sha256.New, []byte(a.config.SecretKey))
	h.Write(message)
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
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
