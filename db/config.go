package db

import "github.com/ceyewan/genesis/xerrors"

// Config DB 组件配置
type Config struct {
	Driver string `json:"driver" yaml:"driver" mapstructure:"driver"`
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

	return nil
}
