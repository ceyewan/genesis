package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// GinMiddleware 返回 Gin 认证中间件
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
		c.Set("username", claims.Username)
		c.Next()
	}
}

// RequireRoles 要求特定角色的中间件
func RequireRoles(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, ok := GetClaims(c)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "unauthorized",
			})
			return
		}

		for _, required := range roles {
			found := false
			for _, role := range claims.Roles {
				if role == required {
					found = true
					break
				}
			}
			if !found {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
					"error": "forbidden: missing role " + required,
				})
				return
			}
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
	return claims.(*Claims), true
}
