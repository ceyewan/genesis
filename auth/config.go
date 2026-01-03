package auth

import "time"

// Config Auth 配置
type Config struct {
	// JWT 配置
	SecretKey     string   `mapstructure:"secret_key"`     // 签名密钥（至少 32 字符）
	SigningMethod string   `mapstructure:"signing_method"` // 签名方法: HS256（目前只支持）
	Issuer        string   `mapstructure:"issuer"`         // 签发者
	Audience      []string `mapstructure:"audience"`       // 接收者

	// Token 有效期
	AccessTokenTTL  time.Duration `mapstructure:"access_token_ttl"`  // Access Token TTL，默认 15m
	RefreshTokenTTL time.Duration `mapstructure:"refresh_token_ttl"` // Refresh Token TTL，默认 7d

	// Token 提取配置（可选，覆盖默认查找顺序）
	// 默认顺序: header:Authorization -> query:token -> cookie:jwt
	// 可指定单一来源如 "header:Authorization" 或 "query:token"
	TokenLookup   string `mapstructure:"token_lookup"`    // 提取方式，留空使用默认多源查找
	TokenHeadName string `mapstructure:"token_head_name"` // Header 前缀，默认 Bearer
}

// setDefaults 设置默认值
func (c *Config) setDefaults() {
	if c.SigningMethod == "" {
		c.SigningMethod = "HS256"
	}
	if c.AccessTokenTTL == 0 {
		c.AccessTokenTTL = 15 * time.Minute
	}
	if c.RefreshTokenTTL == 0 {
		c.RefreshTokenTTL = 7 * 24 * time.Hour
	}
	if c.TokenHeadName == "" {
		c.TokenHeadName = "Bearer"
	}
	// TokenLookup 留空时使用默认多源查找，不设置默认值
}
