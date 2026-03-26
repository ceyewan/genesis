package config

import "strings"

// Config 定义 Loader 的加载参数。
//
// 它只描述“去哪里找配置，以及如何把 key 映射到环境变量”，不承载业务配置本身。
type Config struct {
	Name      string   // 配置文件名称，不含扩展名；默认 "config"
	Paths     []string // 配置文件搜索路径；默认 [".", "./config"]
	FileType  string   // 配置文件类型，如 yaml、json；默认 "yaml"
	EnvPrefix string   // 环境变量前缀；默认 "GENESIS"
}

// validate 设置默认值并验证配置
// nolint:unparam
func (c *Config) validate() error {
	// 设置默认值
	if c.Name == "" {
		c.Name = "config"
	}
	if c.Paths == nil {
		c.Paths = []string{".", "./config"}
	}
	if c.FileType == "" {
		c.FileType = "yaml"
	}
	if c.EnvPrefix == "" {
		c.EnvPrefix = "GENESIS"
	}
	c.EnvPrefix = strings.ToUpper(c.EnvPrefix)
	return nil
}

// New 创建配置加载器。
//
// 如果 cfg 为 nil，使用默认配置。
// New 会复制一份 Config，调用方后续不应依赖修改原始 cfg 来影响已创建的 Loader。
func New(cfg *Config, opts ...Option) (Loader, error) {
	if cfg == nil {
		cfg = &Config{}
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	cfgCopy := *cfg
	if cfg.Paths != nil {
		cfgCopy.Paths = append([]string(nil), cfg.Paths...)
	}

	return newLoader(&cfgCopy, opts...)
}
