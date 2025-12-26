package config

// New 创建一个新的配置加载器实例
//
// config 为配置，opts 为函数式选项。
//
// 基本使用：
//
//	loader, _ := config.New(&config.Config{
//	    Name:     "config",
//	    FileType: "yaml",
//	})
//
// 使用选项：
//
//	loader, _ := config.New(&config.Config{},
//	    config.WithConfigName("app"),
//	    config.WithConfigPath("/etc/app"),
//	)
//
// 参数：
//
//	config - 配置，如果为 nil 会使用默认配置
//	opts   - 函数式选项列表，用于覆盖配置
//
// 返回：
//
//	Loader - 配置加载器
//	error  - 配置验证错误
func New(cfg *Config, opts ...Option) (Loader, error) {
	if cfg == nil {
		cfg = &Config{}
	}

	// 应用选项
	for _, opt := range opts {
		opt(cfg)
	}

	// 验证配置
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return newLoader(cfg)
}
