package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"github.com/ceyewan/genesis/auth"
	"github.com/ceyewan/genesis/clog"
)

type loginRequest struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func main() {
	cfg := &auth.Config{
		SecretKey:       "your-secret-key-must-be-at-least-32-chars",
		SigningMethod:   "HS256",
		Issuer:          "my-app",
		Audience:        []string{"example-client"},
		AccessTokenTTL:  15 * time.Minute,
		RefreshTokenTTL: 7 * 24 * time.Hour,
		TokenLookup:     "header:Authorization",
		TokenHeadName:   "Bearer",
	}

	logger, err := clog.New(clog.NewDevDefaultConfig("example"))
	if err != nil {
		log.Fatalf("create logger: %v", err)
	}

	authenticator, err := auth.New(cfg, auth.WithLogger(logger))
	if err != nil {
		log.Fatalf("create authenticator: %v", err)
	}

	router := gin.New()
	router.Use(gin.Recovery())

	router.POST("/login", func(c *gin.Context) {
		var req loginRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "bad request"})
			return
		}

		claims := &auth.Claims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject: req.UserID,
			},
			Username: req.Username,
			Roles:    []string{"user"},
		}

		pair, err := authenticator.GenerateTokenPair(c.Request.Context(), claims)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}

		c.JSON(http.StatusOK, pair)
	})

	router.POST("/refresh", func(c *gin.Context) {
		var req refreshRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "bad request"})
			return
		}

		pair, err := authenticator.RefreshToken(c.Request.Context(), req.RefreshToken)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		c.JSON(http.StatusOK, pair)
	})

	protected := router.Group("/")
	protected.Use(authenticator.GinMiddleware())
	protected.GET("/profile", func(c *gin.Context) {
		claims, _ := auth.GetClaims(c)
		c.JSON(http.StatusOK, gin.H{
			"user_id":  claims.Subject,
			"username": claims.Username,
			"roles":    claims.Roles,
		})
	})

	go func() {
		logger.Info("auth example server starting", clog.String("addr", ":12345"))
		if err := router.Run(":12345"); err != nil {
			logger.Error("server error", clog.Error(err))
		}
	}()

	time.Sleep(time.Second)
	runBasicTests(logger)
}

func runBasicTests(logger clog.Logger) {
	client := &http.Client{Timeout: 10 * time.Second}
	baseURL := "http://localhost:12345"

	logger.Info("=== 开始基本功能测试 ===")

	logger.Info("测试1: 登录获取双令牌")
	loginBody, _ := json.Marshal(loginRequest{
		UserID:   "user123",
		Username: "Alice",
	})

	resp, err := client.Post(baseURL+"/login", "application/json", bytes.NewReader(loginBody))
	if err != nil {
		logger.Error("登录失败", clog.Error(err))
		return
	}
	defer resp.Body.Close()

	var pair auth.TokenPair
	if err := json.NewDecoder(resp.Body).Decode(&pair); err != nil {
		logger.Error("解析登录响应失败", clog.Error(err))
		return
	}
	if resp.StatusCode != http.StatusOK {
		logger.Error("登录失败", clog.Int("code", resp.StatusCode))
		return
	}

	logger.Info("登录成功",
		clog.String("access_token", pair.AccessToken[:20]+"..."),
		clog.String("refresh_token", pair.RefreshToken[:20]+"..."),
	)

	logger.Info("测试2: 使用 access token 访问受保护路由")
	req, _ := http.NewRequest(http.MethodGet, baseURL+"/profile", nil)
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)

	resp, err = client.Do(req)
	if err != nil {
		logger.Error("访问受保护路由失败", clog.Error(err))
		return
	}
	defer resp.Body.Close()

	var profile map[string]any
	data, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(data, &profile)
	if resp.StatusCode != http.StatusOK {
		logger.Error("访问受保护路由失败", clog.Int("code", resp.StatusCode))
		return
	}

	logger.Info("访问受保护路由成功",
		clog.String("user_id", fmt.Sprint(profile["user_id"])),
		clog.String("username", fmt.Sprint(profile["username"])),
	)

	logger.Info("测试3: 使用 refresh token 换发新双令牌")
	refreshBody, _ := json.Marshal(refreshRequest{RefreshToken: pair.RefreshToken})
	resp, err = client.Post(baseURL+"/refresh", "application/json", bytes.NewReader(refreshBody))
	if err != nil {
		logger.Error("刷新令牌失败", clog.Error(err))
		return
	}
	defer resp.Body.Close()

	var refreshed auth.TokenPair
	if err := json.NewDecoder(resp.Body).Decode(&refreshed); err != nil {
		logger.Error("解析刷新响应失败", clog.Error(err))
		return
	}
	if resp.StatusCode != http.StatusOK {
		logger.Error("刷新令牌失败", clog.Int("code", resp.StatusCode))
		return
	}
	if refreshed.AccessToken == pair.AccessToken || refreshed.RefreshToken == pair.RefreshToken {
		logger.Error("刷新后令牌未轮换")
		return
	}
	logger.Info("刷新令牌成功")

	logger.Info("测试4: 使用 refresh token 直接访问业务接口，应被拒绝")
	req, _ = http.NewRequest(http.MethodGet, baseURL+"/profile", nil)
	req.Header.Set("Authorization", "Bearer "+pair.RefreshToken)

	resp, err = client.Do(req)
	if err != nil {
		logger.Error("测试 refresh token 访问业务接口失败", clog.Error(err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		logger.Error("refresh token 未被正确拒绝", clog.Int("code", resp.StatusCode))
		return
	}
	logger.Info("refresh token 被正确拒绝")

	logger.Info("测试5: 使用无效 access token")
	req, _ = http.NewRequest(http.MethodGet, baseURL+"/profile", nil)
	req.Header.Set("Authorization", "Bearer invalid.token.here")

	resp, err = client.Do(req)
	if err != nil {
		logger.Error("测试无效 token 失败", clog.Error(err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		logger.Error("无效 token 未被正确拒绝", clog.Int("code", resp.StatusCode))
		return
	}
	logger.Info("无效 token 被正确拒绝")

	logger.Info("=== 基本功能测试完成 ===")
}
