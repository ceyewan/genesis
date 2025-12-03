package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/ceyewan/genesis/pkg/auth"
	"github.com/ceyewan/genesis/pkg/clog"
	"github.com/ceyewan/genesis/pkg/metrics"
	"github.com/gin-gonic/gin"
)

func main() {
	// 1. 初始化配置
	cfg := &auth.Config{
		SecretKey:      "your-secret-key-must-be-at-least-32-characters-long-here",
		SigningMethod:  "HS256",
		Issuer:         "my-app",
		AccessTokenTTL: 15 * time.Minute,
		TokenLookup:    "header:Authorization",
		TokenHeadName:  "Bearer",
	}

	// 2. 初始化日志
	logger, err := clog.New(&clog.Config{
		Level:       "info",
		Format:      "console",
		Output:      "stdout",
		AddSource:   true,
		EnableColor: true,
		SourceRoot:  "genesis",
	}, &clog.Option{})
	if err != nil {
		log.Fatalf("create logger: %v", err)
	}

	// 3. 初始化 Metrics
	meter, err := metrics.New(&metrics.Config{
		Enabled:     true,
		ServiceName: "auth-example",
		Version:     "1.0.0",
		Port:        9091, // 使用 9091 端口，避免与 Prometheus 容器的 9090 端口冲突
		Path:        "/metrics",
	})
	if err != nil {
		log.Fatalf("create metrics: %v", err)
	}
	defer meter.Shutdown(context.Background())

	// 4. 创建认证器，注入日志和 metrics
	authenticator, err := auth.New(cfg, auth.WithLogger(logger), auth.WithMeter(meter))
	if err != nil {
		log.Fatalf("create authenticator: %v", err)
	}

	// 5. 创建 Gin 路由
	r := gin.Default()

	// 登录接口 - 生成 Token
	r.POST("/login", func(c *gin.Context) {
		var req struct {
			UserID   string `json:"user_id" binding:"required"`
			Username string `json:"username"`
		}

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// 创建 Claims
		claims := auth.NewClaims(req.UserID,
			auth.WithUsername(req.Username),
			auth.WithRoles("user"),
		)

		// 生成 Token
		token, err := authenticator.GenerateToken(c.Request.Context(), claims)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"token":      token,
			"expires_in": int(cfg.AccessTokenTTL.Seconds()),
		})
	})

	// 刷新 Token 接口
	r.POST("/refresh", func(c *gin.Context) {
		var req struct {
			Token string `json:"token" binding:"required"`
		}

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		newToken, err := authenticator.RefreshToken(c.Request.Context(), req.Token)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"token":      newToken,
			"expires_in": int(cfg.AccessTokenTTL.Seconds()),
		})
	})

	// 受保护的路由 - 需要有效的 Token
	protected := r.Group("/api")
	protected.Use(authenticator.GinMiddleware())
	{
		// 获取个人资料
		protected.GET("/profile", func(c *gin.Context) {
			claims := auth.MustGetClaims(c)
			c.JSON(http.StatusOK, gin.H{
				"user_id":  claims.UserID,
				"username": claims.Username,
				"roles":    claims.Roles,
			})
		})

		// 管理员接口
		protected.GET("/admin", func(c *gin.Context) {
			claims := auth.MustGetClaims(c)

			// 检查是否为管理员
			isAdmin := false
			for _, role := range claims.Roles {
				if role == "admin" {
					isAdmin = true
					break
				}
			}

			if !isAdmin {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
					"error": "admin role required",
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"message": "welcome admin",
				"user_id": claims.UserID,
			})
		})
	}

	// 启动服务器
	go func() {
		logger.Info("auth example server starting", clog.String("addr", ":8080"))
		if err := r.Run(":8080"); err != nil {
			logger.Error("server error", clog.Error(err))
		}
	}()

	// 等待服务器启动
	time.Sleep(1 * time.Second)

	// 启动客户端协程，自动测试 API
	go runClient(logger)

	// 保持主程序运行
	select {}
}

// runClient 客户端协程，模拟用户请求
func runClient(logger clog.Logger) {
	client := &http.Client{Timeout: 10 * time.Second}
	baseURL := "http://localhost:8080"

	logger.Info("client started", clog.String("status", "testing APIs"))

	// 1. 测试登录
	logger.Info("=== Test 1: Login ===")
	loginResp := testLogin(client, baseURL, logger)
	if loginResp == nil {
		return
	}
	token := loginResp["token"].(string)
	logger.Info("login success", clog.String("token", token[:20]+"..."))

	// 2. 测试获取个人资料
	logger.Info("=== Test 2: Get Profile ===")
	testGetProfile(client, baseURL, token, logger)

	// 3. 测试刷新 Token
	logger.Info("=== Test 3: Refresh Token ===")
	testRefreshToken(client, baseURL, token, logger)

	// 4. 测试管理员接口（应该失败，因为没有 admin 角色）
	logger.Info("=== Test 4: Admin API (should fail) ===")
	testAdminAPI(client, baseURL, token, logger)

	// 5. 测试无效 Token
	logger.Info("=== Test 5: Invalid Token ===")
	testInvalidToken(client, baseURL, logger)

	// 6. 批量测试 - 生成多个 Token
	logger.Info("=== Test 6: Batch Token Generation ===")
	for i := 0; i < 5; i++ {
		testBatchLogin(client, baseURL, logger, i)
	}

	// 7. 批量测试 - 验证不同类型的错误 Token
	logger.Info("=== Test 7: Various Invalid Tokens ===")
	testVariousInvalidTokens(client, baseURL, logger)

	// 8. 批量测试 - 多次刷新 Token
	logger.Info("=== Test 8: Multiple Refresh ===")
	testMultipleRefresh(client, baseURL, token, logger)

	logger.Info("initial client tests completed", clog.String("status", "all tests done"))

	// 9. 启动持续负载测试
	logger.Info("=== Starting Continuous Load Test ===")
	go runContinuousLoadTest(client, baseURL, logger)
}

// runContinuousLoadTest 持续负载测试，模拟客户端不停创建、验证 token
func runContinuousLoadTest(client *http.Client, baseURL string, logger clog.Logger) {
	ticker := time.NewTicker(100 * time.Millisecond) // 每100ms执行一次操作
	defer ticker.Stop()

	var tokens []string
	userCounter := 0
	operationCounter := 0

	for {
		select {
		case <-ticker.C:
			operationCounter++

			// 随机选择操作类型
			rand.Seed(time.Now().UnixNano())
			operation := rand.Intn(100)

			switch {
			case operation < 40: // 40% 概率生成新 token
				userID := fmt.Sprintf("load_user_%d", userCounter)
				username := fmt.Sprintf("LoadUser%d", userCounter)

				loginData := map[string]string{
					"user_id":  userID,
					"username": username,
				}
				body, _ := json.Marshal(loginData)

				resp, err := client.Post(baseURL+"/login", "application/json", bytes.NewReader(body))
				if err != nil {
					logger.Error("load test login failed", clog.Error(err), clog.Int("operation", operationCounter))
					continue
				}

				if resp.StatusCode == http.StatusOK {
					data, _ := io.ReadAll(resp.Body)
					result := make(map[string]interface{})
					json.Unmarshal(data, &result)

					if token, ok := result["token"].(string); ok {
						tokens = append(tokens, token)
						// 限制 tokens 数组大小，保留最新的50个
						if len(tokens) > 50 {
							tokens = tokens[1:]
						}
						userCounter++

						if operationCounter%100 == 0 {
							logger.Info("load test progress",
								clog.Int("operations", operationCounter),
								clog.Int("users_created", userCounter),
								clog.Int("active_tokens", len(tokens)))
						}
					}
				}
				resp.Body.Close()

			case operation < 80: // 40% 概率验证现有 token
				if len(tokens) > 0 {
					// 随机选择一个 token
					tokenIndex := rand.Intn(len(tokens))
					token := tokens[tokenIndex]

					req, _ := http.NewRequest("GET", baseURL+"/api/profile", nil)
					req.Header.Add("Authorization", "Bearer "+token)

					resp, err := client.Do(req)
					if err != nil {
						logger.Error("load test validation failed", clog.Error(err), clog.Int("operation", operationCounter))
						continue
					}

					// 如果 token 失效，从列表中移除
					if resp.StatusCode == http.StatusUnauthorized {
						tokens = append(tokens[:tokenIndex], tokens[tokenIndex+1:]...)
					}
					resp.Body.Close()
				}

			case operation < 95: // 15% 概率刷新 token
				if len(tokens) > 0 {
					tokenIndex := rand.Intn(len(tokens))
					token := tokens[tokenIndex]

					refreshData := map[string]string{"token": token}
					body, _ := json.Marshal(refreshData)

					resp, err := client.Post(baseURL+"/refresh", "application/json", bytes.NewReader(body))
					if err != nil {
						logger.Error("load test refresh failed", clog.Error(err), clog.Int("operation", operationCounter))
						continue
					}

					if resp.StatusCode == http.StatusOK {
						data, _ := io.ReadAll(resp.Body)
						result := make(map[string]interface{})
						json.Unmarshal(data, &result)

						if newToken, ok := result["token"].(string); ok {
							tokens[tokenIndex] = newToken // 替换为新的 token
						}
					} else {
						// 刷新失败，移除这个 token
						tokens = append(tokens[:tokenIndex], tokens[tokenIndex+1:]...)
					}
					resp.Body.Close()
				}

			default: // 5% 概率测试无效请求
				testInvalidToken(client, baseURL, logger)
			}

			// 每1000次操作输出一次统计信息
			if operationCounter%1000 == 0 {
				logger.Info("load test statistics",
					clog.Int("total_operations", operationCounter),
					clog.Int("users_created", userCounter),
					clog.Int("active_tokens", len(tokens)),
					clog.Float64("success_rate", float64(len(tokens))/float64(userCounter+1)))
			}
		}
	}
}

// testLogin 测试登录接口
func testLogin(client *http.Client, baseURL string, logger clog.Logger) map[string]interface{} {
	loginData := map[string]string{
		"user_id":  "user123",
		"username": "Alice",
	}
	body, _ := json.Marshal(loginData)

	resp, err := client.Post(baseURL+"/login", "application/json", bytes.NewReader(body))
	if err != nil {
		logger.Error("login failed", clog.Error(err))
		return nil
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	result := make(map[string]interface{})
	json.Unmarshal(data, &result)

	if resp.StatusCode == http.StatusOK {
		logger.Info("login API response", clog.String("status", "success"), clog.Int("code", resp.StatusCode))
	} else {
		logger.Error("login API response", clog.String("status", "failed"), clog.Int("code", resp.StatusCode))
	}
	return result
}

// testGetProfile 测试获取个人资料
func testGetProfile(client *http.Client, baseURL, token string, logger clog.Logger) {
	req, _ := http.NewRequest("GET", baseURL+"/api/profile", nil)
	req.Header.Add("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		logger.Error("get profile failed", clog.Error(err))
		return
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	result := make(map[string]interface{})
	json.Unmarshal(data, &result)

	if resp.StatusCode == http.StatusOK {
		logger.Info("get profile API response",
			clog.String("status", "success"),
			clog.Int("code", resp.StatusCode),
			clog.String("user_id", fmt.Sprint(result["user_id"])),
			clog.String("username", fmt.Sprint(result["username"])),
		)
	} else {
		logger.Error("get profile API response", clog.String("status", "failed"), clog.Int("code", resp.StatusCode))
	}
}

// testRefreshToken 测试刷新 Token
func testRefreshToken(client *http.Client, baseURL, token string, logger clog.Logger) {
	refreshData := map[string]string{"token": token}
	body, _ := json.Marshal(refreshData)

	resp, err := client.Post(baseURL+"/refresh", "application/json", bytes.NewReader(body))
	if err != nil {
		logger.Error("refresh token failed", clog.Error(err))
		return
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	result := make(map[string]interface{})
	json.Unmarshal(data, &result)

	if resp.StatusCode == http.StatusOK {
		newToken := result["token"].(string)
		logger.Info("refresh token API response", clog.String("status", "success"), clog.Int("code", resp.StatusCode), clog.String("new_token", newToken[:20]+"..."))
	} else {
		logger.Error("refresh token API response", clog.String("status", "failed"), clog.Int("code", resp.StatusCode))
	}
}

// testAdminAPI 测试管理员接口
func testAdminAPI(client *http.Client, baseURL, token string, logger clog.Logger) {
	req, _ := http.NewRequest("GET", baseURL+"/api/admin", nil)
	req.Header.Add("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		logger.Error("admin API failed", clog.Error(err))
		return
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	result := make(map[string]interface{})
	json.Unmarshal(data, &result)

	if resp.StatusCode != http.StatusOK {
		logger.Info("admin API response (expected to fail)",
			clog.String("status", "forbidden"),
			clog.Int("code", resp.StatusCode),
			clog.String("error", fmt.Sprint(result["error"])),
		)
	} else {
		logger.Error("admin API response (unexpected success)", clog.Int("code", resp.StatusCode))
	}
}

// testInvalidToken 测试无效 Token
func testInvalidToken(client *http.Client, baseURL string, logger clog.Logger) {
	req, _ := http.NewRequest("GET", baseURL+"/api/profile", nil)
	req.Header.Add("Authorization", "Bearer invalid.token.here")

	resp, err := client.Do(req)
	if err != nil {
		logger.Error("invalid token test failed", clog.Error(err))
		return
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	result := make(map[string]interface{})
	json.Unmarshal(data, &result)

	if resp.StatusCode != http.StatusOK {
		logger.Info("invalid token API response (expected to fail)",
			clog.String("status", "unauthorized"),
			clog.Int("code", resp.StatusCode),
			clog.String("error", fmt.Sprint(result["error"])),
		)
	} else {
		logger.Error("invalid token API response (unexpected success)", clog.Int("code", resp.StatusCode))
	}
}

// testBatchLogin 批量测试登录
func testBatchLogin(client *http.Client, baseURL string, logger clog.Logger, index int) {
	loginData := map[string]string{
		"user_id":  fmt.Sprintf("user%d", index),
		"username": fmt.Sprintf("User%d", index),
	}
	body, _ := json.Marshal(loginData)

	resp, err := client.Post(baseURL+"/login", "application/json", bytes.NewReader(body))
	if err != nil {
		logger.Error("batch login failed", clog.Error(err), clog.Int("index", index))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		logger.Info("batch login success", clog.Int("index", index), clog.Int("code", resp.StatusCode))
	} else {
		logger.Error("batch login failed", clog.Int("index", index), clog.Int("code", resp.StatusCode))
	}
}

// testVariousInvalidTokens 测试各种无效 Token
func testVariousInvalidTokens(client *http.Client, baseURL string, logger clog.Logger) {
	invalidTokens := []string{
		"",                         // 空 token
		"invalid",                  // 格式错误
		"header.payload",           // 缺少签名
		"header.invalid.signature", // 无效 payload
		"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.invalid_signature", // 过期或无效签名
	}

	for i, token := range invalidTokens {
		req, _ := http.NewRequest("GET", baseURL+"/api/profile", nil)
		if token != "" {
			req.Header.Add("Authorization", "Bearer "+token)
		}

		resp, err := client.Do(req)
		if err != nil {
			logger.Error("invalid token test failed", clog.Error(err), clog.Int("test_index", i))
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusUnauthorized {
			logger.Info("invalid token rejected as expected", clog.Int("test_index", i), clog.Int("code", resp.StatusCode))
		} else {
			logger.Error("invalid token not properly rejected", clog.Int("test_index", i), clog.Int("code", resp.StatusCode))
		}
	}
}

// testMultipleRefresh 测试多次刷新 Token
func testMultipleRefresh(client *http.Client, baseURL, initialToken string, logger clog.Logger) {
	currentToken := initialToken

	for i := 0; i < 3; i++ {
		refreshData := map[string]string{"token": currentToken}
		body, _ := json.Marshal(refreshData)

		resp, err := client.Post(baseURL+"/refresh", "application/json", bytes.NewReader(body))
		if err != nil {
			logger.Error("multiple refresh failed", clog.Error(err), clog.Int("attempt", i+1))
			return
		}

		data, _ := io.ReadAll(resp.Body)
		result := make(map[string]interface{})
		json.Unmarshal(data, &result)
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			currentToken = result["token"].(string)
			logger.Info("multiple refresh success", clog.Int("attempt", i+1), clog.Int("code", resp.StatusCode))
		} else {
			logger.Error("multiple refresh failed", clog.Int("attempt", i+1), clog.Int("code", resp.StatusCode))
			break
		}
	}
}

// authMiddleware 认证中间件辅助函数
func authMiddleware(authenticator auth.Authenticator) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 获取 Authorization header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "missing authorization header",
			})
			return
		}

		// 解析 Bearer token
		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid authorization header format",
			})
			return
		}

		token := parts[1]

		// 验证 token
		claims, err := authenticator.ValidateToken(c.Request.Context(), token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": err.Error(),
			})
			return
		}

		// 将 claims 存入 context
		c.Set(auth.ClaimsKey, claims)
		c.Next()
	}
}
