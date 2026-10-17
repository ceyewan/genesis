package config

import "strings"

// Config 定义 Loader 的加载参数。
//
// 它只描述“去哪里找配置，以及如何把 key 映射到环境变量”，不承载业务配置本身。
type Config struct {
	Name      string   `json:"name" yaml:"name" mapstructure:"name"`                   // 配置文件名称，不含扩展名；默认 "config"
	Paths     []string `json:"paths" yaml:"paths" mapstructure:"paths"`                // 配置文件搜索路径；默认 [".", "./config"]
	FileType  string   `json:"file_type" yaml:"file_type" mapstructure:"file_type"`    // 配置文件类型，如 yaml、json；默认 "yaml"
	EnvPrefix string   `json:"env_prefix" yaml:"env_prefix" mapstructure:"env_prefix"` // 环境变量前缀；默认 "GENESIS"
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
	} else {
		cfgCopy := *cfg
		cfgCopy.Paths = append([]string(nil), cfg.Paths...)
		cfg = &cfgCopy
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return newLoader(cfg, opts...)
}
