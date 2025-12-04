package main

import (
	"fmt"
	"log"
	"time"

	"github.com/ceyewan/genesis/pkg/connector"
	"github.com/ceyewan/genesis/pkg/idgen"
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

	// 示例 4: Snowflake (Etcd Mode) - 需要运行 Etcd
	snowflakeEtcdExample()

	// 示例 5: UUID (v4 & v7)
	uuidExample()
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

	id, _ := gen.Int64()
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

	id, _ := gen.Int64()
	fmt.Printf("Generated ID (IP): %d\n", id)
	fmt.Println()
}

func snowflakeRedisExample() {
	fmt.Println("--- Example 3: Snowflake (Redis Mode) ---")
	fmt.Println("Note: This example requires Redis running on localhost:6379")

	// 1. 直接创建 Redis 连接器 (Go Native DI 模式)
	redisCfg := &connector.RedisConfig{
		Addr:        "127.0.0.1:6379",
		DialTimeout: 2 * time.Second, // 快速失败
	}

	redisConn, err := connector.NewRedis(redisCfg)
	if err != nil {
		fmt.Printf("Skipping Redis example: failed to create redis connector: %v\n", err)
		return
	}
	defer redisConn.Close()

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
		id, _ := gen.Int64()
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
		id, _ := gen.Int64()
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
	fmt.Printf("UUID v4 (Random):      %s\n", v4Gen.String())

	// UUID v7 (Time-ordered)
	v7Cfg := &idgen.UUIDConfig{Version: "v7"}
	v7Gen, _ := idgen.NewUUID(v7Cfg)
	fmt.Printf("UUID v7 (Time-ordered): %s\n", v7Gen.String())

	time.Sleep(10 * time.Millisecond)
	fmt.Printf("UUID v7 (Next):         %s\n", v7Gen.String())
	fmt.Println()
}
