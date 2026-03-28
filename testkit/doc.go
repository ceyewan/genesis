// Package testkit 提供 Genesis 组件测试所需的通用 helper。
//
// 这个包只服务于测试代码，核心目标有三点：
//
//  1. 统一测试中的 logger、meter、context 和随机 ID 生成方式。
//  2. 通过 testcontainers 为 Redis、MySQL、PostgreSQL、Etcd、NATS、Kafka
//     等外部依赖提供一次性容器与已连接 client/connector。
//  3. 让组件集成测试尽量贴近真实依赖，同时避免开发者在运行测试前手动执行
//     make up 或维持一整套长期驻留的本地环境。
//
// 典型用法是直接在测试中调用：
//
//	redisClient := testkit.NewRedisContainerClient(t)
//	db := testkit.NewMySQLDB(t)
//	ctx, cancel := testkit.NewContext(t, 5*time.Second)
//	defer cancel()
//
// 所有公开 helper 都以 *testing.T 为生命周期锚点，通过 t.Cleanup 自动回收
// 容器、连接器和临时资源。
package testkit
