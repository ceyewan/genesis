package main

import (
	"fmt"
	"log"
	"time"

	"github.com/ceyewan/genesis/pkg/connector"
	"github.com/ceyewan/genesis/pkg/container"
	"github.com/ceyewan/genesis/pkg/idgen"
	"github.com/ceyewan/genesis/pkg/idgen/types"
)

func main() {
	fmt.Println("=== Genesis ID Generator Examples ===")
	fmt.Println()

	// 示例 1: Snowflake (Static Mode)
	snowflakeStaticExample()

	// 示例 2: Snowflake (IP Mode)
	snowflakeIPExample()

	// 示例 3: Snowflake (Redis Mode)
	snowflakeRedisExample()

	// 示例 4: Snowflake (Etcd Mode)
	snowflakeEtcdExample()

	// 示例 5: UUID (v4 & v7)
	uuidExample()
}

func snowflakeStaticExample() {
	fmt.Println("--- Example 1: Snowflake (Static Mode) ---")

	cfg := &types.SnowflakeConfig{
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

	id, _ := gen.Int64()
	fmt.Printf("Generated ID (Static): %d\n", id)
	fmt.Println()
}

func snowflakeIPExample() {
	fmt.Println("--- Example 2: Snowflake (IP Mode) ---")

	cfg := &types.SnowflakeConfig{
		Method:       "ip_24", // 使用 IP 后 8 位作为 WorkerID
		DatacenterID: 1,
	}

	// IP 模式不需要连接器
	gen, err := idgen.NewSnowflake(cfg, nil, nil)
	if err != nil {
		log.Printf("Failed to create IP snowflake: %v\n", err)
		return
	}

	id, _ := gen.Int64()
	fmt.Printf("Generated ID (IP): %d\n", id)
	fmt.Println()
}

func snowflakeRedisExample() {
	fmt.Println("--- Example 3: Snowflake (Redis Mode) ---")

	// 1. 使用 Container 初始化 Redis 连接器
	// 这是获取连接器的标准方式，避免直接依赖 internal 包
	containerCfg := &container.Config{
		Redis: &connector.RedisConfig{
			Addr:        "127.0.0.1:6379",
			Password:    "your_redis_password",
			DialTimeout: 2 * time.Second, // 快速失败
		},
	}

	c, err := container.New(containerCfg)
	if err != nil {
		fmt.Printf("Skipping Redis example: container init failed: %v\n", err)
		return
	}
	defer c.Close()

	redisConn, err := c.GetRedisConnector(*containerCfg.Redis)
	if err != nil {
		fmt.Printf("Skipping Redis example: get connector failed: %v\n", err)
		return
	}

	// 2. 配置 Snowflake
	snowflakeCfg := &types.SnowflakeConfig{
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
		id, _ := gen.Int64()
		fmt.Printf("  ID %d: %d\n", i+1, id)
		time.Sleep(time.Millisecond)
	}
	fmt.Println()
}

func snowflakeEtcdExample() {
	fmt.Println("--- Example 4: Snowflake (Etcd Mode) ---")

	// 1. 使用 Container 初始化 Etcd 连接器
	containerCfg := &container.Config{
		Etcd: &connector.EtcdConfig{
			Endpoints: []string{"127.0.0.1:2379"},
			Timeout:   2 * time.Second,
		},
	}

	c, err := container.New(containerCfg)
	if err != nil {
		fmt.Printf("Skipping Etcd example: container init failed: %v\n", err)
		return
	}
	defer c.Close()

	etcdConn, err := c.GetEtcdConnector(*containerCfg.Etcd)
	if err != nil {
		fmt.Printf("Skipping Etcd example: get connector failed: %v\n", err)
		return
	}

	// 2. 配置 Snowflake
	snowflakeCfg := &types.SnowflakeConfig{
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
		id, _ := gen.Int64()
		fmt.Printf("  ID %d: %d\n", i+1, id)
		time.Sleep(time.Millisecond)
	}
	fmt.Println()
}

func uuidExample() {
	fmt.Println("--- Example 5: UUID (v4 & v7) ---")

	// UUID v4 (Random)
	v4Cfg := &types.UUIDConfig{Version: "v4"}
	v4Gen, _ := idgen.NewUUID(v4Cfg)
	fmt.Printf("UUID v4 (Random):      %s\n", v4Gen.String())

	// UUID v7 (Time-ordered)
	v7Cfg := &types.UUIDConfig{Version: "v7"}
	v7Gen, _ := idgen.NewUUID(v7Cfg)
	fmt.Printf("UUID v7 (Time-ordered): %s\n", v7Gen.String())

	time.Sleep(10 * time.Millisecond)
	fmt.Printf("UUID v7 (Next):         %s\n", v7Gen.String())
	fmt.Println()
}
