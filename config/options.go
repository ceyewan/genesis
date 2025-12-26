package config

// Option 配置选项模式
type Option func(*Config)

// Config 配置结构
type Config struct {
	Name      string   // 配置文件名称（不含扩展名）
	Paths     []string // 配置文件搜索路径
	FileType  string   // 配置文件类型 (yaml, json, etc.)
	EnvPrefix string   // 环境变量前缀
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
	return nil
}

// WithConfigName 设置配置文件名称（不带扩展名）
func WithConfigName(name string) Option {
	return func(c *Config) {
		c.Name = name
	}
}

// WithConfigPath 添加配置文件搜索路径
func WithConfigPath(path string) Option {
	return func(c *Config) {
		c.Paths = append(c.Paths, path)
	}
}

// WithConfigPaths 设置配置文件搜索路径（覆盖默认值）
func WithConfigPaths(paths ...string) Option {
	return func(c *Config) {
		c.Paths = paths
	}
}

// WithConfigType 设置配置文件类型 (yaml, json, etc.)
func WithConfigType(typ string) Option {
	return func(c *Config) {
		c.FileType = typ
	}
}

// WithEnvPrefix 设置环境变量前缀
func WithEnvPrefix(prefix string) Option {
	return func(c *Config) {
		c.EnvPrefix = prefix
	}
}
