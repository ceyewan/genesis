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

	// 示例 1: Snowflake (Static Mode)
	snowflakeStaticExample()

	// 示例 2: Snowflake (IP Mode)
	snowflakeIPExample()

	// 示例 3: Snowflake (Redis Mode) - 需要运行 Redis
	snowflakeRedisExample()

	// 示例 4: Snowflake (Etcd Mode) - 需要运行 Etcd（暂时跳过）
	// snowflakeEtcdExample()

	// 示例 5: UUID (v4 & v7)
	uuidExample()

	// 示例 6: Sequence (基于 Redis INCR)
	sequenceExample()
}

func snowflakeStaticExample() {
	fmt.Println("--- Example 1: Snowflake (Static Mode) ---")

	cfg := &idgen.SnowflakeConfig{
		Method:       "static",
		WorkerID:     1, // 手动指定 WorkerID
		DatacenterID: 1,
	}

	// Static 模式不需要连接器
	gen, err := idgen.NewSnowflake(cfg, nil, nil) // 使用默认 Logger
	if err != nil {
		log.Printf("Failed to create static snowflake: %v\n", err)
		return
	}

	id, _ := gen.NextInt64()
	fmt.Printf("Generated ID (Static): %d\n", id)
	fmt.Println()
}

func snowflakeIPExample() {
	fmt.Println("--- Example 2: Snowflake (IP Mode) ---")

	cfg := &idgen.SnowflakeConfig{
		Method:       "ip_24", // 使用 IP 后 8 位作为 WorkerID
		DatacenterID: 1,
	}

	// IP 模式不需要连接器
	gen, err := idgen.NewSnowflake(cfg, nil, nil)
	if err != nil {
		log.Printf("Failed to create IP snowflake: %v\n", err)
		return
	}

	id, _ := gen.NextInt64()
	fmt.Printf("Generated ID (IP): %d\n", id)
	fmt.Println()
}

func snowflakeRedisExample() {
	fmt.Println("--- Example 3: Snowflake (Redis Mode) ---")
	fmt.Println("Note: This example requires Redis running on localhost:6379")

	// 1. 直接创建 Redis 连接器 (Go Native DI 模式)
	redisCfg := &connector.RedisConfig{
		Name:         "idgen-redis-example",
		Addr:         "127.0.0.1:6379",
		Password:     os.Getenv("GENESIS_REDIS_PASSWORD"), // 从环境变量读取密码
		DialTimeout:  2 * time.Second,                     // 快速失败
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
	}

	// 创建默认 logger
	logger, _ := clog.New(&clog.Config{
		Level:       "info",
		Format:      "console",
		Output:      "stdout",
		AddSource:   false,
		EnableColor: false,
	})

	redisConn, err := connector.NewRedis(redisCfg, connector.WithLogger(logger))
	if err != nil {
		fmt.Printf("Skipping Redis example: failed to create redis connector: %v\n", err)
		return
	}
	defer redisConn.Close()

	// 2. 建立连接
	ctx := context.Background()
	if err := redisConn.Connect(ctx); err != nil {
		fmt.Printf("Skipping Redis example: failed to connect: %v\n", err)
		return
	}

	// 2. 配置 Snowflake
	snowflakeCfg := &idgen.SnowflakeConfig{
		Method:       "redis",
		DatacenterID: 1,
		KeyPrefix:    "genesis:examples:idgen",
		TTL:          10, // 短 TTL 方便测试
	}

	// 3. 创建生成器
	gen, err := idgen.NewSnowflake(snowflakeCfg, redisConn, nil)
	if err != nil {
		log.Printf("Failed to create Redis snowflake: %v\n", err)
		return
	}

	// 4. 生成 ID
	fmt.Println("Generating 5 IDs:")
	for i := 0; i < 5; i++ {
		id, _ := gen.NextInt64()
		fmt.Printf("  ID %d: %d\n", i+1, id)
		time.Sleep(time.Millisecond)
	}
	fmt.Println()
}

func snowflakeEtcdExample() {
	fmt.Println("--- Example 4: Snowflake (Etcd Mode) ---")
	fmt.Println("Note: This example requires Etcd running on localhost:2379")

	// 1. 直接创建 Etcd 连接器 (Go Native DI 模式)
	etcdCfg := &connector.EtcdConfig{
		Endpoints: []string{"127.0.0.1:2379"},
		Timeout:   2 * time.Second,
	}

	etcdConn, err := connector.NewEtcd(etcdCfg)
	if err != nil {
		fmt.Printf("Skipping Etcd example: failed to create etcd connector: %v\n", err)
		return
	}
	defer etcdConn.Close()

	// 2. 配置 Snowflake
	snowflakeCfg := &idgen.SnowflakeConfig{
		Method:       "etcd",
		DatacenterID: 1,
		KeyPrefix:    "genesis:examples:idgen",
		TTL:          10,
	}

	// 3. 创建生成器
	gen, err := idgen.NewSnowflake(snowflakeCfg, nil, etcdConn)
	if err != nil {
		log.Printf("Failed to create Etcd snowflake: %v\n", err)
		return
	}

	// 4. 生成 ID
	fmt.Println("Generating 5 IDs:")
	for i := 0; i < 5; i++ {
		id, _ := gen.NextInt64()
		fmt.Printf("  ID %d: %d\n", i+1, id)
		time.Sleep(time.Millisecond)
	}
	fmt.Println()
}

func uuidExample() {
	fmt.Println("--- Example 5: UUID (v4 & v7) ---")

	// UUID v4 (Random)
	v4Cfg := &idgen.UUIDConfig{Version: "v4"}
	v4Gen, _ := idgen.NewUUID(v4Cfg)
	fmt.Printf("UUID v4 (Random):      %s\n", v4Gen.Next())

	// UUID v7 (Time-ordered)
	v7Cfg := &idgen.UUIDConfig{Version: "v7"}
	v7Gen, _ := idgen.NewUUID(v7Cfg)
	fmt.Printf("UUID v7 (Time-ordered): %s\n", v7Gen.Next())

	time.Sleep(10 * time.Millisecond)
	fmt.Printf("UUID v7 (Next):         %s\n", v7Gen.Next())
	fmt.Println()
}

func sequenceExample() {
	fmt.Println("--- Example 6: Sequence (基于 Redis INCR) ---")
	fmt.Println("Note: This example requires Redis running on localhost:6379")

	// 1. 创建 Redis 连接器
	redisCfg := &connector.RedisConfig{
		Name:         "idgen-sequence-example",
		Addr:         "127.0.0.1:6379",
		Password:     os.Getenv("GENESIS_REDIS_PASSWORD"), // 从环境变量读取密码
		DialTimeout:  2 * time.Second,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
	}

	// 创建默认 logger
	logger, _ := clog.New(&clog.Config{
		Level:       "info",
		Format:      "console",
		Output:      "stdout",
		AddSource:   false,
		EnableColor: false,
	})

	redisConn, err := connector.NewRedis(redisCfg, connector.WithLogger(logger))
	if err != nil {
		fmt.Printf("Skipping Sequence example: failed to create redis connector: %v\n", err)
		return
	}
	defer redisConn.Close()

	// 2. 建立连接
	ctx := context.Background()
	if err := redisConn.Connect(ctx); err != nil {
		fmt.Printf("Skipping Sequence example: failed to connect: %v\n", err)
		return
	}

	// 3. 基本序列号生成（IM 消息序列号场景）
	fmt.Println("\n=== IM 消息序列号场景 ===")
	imCfg := &idgen.SequenceConfig{
		KeyPrefix: "im:msg_seq",
		Step:      1,
		TTL:       int64(time.Hour), // 1小时过期
	}

	imGen, err := idgen.NewSequencer(imCfg, redisConn)
	if err != nil {
		log.Printf("Failed to create IM sequence generator: %v\n", err)
		return
	}

	fmt.Println("为不同用户生成消息序列号:")
	users := []string{"alice", "bob", "charlie"}
	for _, user := range users {
		// 为每个用户生成 3 条消息序列号
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

	// 4. 批量生成序列号
	fmt.Println("\n=== 批量生成序列号 ===")
	fmt.Println("为 Alice 批量生成 5 条消息序列号:")
	batchSeqs, err := imGen.NextBatch(ctx, "alice", 5)
	if err != nil {
		log.Printf("Failed to generate batch sequence: %v\n", err)
	} else {
		fmt.Printf("批量结果: %v\n", batchSeqs)
	}

	// 5. 业务流水号场景
	fmt.Println("\n=== 业务流水号场景 ===")
	businessCfg := &idgen.SequenceConfig{
		KeyPrefix: "business:seq",
		Step:      1000,                  // 步长 1000
		MaxValue:  9999,                  // 最大值限制
		TTL:       int64(24 * time.Hour), // 1天过期
	}

	businessGen, err := idgen.NewSequencer(businessCfg, redisConn)
	if err != nil {
		log.Printf("Failed to create business sequence generator: %v\n", err)
		return
	}

	// 为不同业务生成流水号
	businesses := []string{"order", "payment", "refund"}
	for _, business := range businesses {
		seq, err := businessGen.Next(ctx, business)
		if err != nil {
			log.Printf("Failed to generate business sequence for %s: %v\n", business, err)
			continue
		}
		fmt.Printf("业务流水号 - %s: %d\n", business, seq)
	}

	fmt.Println("\n=== 序列号生成器测试完成 ===")
}
