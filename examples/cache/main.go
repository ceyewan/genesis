package main

import (
	"context"
	"log"
	"time"

	"github.com/ceyewan/genesis/pkg/cache"
	"github.com/ceyewan/genesis/pkg/clog"
	clogtypes "github.com/ceyewan/genesis/pkg/clog/types"
	"github.com/ceyewan/genesis/pkg/connector"
	"github.com/ceyewan/genesis/pkg/container"
)

// 用户路由信息
type UserRoute struct {
	UserID    string `json:"user_id" msgpack:"user_id"`
	GatewayID string `json:"gateway_id" msgpack:"gateway_id"`
	ServerID  string `json:"server_id" msgpack:"server_id"`
}

// 会话信息
type Session struct {
	SessionID string    `json:"session_id" msgpack:"session_id"`
	UserID1   string    `json:"user_id1" msgpack:"user_id1"`
	UserID2   string    `json:"user_id2" msgpack:"user_id2"`
	LastTime  time.Time `json:"last_time" msgpack:"last_time"`
}

// 消息
type Message struct {
	MessageID string    `json:"message_id" msgpack:"message_id"`
	SessionID string    `json:"session_id" msgpack:"session_id"`
	UserID    string    `json:"user_id" msgpack:"user_id"`
	Content   string    `json:"content" msgpack:"content"`
	Timestamp time.Time `json:"timestamp" msgpack:"timestamp"`
	Seq       int64     `json:"seq" msgpack:"seq"`
}

func main() {
	// 初始化日志记录器
	logger, err := clog.New(&clogtypes.Config{
		Level:  "info",
		Format: "console",
		Output: "stdout",
	}, &clogtypes.Option{
		NamespaceParts: []string{"example", "cache", "im"},
	})
	if err != nil {
		log.Fatalf("初始化日志记录器失败: %v", err)
	}

	logger.Info("=== Genesis Cache 组件示例 ===")
	logger.Info("本示例演示 Cache 组件的两种使用模式:")
	logger.Info("  1. 独立模式 (Standalone): 手动创建连接器和组件")
	logger.Info("  2. 容器模式 (Container): 由 Container 统一管理")
	logger.Info("")

	// 演示容器模式
	containerModeExample(logger)

	logger.Info("")
	logger.Info("=== IM 场景示例 (独立模式) ===")

	// 示例 1: 用户路由缓存
	userRouteExample(logger)

	// 示例 2: 会话列表缓存（按最后通话时间排序）
	sessionListExample(logger)

	// 示例 3: 最新消息缓存（定长列表）
	recentMessagesExample(logger)
}

func containerModeExample(logger clog.Logger) {
	logger.Info("--- 容器模式示例 ---")

	// 使用 Container 统一管理 Cache 组件
	containerCfg := &container.Config{
		Redis: &connector.RedisConfig{
			Addr:        "127.0.0.1:6379",
			Password:    "your_redis_password",
			DialTimeout: 2 * time.Second,
		},
		Cache: &cache.Config{
			Prefix:     "container:",
			Serializer: "json",
		},
	}

	c, err := container.New(containerCfg, container.WithLogger(logger))
	if err != nil {
		logger.Warn("跳过容器模式示例: 容器初始化失败", clog.Error(err))
		return
	}
	defer c.Close()

	ctx := context.Background()

	// 直接使用 Container 中的 Cache
	if c.Cache == nil {
		logger.Warn("Cache 组件未初始化")
		return
	}

	// 缓存用户信息
	route := UserRoute{
		UserID:    "user_2001",
		GatewayID: "gateway_02",
		ServerID:  "server_02",
	}

	err = c.Cache.Set(ctx, "user:"+route.UserID, route, 24*time.Hour)
	if err != nil {
		logger.Error("设置用户路由失败", clog.Error(err))
		return
	}
	logger.Info("✓ 容器模式: 已缓存用户路由", clog.Any("route", route))

	// 获取用户路由信息
	var cachedRoute UserRoute
	err = c.Cache.Get(ctx, "user:"+route.UserID, &cachedRoute)
	if err != nil {
		logger.Error("获取用户路由失败", clog.Error(err))
		return
	}
	logger.Info("✓ 容器模式: 获取到的用户路由", clog.Any("route", cachedRoute))
}

func userRouteExample(logger clog.Logger) {
	logger.Info("--- 示例 1: 用户路由缓存 ---")

	// 1. 使用 Container 初始化 Redis 连接器
	containerCfg := &container.Config{
		Redis: &connector.RedisConfig{
			Addr:        "127.0.0.1:6379",
			Password:    "your_redis_password",
			DialTimeout: 2 * time.Second,
		},
	}

	c, err := container.New(containerCfg, container.WithLogger(logger))
	if err != nil {
		logger.Warn("跳过用户路由示例: 容器初始化失败", clog.Error(err))
		return
	}
	defer c.Close()

	redisConn, err := c.GetRedisConnector(*containerCfg.Redis)
	if err != nil {
		logger.Warn("跳过用户路由示例: 获取连接器失败", clog.Error(err))
		return
	}

	// 2. 配置缓存
	cacheCfg := &cache.Config{
		Prefix:             "im:route:",
		RedisConnectorName: "default",
		Serializer:         "json",
	}

	// 3. 创建缓存实例 (独立模式)
	cacheClient, err := cache.New(redisConn, cacheCfg, cache.WithLogger(logger))
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
	logger.Info("已缓存用户路由", clog.Any("route", route))

	// 5. 获取用户路由信息
	var cachedRoute UserRoute
	err = cacheClient.Get(ctx, "user:"+route.UserID, &cachedRoute)
	if err != nil {
		logger.Error("获取用户路由失败", clog.Error(err))
		return
	}
	logger.Info("获取到的用户路由", clog.Any("route", cachedRoute))
}

func sessionListExample(logger clog.Logger) {
	logger.Info("--- 示例 2: 会话列表缓存（ZSet 按时间排序）---")

	// 1. 使用 Container 初始化 Redis 连接器
	containerCfg := &container.Config{
		Redis: &connector.RedisConfig{
			Addr:        "127.0.0.1:6379",
			Password:    "your_redis_password",
			DialTimeout: 2 * time.Second,
		},
	}

	c, err := container.New(containerCfg, container.WithLogger(logger))
	if err != nil {
		logger.Warn("跳过会话列表示例: 容器初始化失败", clog.Error(err))
		return
	}
	defer c.Close()

	redisConn, err := c.GetRedisConnector(*containerCfg.Redis)
	if err != nil {
		logger.Warn("跳过会话列表示例: 获取连接器失败", clog.Error(err))
		return
	}

	// 2. 配置缓存
	cacheCfg := &cache.Config{
		Prefix:             "im:session:",
		RedisConnectorName: "default",
		Serializer:         "json",
	}

	// 3. 创建缓存实例 (独立模式)
	cacheClient, err := cache.New(redisConn, cacheCfg, cache.WithLogger(logger))
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
	logger.Info("已添加会话到用户会话列表", clog.Int("count", len(sessions)))

	// 5. 获取最近活跃的会话（按时间倒序）
	var recentSessions []string
	err = cacheClient.ZRevRange(ctx, "user:"+userID+":sessions", 0, 2, &recentSessions)
	if err != nil {
		logger.Error("获取会话列表失败", clog.Error(err))
		return
	}

	logger.Info("用户最近活跃的会话", clog.Any("sessions", recentSessions))

	// 6. 获取会话详细信息
	for _, sessionID := range recentSessions {
		var sessionInfo map[string]string
		err = cacheClient.HGetAll(ctx, "info:"+sessionID, &sessionInfo)
		if err != nil {
			logger.Error("获取会话详情失败", clog.Error(err))
			continue
		}
		logger.Info("会话详情", clog.String("session_id", sessionID), clog.Any("info", sessionInfo))
	}
}

func recentMessagesExample(logger clog.Logger) {
	logger.Info("--- 示例 3: 最新消息缓存（定长列表）---")

	// 1. 使用 Container 初始化 Redis 连接器
	containerCfg := &container.Config{
		Redis: &connector.RedisConfig{
			Addr:        "127.0.0.1:6379",
			Password:    "your_redis_password",
			DialTimeout: 2 * time.Second,
		},
	}

	c, err := container.New(containerCfg, container.WithLogger(logger))
	if err != nil {
		logger.Warn("跳过最新消息示例: 容器初始化失败", clog.Error(err))
		return
	}
	defer c.Close()

	redisConn, err := c.GetRedisConnector(*containerCfg.Redis)
	if err != nil {
		logger.Warn("跳过最新消息示例: 获取连接器失败", clog.Error(err))
		return
	}

	// 2. 配置缓存
	cacheCfg := &cache.Config{
		Prefix:             "im:message:",
		RedisConnectorName: "default",
		Serializer:         "json",
	}

	// 3. 创建缓存实例 (独立模式)
	cacheClient, err := cache.New(redisConn, cacheCfg, cache.WithLogger(logger))
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
	logger.Info("已添加消息到会话消息列表", clog.Int("count", len(messages)), clog.Int("limit", 10))

	// 5. 获取最近的消息
	var recentMessages []Message
	err = cacheClient.LRange(ctx, "session:"+sessionID+":messages", 0, 9, &recentMessages)
	if err != nil {
		logger.Error("获取消息列表失败", clog.Error(err))
		return
	}

	logger.Info("会话最近的消息:")
	for i, msg := range recentMessages {
		logger.Info("  消息",
			clog.Int("index", i+1),
			clog.String("user_id", msg.UserID),
			clog.String("time", msg.Timestamp.Format("15:04:05")),
			clog.String("content", msg.Content))
	}
}
