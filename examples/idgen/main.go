package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/idgen"
)

func main() {
	fmt.Println("=== Genesis ID Generator Examples ===")
	fmt.Println()

	// 示例 1: Snowflake (手动指定 WorkerID)
	snowflakeManualExample()

	// 示例 2: Snowflake (Allocator 自动分配 WorkerID)
	snowflakeAllocatorExample()

	// 示例 3: UUID (配置驱动)
	uuidExample()

	// 示例 4: Sequence (基于 Redis)
	sequenceExample()
}

// ========================================
// Snowflake 示例
// ========================================

func snowflakeManualExample() {
	fmt.Println("--- Example 1: Snowflake (手动指定 WorkerID) ---")

	// 创建 Snowflake 实例
	sf, err := idgen.NewGenerator(&idgen.GeneratorConfig{
		WorkerID:     23,
		DatacenterID: 1, // 可选：设置数据中心 ID
	})
	if err != nil {
		log.Printf("Failed to create Snowflake: %v\n", err)
		return
	}

	fmt.Println("生成 5 个 Snowflake ID:")
	for i := 0; i < 5; i++ {
		id := sf.Next()
		fmt.Printf("  ID %d: %d\n", i+1, id)
		time.Sleep(time.Millisecond)
	}
	fmt.Println()
}

func snowflakeAllocatorExample() {
	fmt.Println("--- Example 2: Snowflake (Allocator 自动分配 WorkerID) ---")
	fmt.Println("Note: 此示例需要 Redis 运行在 localhost:6379")

	// 1. 创建 Redis 连接器
	redisCfg := &connector.RedisConfig{
		Name:         "idgen-redis-example",
		Addr:         "127.0.0.1:6379",
		Password:     os.Getenv("GENESIS_REDIS_PASSWORD"),
		DialTimeout:  2 * time.Second,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
	}

	logger, _ := clog.New(&clog.Config{
		Level:       "info",
		Format:      "console",
		Output:      "stdout",
		AddSource:   false,
		EnableColor: false,
	})

	redisConn, err := connector.NewRedis(redisCfg, connector.WithLogger(logger))
	if err != nil {
		fmt.Printf("跳过 Redis 示例: 无法创建 redis connector: %v\n", err)
		fmt.Println()
		return
	}
	defer redisConn.Close()

	// 2. 建立连接
	ctx := context.Background()
	if err := redisConn.Connect(ctx); err != nil {
		fmt.Printf("跳过 Redis 示例: 无法连接: %v\n", err)
		fmt.Println()
		return
	}

	// 3. 创建 Allocator 并分配 WorkerID
	allocator, err := idgen.NewAllocator(&idgen.AllocatorConfig{
		Driver:    "redis",
		KeyPrefix: "myapp:idgen",
		MaxID:     1024,
		TTL:       30,
	}, idgen.WithRedisConnector(redisConn))
	if err != nil {
		log.Printf("Failed to create allocator: %v\n", err)
		return
	}

	workerID, err := allocator.Allocate(ctx)
	if err != nil {
		log.Printf("Failed to allocate WorkerID: %v\n", err)
		return
	}
	defer allocator.Stop()

	// 监听保活失败
	go func() {
		if err := <-allocator.KeepAlive(ctx); err != nil {
			log.Printf("WorkerID 租约保活失败: %v\n", err)
		}
	}()

	// 4. 使用分配的 WorkerID 创建 Snowflake
	sf, err := idgen.NewGenerator(&idgen.GeneratorConfig{
		WorkerID: workerID,
	})
	if err != nil {
		log.Printf("Failed to create Snowflake: %v\n", err)
		return
	}

	fmt.Printf("自动分配的 WorkerID: %d\n", workerID)

	fmt.Println("生成 5 个 Snowflake ID:")
	for i := 0; i < 5; i++ {
		id := sf.Next()
		fmt.Printf("  ID %d: %d\n", i+1, id)
		time.Sleep(time.Millisecond)
	}
	fmt.Println()
}

// ========================================
// UUID 示例
// ========================================

func uuidExample() {
	fmt.Println("--- Example 3: UUID (简化 API) ---")

	// 直接调用 UUID() 生成 v7 (时间排序)
	fmt.Println("UUID v7 (时间排序，适合数据库主键):")
	for i := 0; i < 3; i++ {
		fmt.Printf("  UUID #%d: %s\n", i+1, idgen.UUID())
		time.Sleep(5 * time.Millisecond)
	}

	fmt.Println()
}

// ========================================
// Sequence 示例
// ========================================

func sequenceExample() {
	fmt.Println("--- Example 4: Sequence (基于 Redis) ---")
	fmt.Println("Note: 此示例需要 Redis 运行在 localhost:6379")

	// 1. 创建 Redis 连接器
	redisCfg := &connector.RedisConfig{
		Name:         "idgen-sequence-example",
		Addr:         "127.0.0.1:6379",
		Password:     os.Getenv("GENESIS_REDIS_PASSWORD"),
		DialTimeout:  2 * time.Second,
		ReadTimeout:  2 * time.Second,
		WriteTimeout:  2 * time.Second,
	}

	logger, _ := clog.New(&clog.Config{
		Level:       "info",
		Format:      "console",
		Output:      "stdout",
		AddSource:   false,
		EnableColor: false,
	})

	redisConn, err := connector.NewRedis(redisCfg, connector.WithLogger(logger))
	if err != nil {
		fmt.Printf("跳过 Sequence 示例: 无法创建 redis connector: %v\n", err)
		fmt.Println()
		return
	}
	defer redisConn.Close()

	// 2. 建立连接
	ctx := context.Background()
	if err := redisConn.Connect(ctx); err != nil {
		fmt.Printf("跳过 Sequence 示例: 无法连接: %v\n", err)
		fmt.Println()
		return
	}

	// 3. IM 消息序列号场景
	fmt.Println("IM 消息序列号场景:")
	imCfg := &idgen.SequencerConfig{
		Driver:    "redis",
		KeyPrefix: "im:msg_seq",
		Step:      1,
		TTL:       3600, // 1 hour (秒)
	}

	imGen, err := idgen.NewSequencer(imCfg, idgen.WithRedisConnector(redisConn))
	if err != nil {
		log.Printf("Failed to create IM sequence generator: %v\n", err)
		return
	}

	users := []string{"alice", "bob", "charlie"}
	for _, user := range users {
		fmt.Printf("\n用户 %s 的消息序列号:\n", user)
		for i := 0; i < 3; i++ {
			seq, err := imGen.Next(ctx, user)
			if err != nil {
				log.Printf("Failed to generate message sequence for %s: %v\n", user, err)
				continue
			}
			fmt.Printf("  消息 %d: %d\n", i+1, seq)
		}
	}

	// 4. 批量生成
	fmt.Println("\n批量生成序列号:")
	batchSeqs, err := imGen.NextBatch(ctx, "alice", 5)
	if err != nil {
		log.Printf("Failed to generate batch sequence: %v\n", err)
	} else {
		fmt.Printf("为 Alice 批量生成 5 条消息序列号: %v\n", batchSeqs)
	}

	// 5. 业务流水号场景 (步长 1000)
	fmt.Println("\n业务流水号场景 (步长 1000):")
	businessCfg := &idgen.SequencerConfig{
		Driver:    "redis",
		KeyPrefix: "business:seq",
		Step:      1000,
		MaxValue:  9999,
		TTL:       86400, // 24 hours (秒)
	}

	businessGen, err := idgen.NewSequencer(businessCfg, idgen.WithRedisConnector(redisConn))
	if err != nil {
		log.Printf("Failed to create business sequence generator: %v\n", err)
		return
	}

	businesses := []string{"order", "payment", "refund"}
	for _, business := range businesses {
		seq, err := businessGen.Next(ctx, business)
		if err != nil {
			log.Printf("Failed to generate business sequence for %s: %v\n", business, err)
			continue
		}
		fmt.Printf("  业务流水号 - %s: %d\n", business, seq)
	}

	fmt.Println("\n=== 序列号生成器测试完成 ===")
}
