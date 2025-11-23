package types

// Option 配置选项模式
type Option func(*Options)

type Options struct {
	Name       string
	Paths      []string
	FileType   string
	EnvPrefix  string
	RemoteOpts *RemoteOptions
}

type RemoteOptions struct {
	Provider string
	Endpoint string
}

// WithConfigName 设置配置文件名称（不带扩展名）
func WithConfigName(name string) Option {
	return func(o *Options) {
		o.Name = name
	}
}

// WithConfigPath 添加配置文件搜索路径
func WithConfigPath(path string) Option {
	return func(o *Options) {
		o.Paths = append(o.Paths, path)
	}
}

// WithConfigPaths 设置配置文件搜索路径（覆盖默认值）
func WithConfigPaths(paths ...string) Option {
	return func(o *Options) {
		o.Paths = paths
	}
}

// WithConfigType 设置配置文件类型 (yaml, json, etc.)
func WithConfigType(typ string) Option {
	return func(o *Options) {
		o.FileType = typ
	}
}

// WithEnvPrefix 设置环境变量前缀
func WithEnvPrefix(prefix string) Option {
	return func(o *Options) {
		o.EnvPrefix = prefix
	}
}

// WithRemote 设置远程配置中心选项
func WithRemote(provider, endpoint string) Option {
	return func(o *Options) {
		o.RemoteOpts = &RemoteOptions{
			Provider: provider,
			Endpoint: endpoint,
		}
	}
}

// DefaultOptions 返回默认选项
func DefaultOptions() *Options {
	return &Options{
		Name:      "config",
		Paths:     []string{".", "./config"},
		FileType:  "yaml",
		EnvPrefix: "GENESIS",
	}
}
