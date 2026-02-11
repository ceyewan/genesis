package main

import (
	"context"
	"log"
	"time"

	"github.com/ceyewan/genesis/cache"
	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/redis/go-redis/v9"
)

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
	// 初始化日志记录器
	logger, err := clog.New(&clog.Config{
		Level:  "info",
		Format: "console",
		Output: "stdout",
	}, clog.WithNamespace("example", "cache"))
	if err != nil {
		log.Fatalf("初始化日志记录器失败: %v", err)
	}

	logger.Info("=== Genesis Cache 组件示例 ===")

	// 创建共享的 Redis 连接
	redisConn := setupRedis(logger)
	if redisConn == nil {
		logger.Warn("Redis 连接失败，跳过 Redis 相关示例")
	} else {
		defer redisConn.Close()
	}

	// 示例 1: Key-Value 缓存
	kvExample(logger, redisConn)

	// 示例 2: Hash 操作
	hashExample(logger, redisConn)

	// 示例 3: Sorted Set 排行榜
	sortedSetExample(logger, redisConn)

	// 示例 4: List 定长队列
	listExample(logger, redisConn)

	// 示例 5: 批量操作
	batchExample(logger, redisConn)

	// 示例 6: 单机内存缓存
	standaloneExample(logger)
}

// setupRedis 创建并连接 Redis
func setupRedis(logger clog.Logger) connector.RedisConnector {
	redisConn, err := connector.NewRedis(&connector.RedisConfig{
		Addr:         "127.0.0.1:6379",
		DialTimeout:  2 * time.Second,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
		PoolSize:     10,
	}, connector.WithLogger(logger))
	if err != nil {
		logger.Error("创建 Redis 连接器失败", clog.Error(err))
		return nil
	}

	if err := redisConn.Connect(context.Background()); err != nil {
		logger.Error("连接 Redis 失败", clog.Error(err))
		return nil
	}

	logger.Info("Redis 连接成功")
	return redisConn
}

// newCache 创建 Redis 缓存实例
func newCache(conn connector.RedisConnector, prefix string, logger clog.Logger) cache.Cache {
	cfg := &cache.Config{
		Driver:     cache.DriverRedis,
		Prefix:     prefix,
		Serializer: "json",
	}
	c, err := cache.New(cfg, cache.WithRedisConnector(conn), cache.WithLogger(logger))
	if err != nil {
		logger.Error("创建缓存失败", clog.Error(err))
		return nil
	}
	return c
}

func kvExample(logger clog.Logger, redisConn connector.RedisConnector) {
	logger.Info("--- 示例 1: Key-Value 缓存 ---")

	if redisConn == nil {
		logger.Warn("跳过：Redis 未连接")
		return
	}

	c := newCache(redisConn, "demo:kv:", logger)
	if c == nil {
		return
	}

	ctx := context.Background()

	// 设置缓存
	route := UserRoute{
		UserID:    "user_1001",
		GatewayID: "gateway_01",
		ServerID:  "server_01",
	}

	if err := c.Set(ctx, "route:"+route.UserID, route, 10*time.Minute); err != nil {
		logger.Error("设置缓存失败", clog.Error(err))
		return
	}
	logger.Info("已缓存用户路由", clog.String("user_id", route.UserID))

	// 获取缓存
	var cachedRoute UserRoute
	if err := c.Get(ctx, "route:"+route.UserID, &cachedRoute); err != nil {
		logger.Error("获取缓存失败", clog.Error(err))
		return
	}
	logger.Info("获取到用户路由", clog.String("server", cachedRoute.ServerID))

	// 检查是否存在
	exists, _ := c.Has(ctx, "route:"+route.UserID)
	logger.Info("缓存存在性检查", clog.Bool("exists", exists))

	// 删除缓存
	c.Delete(ctx, "route:"+route.UserID)
}

func hashExample(logger clog.Logger, redisConn connector.RedisConnector) {
	logger.Info("--- 示例 2: Hash 字段操作 ---")

	if redisConn == nil {
		logger.Warn("跳过：Redis 未连接")
		return
	}

	c := newCache(redisConn, "demo:hash:", logger)
	if c == nil {
		return
	}

	ctx := context.Background()
	userID := "user_1001"

	// 设置字段
	c.HSet(ctx, "user:"+userID, "name", "Alice")
	c.HSet(ctx, "user:"+userID, "age", 28)
	c.HSet(ctx, "user:"+userID, "city", "Beijing")
	logger.Info("已设置用户字段")

	// 获取单个字段
	var name string
	c.HGet(ctx, "user:"+userID, "name", &name)
	logger.Info("获取用户名", clog.String("name", name))

	// 原子递增
	newAge, _ := c.HIncrBy(ctx, "user:"+userID, "age", 1)
	logger.Info("年龄递增后", clog.Int64("age", newAge))

	// 获取所有字段
	var allFields map[string]string
	c.HGetAll(ctx, "user:"+userID, &allFields)
	logger.Info("所有字段", clog.Any("fields", allFields))

	// 清理
	c.HDel(ctx, "user:"+userID, "name", "age", "city")
}

func sortedSetExample(logger clog.Logger, redisConn connector.RedisConnector) {
	logger.Info("--- 示例 3: Sorted Set 排行榜 ---")

	if redisConn == nil {
		logger.Warn("跳过：Redis 未连接")
		return
	}

	c := newCache(redisConn, "demo:zset:", logger)
	if c == nil {
		return
	}

	ctx := context.Background()
	key := "leaderboard"

	// 添加成员（分数为得分）
	c.ZAdd(ctx, key, 100, "player_alice")
	c.ZAdd(ctx, key, 85, "player_bob")
	c.ZAdd(ctx, key, 92, "player_charlie")
	c.ZAdd(ctx, key, 78, "player_david")
	logger.Info("已添加排行榜成员")

	// 获取分数
	score, _ := c.ZScore(ctx, key, "player_alice")
	logger.Info("Alice 的分数", clog.Float64("score", score))

	// 获取前 3 名（分数倒序）
	var top3 []string
	c.ZRevRange(ctx, key, 0, 2, &top3)
	logger.Info("排行榜前 3 名", clog.Any("players", top3))

	// 按分数范围查询
	var qualified []string
	c.ZRangeByScore(ctx, key, 80, 100, &qualified)
	logger.Info("分数 >= 80 的玩家", clog.Any("players", qualified))

	// 清理
	c.ZRem(ctx, key, "player_alice", "player_bob", "player_charlie", "player_david")
}

func listExample(logger clog.Logger, redisConn connector.RedisConnector) {
	logger.Info("--- 示例 4: List 定长队列 ---")

	if redisConn == nil {
		logger.Warn("跳过：Redis 未连接")
		return
	}

	c := newCache(redisConn, "demo:list:", logger)
	if c == nil {
		return
	}

	ctx := context.Background()
	key := "recent_logs"

	// 清空旧数据
	c.Delete(ctx, key)

	// 添加多条日志（只保留最新 5 条）
	logs := []string{"log1", "log2", "log3", "log4", "log5", "log6", "log7"}
	values := make([]any, len(logs))
	for i, v := range logs {
		values[i] = v
	}
	c.LPushCapped(ctx, key, 5, values...)
	logger.Info("已添加日志（保留最新 5 条）")

	// 获取所有日志
	var allLogs []string
	c.LRange(ctx, key, 0, -1, &allLogs)
	logger.Info("当前日志列表", clog.Any("logs", allLogs))

	// 从左侧弹出
	var firstLog string
	c.LPop(ctx, key, &firstLog)
	logger.Info("弹出的日志", clog.String("log", firstLog))

	// 清理
	c.Delete(ctx, key)
}

func batchExample(logger clog.Logger, redisConn connector.RedisConnector) {
	logger.Info("--- 示例 5: 批量操作 ---")

	if redisConn == nil {
		logger.Warn("跳过：Redis 未连接")
		return
	}

	c := newCache(redisConn, "demo:batch:", logger)
	if c == nil {
		return
	}

	ctx := context.Background()

	// 批量设置
	items := map[string]any{
		"user:1": UserRoute{UserID: "1", GatewayID: "gw1", ServerID: "sv1"},
		"user:2": UserRoute{UserID: "2", GatewayID: "gw2", ServerID: "sv2"},
		"user:3": UserRoute{UserID: "3", GatewayID: "gw3", ServerID: "sv3"},
	}
	c.MSet(ctx, items, 5*time.Minute)
	logger.Info("批量设置完成", clog.Int("count", len(items)))

	// 批量获取
	keys := []string{"user:1", "user:2", "user:3", "user:999"}
	var results []UserRoute
	c.MGet(ctx, keys, &results)
	logger.Info("批量获取结果", clog.Int("count", len(results)))

	// 清理
	c.Delete(ctx, "user:1")
	c.Delete(ctx, "user:2")
	c.Delete(ctx, "user:3")

	// 演示 Client() 方法：访问底层 Redis 客户执行 Pipeline
	logger.Info("--- 使用底层客户端执行 Pipeline ---")
	client := c.Client()
	if client != nil {
		pipe := client.(*redis.Client).Pipeline()
		pipe.Set(ctx, "demo:batch:pipe:1", "value1", 0)
		pipe.Set(ctx, "demo:batch:pipe:2", "value2", 0)
		pipe.Get(ctx, "demo:batch:pipe:1")
		_, _ = pipe.Exec(ctx)
		logger.Info("Pipeline 执行完成")
		c.Delete(ctx, "pipe:1")
		c.Delete(ctx, "pipe:2")
	}
}

func standaloneExample(logger clog.Logger) {
	logger.Info("--- 示例 6: 单机内存缓存 ---")

	cfg := &cache.Config{
		Driver: cache.DriverMemory,
		Standalone: &cache.StandaloneConfig{
			Capacity: 1000,
		},
	}

	c, err := cache.New(cfg, cache.WithLogger(logger))
	if err != nil {
		logger.Error("创建内存缓存失败", clog.Error(err))
		return
	}
	defer c.Close()

	ctx := context.Background()

	// 设置缓存
	c.Set(ctx, "local_key", map[string]string{"theme": "dark", "lang": "zh-CN"}, 5*time.Minute)
	logger.Info("已设置本地缓存")

	// 获取缓存
	var value map[string]string
	c.Get(ctx, "local_key", &value)
	logger.Info("获取到本地缓存", clog.Any("value", value))

	// 验证不支持的操作
	err = c.HSet(ctx, "hash_key", "field", "value")
	logger.Info("Hash 操作预期返回错误", clog.String("error", err.Error()))
}
