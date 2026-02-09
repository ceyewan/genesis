package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/ceyewan/genesis/auth"
	"github.com/ceyewan/genesis/clog"
	"github.com/golang-jwt/jwt/v5"
)

func main() {
	// 1. 初始化配置
	cfg := &auth.Config{
		SecretKey:      "your-secret-key-must-be-at-least-32-chars",
		SigningMethod:  "HS256",
		Issuer:         "my-app",
		AccessTokenTTL: 15 * time.Minute,
		TokenLookup:    "header:Authorization",
		TokenHeadName:  "Bearer",
	}

	// 2. 初始化日志
	logger, err := clog.New(clog.NewDevDefaultConfig("example"))
	if err != nil {
		log.Fatalf("create logger: %v", err)
	}

	// 3. 创建认证器
	authenticator, err := auth.New(cfg, auth.WithLogger(logger))
	if err != nil {
		log.Fatalf("create authenticator: %v", err)
	}

	// 4. 创建 HTTP 路由
	mux := http.NewServeMux()

	// 登录接口 - 生成 Token
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			UserID   string `json:"user_id" binding:"required"`
			Username string `json:"username"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// 创建 Claims
		claims := &auth.Claims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject: req.UserID,
			},
			Username: req.Username,
			Roles:    []string{"user"},
		}

		// 生成 Token
		token, err := authenticator.GenerateToken(r.Context(), claims)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		response := map[string]interface{}{
			"token":      token,
			"expires_in": int(cfg.AccessTokenTTL.Seconds()),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	})

	// 受保护的路由
	mux.HandleFunc("/profile", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// 从 Authorization header 获取 token
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "missing authorization header", http.StatusUnauthorized)
			return
		}

		// 解析 Bearer token
		if len(authHeader) < 7 || authHeader[:7] != "Bearer " {
			http.Error(w, "invalid authorization header format", http.StatusUnauthorized)
			return
		}
		token := authHeader[7:]

		// 验证 token
		claims, err := authenticator.ValidateToken(r.Context(), token)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}

		// 返回用户信息
		response := map[string]interface{}{
			"user_id":  claims.Subject,
			"username": claims.Username,
			"roles":    claims.Roles,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	})

	// 启动服务器
	go func() {
		logger.Info("auth example server starting", clog.String("addr", ":12345"))
		if err := http.ListenAndServe(":12345", mux); err != nil {
			logger.Error("server error", clog.Error(err))
		}
	}()

	// 等待服务器启动
	time.Sleep(1 * time.Second)

	// 运行基本测试
	runBasicTests(logger)
}

// runBasicTests 运行基本功能测试
func runBasicTests(logger clog.Logger) {
	client := &http.Client{Timeout: 10 * time.Second}
	baseURL := "http://localhost:12345"

	logger.Info("=== 开始基本功能测试 ===")

	// 测试1: 登录获取 token
	logger.Info("测试1: 登录获取 token")
	loginData := map[string]string{
		"user_id":  "user123",
		"username": "Alice",
	}
	body, _ := json.Marshal(loginData)

	resp, err := client.Post(baseURL+"/login", "application/json", bytes.NewReader(body))
	if err != nil {
		logger.Error("登录失败", clog.Error(err))
		return
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	result := make(map[string]interface{})
	json.Unmarshal(data, &result)

	if resp.StatusCode != http.StatusOK {
		logger.Error("登录失败", clog.Int("code", resp.StatusCode))
		return
	}

	token := result["token"].(string)
	logger.Info("登录成功", clog.String("token", token[:20]+"..."))

	// 测试2: 使用 token 访问受保护的路由
	logger.Info("测试2: 访问受保护的路由")
	req, _ := http.NewRequest("GET", baseURL+"/profile", nil)
	req.Header.Add("Authorization", "Bearer "+token)

	resp, err = client.Do(req)
	if err != nil {
		logger.Error("访问受保护路由失败", clog.Error(err))
		return
	}
	defer resp.Body.Close()

	data, _ = io.ReadAll(resp.Body)
	json.Unmarshal(data, &result)

	if resp.StatusCode != http.StatusOK {
		logger.Error("访问受保护路由失败", clog.Int("code", resp.StatusCode))
		return
	}

	logger.Info("访问受保护路由成功",
		clog.String("user_id", fmt.Sprint(result["user_id"])),
		clog.String("username", fmt.Sprint(result["username"])),
	)

	// 测试3: 使用无效 token
	logger.Info("测试3: 使用无效 token")
	req, _ = http.NewRequest("GET", baseURL+"/profile", nil)
	req.Header.Add("Authorization", "Bearer invalid.token.here")

	resp, err = client.Do(req)
	if err != nil {
		logger.Error("测试无效token失败", clog.Error(err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		logger.Error("无效token未被正确拒绝", clog.Int("code", resp.StatusCode))
	} else {
		logger.Info("无效token被正确拒绝")
	}

	// 测试4: 缺少 Authorization header
	logger.Info("测试4: 缺少 Authorization header")
	req, _ = http.NewRequest("GET", baseURL+"/profile", nil)

	resp, err = client.Do(req)
	if err != nil {
		logger.Error("测试缺少header失败", clog.Error(err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		logger.Error("缺少header未被正确拒绝", clog.Int("code", resp.StatusCode))
	} else {
		logger.Info("缺少header被正确拒绝")
	}

	logger.Info("=== 基本功能测试完成 ===")
}
