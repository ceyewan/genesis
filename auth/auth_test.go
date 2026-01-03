package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/golang-jwt/jwt/v5"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		wantErr error
	}{
		{
			name:    "nil config",
			cfg:     nil,
			wantErr: ErrInvalidConfig,
		},
		{
			name:    "empty secret key",
			cfg:     &Config{},
			wantErr: ErrInvalidConfig,
		},
		{
			name: "secret key too short",
			cfg: &Config{
				SecretKey: "short",
			},
			wantErr: ErrInvalidConfig,
		},
		{
			name: "valid config",
			cfg: &Config{
				SecretKey: "this-is-a-valid-secret-key-at-least-32-chars",
			},
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth, err := New(tt.cfg)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, auth)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, auth)
			}
		})
	}
}

func TestAuthenticator_GenerateToken(t *testing.T) {
	auth := createTestAuthenticator(t)
	ctx := context.Background()

	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-123",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		Username: "alice",
		Roles:    []string{"admin", "user"},
	}

	token, err := auth.GenerateToken(ctx, claims)
	require.NoError(t, err)
	assert.NotEmpty(t, token)
}

func TestAuthenticator_GenerateToken_NilClaims(t *testing.T) {
	auth := createTestAuthenticator(t)
	ctx := context.Background()

	token, err := auth.GenerateToken(ctx, nil)
	assert.ErrorIs(t, err, ErrInvalidClaims)
	assert.Empty(t, token)
}

func TestAuthenticator_ValidateToken(t *testing.T) {
	auth := createTestAuthenticator(t)
	ctx := context.Background()

	// 生成 token
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: "user-123",
		},
		Username: "alice",
		Roles:    []string{"admin"},
	}
	token, err := auth.GenerateToken(ctx, claims)
	require.NoError(t, err)

	// 验证 token
	validatedClaims, err := auth.ValidateToken(ctx, token)
	require.NoError(t, err)
	assert.Equal(t, "user-123", validatedClaims.Subject)
	assert.Equal(t, "alice", validatedClaims.Username)
	assert.Equal(t, []string{"admin"}, validatedClaims.Roles)
}

func TestAuthenticator_ValidateToken_InvalidToken(t *testing.T) {
	auth := createTestAuthenticator(t)
	ctx := context.Background()

	tests := []struct {
		name    string
		token   string
		wantErr error
	}{
		{
			name:    "empty token",
			token:   "",
			wantErr: ErrInvalidToken,
		},
		{
			name:    "malformed token",
			token:   "not-a-jwt",
			wantErr: ErrInvalidToken,
		},
		{
			name:    "invalid signature",
			token:   "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dummy",
			wantErr: ErrInvalidToken, // JWT 库对签名错误返回 ErrTokenInvalid
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := auth.ValidateToken(ctx, tt.token)
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestAuthenticator_ValidateToken_ExpiredToken(t *testing.T) {
	auth := createTestAuthenticator(t)
	ctx := context.Background()

	// 生成已过期的 token
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-123",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
		},
	}
	token, err := auth.GenerateToken(ctx, claims)
	require.NoError(t, err)

	// 验证过期 token
	_, err = auth.ValidateToken(ctx, token)
	assert.ErrorIs(t, err, ErrExpiredToken)
}

func TestAuthenticator_RefreshToken(t *testing.T) {
	auth := createTestAuthenticator(t)
	ctx := context.Background()

	// 生成即将过期的 token
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-123",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Minute)),
		},
		Username: "alice",
	}
	oldToken, err := auth.GenerateToken(ctx, claims)
	require.NoError(t, err)

	// 刷新 token
	newToken, err := auth.RefreshToken(ctx, oldToken)
	require.NoError(t, err)
	assert.NotEmpty(t, newToken)
	assert.NotEqual(t, oldToken, newToken)

	// 验证新 token
	validatedClaims, err := auth.ValidateToken(ctx, newToken)
	require.NoError(t, err)
	assert.Equal(t, "user-123", validatedClaims.Subject)
	assert.Equal(t, "alice", validatedClaims.Username)
}

func TestAuthenticator_RefreshToken_InvalidToken(t *testing.T) {
	auth := createTestAuthenticator(t)
	ctx := context.Background()

	newToken, err := auth.RefreshToken(ctx, "invalid-token")
	assert.ErrorIs(t, err, ErrInvalidToken)
	assert.Empty(t, newToken)
}

func TestExtractToken_Header(t *testing.T) {
	auth := createTestAuthenticator(t).(*jwtAuth)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer test-token-123")

	token, err := auth.ExtractToken(req)
	require.NoError(t, err)
	assert.Equal(t, "test-token-123", token)
}

func TestExtractToken_Query(t *testing.T) {
	auth := createTestAuthenticator(t).(*jwtAuth)

	req := httptest.NewRequest("GET", "/test?token=test-token-456", nil)

	token, err := auth.ExtractToken(req)
	require.NoError(t, err)
	assert.Equal(t, "test-token-456", token)
}

func TestExtractToken_Cookie(t *testing.T) {
	auth := createTestAuthenticator(t).(*jwtAuth)

	req := httptest.NewRequest("GET", "/test", nil)
	req.AddCookie(&http.Cookie{Name: "jwt", Value: "test-token-789"})

	token, err := auth.ExtractToken(req)
	require.NoError(t, err)
	assert.Equal(t, "test-token-789", token)
}

func TestExtractToken_HeaderPriority(t *testing.T) {
	auth := createTestAuthenticator(t).(*jwtAuth)

	// Header 优先级最高
	req := httptest.NewRequest("GET", "/test?token=query-token", nil)
	req.Header.Set("Authorization", "Bearer header-token")
	req.AddCookie(&http.Cookie{Name: "jwt", Value: "cookie-token"})

	token, err := auth.ExtractToken(req)
	require.NoError(t, err)
	assert.Equal(t, "header-token", token)
}

func TestExtractToken_QueryFallback(t *testing.T) {
	auth := createTestAuthenticator(t).(*jwtAuth)

	// 没有 header 时使用 query
	req := httptest.NewRequest("GET", "/test?token=query-token", nil)
	req.AddCookie(&http.Cookie{Name: "jwt", Value: "cookie-token"})

	token, err := auth.ExtractToken(req)
	require.NoError(t, err)
	assert.Equal(t, "query-token", token)
}

func TestExtractToken_CookieFallback(t *testing.T) {
	auth := createTestAuthenticator(t).(*jwtAuth)

	// 没有 header 和 query 时使用 cookie
	req := httptest.NewRequest("GET", "/test", nil)
	req.AddCookie(&http.Cookie{Name: "jwt", Value: "cookie-token"})

	token, err := auth.ExtractToken(req)
	require.NoError(t, err)
	assert.Equal(t, "cookie-token", token)
}

func TestExtractToken_NoToken(t *testing.T) {
	auth := createTestAuthenticator(t).(*jwtAuth)

	req := httptest.NewRequest("GET", "/test", nil)

	token, err := auth.ExtractToken(req)
	assert.ErrorIs(t, err, ErrMissingToken)
	assert.Empty(t, token)
}

func TestExtractToken_InvalidHeaderFormat(t *testing.T) {
	auth := createTestAuthenticator(t).(*jwtAuth)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "InvalidFormat test-token")

	token, err := auth.ExtractToken(req)
	assert.ErrorIs(t, err, ErrMissingToken)
	assert.Empty(t, token)
}

func TestExtractToken_SingleSource_Header(t *testing.T) {
	cfg := &Config{
		SecretKey:   "this-is-a-valid-secret-key-at-least-32-chars",
		TokenLookup: "header:Authorization",
	}
	auth, err := New(cfg)
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/test?token=query-token", nil)
	req.Header.Set("Authorization", "Bearer header-token")

	token, err := auth.(*jwtAuth).ExtractToken(req)
	require.NoError(t, err)
	assert.Equal(t, "header-token", token)
}

func TestExtractToken_SingleSource_Query(t *testing.T) {
	cfg := &Config{
		SecretKey:   "this-is-a-valid-secret-key-at-least-32-chars",
		TokenLookup: "query:token",
	}
	auth, err := New(cfg)
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/test?token=query-token", nil)
	req.Header.Set("Authorization", "Bearer header-token")

	token, err := auth.(*jwtAuth).ExtractToken(req)
	require.NoError(t, err)
	assert.Equal(t, "query-token", token)
}

func TestGinMiddleware(t *testing.T) {
	auth := createTestAuthenticator(t)
	ctx := context.Background()

	// 生成有效 token
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: "user-123",
		},
		Username: "alice",
	}
	token, err := auth.GenerateToken(ctx, claims)
	require.NoError(t, err)

	// 设置测试路由
	middleware := auth.GinMiddleware()
	router := gin.New()
	router.Use(middleware)
	router.GET("/test", func(c *gin.Context) {
		claims, ok := GetClaims(c)
		if ok {
			c.JSON(200, gin.H{"user_id": claims.Subject})
		} else {
			c.JSON(500, gin.H{"error": "no claims"})
		}
	})

	// 测试有效 token
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "user-123")
}

func TestGinMiddleware_NoToken(t *testing.T) {
	auth := createTestAuthenticator(t)

	middleware := auth.GinMiddleware()
	router := gin.New()
	router.Use(middleware)
	router.GET("/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 401, w.Code)
}

func TestGinMiddleware_InvalidToken(t *testing.T) {
	auth := createTestAuthenticator(t)

	middleware := auth.GinMiddleware()
	router := gin.New()
	router.Use(middleware)
	router.GET("/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 401, w.Code)
}

// Benchmark functions

func BenchmarkGenerateToken(b *testing.B) {
	auth := createBenchmarkAuthenticator()
	ctx := context.Background()
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: "user-123",
		},
		Username: "alice",
		Roles:    []string{"admin", "user"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = auth.GenerateToken(ctx, claims)
	}
}

func BenchmarkValidateToken(b *testing.B) {
	auth := createBenchmarkAuthenticator()
	ctx := context.Background()
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: "user-123",
		},
	}
	token, _ := auth.GenerateToken(ctx, claims)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = auth.ValidateToken(ctx, token)
	}
}

func BenchmarkRefreshToken(b *testing.B) {
	auth := createBenchmarkAuthenticator()
	ctx := context.Background()
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: "user-123",
		},
	}
	token, _ := auth.GenerateToken(ctx, claims)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = auth.RefreshToken(ctx, token)
	}
}

func BenchmarkExtractToken_Header(b *testing.B) {
	auth := createBenchmarkAuthenticator().(*jwtAuth)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer test-token-123")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = auth.ExtractToken(req)
	}
}

func BenchmarkExtractToken_Query(b *testing.B) {
	auth := createBenchmarkAuthenticator().(*jwtAuth)
	req := httptest.NewRequest("GET", "/test?token=test-token-123", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = auth.ExtractToken(req)
	}
}

// Helper functions

func createTestAuthenticator(t *testing.T) Authenticator {
	auth, err := New(&Config{
		SecretKey: "this-is-a-valid-secret-key-at-least-32-chars",
	}, WithLogger(clog.Discard()), WithMeter(metrics.Discard()))
	require.NoError(t, err)
	return auth
}

func createBenchmarkAuthenticator() Authenticator {
	auth, _ := New(&Config{
		SecretKey: "this-is-a-valid-secret-key-at-least-32-chars",
	}, WithLogger(clog.Discard()), WithMeter(metrics.Discard()))
	return auth
}
