package db

import "errors"

// Config DB 组件配置
type Config struct {
	// 是否开启分片特性
	EnableSharding bool `json:"enable_sharding" yaml:"enable_sharding"`

	// 分片规则配置列表
	// 允许为不同的表组配置不同的分片规则
	ShardingRules []ShardingRule `json:"sharding_rules" yaml:"sharding_rules"`
}

// ShardingRule 分片规则
type ShardingRule struct {
	// 分片键 (例如 "user_id")
	ShardingKey string `json:"sharding_key" yaml:"sharding_key"`

	// 分片数量 (例如 64)
	NumberOfShards uint `json:"number_of_shards" yaml:"number_of_shards"`

	// 应用此规则的逻辑表名列表 (例如 ["orders", "audit_logs"])
	Tables []string `json:"tables" yaml:"tables"`
}

// SetDefaults 设置配置的默认值
func (c *Config) SetDefaults() {
	// DB 组件目前没有需要设置默认值的配置项
	// 保持此方法以符合 Genesis 组件规范
}

// Validate 验证配置的有效性
func (c *Config) Validate() error {
	// 如果启用分片，必须有分片规则
	if c.EnableSharding && len(c.ShardingRules) == 0 {
		return errors.New("db: sharding is enabled but no rules provided")
	}

	// 验证每个分片规则
	for _, rule := range c.ShardingRules {
		if rule.ShardingKey == "" {
			return errors.New("db: sharding rule: sharding key cannot be empty")
		}
		if rule.NumberOfShards == 0 {
			return errors.New("db: sharding rule: number of shards must be greater than 0")
		}
		if len(rule.Tables) == 0 {
			return errors.New("db: sharding rule: tables cannot be empty")
		}

		// 验证表名不为空
		for _, table := range rule.Tables {
			if table == "" {
				return errors.New("db: sharding rule: table name cannot be empty")
			}
		}
	}

	return nil
}
