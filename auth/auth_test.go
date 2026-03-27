package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		wantErr error
	}{
		{name: "nil config", cfg: nil, wantErr: ErrInvalidConfig},
		{name: "empty secret key", cfg: &Config{}, wantErr: ErrInvalidConfig},
		{name: "secret key too short", cfg: &Config{SecretKey: "short"}, wantErr: ErrInvalidConfig},
		{name: "invalid signing method", cfg: &Config{
			SecretKey:     "this-is-a-valid-secret-key-at-least-32-chars",
			SigningMethod: "RS256",
		}, wantErr: ErrInvalidConfig},
		{name: "invalid token lookup format", cfg: &Config{
			SecretKey:   "this-is-a-valid-secret-key-at-least-32-chars",
			TokenLookup: "Authorization",
		}, wantErr: ErrInvalidConfig},
		{name: "invalid token lookup source", cfg: &Config{
			SecretKey:   "this-is-a-valid-secret-key-at-least-32-chars",
			TokenLookup: "body:token",
		}, wantErr: ErrInvalidConfig},
		{name: "valid config", cfg: &Config{
			SecretKey: "this-is-a-valid-secret-key-at-least-32-chars",
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth, err := New(tt.cfg)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, auth)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, auth)
		})
	}
}

func TestAuthenticator_GenerateTokenPair(t *testing.T) {
	auth := createTestAuthenticator(t)
	ctx := context.Background()

	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "user-123"},
		Username:         "alice",
		Roles:            []string{"admin", "user"},
	}

	pair, err := auth.GenerateTokenPair(ctx, claims)
	require.NoError(t, err)
	require.NotNil(t, pair)
	assert.NotEmpty(t, pair.AccessToken)
	assert.NotEmpty(t, pair.RefreshToken)
	assert.NotEqual(t, pair.AccessToken, pair.RefreshToken)
	assert.Equal(t, "Bearer", pair.TokenType)
	assert.True(t, pair.AccessTokenExpiresAt.Before(pair.RefreshTokenExpiresAt))
}

func TestAuthenticator_GenerateTokenPair_NilClaims(t *testing.T) {
	auth := createTestAuthenticator(t)

	pair, err := auth.GenerateTokenPair(context.Background(), nil)
	assert.ErrorIs(t, err, ErrInvalidClaims)
	assert.Nil(t, pair)
}

func TestAuthenticator_GenerateTokenPair_NoMutation(t *testing.T) {
	auth := createTestAuthenticator(t)
	ctx := context.Background()

	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "user-123"},
		Username:         "alice",
		Roles:            []string{"admin"},
	}

	_, err := auth.GenerateTokenPair(ctx, claims)
	require.NoError(t, err)
	assert.Empty(t, claims.TokenType)
	assert.Nil(t, claims.ExpiresAt)
	assert.Nil(t, claims.IssuedAt)
}

func TestAuthenticator_ValidateAccessToken(t *testing.T) {
	auth := createTestAuthenticator(t)
	ctx := context.Background()

	pair := createTokenPair(t, auth, ctx)
	validatedClaims, err := auth.ValidateAccessToken(ctx, pair.AccessToken)
	require.NoError(t, err)
	assert.Equal(t, "user-123", validatedClaims.Subject)
	assert.Equal(t, "alice", validatedClaims.Username)
	assert.Equal(t, []string{"admin"}, validatedClaims.Roles)
	assert.Equal(t, TokenTypeAccess, validatedClaims.TokenType)
}

func TestAuthenticator_ValidateRefreshToken(t *testing.T) {
	auth := createTestAuthenticator(t)
	ctx := context.Background()

	pair := createTokenPair(t, auth, ctx)
	validatedClaims, err := auth.ValidateRefreshToken(ctx, pair.RefreshToken)
	require.NoError(t, err)
	assert.Equal(t, "user-123", validatedClaims.Subject)
	assert.Equal(t, "alice", validatedClaims.Username)
	assert.Equal(t, TokenTypeRefresh, validatedClaims.TokenType)
}

func TestAuthenticator_ValidateAccessToken_RejectRefreshToken(t *testing.T) {
	auth := createTestAuthenticator(t)
	ctx := context.Background()

	pair := createTokenPair(t, auth, ctx)
	claims, err := auth.ValidateAccessToken(ctx, pair.RefreshToken)
	assert.ErrorIs(t, err, ErrInvalidToken)
	assert.Nil(t, claims)
}

func TestAuthenticator_ValidateRefreshToken_RejectAccessToken(t *testing.T) {
	auth := createTestAuthenticator(t)
	ctx := context.Background()

	pair := createTokenPair(t, auth, ctx)
	claims, err := auth.ValidateRefreshToken(ctx, pair.AccessToken)
	assert.ErrorIs(t, err, ErrInvalidToken)
	assert.Nil(t, claims)
}

func TestAuthenticator_ValidateTypedToken_InvalidToken(t *testing.T) {
	auth := createTestAuthenticator(t)
	ctx := context.Background()

	tests := []struct {
		name    string
		token   string
		wantErr error
	}{
		{name: "empty token", token: "", wantErr: ErrInvalidToken},
		{name: "malformed token", token: "not-a-jwt", wantErr: ErrInvalidToken},
		{name: "invalid signature", token: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dummy", wantErr: ErrInvalidToken},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := auth.ValidateAccessToken(ctx, tt.token)
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestAuthenticator_ValidateAccessToken_InvalidSignature(t *testing.T) {
	authA, err := New(&Config{
		SecretKey: "this-is-a-valid-secret-key-at-least-32-chars",
	}, WithLogger(clog.Discard()), WithMeter(metrics.Discard()))
	require.NoError(t, err)
	authB, err := New(&Config{
		SecretKey: "another-valid-secret-key-at-least-32-chars",
	}, WithLogger(clog.Discard()), WithMeter(metrics.Discard()))
	require.NoError(t, err)

	pair, err := authB.GenerateTokenPair(context.Background(), &Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "user-123"},
	})
	require.NoError(t, err)

	_, err = authA.ValidateAccessToken(context.Background(), pair.AccessToken)
	assert.ErrorIs(t, err, ErrInvalidSignature)
}

func TestAuthenticator_ValidateAccessToken_ExpiredToken(t *testing.T) {
	auth := createTestAuthenticator(t)
	token := signTestClaims(t, auth.(*jwtAuth), &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-123",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
		},
		TokenType: TokenTypeAccess,
	})

	_, err := auth.ValidateAccessToken(context.Background(), token)
	assert.ErrorIs(t, err, ErrExpiredToken)
}

func TestAuthenticator_RefreshToken(t *testing.T) {
	auth := createTestAuthenticator(t)
	ctx := context.Background()

	oldPair := createTokenPair(t, auth, ctx)
	newPair, err := auth.RefreshToken(ctx, oldPair.RefreshToken)
	require.NoError(t, err)
	require.NotNil(t, newPair)
	assert.NotEmpty(t, newPair.AccessToken)
	assert.NotEmpty(t, newPair.RefreshToken)
	assert.NotEqual(t, oldPair.AccessToken, newPair.AccessToken)
	assert.NotEqual(t, oldPair.RefreshToken, newPair.RefreshToken)

	accessClaims, err := auth.ValidateAccessToken(ctx, newPair.AccessToken)
	require.NoError(t, err)
	assert.Equal(t, "user-123", accessClaims.Subject)
	assert.Equal(t, TokenTypeAccess, accessClaims.TokenType)

	refreshClaims, err := auth.ValidateRefreshToken(ctx, newPair.RefreshToken)
	require.NoError(t, err)
	assert.Equal(t, "user-123", refreshClaims.Subject)
	assert.Equal(t, TokenTypeRefresh, refreshClaims.TokenType)
}

func TestAuthenticator_RefreshToken_InvalidToken(t *testing.T) {
	auth := createTestAuthenticator(t)

	pair, err := auth.RefreshToken(context.Background(), "invalid-token")
	assert.ErrorIs(t, err, ErrInvalidToken)
	assert.Nil(t, pair)
}

func TestAuthenticator_RefreshToken_ExpiredRefreshToken(t *testing.T) {
	auth := createTestAuthenticator(t)
	token := signTestClaims(t, auth.(*jwtAuth), &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-123",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-30 * time.Minute)),
		},
		TokenType: TokenTypeRefresh,
	})

	pair, err := auth.RefreshToken(context.Background(), token)
	assert.ErrorIs(t, err, ErrExpiredToken)
	assert.Nil(t, pair)
}

func TestAuthenticator_RefreshToken_InvalidIssuerOrAudience(t *testing.T) {
	ctx := context.Background()
	authTokenIssuer, err := New(&Config{
		SecretKey:       "this-is-a-valid-secret-key-at-least-32-chars",
		Issuer:          "service-a",
		Audience:        []string{"frontend"},
		RefreshTokenTTL: 2 * time.Hour,
	}, WithLogger(clog.Discard()), WithMeter(metrics.Discard()))
	require.NoError(t, err)

	authRefreshWrong, err := New(&Config{
		SecretKey:       "this-is-a-valid-secret-key-at-least-32-chars",
		Issuer:          "service-b",
		Audience:        []string{"mobile"},
		RefreshTokenTTL: 2 * time.Hour,
	}, WithLogger(clog.Discard()), WithMeter(metrics.Discard()))
	require.NoError(t, err)

	pair, err := authTokenIssuer.GenerateTokenPair(ctx, &Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "user-123"},
	})
	require.NoError(t, err)

	newPair, err := authRefreshWrong.RefreshToken(ctx, pair.RefreshToken)
	assert.ErrorIs(t, err, ErrInvalidToken)
	assert.Nil(t, newPair)
}

func TestExtractToken_Header(t *testing.T) {
	auth := createTestAuthenticator(t).(*jwtAuth)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer test-token-123")

	token, err := auth.ExtractToken(req)
	require.NoError(t, err)
	assert.Equal(t, "test-token-123", token)
}

func TestExtractToken_HeaderCaseInsensitive(t *testing.T) {
	auth := createTestAuthenticator(t).(*jwtAuth)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "bearer test-token-123")

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
	req := httptest.NewRequest("GET", "/test?token=query-token", nil)
	req.Header.Set("Authorization", "Bearer header-token")
	req.AddCookie(&http.Cookie{Name: "jwt", Value: "cookie-token"})

	token, err := auth.ExtractToken(req)
	require.NoError(t, err)
	assert.Equal(t, "header-token", token)
}

func TestExtractToken_QueryFallback(t *testing.T) {
	auth := createTestAuthenticator(t).(*jwtAuth)
	req := httptest.NewRequest("GET", "/test?token=query-token", nil)
	req.AddCookie(&http.Cookie{Name: "jwt", Value: "cookie-token"})

	token, err := auth.ExtractToken(req)
	require.NoError(t, err)
	assert.Equal(t, "query-token", token)
}

func TestExtractToken_CookieFallback(t *testing.T) {
	auth := createTestAuthenticator(t).(*jwtAuth)
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
	pair := createTokenPair(t, auth, context.Background())

	router := gin.New()
	router.Use(auth.GinMiddleware())
	router.GET("/test", func(c *gin.Context) {
		claims, ok := GetClaims(c)
		if ok {
			c.JSON(200, gin.H{"user_id": claims.Subject})
			return
		}
		c.JSON(500, gin.H{"error": "no claims"})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "user-123")
}

func TestGinMiddleware_RejectRefreshToken(t *testing.T) {
	auth := createTestAuthenticator(t)
	pair := createTokenPair(t, auth, context.Background())

	router := gin.New()
	router.Use(auth.GinMiddleware())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+pair.RefreshToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 401, w.Code)
	assert.JSONEq(t, `{"error":"unauthorized"}`, w.Body.String())
}

func TestGinMiddleware_NoToken(t *testing.T) {
	auth := createTestAuthenticator(t)

	router := gin.New()
	router.Use(auth.GinMiddleware())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 401, w.Code)
	assert.JSONEq(t, `{"error":"unauthorized"}`, w.Body.String())
}

func TestGinMiddleware_InvalidToken(t *testing.T) {
	auth := createTestAuthenticator(t)

	router := gin.New()
	router.Use(auth.GinMiddleware())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 401, w.Code)
	assert.JSONEq(t, `{"error":"unauthorized"}`, w.Body.String())
}

func TestRequireRoles(t *testing.T) {
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(ClaimsKey, &Claims{
			RegisteredClaims: jwt.RegisteredClaims{Subject: "user-123"},
			Roles:            []string{"editor"},
		})
		c.Next()
	})
	router.GET("/allowed", RequireRoles("admin", "editor"), func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})
	router.GET("/denied", RequireRoles("admin"), func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	t.Run("allow any matching role", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/allowed", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, 200, w.Code)
	})

	t.Run("missing role returns forbidden", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/denied", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, 403, w.Code)
		assert.JSONEq(t, `{"error":"forbidden"}`, w.Body.String())
	})
}

func TestRequireRoles_NoClaims(t *testing.T) {
	router := gin.New()
	router.GET("/test", RequireRoles("admin"), func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 401, w.Code)
	assert.JSONEq(t, `{"error":"unauthorized"}`, w.Body.String())
}

func TestGetClaims_TypeMismatch(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(ClaimsKey, "not-claims")
	claims, ok := GetClaims(c)
	assert.False(t, ok)
	assert.Nil(t, claims)
}

func BenchmarkGenerateTokenPair(b *testing.B) {
	auth := createBenchmarkAuthenticator()
	ctx := context.Background()
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "user-123"},
		Username:         "alice",
		Roles:            []string{"admin", "user"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = auth.GenerateTokenPair(ctx, claims)
	}
}

func BenchmarkValidateAccessToken(b *testing.B) {
	auth := createBenchmarkAuthenticator()
	ctx := context.Background()
	pair, _ := auth.GenerateTokenPair(ctx, &Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "user-123"},
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = auth.ValidateAccessToken(ctx, pair.AccessToken)
	}
}

func BenchmarkRefreshToken(b *testing.B) {
	auth := createBenchmarkAuthenticator()
	ctx := context.Background()
	pair, _ := auth.GenerateTokenPair(ctx, &Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "user-123"},
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = auth.RefreshToken(ctx, pair.RefreshToken)
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

func createTokenPair(t *testing.T, auth Authenticator, ctx context.Context) *TokenPair {
	t.Helper()

	pair, err := auth.GenerateTokenPair(ctx, &Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "user-123"},
		Username:         "alice",
		Roles:            []string{"admin"},
	})
	require.NoError(t, err)
	return pair
}

func signTestClaims(t *testing.T, auth *jwtAuth, claims *Claims) string {
	t.Helper()

	if claims.Issuer == "" && auth.config.Issuer != "" {
		claims.Issuer = auth.config.Issuer
	}
	if len(claims.Audience) == 0 && len(auth.config.Audience) > 0 {
		claims.Audience = append(jwt.ClaimStrings(nil), auth.config.Audience...)
	}

	token, err := auth.signClaims(claims)
	require.NoError(t, err)
	return token
}
