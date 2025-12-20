package cache

// Config 缓存组件配置
type Config struct {
	// Prefix: 全局 Key 前缀 (e.g., "app:v1:")
	Prefix string `json:"prefix" yaml:"prefix"`

	// RedisConnectorName: 使用的 Redis 连接器名称 (e.g., "default")
	RedisConnectorName string `json:"redis_connector_name" yaml:"redis_connector_name"`

	// Serializer: "json" | "msgpack"
	Serializer string `json:"serializer" yaml:"serializer"`
}
