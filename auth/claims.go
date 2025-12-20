package auth

import (
	"time"
)

// Claims JWT 声明
type Claims struct {
	// 标准声明
	UserID    string         `json:"sub"` // 用户ID
	Username  string         `json:"uname,omitempty"`
	Roles     []string       `json:"roles,omitempty"`
	Extra     map[string]any `json:"extra,omitempty"`
	Issuer    string         `json:"iss,omitempty"`
	Audience  []string       `json:"aud,omitempty"`
	ExpiresAt *time.Time     `json:"exp,omitempty"`
	IssuedAt  *time.Time     `json:"iat,omitempty"`
	NotBefore *time.Time     `json:"nbf,omitempty"`
}

// ClaimsOption Claims 配置选项
type ClaimsOption func(*Claims)

// NewClaims 创建 Claims
func NewClaims(userID string, opts ...ClaimsOption) *Claims {
	claims := &Claims{
		UserID: userID,
		Extra:  make(map[string]any),
	}
	for _, opt := range opts {
		opt(claims)
	}
	return claims
}

// WithUsername 设置用户名
func WithUsername(username string) ClaimsOption {
	return func(c *Claims) {
		c.Username = username
	}
}

// WithRoles 设置角色
func WithRoles(roles ...string) ClaimsOption {
	return func(c *Claims) {
		c.Roles = append(c.Roles, roles...)
	}
}

// WithExtra 设置额外字段
func WithExtra(key string, value any) ClaimsOption {
	return func(c *Claims) {
		c.Extra[key] = value
	}
}

// WithExpiration 设置过期时间
func WithExpiration(d time.Duration) ClaimsOption {
	return func(c *Claims) {
		expiresAt := time.Now().Add(d)
		c.ExpiresAt = &expiresAt
	}
}

// WithIssuer 设置签发者
func WithIssuer(issuer string) ClaimsOption {
	return func(c *Claims) {
		c.Issuer = issuer
	}
}

// WithAudience 设置接收者
func WithAudience(audience ...string) ClaimsOption {
	return func(c *Claims) {
		c.Audience = audience
	}
}
