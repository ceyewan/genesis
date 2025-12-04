// pkg/connector/config.go
package connector

import (
	"fmt"
	"time"
)

// BaseConfig 通用连接配置
type BaseConfig struct {
	Name            string        `mapstructure:"name"`              // 连接器名称
	MaxRetries      int           `mapstructure:"max_retries"`       // 最大重试次数
	RetryInterval   time.Duration `mapstructure:"retry_interval"`    // 重试间隔
	ConnectTimeout  time.Duration `mapstructure:"connect_timeout"`   // 连接超时
	HealthCheckFreq time.Duration `mapstructure:"health_check_freq"` // 健康检查频率
}

// SetDefaults 设置默认值
func (c *BaseConfig) SetDefaults() {
	if c.Name == "" {
		c.Name = "default"
	}
	if c.MaxRetries == 0 {
		c.MaxRetries = 3
	}
	if c.RetryInterval == 0 {
		c.RetryInterval = time.Second
	}
	if c.ConnectTimeout == 0 {
		c.ConnectTimeout = 5 * time.Second
	}
	if c.HealthCheckFreq == 0 {
		c.HealthCheckFreq = 30 * time.Second
	}
}

// MySQLConfig MySQL连接配置
type MySQLConfig struct {
	BaseConfig      `mapstructure:",squash"`
	DSN             string        `mapstructure:"dsn"`               // 完整 DSN（可选，优先级最高）
	Host            string        `mapstructure:"host"`              // 主机地址
	Port            int           `mapstructure:"port"`              // 端口
	Username        string        `mapstructure:"username"`          // 用户名
	Password        string        `mapstructure:"password"`          // 密码
	Database        string        `mapstructure:"database"`          // 数据库名
	Charset         string        `mapstructure:"charset"`           // 字符集
	Timeout         time.Duration `mapstructure:"timeout"`           // 连接超时（向后兼容）
	MaxIdleConns    int           `mapstructure:"max_idle_conns"`    // 最大空闲连接数
	MaxOpenConns    int           `mapstructure:"max_open_conns"`    // 最大打开连接数
	MaxLifetime     time.Duration `mapstructure:"max_lifetime"`      // 连接最大生命周期（向后兼容）
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"` // 连接最大生命周期
}

// SetDefaults 设置默认值
func (c *MySQLConfig) SetDefaults() {
	c.BaseConfig.SetDefaults()
	if c.Port == 0 {
		c.Port = 3306
	}
	if c.Charset == "" {
		c.Charset = "utf8mb4"
	}
	if c.MaxIdleConns == 0 {
		c.MaxIdleConns = 10
	}
	if c.MaxOpenConns == 0 {
		c.MaxOpenConns = 100
	}
	if c.ConnMaxLifetime == 0 {
		c.ConnMaxLifetime = time.Hour
	}
}

// Validate 实现 Configurable 接口
func (c *MySQLConfig) Validate() error {
	c.SetDefaults()
	if c.Host == "" {
		return fmt.Errorf("主机地址不能为空")
	}
	if c.Port <= 0 {
		return fmt.Errorf("端口必须大于0")
	}
	if c.Username == "" {
		return fmt.Errorf("用户名不能为空")
	}
	if c.Database == "" {
		return fmt.Errorf("数据库名不能为空")
	}
	return nil
}

// RedisConfig Redis连接配置
type RedisConfig struct {
	BaseConfig   `mapstructure:",squash"`
	Addr         string        `mapstructure:"addr"`           // 连接地址，如 "127.0.0.1:6379"
	Password     string        `mapstructure:"password"`       // 认证密码（可选）
	DB           int           `mapstructure:"db"`             // 数据库编号（默认0）
	PoolSize     int           `mapstructure:"pool_size"`      // 连接池大小（默认10）
	MinIdleConns int           `mapstructure:"min_idle_conns"` // 最小空闲连接数（默认5）
	DialTimeout  time.Duration `mapstructure:"dial_timeout"`   // 连接超时（默认5s）
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`   // 读取超时（默认3s）
	WriteTimeout time.Duration `mapstructure:"write_timeout"`  // 写入超时（默认3s）
}

// SetDefaults 设置默认值
func (c *RedisConfig) SetDefaults() {
	c.BaseConfig.SetDefaults()
	if c.PoolSize <= 0 {
		c.PoolSize = 10
	}
	if c.MinIdleConns < 0 {
		c.MinIdleConns = 5
	}
	if c.DialTimeout == 0 {
		c.DialTimeout = 5 * time.Second
	}
	if c.ReadTimeout == 0 {
		c.ReadTimeout = 3 * time.Second
	}
	if c.WriteTimeout == 0 {
		c.WriteTimeout = 3 * time.Second
	}
}

// Validate 实现 Configurable 接口
func (c *RedisConfig) Validate() error {
	c.SetDefaults()
	if c.Addr == "" {
		return fmt.Errorf("Redis地址不能为空")
	}
	if c.DB < 0 {
		return fmt.Errorf("数据库编号不能小于0")
	}
	return nil
}

// EtcdConfig Etcd连接配置
type EtcdConfig struct {
	BaseConfig       `mapstructure:",squash"`
	Endpoints        []string      `mapstructure:"endpoints"`          // 连接地址
	Username         string        `mapstructure:"username"`           // 认证用户（可选）
	Password         string        `mapstructure:"password"`           // 认证密码（可选）
	DialTimeout      time.Duration `mapstructure:"dial_timeout"`       // 连接超时（默认5s）
	Timeout          time.Duration `mapstructure:"timeout"`            // 连接超时（向后兼容）
	KeepAliveTime    time.Duration `mapstructure:"keep_alive_time"`    // 心跳间隔（默认10s）
	KeepAliveTimeout time.Duration `mapstructure:"keep_alive_timeout"` // 心跳超时（默认3s）
}

// SetDefaults 设置默认值
func (c *EtcdConfig) SetDefaults() {
	c.BaseConfig.SetDefaults()
	if c.DialTimeout == 0 {
		c.DialTimeout = 5 * time.Second
	}
	if c.KeepAliveTime == 0 {
		c.KeepAliveTime = 10 * time.Second
	}
	if c.KeepAliveTimeout == 0 {
		c.KeepAliveTimeout = 3 * time.Second
	}
}

// Validate 实现 Configurable 接口
func (c *EtcdConfig) Validate() error {
	c.SetDefaults()
	if len(c.Endpoints) == 0 {
		return fmt.Errorf("Etcd端点不能为空")
	}
	return nil
}

// NATSConfig NATS连接配置
type NATSConfig struct {
	BaseConfig    `mapstructure:",squash"`
	URL           string        `mapstructure:"url"`            // 连接地址，如 "nats://127.0.0.1:4222"
	Name          string        `mapstructure:"name"`           // 客户端名称（可选）
	Username      string        `mapstructure:"username"`       // 用户名（可选）
	Password      string        `mapstructure:"password"`       // 密码（可选）
	Token         string        `mapstructure:"token"`          // 令牌（可选）
	Timeout       time.Duration `mapstructure:"timeout"`        // 连接超时（向后兼容）
	MaxReconnects int           `mapstructure:"max_reconnects"` // 最大重连次数（默认60）
	ReconnectWait time.Duration `mapstructure:"reconnect_wait"` // 重连等待时间（默认2s）
	PingInterval  time.Duration `mapstructure:"ping_interval"`  // ping间隔（默认2m）
	MaxPingsOut   int           `mapstructure:"max_pings_out"`  // 最大未响应ping数（默认2）
}

// SetDefaults 设置默认值
func (c *NATSConfig) SetDefaults() {
	c.BaseConfig.SetDefaults()
	if c.MaxReconnects == 0 {
		c.MaxReconnects = 60
	}
	if c.ReconnectWait == 0 {
		c.ReconnectWait = 2 * time.Second
	}
	if c.PingInterval == 0 {
		c.PingInterval = 2 * time.Minute
	}
	if c.MaxPingsOut == 0 {
		c.MaxPingsOut = 2
	}
}

// Validate 实现 Configurable 接口
func (c *NATSConfig) Validate() error {
	c.SetDefaults()
	if c.URL == "" {
		return fmt.Errorf("NATS URL不能为空")
	}
	return nil
}
