package types

import "time"

// Config 熔断器组件配置
type Config struct {
	// Default 默认策略（应用到所有未单独配置的服务）
	Default Policy `yaml:"default" json:"default"`

	// Services 按服务名配置不同的策略（可选）
	// Key 为服务名（如 "user.v1.UserService"）
	Services map[string]Policy `yaml:"services" json:"services"`
}

// Policy 熔断策略
type Policy struct {
	// FailureThreshold 失败率阈值 (0.0-1.0)
	// 当失败率超过此值时触发熔断
	// 默认: 0.5 (50%)
	FailureThreshold float64 `yaml:"failure_threshold" json:"failure_threshold"`

	// WindowSize 滑动窗口大小（统计最近 N 次请求）
	// 默认: 100
	WindowSize int `yaml:"window_size" json:"window_size"`

	// MinRequests 最小请求数
	// 在窗口内请求数未达到此值前，不触发熔断判断
	// 默认: 10
	MinRequests int `yaml:"min_requests" json:"min_requests"`

	// OpenTimeout 熔断持续时间
	// 熔断器进入 Open 状态后，等待此时间后转为 HalfOpen
	// 默认: 30s
	OpenTimeout time.Duration `yaml:"open_timeout" json:"open_timeout"`

	// HalfOpenMaxRequests 半开状态允许的最大探测请求数
	// 默认: 3
	HalfOpenMaxRequests int `yaml:"half_open_max_requests" json:"half_open_max_requests"`

	// CountTimeout 是否将超时也计入失败统计
	// 默认: false (仅统计明确的错误，如 codes.Unavailable)
	CountTimeout bool `yaml:"count_timeout" json:"count_timeout"`
}

// DefaultPolicy 返回默认策略
func DefaultPolicy() Policy {
	return Policy{
		FailureThreshold:    0.5,
		WindowSize:          100,
		MinRequests:         10,
		OpenTimeout:         30 * time.Second,
		HalfOpenMaxRequests: 3,
		CountTimeout:        false,
	}
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		Default:  DefaultPolicy(),
		Services: make(map[string]Policy),
	}
}
