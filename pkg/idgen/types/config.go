package types

// Config 是 ID 生成器的通用配置
type Config struct {
	// Mode 指定生成器模式: "snowflake" | "uuid"
	Mode string `yaml:"mode" json:"mode"`

	// Snowflake 雪花算法配置 (Mode="snowflake" 时必填)
	Snowflake *SnowflakeConfig `yaml:"snowflake" json:"snowflake"`

	// UUID UUID 配置 (Mode="uuid" 时必填)
	UUID *UUIDConfig `yaml:"uuid" json:"uuid"`
}

// SnowflakeConfig 雪花算法配置
type SnowflakeConfig struct {
	// Method 指定 WorkerID 的获取方式
	// 可选: "static" | "ip_24" | "redis" | "etcd"
	Method string `yaml:"method" json:"method"`

	// WorkerID 当 Method="static" 时手动指定
	WorkerID int64 `yaml:"worker_id" json:"worker_id"`

	// DatacenterID 数据中心 ID (可选，默认 0)
	DatacenterID int64 `yaml:"datacenter_id" json:"datacenter_id"`

	// KeyPrefix Redis/Etcd 键前缀 (可选，默认 "genesis:idgen:worker")
	KeyPrefix string `yaml:"key_prefix" json:"key_prefix"`

	// TTL 租约 TTL 秒数 (可选，默认 30)
	TTL int `yaml:"ttl" json:"ttl"`

	// MaxDriftMs 允许的最大时钟回拨毫秒数 (可选，默认 5ms)
	MaxDriftMs int64 `yaml:"max_drift_ms" json:"max_drift_ms"`

	// MaxWaitMs 时钟回拨时最大等待毫秒数 (可选，默认 1000ms)
	// 超过此值则直接熔断
	MaxWaitMs int64 `yaml:"max_wait_ms" json:"max_wait_ms"`
}

// UUIDConfig UUID 配置
type UUIDConfig struct {
	// Version UUID 版本 (可选，默认 "v4")
	// 支持: "v4" | "v7"
	Version string `yaml:"version" json:"version"`
}
