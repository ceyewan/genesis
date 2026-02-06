package auth

import (
	"github.com/golang-jwt/jwt/v5"
)

// Claims 定义了 JWT 载荷结构。
//
// 它内嵌了 jwt.RegisteredClaims 以支持标准声明（如 exp, sub, iss 等），
// 同时扩展了 Genesis 框架常用的业务字段。
//
// 字段说明：
//   - Username: 用户名 (对应 uname)
//   - Roles: 角色列表 (对应 roles)
//   - Extra: 扩展字段 (对应 extra)
type Claims struct {
	// 标准声明 (包含 Subject, Issuer, ExpiresAt 等)
	jwt.RegisteredClaims

	// 业务扩展声明
	Username string         `json:"uname,omitempty"` // 用户名
	Roles    []string       `json:"roles,omitempty"` // 角色列表
	Extra    map[string]any `json:"extra,omitempty"` // 扩展信息
}
