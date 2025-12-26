package connector

import (
	"fmt"
	"time"
)

// MySQLConfig MySQL连接配置
type MySQLConfig struct {
	// 基础配置（可选，有默认值）
	Name            string        `mapstructure:"name"`              // 连接器名称 (默认: "default")
	MaxRetries      int           `mapstructure:"max_retries"`       // 最大重试次数 (默认: 3)
	RetryInterval   time.Duration `mapstructure:"retry_interval"`    // 重试间隔 (默认: 1s)
	ConnectTimeout  time.Duration `mapstructure:"connect_timeout"`   // 连接超时 (默认: 5s)
	HealthCheckFreq time.Duration `mapstructure:"health_check_freq"` // 健康检查频率 (默认: 30s)

	// 核心配置
	DSN      string `mapstructure:"dsn"`      // 完整 DSN (可选，若提供则忽略 Host/Port 等，优先级最高)
	Host     string `mapstructure:"host"`     // [必填] 主机地址
	Port     int    `mapstructure:"port"`     // [必填] 端口 (默认: 3306)
	Username string `mapstructure:"username"` // [必填] 用户名
	Database string `mapstructure:"database"` // [必填] 数据库名
	Password string `mapstructure:"password"` // [必填] 密码

	// 高级配置（可选，有默认值）
	Charset         string        `mapstructure:"charset"`           // 字符集 (默认: "utf8mb4")
	Timeout         time.Duration `mapstructure:"timeout"`           // 连接超时 (默认: 5s)
	MaxIdleConns    int           `mapstructure:"max_idle_conns"`    // 最大空闲连接数 (默认: 10)
	MaxOpenConns    int           `mapstructure:"max_open_conns"`    // 最大打开连接数 (默认: 100)
	MaxLifetime     time.Duration `mapstructure:"max_lifetime"`      // 连接最大生命周期 (默认: 1h)
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"` // 连接最大生命周期 (同 MaxLifetime)
}

// setDefaults 设置默认值
func (c *MySQLConfig) setDefaults() {
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

// validate 实现 Configurable 接口
func (c *MySQLConfig) validate() error {
	c.setDefaults()
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
	// 基础配置（可选，有默认值）
	Name            string        `mapstructure:"name"`              // 连接器名称 (默认: "default")
	MaxRetries      int           `mapstructure:"max_retries"`       // 最大重试次数 (默认: 3)
	RetryInterval   time.Duration `mapstructure:"retry_interval"`    // 重试间隔 (默认: 1s)
	ConnectTimeout  time.Duration `mapstructure:"connect_timeout"`   // 连接超时 (默认: 5s)
	HealthCheckFreq time.Duration `mapstructure:"health_check_freq"` // 健康检查频率 (默认: 30s)

	// 核心配置
	Addr     string `mapstructure:"addr"`     // [必填] 连接地址，如 "127.0.0.1:6379"
	Password string `mapstructure:"password"` // [可选] 认证密码
	DB       int    `mapstructure:"db"`       // [可选] 数据库编号 (默认: 0)

	// 高级配置（可选，有默认值）
	PoolSize     int           `mapstructure:"pool_size"`      // 连接池大小 (默认: 10)
	MinIdleConns int           `mapstructure:"min_idle_conns"` // 最小空闲连接数 (默认: 5)
	DialTimeout  time.Duration `mapstructure:"dial_timeout"`   // 连接超时 (默认: 5s)
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`   // 读取超时 (默认: 3s)
	WriteTimeout time.Duration `mapstructure:"write_timeout"`  // 写入超时 (默认: 3s)
}

// setDefaults 设置默认值
func (c *RedisConfig) setDefaults() {
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

// validate 实现 Configurable 接口
func (c *RedisConfig) validate() error {
	c.setDefaults()
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
	// 基础配置（可选，有默认值）
	Name            string        `mapstructure:"name"`              // 连接器名称 (默认: "default")
	MaxRetries      int           `mapstructure:"max_retries"`       // 最大重试次数 (默认: 3)
	RetryInterval   time.Duration `mapstructure:"retry_interval"`    // 重试间隔 (默认: 1s)
	ConnectTimeout  time.Duration `mapstructure:"connect_timeout"`   // 连接超时 (默认: 5s)
	HealthCheckFreq time.Duration `mapstructure:"health_check_freq"` // 健康检查频率 (默认: 30s)

	// 核心配置
	Endpoints []string `mapstructure:"endpoints"` // [必填] 连接地址列表
	Username  string   `mapstructure:"username"`  // [可选] 认证用户
	Password  string   `mapstructure:"password"`  // [可选] 认证密码

	// 高级配置（可选，有默认值）
	DialTimeout      time.Duration `mapstructure:"dial_timeout"`       // 连接超时 (默认: 5s)
	Timeout          time.Duration `mapstructure:"timeout"`            // 连接超时 (同 DialTimeout)
	KeepAliveTime    time.Duration `mapstructure:"keep_alive_time"`    // 心跳间隔 (默认: 10s)
	KeepAliveTimeout time.Duration `mapstructure:"keep_alive_timeout"` // 心跳超时 (默认: 3s)
}

// setDefaults 设置默认值
func (c *EtcdConfig) setDefaults() {
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

// validate 实现 Configurable 接口
func (c *EtcdConfig) validate() error {
	c.setDefaults()
	if len(c.Endpoints) == 0 {
		return fmt.Errorf("Etcd端点不能为空")
	}
	return nil
}

// NATSConfig NATS连接配置
type NATSConfig struct {
	// 基础配置（可选，有默认值）
	Name            string        `mapstructure:"name"`              // 连接器名称 (默认: "default")
	MaxRetries      int           `mapstructure:"max_retries"`       // 最大重试次数 (默认: 3)
	RetryInterval   time.Duration `mapstructure:"retry_interval"`    // 重试间隔 (默认: 1s)
	ConnectTimeout  time.Duration `mapstructure:"connect_timeout"`   // 连接超时 (默认: 5s)
	HealthCheckFreq time.Duration `mapstructure:"health_check_freq"` // 健康检查频率 (默认: 30s)

	// 核心配置
	URL      string `mapstructure:"url"`      // [必填] 连接地址，如 "nats://127.0.0.1:4222"
	Username string `mapstructure:"username"` // [可选] 用户名
	Password string `mapstructure:"password"` // [可选] 密码
	Token    string `mapstructure:"token"`    // [可选] 令牌

	// 高级配置（可选，有默认值）
	Timeout       time.Duration `mapstructure:"timeout"`        // 连接超时 (默认: 5s)
	MaxReconnects int           `mapstructure:"max_reconnects"` // 最大重连次数 (默认: 60)
	ReconnectWait time.Duration `mapstructure:"reconnect_wait"` // 重连等待时间 (默认: 2s)
	PingInterval  time.Duration `mapstructure:"ping_interval"`  // ping间隔 (默认: 2m)
	MaxPingsOut   int           `mapstructure:"max_pings_out"`  // 最大未响应ping数 (默认: 2)
}

// setDefaults 设置默认值
func (c *NATSConfig) setDefaults() {
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

// validate 实现 Configurable 接口
func (c *NATSConfig) validate() error {
	c.setDefaults()
	if c.URL == "" {
		return fmt.Errorf("NATS URL不能为空")
	}
	return nil
}

// KafkaConfig Kafka连接配置
type KafkaConfig struct {
	// 基础配置
	Name string   `mapstructure:"name"` // 连接器名称
	Seed []string `mapstructure:"seed"` // 初始连接节点 (Brokers)

	// 认证配置
	User     string `mapstructure:"user"`      // SASL 用户名
	Password string `mapstructure:"password"`  // SASL 密码
	ClientID string `mapstructure:"client_id"` // 客户端 ID

	// 连接配置
	ConnectTimeout time.Duration `mapstructure:"connect_timeout"` // 连接超时
	RequestTimeout time.Duration `mapstructure:"request_timeout"` // 请求超时
}

func (c *KafkaConfig) setDefaults() {
	if c.Name == "" {
		c.Name = "default"
	}
	if c.ConnectTimeout == 0 {
		c.ConnectTimeout = 10 * time.Second
	}
	if c.RequestTimeout == 0 {
		c.RequestTimeout = 10 * time.Second
	}
	if c.ClientID == "" {
		c.ClientID = "genesis-connector"
	}
}

func (c *KafkaConfig) validate() error {
	c.setDefaults()
	if len(c.Seed) == 0 {
		return fmt.Errorf("Kafka seed brokers不能为空")
	}
	return nil
}
