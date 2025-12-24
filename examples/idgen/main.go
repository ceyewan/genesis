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

	// 示例 1: Snowflake (静态模式)
	snowflakeStaticExample()

	// 示例 2: Snowflake (实例模式)
	snowflakeInstanceExample()

	// 示例 3: Snowflake (Redis 自动分配 WorkerID)
	snowflakeRedisExample()

	// 示例 4: UUID (v4 & v7)
	uuidExample()

	// 示例 5: Sequence (基于 Redis INCR)
	sequenceExample()
}

// ========================================
// Snowflake 示例
// ========================================

func snowflakeStaticExample() {
	fmt.Println("--- Example 1: Snowflake (静态模式) ---")

	// 使用全局静态 API (最简单)
	idgen.Setup(1) // 设置 workerID = 1

	fmt.Println("生成 5 个 Snowflake ID:")
	for i := 0; i < 5; i++ {
		id := idgen.Next()
		fmt.Printf("  ID %d: %d\n", i+1, id)
		time.Sleep(time.Millisecond)
	}
	fmt.Println()
}

func snowflakeInstanceExample() {
	fmt.Println("--- Example 2: Snowflake (实例模式) ---")

	// 创建独立的 Snowflake 实例
	sf, err := idgen.NewSnowflake(23,
		idgen.WithDatacenterID(1), // 可选：设置数据中心 ID
	)
	if err != nil {
		log.Printf("Failed to create Snowflake: %v\n", err)
		return
	}

	fmt.Println("生成 5 个 Snowflake ID (实例模式):")
	for i := 0; i < 5; i++ {
		id := sf.Next()
		fmt.Printf("  ID %d: %d\n", i+1, id)
		time.Sleep(time.Millisecond)
	}
	fmt.Println()
}

func snowflakeRedisExample() {
	fmt.Println("--- Example 3: Snowflake (Redis 自动分配 WorkerID) ---")
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

	// 3. 使用 Redis 自动分配唯一 WorkerID
	workerID, stop, failCh, err := idgen.AssignInstanceID(ctx, redisConn, "myapp:idgen", 1024)
	if err != nil {
		log.Printf("Failed to assign worker ID: %v\n", err)
		return
	}
	defer stop()

	// 监听保活失败
	go func() {
		if err := <-failCh; err != nil {
			log.Printf("WorkerID 租约保活失败: %v\n", err)
		}
	}()

	fmt.Printf("自动分配的 WorkerID: %d\n", workerID)

	// 4. 使用分配的 WorkerID 初始化
	idgen.Setup(workerID, idgen.WithDatacenterID(1))

	fmt.Println("生成 5 个 Snowflake ID:")
	for i := 0; i < 5; i++ {
		id := idgen.Next()
		fmt.Printf("  ID %d: %d\n", i+1, id)
		time.Sleep(time.Millisecond)
	}
	fmt.Println()
}

// ========================================
// UUID 示例
// ========================================

func uuidExample() {
	fmt.Println("--- Example 4: UUID (v4 & v7) ---")

	// 静态方法 (最常用)
	fmt.Println("静态方法 (默认 v7):")
	for i := 0; i < 3; i++ {
		uid := idgen.NextUUID()
		fmt.Printf("  UUID v7 #%d: %s\n", i+1, uid)
		time.Sleep(5 * time.Millisecond)
	}

	// v4 (随机)
	fmt.Println("\nUUID v4 (随机):")
	v4 := idgen.NewUUIDV4()
	fmt.Printf("  UUID v4: %s\n", v4)

	// 实例模式
	fmt.Println("\n实例模式:")
	gen := idgen.NewUUID()
	fmt.Printf("  UUID (默认 v7): %s\n", gen.Next())

	genV4 := idgen.NewUUID(idgen.WithUUIDVersion("v4"))
	fmt.Printf("  UUID (v4): %s\n", genV4.Next())

	fmt.Println()
}

// ========================================
// Sequence 示例
// ========================================

func sequenceExample() {
	fmt.Println("--- Example 5: Sequence (基于 Redis INCR) ---")
	fmt.Println("Note: 此示例需要 Redis 运行在 localhost:6379")

	// 1. 创建 Redis 连接器
	redisCfg := &connector.RedisConfig{
		Name:         "idgen-sequence-example",
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

	// 3. 静态便捷方法 (最简单)
	fmt.Println("静态便捷方法 (简单计数):")
	for i := 0; i < 5; i++ {
		seq, _ := idgen.NextSequence(ctx, redisConn, "simple:counter")
		fmt.Printf("  计数: %d\n", seq)
	}

	// 4. 实例模式 (IM 消息序列号场景)
	fmt.Println("\n实例模式 (IM 消息序列号):")
	imCfg := &idgen.SequenceConfig{
		KeyPrefix: "im:msg_seq",
		Step:      1,
		TTL:       int64(time.Hour),
	}

	imGen, err := idgen.NewSequencer(imCfg, redisConn)
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

	// 5. 批量生成
	fmt.Println("\n批量生成序列号:")
	batchSeqs, err := imGen.NextBatch(ctx, "alice", 5)
	if err != nil {
		log.Printf("Failed to generate batch sequence: %v\n", err)
	} else {
		fmt.Printf("为 Alice 批量生成 5 条消息序列号: %v\n", batchSeqs)
	}

	// 6. 业务流水号场景 (步长 1000)
	fmt.Println("\n业务流水号场景 (步长 1000):")
	businessCfg := &idgen.SequenceConfig{
		KeyPrefix: "business:seq",
		Step:      1000,
		MaxValue:  9999,
		TTL:       int64(24 * time.Hour),
	}

	businessGen, err := idgen.NewSequencer(businessCfg, redisConn)
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
