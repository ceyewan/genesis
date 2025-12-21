package auth

import (
	"net/http"

	"github.com/ceyewan/genesis/metrics"
	"github.com/gin-gonic/gin"
)

// GinMiddleware 返回 Gin 认证中间件
func (a *jwtAuth) GinMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := a.ExtractToken(c.Request)
		if err != nil {
			// Metrics: 访问拒绝 - Token 缺失
			if counter := a.options.GetCounter("auth_access_denied_total", "Total number of access denied"); counter != nil {
				counter.Add(c.Request.Context(), 1, metrics.L("reason", "missing_token"))
			}
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": err.Error(),
			})
			return
		}

		claims, err := a.ValidateToken(c.Request.Context(), token)
		if err != nil {
			// Metrics: 访问拒绝 - Token 无效
			if counter := a.options.GetCounter("auth_access_denied_total", "Total number of access denied"); counter != nil {
				counter.Add(c.Request.Context(), 1, metrics.L("reason", "invalid_token"))
			}
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

// OptionalMiddleware 可选认证中间件，不强制要求 Token
func (a *jwtAuth) OptionalMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := a.ExtractToken(c.Request)
		if err != nil {
			// Token 不存在或格式错误，继续处理
			c.Next()
			return
		}

		claims, err := a.ValidateToken(c.Request.Context(), token)
		if err != nil {
			// Token 无效，继续处理（但不设置 Claims）
			c.Next()
			return
		}

		c.Set(ClaimsKey, claims)
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

