package simple

import "time"

// Config 连接配置（必需参数）
type Config struct {
	Backend   string        // 后端类型: etcd, redis
	Endpoints []string      // 连接地址
	Username  string        // 认证用户（可选）
	Password  string        // 认证密码（可选）
	Timeout   time.Duration // 连接超时（可选，默认5s）
}

// DefaultConfig 默认连接配置
func DefaultConfig() *Config {
	return &Config{
		Backend:   "etcd",
		Endpoints: []string{"127.0.0.1:2379"},
		Timeout:   5 * time.Second,
	}
}
