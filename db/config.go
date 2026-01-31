package db

import "github.com/ceyewan/genesis/xerrors"

// Config DB 组件配置
type Config struct {
	Driver         string         `json:"driver" yaml:"driver"`
	EnableSharding bool           `json:"enable_sharding" yaml:"enable_sharding"`
	ShardingRules  []ShardingRule `json:"sharding_rules" yaml:"sharding_rules"`
}

// ShardingRule 分片规则
type ShardingRule struct {
	ShardingKey    string   `json:"sharding_key" yaml:"sharding_key"`
	NumberOfShards uint     `json:"number_of_shards" yaml:"number_of_shards"`
	Tables         []string `json:"tables" yaml:"tables"`
}

func (c *Config) setDefaults() {
	if c.Driver == "" {
		c.Driver = "mysql"
	}
}

func (c *Config) validate() error {
	if c.Driver != "mysql" && c.Driver != "postgresql" && c.Driver != "sqlite" {
		return xerrors.Wrapf(ErrInvalidConfig, "unsupported driver: %s", c.Driver)
	}

	if c.EnableSharding && len(c.ShardingRules) == 0 {
		return xerrors.Wrap(ErrInvalidConfig, "sharding enabled but no rules provided")
	}

	for _, rule := range c.ShardingRules {
		if rule.ShardingKey == "" {
			return xerrors.Wrap(ErrInvalidConfig, "sharding key cannot be empty")
		}
		if rule.NumberOfShards == 0 {
			return xerrors.Wrap(ErrInvalidConfig, "number of shards must be greater than 0")
		}
		if len(rule.Tables) == 0 {
			return xerrors.Wrap(ErrInvalidConfig, "sharding tables cannot be empty")
		}
		for _, table := range rule.Tables {
			if table == "" {
				return xerrors.Wrap(ErrInvalidConfig, "sharding table name cannot be empty")
			}
		}
	}

	return nil
}
