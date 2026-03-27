package connector

import (
	"time"
)

// MySQLConfig MySQL连接配置
type MySQLConfig struct {
	// 基础配置
	Name string `mapstructure:"name" json:"name" yaml:"name"` // 连接器名称 (默认: "default")

	// 核心配置
	DSN      string `mapstructure:"dsn" json:"dsn" yaml:"dsn"`           // 完整 DSN (可选，若提供则忽略 Host/Port 等，优先级最高)
	Host     string `mapstructure:"host" json:"host" yaml:"host"`         // 主机地址 (DSN 未设置时必填)
	Port     int    `mapstructure:"port" json:"port" yaml:"port"`         // 端口 (默认: 3306)
	Username string `mapstructure:"username" json:"username" yaml:"username"` // 用户名 (DSN 未设置时必填)
	Password string `mapstructure:"password" json:"password" yaml:"password"` // 密码
	Database string `mapstructure:"database" json:"database" yaml:"database"` // 数据库名 (DSN 未设置时必填)

	// 高级配置
	Charset         string        `mapstructure:"charset" json:"charset" yaml:"charset"`                       // 字符集 (默认: "utf8mb4")
	MaxIdleConns    int           `mapstructure:"max_idle_conns" json:"max_idle_conns" yaml:"max_idle_conns"` // 最大空闲连接数 (默认: 10)
	MaxOpenConns    int           `mapstructure:"max_open_conns" json:"max_open_conns" yaml:"max_open_conns"` // 最大打开连接数 (默认: 100)
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime" json:"conn_max_lifetime" yaml:"conn_max_lifetime"` // 连接最大生命周期 (默认: 1h)
	ConnectTimeout  time.Duration `mapstructure:"connect_timeout" json:"connect_timeout" yaml:"connect_timeout"`       // 连接超时 (默认: 5s)
}

// setDefaults 设置默认值
func (c *MySQLConfig) setDefaults() {
	if c.Name == "" {
		c.Name = "default"
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
	if c.ConnectTimeout == 0 {
		c.ConnectTimeout = 5 * time.Second
	}
}

// validate 验证配置
func (c *MySQLConfig) validate() error {
	c.setDefaults()
	// 如果提供了 DSN，则跳过其他字段的校验
	if c.DSN != "" {
		return nil
	}
	if c.Host == "" {
		return ErrConfig
	}
	if c.Port <= 0 {
		return ErrConfig
	}
	if c.Username == "" {
		return ErrConfig
	}
	if c.Database == "" {
		return ErrConfig
	}
	return nil
}

// RedisConfig Redis连接配置
type RedisConfig struct {
	// 基础配置
	Name string `mapstructure:"name" json:"name" yaml:"name"` // 连接器名称 (默认: "default")

	// 核心配置
	Addr     string `mapstructure:"addr" json:"addr" yaml:"addr"`         // 连接地址 (必填)，如 "127.0.0.1:6379"
	Password string `mapstructure:"password" json:"password" yaml:"password"` // 认证密码 (可选)
	DB       int    `mapstructure:"db" json:"db" yaml:"db"`               // 数据库编号 (默认: 0)

	// 高级配置
	PoolSize     int           `mapstructure:"pool_size" json:"pool_size" yaml:"pool_size"`           // 连接池大小 (默认: 10)
	MinIdleConns int           `mapstructure:"min_idle_conns" json:"min_idle_conns" yaml:"min_idle_conns"` // 最小空闲连接数 (默认: 5)
	DialTimeout  time.Duration `mapstructure:"dial_timeout" json:"dial_timeout" yaml:"dial_timeout"`  // 连接超时 (默认: 5s)
	ReadTimeout  time.Duration `mapstructure:"read_timeout" json:"read_timeout" yaml:"read_timeout"`  // 读取超时 (默认: 3s)
	WriteTimeout time.Duration `mapstructure:"write_timeout" json:"write_timeout" yaml:"write_timeout"` // 写入超时 (默认: 3s)

	// 可观测性
	EnableTracing bool `mapstructure:"enable_tracing" json:"enable_tracing" yaml:"enable_tracing"` // 是否启用 Tracing (透传给 redisotel)
}

// setDefaults 设置默认值
func (c *RedisConfig) setDefaults() {
	if c.Name == "" {
		c.Name = "default"
	}
	if c.PoolSize <= 0 {
		c.PoolSize = 10
	}
	if c.MinIdleConns <= 0 {
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

// validate 验证配置
func (c *RedisConfig) validate() error {
	c.setDefaults()
	if c.Addr == "" {
		return ErrConfig
	}
	if c.DB < 0 {
		return ErrConfig
	}
	return nil
}

// EtcdConfig Etcd连接配置
type EtcdConfig struct {
	// 基础配置
	Name string `mapstructure:"name" json:"name" yaml:"name"` // 连接器名称 (默认: "default")

	// 核心配置
	Endpoints []string `mapstructure:"endpoints" json:"endpoints" yaml:"endpoints"` // 连接地址列表 (必填)
	Username  string   `mapstructure:"username" json:"username" yaml:"username"`    // 认证用户 (可选)
	Password  string   `mapstructure:"password" json:"password" yaml:"password"`    // 认证密码 (可选)

	// 高级配置
	DialTimeout      time.Duration `mapstructure:"dial_timeout" json:"dial_timeout" yaml:"dial_timeout"`             // 连接超时 (默认: 5s)
	KeepAliveTime    time.Duration `mapstructure:"keep_alive_time" json:"keep_alive_time" yaml:"keep_alive_time"`    // 心跳间隔 (默认: 10s)
	KeepAliveTimeout time.Duration `mapstructure:"keep_alive_timeout" json:"keep_alive_timeout" yaml:"keep_alive_timeout"` // 心跳超时 (默认: 3s)
}

// setDefaults 设置默认值
func (c *EtcdConfig) setDefaults() {
	if c.Name == "" {
		c.Name = "default"
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

// validate 验证配置
func (c *EtcdConfig) validate() error {
	c.setDefaults()
	if len(c.Endpoints) == 0 {
		return ErrConfig
	}
	return nil
}

// NATSConfig NATS连接配置
type NATSConfig struct {
	// 基础配置
	Name string `mapstructure:"name" json:"name" yaml:"name"` // 连接器名称 (默认: "default")

	// 核心配置
	URL      string `mapstructure:"url" json:"url" yaml:"url"`            // 连接地址 (必填)，如 "nats://127.0.0.1:4222"
	Username string `mapstructure:"username" json:"username" yaml:"username"` // 用户名 (可选)
	Password string `mapstructure:"password" json:"password" yaml:"password"` // 密码 (可选)
	Token    string `mapstructure:"token" json:"token" yaml:"token"`      // 令牌 (可选)

	// 高级配置
	ConnectTimeout time.Duration `mapstructure:"connect_timeout" json:"connect_timeout" yaml:"connect_timeout"` // 连接超时 (默认: 5s)
	MaxReconnects  int           `mapstructure:"max_reconnects" json:"max_reconnects" yaml:"max_reconnects"`    // 最大重连次数 (默认: 60)
	ReconnectWait  time.Duration `mapstructure:"reconnect_wait" json:"reconnect_wait" yaml:"reconnect_wait"`    // 重连等待时间 (默认: 2s)
	PingInterval   time.Duration `mapstructure:"ping_interval" json:"ping_interval" yaml:"ping_interval"`       // ping间隔 (默认: 2m)
}

// setDefaults 设置默认值
func (c *NATSConfig) setDefaults() {
	if c.Name == "" {
		c.Name = "default"
	}
	if c.ConnectTimeout == 0 {
		c.ConnectTimeout = 5 * time.Second
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
}

// validate 验证配置
func (c *NATSConfig) validate() error {
	c.setDefaults()
	if c.URL == "" {
		return ErrConfig
	}
	return nil
}

// KafkaConfig Kafka连接配置
type KafkaConfig struct {
	// 基础配置
	Name string `mapstructure:"name" json:"name" yaml:"name"` // 连接器名称 (默认: "default")

	// 核心配置
	Seed []string `mapstructure:"seed" json:"seed" yaml:"seed"` // 初始连接节点 (必填)

	// 认证配置
	User     string `mapstructure:"user" json:"user" yaml:"user"`           // SASL 用户名 (可选)
	Password string `mapstructure:"password" json:"password" yaml:"password"` // SASL 密码 (可选)
	ClientID string `mapstructure:"client_id" json:"client_id" yaml:"client_id"` // 客户端 ID (默认: "genesis-connector")

	// 连接配置
	ConnectTimeout       time.Duration `mapstructure:"connect_timeout" json:"connect_timeout" yaml:"connect_timeout"`                   // 连接超时 (默认: 10s)
	RequestTimeout       time.Duration `mapstructure:"request_timeout" json:"request_timeout" yaml:"request_timeout"`                   // 请求超时 (默认: 10s)
	AllowAutoTopicCreate bool          `mapstructure:"allow_auto_topic_create" json:"allow_auto_topic_create" yaml:"allow_auto_topic_create"` // 允许自动创建 Topic (默认: false)
}

// setDefaults 设置默认值
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

// validate 验证配置
func (c *KafkaConfig) validate() error {
	c.setDefaults()
	if len(c.Seed) == 0 {
		return ErrConfig
	}
	return nil
}

// SQLiteConfig SQLite连接配置
type SQLiteConfig struct {
	// 基础配置
	Name string `mapstructure:"name" json:"name" yaml:"name"` // 连接器名称 (默认: "default")

	// 核心配置
	Path string `mapstructure:"path" json:"path" yaml:"path"` // 数据库文件路径 (必填)，如 "./test.db" 或 "file::memory:?cache=shared"
}

// setDefaults 设置默认值
func (c *SQLiteConfig) setDefaults() {
	if c.Name == "" {
		c.Name = "default"
	}
}

// validate 验证配置
func (c *SQLiteConfig) validate() error {
	c.setDefaults()
	if c.Path == "" {
		return ErrConfig
	}
	return nil
}

// PostgreSQLConfig PostgreSQL连接配置
type PostgreSQLConfig struct {
	// 基础配置
	Name string `mapstructure:"name" json:"name" yaml:"name"` // 连接器名称 (默认: "default")

	// 核心配置
	DSN      string `mapstructure:"dsn" json:"dsn" yaml:"dsn"`           // 完整 DSN (可选，若提供则忽略 Host/Port 等，优先级最高)
	Host     string `mapstructure:"host" json:"host" yaml:"host"`         // 主机地址 (DSN 未设置时必填)
	Port     int    `mapstructure:"port" json:"port" yaml:"port"`         // 端口 (默认: 5432)
	Username string `mapstructure:"username" json:"username" yaml:"username"` // 用户名 (DSN 未设置时必填)
	Password string `mapstructure:"password" json:"password" yaml:"password"` // 密码
	Database string `mapstructure:"database" json:"database" yaml:"database"` // 数据库名 (DSN 未设置时必填)

	// 高级配置
	SSLMode         string        `mapstructure:"sslmode" json:"sslmode" yaml:"sslmode"`                               // SSL 模式 (默认: "disable")
	MaxIdleConns    int           `mapstructure:"max_idle_conns" json:"max_idle_conns" yaml:"max_idle_conns"`           // 最大空闲连接数 (默认: 10)
	MaxOpenConns    int           `mapstructure:"max_open_conns" json:"max_open_conns" yaml:"max_open_conns"`           // 最大打开连接数 (默认: 100)
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime" json:"conn_max_lifetime" yaml:"conn_max_lifetime"`  // 连接最大生命周期 (默认: 1h)
	ConnectTimeout  time.Duration `mapstructure:"connect_timeout" json:"connect_timeout" yaml:"connect_timeout"`        // 连接超时 (默认: 5s)
	Timezone        string        `mapstructure:"timezone" json:"timezone" yaml:"timezone"`                             // 时区 (默认: "UTC")
}

// setDefaults 设置默认值
func (c *PostgreSQLConfig) setDefaults() {
	if c.Name == "" {
		c.Name = "default"
	}
	if c.Port == 0 {
		c.Port = 5432
	}
	if c.SSLMode == "" {
		c.SSLMode = "disable"
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
	if c.ConnectTimeout == 0 {
		c.ConnectTimeout = 5 * time.Second
	}
	if c.Timezone == "" {
		c.Timezone = "UTC"
	}
}

// validate 验证配置
func (c *PostgreSQLConfig) validate() error {
	c.setDefaults()
	// 如果提供了 DSN，则跳过其他字段的校验
	if c.DSN != "" {
		return nil
	}
	if c.Host == "" {
		return ErrConfig
	}
	if c.Port <= 0 {
		return ErrConfig
	}
	if c.Username == "" {
		return ErrConfig
	}
	if c.Database == "" {
		return ErrConfig
	}
	return nil
}
