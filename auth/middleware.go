package auth

import (
	"net/http"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
)

// GinMiddleware 返回 Gin 认证中间件，将验证请求中的 JWT Token
// 并将 Claims 存入 Context（ClaimsKey），可通过 GetClaims 获取
func (a *jwtAuth) GinMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := a.ExtractToken(c.Request)
		if err != nil {
			// Token 缺失不计入指标（用户未提供 token，不属于验证失败）
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": err.Error(),
			})
			return
		}

		claims, err := a.ValidateToken(c.Request.Context(), token)
		// 指标已在 ValidateToken 内部记录
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": err.Error(),
			})
			return
		}

		// 将 Claims 存入 Context
		c.Set(ClaimsKey, claims)
		c.Next()
	}
}

// RequireRoles 要求用户拥有其中一个角色的中间件，采用 OR 逻辑
// RequireRoles("admin", "editor") 表示用户必须拥有 admin 或 editor 角色之一
func RequireRoles(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, ok := GetClaims(c)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "unauthorized",
			})
			return
		}

		// 检查用户是否拥有任意一个所需角色（OR 逻辑）
		hasRequiredRole := false
		for _, required := range roles {
			if slices.Contains(claims.Roles, required) {
				hasRequiredRole = true
				break
			}
		}

		if !hasRequiredRole {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "forbidden: required one of roles: " + strings.Join(roles, ", "),
			})
			return
		}

		c.Next()
	}
}

// GetClaims 从 Gin Context 获取 Claims
func GetClaims(c *gin.Context) (*Claims, bool) {
	claims, exists := c.Get(ClaimsKey)
	if !exists {
		return nil, false
	}
	authClaims, ok := claims.(*Claims)
	if !ok {
		return nil, false
	}
	return authClaims, true
}
