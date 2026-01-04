package main

import (
	"context"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/ceyewan/genesis/cache"
	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/joho/godotenv"
)

// getEnvOrDefault 获取环境变量，如果不存在则返回默认值
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvIntOrDefault 获取环境变量并转换为 int，如果不存在或转换失败则返回默认值
func getEnvIntOrDefault(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// 用户路由信息
type UserRoute struct {
	UserID    string `json:"user_id"`
	GatewayID string `json:"gateway_id"`
	ServerID  string `json:"server_id"`
}

// 会话信息
type Session struct {
	SessionID string    `json:"session_id"`
	UserID1   string    `json:"user_id1"`
	UserID2   string    `json:"user_id2"`
	LastTime  time.Time `json:"last_time"`
}

// 消息
type Message struct {
	MessageID string    `json:"message_id"`
	SessionID string    `json:"session_id"`
	UserID    string    `json:"user_id"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
	Seq       int64     `json:"seq"`
}

func main() {
	// 0. 加载环境变量（从根目录）
	if err := godotenv.Load("/Users/ceyewan/CodeField/genesis/.env"); err != nil {
		log.Printf("Warning: could not load .env file: %v", err)
	}

	// 初始化日志记录器
	logger, err := clog.New(&clog.Config{
		Level:  "info",
		Format: "console",
		Output: "stdout",
	}, clog.WithNamespace("example", "cache", "im"))
	if err != nil {
		log.Fatalf("初始化日志记录器失败: %v", err)
	}

	logger.Info("=== Genesis Cache 组件示例 (Go Native DI) ===")
	logger.Info("本示例演示 Cache 组件的标准使用模式:")
	logger.Info("  1. 显式创建连接器")
	logger.Info("  2. 显式创建缓存组件")
	logger.Info("  3. 使用 Redis 的各种数据结构")
	logger.Info("")

	// 示例 1: 用户路由缓存
	userRouteExample(logger)

	// 示例 2: 会话列表缓存（按最后通话时间排序）
	sessionListExample(logger)

	// 示例 3: 最新消息缓存（定长列表）
	recentMessagesExample(logger)

	// 示例 4: 单机内存缓存
	standaloneExample(logger)
}

func userRouteExample(logger clog.Logger) {
	logger.Info("--- 示例 1: 用户路由缓存 ---")

	// 1. 创建 Redis 连接器
	redisConn, err := connector.NewRedis(&connector.RedisConfig{
		Addr:         getEnvOrDefault("REDIS_ADDR", "127.0.0.1:6379"),
		Password:     getEnvOrDefault("REDIS_PASSWORD", ""), // 从环境变量读取密码
		DB:           getEnvIntOrDefault("REDIS_DB", 0),
		DialTimeout:  2 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     getEnvIntOrDefault("REDIS_POOL_SIZE", 10),
	}, connector.WithLogger(logger))
	if err != nil {
		logger.Warn("跳过用户路由示例: 连接器初始化失败", clog.Error(err))
		return
	}
	defer redisConn.Close()

	// 2. 配置缓存
	cacheCfg := &cache.Config{
		Driver:     cache.DriverRedis,
		Prefix:     "im:route:",
		Serializer: "json",
	}

	// 3. 创建缓存实例 (Go Native DI)
	cacheClient, err := cache.New(cacheCfg, cache.WithRedisConnector(redisConn), cache.WithLogger(logger))
	if err != nil {
		logger.Error("创建缓存失败", clog.Error(err))
		return
	}

	ctx := context.Background()

	// 4. 缓存用户路由信息
	route := UserRoute{
		UserID:    "user_1001",
		GatewayID: "gateway_01",
		ServerID:  "server_01",
	}

	err = cacheClient.Set(ctx, "user:"+route.UserID, route, 24*time.Hour)
	if err != nil {
		logger.Error("设置用户路由失败", clog.Error(err))
		return
	}
	logger.Info("✓ 已缓存用户路由", clog.Any("route", route))

	// 5. 获取用户路由信息
	var cachedRoute UserRoute
	err = cacheClient.Get(ctx, "user:"+route.UserID, &cachedRoute)
	if err != nil {
		logger.Error("获取用户路由失败", clog.Error(err))
		return
	}
	logger.Info("✓ 获取到的用户路由", clog.Any("route", cachedRoute))
}

func sessionListExample(logger clog.Logger) {
	logger.Info("--- 示例 2: 会话列表缓存（ZSet 按时间排序）---")

	// 1. 创建 Redis 连接器
	redisConn, err := connector.NewRedis(&connector.RedisConfig{
		Addr:         getEnvOrDefault("REDIS_ADDR", "127.0.0.1:6379"),
		Password:     getEnvOrDefault("REDIS_PASSWORD", ""), // 从环境变量读取密码
		DB:           getEnvIntOrDefault("REDIS_DB", 0),
		DialTimeout:  2 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     getEnvIntOrDefault("REDIS_POOL_SIZE", 10),
	}, connector.WithLogger(logger))
	if err != nil {
		logger.Warn("跳过会话列表示例: 连接器初始化失败", clog.Error(err))
		return
	}
	defer redisConn.Close()

	// 2. 配置缓存
	cacheCfg := &cache.Config{
		Driver:     cache.DriverRedis,
		Prefix:     "im:session:",
		Serializer: "json",
	}

	// 3. 创建缓存实例
	cacheClient, err := cache.New(cacheCfg, cache.WithRedisConnector(redisConn), cache.WithLogger(logger))
	if err != nil {
		logger.Error("创建缓存失败", clog.Error(err))
		return
	}

	ctx := context.Background()
	userID := "user_1001"

	// 4. 添加会话（使用时间戳作为分数）
	sessions := []Session{
		{SessionID: "session_001", UserID1: userID, UserID2: "user_1002", LastTime: time.Now().Add(-2 * time.Hour)},
		{SessionID: "session_002", UserID1: userID, UserID2: "user_1003", LastTime: time.Now().Add(-1 * time.Hour)},
		{SessionID: "session_003", UserID1: userID, UserID2: "user_1004", LastTime: time.Now()},
	}

	for _, session := range sessions {
		// 将会话信息缓存为 Hash
		err = cacheClient.HSet(ctx, "info:"+session.SessionID, "user_id1", session.UserID1)
		if err != nil {
			logger.Error("缓存会话信息失败", clog.Error(err))
			continue
		}
		err = cacheClient.HSet(ctx, "info:"+session.SessionID, "user_id2", session.UserID2)
		if err != nil {
			logger.Error("缓存会话信息失败", clog.Error(err))
			continue
		}

		// 将会话ID添加到用户的会话列表（按时间排序）
		score := float64(session.LastTime.Unix())
		err = cacheClient.ZAdd(ctx, "user:"+userID+":sessions", score, session.SessionID)
		if err != nil {
			logger.Error("添加会话到列表失败", clog.Error(err))
			continue
		}
	}
	logger.Info("✓ 已添加会话到用户会话列表", clog.Int("count", len(sessions)))

	// 5. 获取最近活跃的会话（按时间倒序）
	var recentSessions []string
	err = cacheClient.ZRevRange(ctx, "user:"+userID+":sessions", 0, 2, &recentSessions)
	if err != nil {
		logger.Error("获取会话列表失败", clog.Error(err))
		return
	}

	logger.Info("✓ 用户最近活跃的会话", clog.Any("sessions", recentSessions))

	// 6. 获取会话详细信息
	for _, sessionID := range recentSessions {
		var sessionInfo map[string]string
		err = cacheClient.HGetAll(ctx, "info:"+sessionID, &sessionInfo)
		if err != nil {
			logger.Error("获取会话详情失败", clog.Error(err))
			continue
		}
		logger.Info("✓ 会话详情", clog.String("session_id", sessionID), clog.Any("info", sessionInfo))
	}
}

func recentMessagesExample(logger clog.Logger) {
	logger.Info("--- 示例 3: 最新消息缓存（定长列表）---")

	// 1. 创建 Redis 连接器
	redisConn, err := connector.NewRedis(&connector.RedisConfig{
		Addr:         getEnvOrDefault("REDIS_ADDR", "127.0.0.1:6379"),
		Password:     getEnvOrDefault("REDIS_PASSWORD", ""), // 从环境变量读取密码
		DB:           getEnvIntOrDefault("REDIS_DB", 0),
		DialTimeout:  2 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     getEnvIntOrDefault("REDIS_POOL_SIZE", 10),
	}, connector.WithLogger(logger))
	if err != nil {
		logger.Warn("跳过最新消息示例: 连接器初始化失败", clog.Error(err))
		return
	}
	defer redisConn.Close()

	// 2. 配置缓存
	cacheCfg := &cache.Config{
		Driver:     cache.DriverRedis,
		Prefix:     "im:message:",
		Serializer: "json",
	}

	// 3. 创建缓存实例
	cacheClient, err := cache.New(cacheCfg, cache.WithRedisConnector(redisConn), cache.WithLogger(logger))
	if err != nil {
		logger.Error("创建缓存失败", clog.Error(err))
		return
	}

	ctx := context.Background()
	sessionID := "session_001"

	// 4. 添加消息到定长列表（只保留最近10条）
	messages := []Message{
		{MessageID: "msg_001", SessionID: sessionID, UserID: "user_1001", Content: "你好", Timestamp: time.Now().Add(-5 * time.Minute), Seq: 1},
		{MessageID: "msg_002", SessionID: sessionID, UserID: "user_1002", Content: "你好，很高兴认识你", Timestamp: time.Now().Add(-4 * time.Minute), Seq: 2},
		{MessageID: "msg_003", SessionID: sessionID, UserID: "user_1001", Content: "最近怎么样？", Timestamp: time.Now().Add(-3 * time.Minute), Seq: 3},
		{MessageID: "msg_004", SessionID: sessionID, UserID: "user_1002", Content: "还不错，谢谢关心", Timestamp: time.Now().Add(-2 * time.Minute), Seq: 4},
		{MessageID: "msg_005", SessionID: sessionID, UserID: "user_1001", Content: "那就好", Timestamp: time.Now().Add(-1 * time.Minute), Seq: 5},
	}

	// 使用定长列表，只保留最近10条消息
	// 将 Message 切片转换为 any 切片
	messageValues := make([]any, len(messages))
	for i, msg := range messages {
		messageValues[i] = msg
	}
	err = cacheClient.LPushCapped(ctx, "session:"+sessionID+":messages", 10, messageValues...)
	if err != nil {
		logger.Error("添加消息到定长列表失败", clog.Error(err))
		return
	}
	logger.Info("✓ 已添加消息到会话消息列表", clog.Int("count", len(messages)), clog.Int("limit", 10))

	// 5. 获取最近的消息
	var recentMessages []Message
	err = cacheClient.LRange(ctx, "session:"+sessionID+":messages", 0, 9, &recentMessages)
	if err != nil {
		logger.Error("获取消息列表失败", clog.Error(err))
		return
	}

	logger.Info("✓ 会话最近的消息:")
	for i, msg := range recentMessages {
		logger.Info("  消息",
			clog.Int("index", i+1),
			clog.String("user_id", msg.UserID),
			clog.String("time", msg.Timestamp.Format("15:04:05")),
			clog.String("content", msg.Content))
	}
}

func standaloneExample(logger clog.Logger) {
	logger.Info("--- 示例 4: 单机内存缓存 ---")

	// 1. 配置缓存
	cacheCfg := &cache.Config{
		Driver: cache.DriverMemory,
		Standalone: &cache.StandaloneConfig{
			Capacity: 1000,
		},
	}

	// 2. 创建缓存实例
	cacheClient, err := cache.New(cacheCfg, cache.WithLogger(logger))
	if err != nil {
		logger.Error("创建单机缓存失败", clog.Error(err))
		return
	}
	defer cacheClient.Close()

	ctx := context.Background()

	// 3. 缓存本地数据
	key := "local_config"
	value := map[string]string{
		"theme": "dark",
		"lang":  "zh-CN",
	}

	err = cacheClient.Set(ctx, key, value, 10*time.Minute)
	if err != nil {
		logger.Error("设置本地缓存失败", clog.Error(err))
		return
	}
	logger.Info("✓ 已缓存本地配置", clog.Any("config", value))

	// 4. 获取本地数据
	var cachedValue map[string]string
	err = cacheClient.Get(ctx, key, &cachedValue)
	if err != nil {
		logger.Error("获取本地缓存失败", clog.Error(err))
		return
	}
	logger.Info("✓ 获取到的本地配置", clog.Any("config", cachedValue))

	// 5. 尝试不支持的操作
	err = cacheClient.HSet(ctx, "hash_key", "field", "value")
	if err != nil {
		logger.Info("✓ 验证不支持的操作返回错误: " + err.Error())
	}
}
