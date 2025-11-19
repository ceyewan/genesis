// pkg/connector/config.go
package connector

import (
	"fmt"
	"time"
)

// MySQLConfig MySQL连接配置
type MySQLConfig struct {
	Host         string        // 主机地址
	Port         int           // 端口
	Username     string        // 用户名
	Password     string        // 密码
	Database     string        // 数据库名
	Charset      string        // 字符集
	Timeout      time.Duration // 连接超时
	MaxIdleConns int           // 最大空闲连接数
	MaxOpenConns int           // 最大打开连接数
	MaxLifetime  time.Duration // 连接最大生命周期
	LogNamespace string        // 日志命名空间，如 "user-service.db"
}

// Validate 实现 Configurable 接口
func (c MySQLConfig) Validate() error {
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
	Addr         string        // 连接地址，如 "127.0.0.1:6379"
	Password     string        // 认证密码（可选）
	DB           int           // 数据库编号（默认0）
	PoolSize     int           // 连接池大小（默认10）
	MinIdleConns int           // 最小空闲连接数（默认5）
	MaxRetries   int           // 最大重试次数（默认3）
	DialTimeout  time.Duration // 连接超时（默认5s）
	ReadTimeout  time.Duration // 读取超时（默认3s）
	WriteTimeout time.Duration // 写入超时（默认3s）
	LogNamespace string        // 日志命名空间，如 "user-service.cache"
}

// Validate 实现 Configurable 接口
func (c RedisConfig) Validate() error {
	if c.Addr == "" {
		return fmt.Errorf("Redis地址不能为空")
	}
	if c.DB < 0 {
		return fmt.Errorf("数据库编号不能小于0")
	}
	if c.PoolSize <= 0 {
		c.PoolSize = 10
	}
	if c.MinIdleConns < 0 {
		c.MinIdleConns = 5
	}
	if c.MaxRetries < 0 {
		c.MaxRetries = 3
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
	return nil
}

// EtcdConfig Etcd连接配置
type EtcdConfig struct {
	Endpoints        []string      // 连接地址
	Username         string        // 认证用户（可选）
	Password         string        // 认证密码（可选）
	Timeout          time.Duration // 连接超时（默认5s）
	KeepAliveTime    time.Duration // 心跳间隔（默认10s）
	KeepAliveTimeout time.Duration // 心跳超时（默认3s）
	LogNamespace     string        // 日志命名空间，如 "user-service.etcd"
}

// Validate 实现 Configurable 接口
func (c EtcdConfig) Validate() error {
	if len(c.Endpoints) == 0 {
		return fmt.Errorf("Etcd端点不能为空")
	}
	if c.Timeout == 0 {
		c.Timeout = 5 * time.Second
	}
	if c.KeepAliveTime == 0 {
		c.KeepAliveTime = 10 * time.Second
	}
	if c.KeepAliveTimeout == 0 {
		c.KeepAliveTimeout = 3 * time.Second
	}
	return nil
}

// NATSConfig NATS连接配置
type NATSConfig struct {
	URL           string        // 连接地址，如 "nats://127.0.0.1:4222"
	Name          string        // 客户端名称（可选）
	Username      string        // 用户名（可选）
	Password      string        // 密码（可选）
	Token         string        // 令牌（可选）
	ReconnectWait time.Duration // 重连等待时间（默认2s）
	MaxReconnects int           // 最大重连次数（默认60）
	PingInterval  time.Duration // ping间隔（默认2m）
	MaxPingsOut   int           // 最大未响应ping数（默认2）
	Timeout       time.Duration // 连接超时（默认5s）
	LogNamespace  string        // 日志命名空间，如 "user-service.mq"
}

// Validate 实现 Configurable 接口
func (c NATSConfig) Validate() error {
	if c.URL == "" {
		return fmt.Errorf("NATS URL不能为空")
	}
	if c.ReconnectWait == 0 {
		c.ReconnectWait = 2 * time.Second
	}
	if c.MaxReconnects == 0 {
		c.MaxReconnects = 60
	}
	if c.PingInterval == 0 {
		c.PingInterval = 2 * time.Minute
	}
	if c.MaxPingsOut == 0 {
		c.MaxPingsOut = 2
	}
	if c.Timeout == 0 {
		c.Timeout = 5 * time.Second
	}
	return nil
}
