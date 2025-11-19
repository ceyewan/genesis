package db

type Config struct {
	// 是否开启分片特性
	EnableSharding bool `json:"enable_sharding" yaml:"enable_sharding"`

	// 分片规则配置列表
	// 允许为不同的表组配置不同的分片规则
	ShardingRules []ShardingRule `json:"sharding_rules" yaml:"sharding_rules"`
}

type ShardingRule struct {
	// 分片键 (例如 "user_id")
	ShardingKey string `json:"sharding_key" yaml:"sharding_key"`

	// 分片数量 (例如 64)
	NumberOfShards uint `json:"number_of_shards" yaml:"number_of_shards"`

	// 应用此规则的逻辑表名列表 (例如 ["orders", "audit_logs"])
	Tables []string `json:"tables" yaml:"tables"`
}
