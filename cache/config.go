package cache

// Config 缓存组件统一配置
type Config struct {
	// Mode 缓存模式: "standalone" | "distributed" (默认 "distributed")
	Mode string `json:"mode" yaml:"mode"`

	// Prefix: 全局 Key 前缀 (e.g., "app:v1:")
	Prefix string `json:"prefix" yaml:"prefix"`

	// RedisConnectorName: 使用的 Redis 连接器名称 (e.g., "default")
	RedisConnectorName string `json:"redis_connector_name" yaml:"redis_connector_name"`

	// Serializer: "json" | "msgpack"
	Serializer string `json:"serializer" yaml:"serializer"`

	// Standalone 单机缓存配置
	Standalone *StandaloneConfig `json:"standalone" yaml:"standalone"`
}

// StandaloneConfig 单机缓存配置
type StandaloneConfig struct {
	// Capacity 缓存最大容量（条目数，默认：10000）
	Capacity int `json:"capacity" yaml:"capacity"`
}
