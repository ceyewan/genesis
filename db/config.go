package db

import "github.com/ceyewan/genesis/xerrors"

// Config DB 组件配置
type Config struct {
	// Driver 指定数据库驱动类型: "mysql" 或 "sqlite"
	// 默认值: "mysql"
	Driver string `json:"driver" yaml:"driver"`

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

// setDefaults 设置配置的默认值（内部使用）
func (c *Config) setDefaults() {
	if c.Driver == "" {
		c.Driver = "mysql"
	}
}

// validate 验证配置的有效性（内部使用）
func (c *Config) validate() error {
	// 验证 Driver
	if c.Driver != "mysql" && c.Driver != "sqlite" {
		return xerrors.Wrapf(xerrors.ErrInvalidInput, "unsupported driver: %s (must be 'mysql' or 'sqlite')", c.Driver)
	}

	// 如果启用分片，必须有分片规则
	if c.EnableSharding && len(c.ShardingRules) == 0 {
		return xerrors.Wrap(xerrors.ErrInvalidInput, "sharding enabled but no rules provided")
	}

	// 验证每个分片规则
	for _, rule := range c.ShardingRules {
		if rule.ShardingKey == "" {
			return xerrors.Wrap(xerrors.ErrInvalidInput, "sharding key cannot be empty")
		}
		if rule.NumberOfShards == 0 {
			return xerrors.Wrap(xerrors.ErrInvalidInput, "number of shards must be greater than 0")
		}
		if len(rule.Tables) == 0 {
			return xerrors.Wrap(xerrors.ErrInvalidInput, "sharding tables cannot be empty")
		}
		for _, table := range rule.Tables {
			if table == "" {
				return xerrors.Wrap(xerrors.ErrInvalidInput, "sharding table name cannot be empty")
			}
		}
	}

	return nil
}
