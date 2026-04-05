// Package auth 提供基于 JWT 的双令牌认证能力。
//
// auth 是 Genesis 的 L3 治理层组件，面向"应用自己签发并校验 JWT"的场景，
// 提供 access token / refresh token 的签发、校验与换发能力，以及 Gin 接入层。
//
// 组件边界：
//   - 提供双 JWT 令牌模型，不依赖外部存储。
//   - GinMiddleware 只接受 access token。
//   - RefreshToken 只接受 refresh token，并返回一对新的 token。
//   - 不提供 token 撤销、会话管理、黑名单、重放检测、OAuth2/OIDC 能力。
//
// 典型用法：
//
//	authenticator, _ := auth.New(&auth.Config{SecretKey: "..."})
//	pair, _ := authenticator.GenerateTokenPair(ctx, &auth.Claims{
//	    RegisteredClaims: jwt.RegisteredClaims{Subject: "user-123"},
//	})
//	claims, _ := authenticator.ValidateAccessToken(ctx, pair.AccessToken)
package auth

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
	"github.com/ceyewan/genesis/xerrors"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// TokenPair 表示一对 access / refresh 令牌。
type TokenPair struct {
	AccessToken           string
	RefreshToken          string
	AccessTokenExpiresAt  time.Time
	RefreshTokenExpiresAt time.Time
	AuthorizationScheme   string
}

// Authenticator 认证器接口。
type Authenticator interface {
	// GenerateTokenPair 生成 access / refresh 双令牌。
	GenerateTokenPair(ctx context.Context, claims *Claims) (*TokenPair, error)

	// ValidateAccessToken 验证 access token，返回 Claims。
	ValidateAccessToken(ctx context.Context, token string) (*Claims, error)

	// ValidateRefreshToken 验证 refresh token，返回 Claims。
	ValidateRefreshToken(ctx context.Context, token string) (*Claims, error)

	// RefreshToken 使用 refresh token 换发新的 access / refresh 双令牌。
	RefreshToken(ctx context.Context, refreshToken string) (*TokenPair, error)

	// GinMiddleware 返回 Gin 认证中间件。
	GinMiddleware() gin.HandlerFunc
}

// jwtAuth JWT 认证实现。
type jwtAuth struct {
	config         *Config
	options        *options
	validatedCount metrics.Counter
	refreshedCount metrics.Counter
}

// New 创建 Authenticator。
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

	if err := auth.config.validate(); err != nil {
		return nil, err
	}

	auth.validatedCount = auth.initCounter(
		MetricTokensValidated,
		"Total number of tokens validated",
	)
	auth.refreshedCount = auth.initCounter(
		MetricTokensRefreshed,
		"Total number of tokens refreshed",
	)

	return auth, nil
}

func (a *jwtAuth) initCounter(name, desc string) metrics.Counter {
	counter, err := a.options.meter.Counter(name, desc)
	if err == nil {
		return counter
	}

	a.options.logger.Warn("init auth counter failed, falling back to discard meter",
		clog.String("metric", name),
		clog.Error(err),
	)

	counter, _ = metrics.Discard().Counter(name, desc)
	return counter
}

// GenerateTokenPair 生成双令牌。
func (a *jwtAuth) GenerateTokenPair(ctx context.Context, claims *Claims) (*TokenPair, error) {
	if claims == nil {
		return nil, ErrInvalidClaims
	}

	now := time.Now()
	accessClaims := cloneClaims(claims)
	accessClaims.TokenType = TokenTypeAccess
	accessExpiresAt := now.Add(a.config.AccessTokenTTL)
	accessClaims.ExpiresAt = jwt.NewNumericDate(accessExpiresAt)
	accessClaims.IssuedAt = jwt.NewNumericDate(now)
	if accessClaims.Issuer == "" && a.config.Issuer != "" {
		accessClaims.Issuer = a.config.Issuer
	}
	if len(accessClaims.Audience) == 0 && len(a.config.Audience) > 0 {
		accessClaims.Audience = append(jwt.ClaimStrings(nil), a.config.Audience...)
	}
	if accessClaims.ID == "" {
		accessClaims.ID = newTokenID(TokenTypeAccess)
	}

	accessToken, err := a.signClaims(accessClaims)
	if err != nil {
		return nil, err
	}

	refreshClaims := cloneClaims(claims)
	refreshClaims.TokenType = TokenTypeRefresh
	refreshExpiresAt := now.Add(a.config.RefreshTokenTTL)
	refreshClaims.ExpiresAt = jwt.NewNumericDate(refreshExpiresAt)
	refreshClaims.IssuedAt = jwt.NewNumericDate(now)
	if refreshClaims.Issuer == "" && a.config.Issuer != "" {
		refreshClaims.Issuer = a.config.Issuer
	}
	if len(refreshClaims.Audience) == 0 && len(a.config.Audience) > 0 {
		refreshClaims.Audience = append(jwt.ClaimStrings(nil), a.config.Audience...)
	}
	if refreshClaims.ID == "" {
		refreshClaims.ID = newTokenID(TokenTypeRefresh)
	}

	refreshToken, err := a.signClaims(refreshClaims)
	if err != nil {
		return nil, err
	}

	a.options.logger.Info("token pair generated", clog.String("user_id", claims.Subject))

	return &TokenPair{
		AccessToken:           accessToken,
		RefreshToken:          refreshToken,
		AccessTokenExpiresAt:  accessExpiresAt,
		RefreshTokenExpiresAt: refreshExpiresAt,
		AuthorizationScheme:   a.config.TokenHeadName,
	}, nil
}

func (a *jwtAuth) signClaims(claims *Claims) (string, error) {
	method := jwt.GetSigningMethod(a.config.SigningMethod)
	if method == nil || method.Alg() != jwt.SigningMethodHS256.Alg() {
		return "", ErrInvalidConfig
	}

	token := jwt.NewWithClaims(method, claims)
	tokenString, err := token.SignedString([]byte(a.config.SecretKey))
	if err != nil {
		return "", xerrors.Wrap(err, "failed to sign token")
	}
	return tokenString, nil
}

// ValidateAccessToken 验证 access token。
func (a *jwtAuth) ValidateAccessToken(ctx context.Context, tokenString string) (*Claims, error) {
	return a.validateTypedToken(ctx, tokenString, TokenTypeAccess)
}

// ValidateRefreshToken 验证 refresh token。
func (a *jwtAuth) ValidateRefreshToken(ctx context.Context, tokenString string) (*Claims, error) {
	return a.validateTypedToken(ctx, tokenString, TokenTypeRefresh)
}

func (a *jwtAuth) validateTypedToken(ctx context.Context, tokenString string, expected TokenType) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, a.keyFunc(), a.validationParserOptions()...)
	if err != nil {
		var errType string
		if xerrors.Is(err, jwt.ErrTokenExpired) {
			errType = "expired"
			err = ErrExpiredToken
		} else if xerrors.Is(err, jwt.ErrTokenSignatureInvalid) {
			errType = "invalid_signature"
			err = ErrInvalidSignature
		} else {
			errType = "invalid_token"
			err = ErrInvalidToken
		}

		a.validatedCount.Add(ctx, 1, metrics.L("status", "error"), metrics.L("error_type", errType))
		return nil, err
	}

	if !token.Valid || claims.TokenType != expected {
		a.validatedCount.Add(ctx, 1, metrics.L("status", "error"), metrics.L("error_type", "invalid_token"))
		return nil, ErrInvalidToken
	}

	a.options.logger.Info("token validated",
		clog.String("user_id", claims.Subject),
		clog.String("token_type", string(claims.TokenType)),
	)

	a.validatedCount.Add(ctx, 1, metrics.L("status", "success"))
	return claims, nil
}

// RefreshToken 使用 refresh token 换发新双令牌。
func (a *jwtAuth) RefreshToken(ctx context.Context, refreshToken string) (*TokenPair, error) {
	claims, err := a.ValidateRefreshToken(ctx, refreshToken)
	if err != nil {
		a.refreshedCount.Add(ctx, 1, metrics.L("status", "error"))
		return nil, err
	}

	nextClaims := cloneClaims(claims)
	nextClaims.TokenType = ""
	nextClaims.ExpiresAt = nil
	nextClaims.IssuedAt = nil
	nextClaims.ID = ""

	pair, err := a.GenerateTokenPair(ctx, nextClaims)
	if err != nil {
		a.refreshedCount.Add(ctx, 1, metrics.L("status", "error"))
		return nil, err
	}

	a.options.logger.Info("token pair refreshed", clog.String("user_id", claims.Subject))
	a.refreshedCount.Add(ctx, 1, metrics.L("status", "success"))

	return pair, nil
}

// ExtractToken 从请求中提取 access token（导出用于中间件）。
//
// 查找顺序（如果 TokenLookup 未配置）:
// 1. header:Authorization (Bearer token)
// 2. query:token
// 3. cookie:jwt
//
// 如果配置了 TokenLookup，则只按指定方式提取。
func (a *jwtAuth) ExtractToken(r *http.Request) (string, error) {
	if a.config.TokenLookup != "" {
		parts := strings.Split(a.config.TokenLookup, ":")
		source, key := parts[0], parts[1]
		token, ok := a.extractFromSource(r, source, key)
		if !ok {
			return "", ErrMissingToken
		}
		return token, nil
	}

	if token, ok := a.extractFromSource(r, "header", "Authorization"); ok {
		return token, nil
	}
	if token, ok := a.extractFromSource(r, "query", "token"); ok {
		return token, nil
	}
	if token, ok := a.extractFromSource(r, "cookie", "jwt"); ok {
		return token, nil
	}

	return "", ErrMissingToken
}

// extractFromSource 从指定来源提取 token。
func (a *jwtAuth) extractFromSource(r *http.Request, source, key string) (string, bool) {
	switch source {
	case "header":
		authHeader := r.Header.Get(key)
		if authHeader == "" {
			return "", false
		}
		tokenParts := strings.Fields(authHeader)
		if len(tokenParts) != 2 || !strings.EqualFold(tokenParts[0], a.config.TokenHeadName) {
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

func (a *jwtAuth) validationParserOptions() []jwt.ParserOption {
	opts := []jwt.ParserOption{
		jwt.WithValidMethods([]string{a.config.SigningMethod}),
	}
	if a.config.Issuer != "" {
		opts = append(opts, jwt.WithIssuer(a.config.Issuer))
	}
	if len(a.config.Audience) > 0 {
		opts = append(opts, jwt.WithAudience(a.config.Audience...))
	}
	return opts
}

func (a *jwtAuth) keyFunc() jwt.Keyfunc {
	return func(token *jwt.Token) (any, error) {
		return []byte(a.config.SecretKey), nil
	}
}

func (a *jwtAuth) parseClaimsWithoutTimeValidation(tokenString string) (*Claims, error) {
	claims := &Claims{}
	opts := append(a.validationParserOptions(), jwt.WithoutClaimsValidation())
	token, err := jwt.ParseWithClaims(tokenString, claims, a.keyFunc(), opts...)
	if err != nil {
		if errors.Is(err, jwt.ErrTokenSignatureInvalid) {
			return nil, ErrInvalidSignature
		}
		return nil, ErrInvalidToken
	}
	if !token.Valid {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

func cloneClaims(claims *Claims) *Claims {
	copied := *claims
	if claims.Roles != nil {
		copied.Roles = append([]string(nil), claims.Roles...)
	}
	if claims.Extra != nil {
		copied.Extra = make(map[string]any, len(claims.Extra))
		maps.Copy(copied.Extra, claims.Extra)
	}
	if claims.Audience != nil {
		copied.Audience = append(jwt.ClaimStrings(nil), claims.Audience...)
	}
	return &copied
}

func hasAnyAudience(tokenAud jwt.ClaimStrings, expected []string) bool {
	for _, ta := range tokenAud {
		if slices.Contains(expected, ta) {
			return true
		}
	}
	return false
}

func newTokenID(tokenType TokenType) string {
	return fmt.Sprintf("%s-%d", tokenType, time.Now().UnixNano())
}
