package config

import "strings"

// Config 配置结构
type Config struct {
	Name      string   // 配置文件名称（不含扩展名）
	Paths     []string // 配置文件搜索路径，默认 ["./", "./config"]
	FileType  string   // 配置文件类型 (yaml, json, etc.)
	EnvPrefix string   // 环境变量前缀，默认 "GENESIS"
}

// validate 设置默认值并验证配置
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
func New(cfg *Config) (Loader, error) {
	if cfg == nil {
		cfg = &Config{}
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return newLoader(cfg)
}
