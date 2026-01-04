# API 库

## clog

```go
package clog // import "github.com/ceyewan/genesis/clog"

基于 slog 的结构化日志组件。

type Config struct {
    Level       string // debug|info|warn|error|fatal
    Format      string // json|console
    Output      string // stdout|stderr|<file path>
    EnableColor bool   // 仅 console 格式有效
    AddSource   bool
    SourceRoot  string
}

type Logger interface {
    Debug/Info/Warn/Error/Fatal(msg string, fields ...Field)
    DebugContext/InfoContext/WarnContext/ErrorContext/FatalContext(ctx, msg, fields...)
    With(fields ...Field) Logger
    WithNamespace(parts ...string) Logger
    SetLevel(level Level) error
    Flush()
}

type Level int
const (
    DebugLevel Level = iota - 4; InfoLevel; WarnLevel; ErrorLevel; FatalLevel
)

type Field = slog.Attr
    func String/Int/Float64/Bool/Time/Duration/Int64/Any(k, v) Field
    func Error(err error) Field                  // 仅 err_msg
    func ErrorWithCode(err, code) Field          // error={msg, code}
    func ErrorWithStack(err) Field               // error={msg, type, stack}
    func ErrorWithCodeStack(err, code) Field     // error={msg, type, stack, code}

type Option func(*options)
    func WithContextField(key, fieldName) Option
    func WithNamespace(parts ...) Option
    func WithStandardContext() Option            // trace_id, user_id, request_id

func Discard() Logger
func New(config *Config, opts ...Option) (Logger, error)
func NewDevDefaultConfig(sourceRoot) *Config
func NewProdDefaultConfig(sourceRoot) *Config
```

## config

```go
package config // import "github.com/ceyewan/genesis/config"

基于 Viper 的多源配置加载组件。

type Loader struct { ... }
    func New(paths ...) *Loader
    func (l *Loader) Load() (*Config, error)
    func (l *Loader) UnmarshalKey(key string, v any) error
    func (l *Loader) String(key) string
    func (l *Loader) Int(key) int
    func (l *Loader) Bool(key) bool
```

## metrics

```go
package metrics // import "github.com/ceyewan/genesis/metrics"

基于 OpenTelemetry 的指标组件。

type Meter interface {
    // Counter 单调递增计数器
    Inc(ctx, increment float64, attrs ...Attr)
    // Histogram 分布直方图
    Record(ctx, value float64, attrs ...Attr)
    // Gauge 任意值仪表
    Set(ctx, value float64, attrs ...Attr)
}

func NewMeter(opts ...Option) (Meter, error)
func NewStdMeter() Meter  // stdout 输出
```

## xerrors

```go
package xerrors // import "github.com/ceyewan/genesis/xerrors"

零依赖的错误处理工具。

var (
    ErrNotFound/ErrAlreadyExists/ErrInvalidInput/ErrTimeout/ErrUnavailable
    ErrUnauthorized/ErrForbidden/ErrConflict/ErrInternal/ErrCanceled
)

func Wrap(err, msg) error
func Wrapf(err, format, args...) error
func WithCode(err, code) error
func GetCode(err) string
func Combine(errs...) error           // 智能合并多错误
func Must[T](v, err) T                // 初始化时使用

type Collector struct { ... }
    func (c *Collector) Collect(err)
    func (c *Collector) Err() error

var New/Is/As/Unwrap/Join = errors.New/Is/As/Unwrap/Join
```

## connector

```go
package connector // import "github.com/ceyewan/genesis/connector"

数据库/缓存连接管理。

type MySQLConnector interface { DB(); Close() }
type RedisConnector interface { Client(); Close() }
type EtcdConnector interface { Client(); Close() }
type NATSConnector interface { Conn(); Close() }

func NewMySQL(cfg *MySQLConfig, opts ...Option) (MySQLConnector, error)
func NewRedis(cfg *RedisConfig, opts ...Option) (RedisConnector, error)
func NewEtcd(cfg *EtcdConfig, opts ...Option) (EtcdConnector, error)
func NewNATS(cfg *NATSConfig, opts ...Option) (NATSConnector, error)
```

## cache

```go
package cache // import "github.com/ceyewan/genesis/cache"

缓存组件，支持 Redis 和内存后端。

type Cache interface {
    Get(ctx, key, dst any) error
    Set(ctx, key, value any, ttl time.Duration) error
    Delete(ctx, key) error
    Exists(ctx, key) (bool, error)
    Clear(ctx) error
}

type Config struct {
    Driver DriverType       // redis|memory
    Prefix string
    Serializer string
    Standalone *StandaloneConfig
}

func New(cfg *Config, opts ...Option) (Cache, error)
```

## dlock

```go
package dlock // import "github.com/ceyewan/genesis/dlock"

分布式锁组件，支持 Redis 和 Etcd 后端。

type Lock interface {
    Lock(ctx) error
    TryLock(ctx) (bool, error)
    Unlock() error
}

type Config struct {
    Driver string  // redis|etcd
    TTL    time.Duration
}

func New(cfg *Config, opts ...Option) (Lock, error)
func NewWithRedis(redisConn RedisConnector, cfg *Config, opts ...Option) (Lock, error)
func NewWithEtcd(etcdConn EtcdConnector, cfg *Config, opts ...Option) (Lock, error)
```

## idgen

```go
package idgen // import "github.com/ceyewan/genesis/idgen"

ID 生成组件，支持 Snowflake 和 UUID。

type Sequencer interface { Next() int64; MustNext() int64 }
type Generator interface { String() string; MustGenerate() string }

func NewSnowflake(instanceID int64) Sequencer
func NewUUID() Generator
func NewULID() Generator
```

## mq

```go
package mq // import "github.com/ceyewan/genesis/mq"

消息队列组件，基于 NATS。

type Publisher interface { Publish(ctx, subject string, msg any) error }
type Subscriber interface { Subscribe(subject, queue string, handler Handler) error }

type Message struct { Subject string; Data []byte; Reply string }

func NewPublisher(conn NATSConnector, opts ...Option) (Publisher, error)
func NewSubscriber(conn NATSConnector, opts ...Option) (Subscriber, error)
```

## idem

```go
package idem // import "github.com/ceyewan/genesis/idem"

幂等组件，基于 Redis。

type Idempotency interface {
    Execute(ctx, key string, fn func(ctx context.Context) (interface{}, error)) (interface{}, error)
    GinMiddleware(opts ...MiddlewareOption) interface{}
    UnaryServerInterceptor(opts ...InterceptorOption) grpc.UnaryServerInterceptor
}

type Config struct {
    Driver     DriverType
    Prefix     string
    DefaultTTL time.Duration
    LockTTL    time.Duration
}

func New(cfg *Config, opts ...Option) (Idempotency, error)
```

## auth

```go
package auth // import "github.com/ceyewan/genesis/auth"

认证授权组件。

type Verifier interface {
    Verify(ctx, token string) (bool, error)
}
type Authorizer interface {
    Allow(ctx, subject, action, resource string) (bool, error)
}

func NewAPIKey(cfg *APIKeyConfig) (Verifier, error)
func NewJWT(cfg *JWTConfig) (Verifier, error)
func NewACL(rules ...Rule) (Authorizer, error)
```

## ratelimit

```go
package ratelimit // import "github.com/ceyewan/genesis/ratelimit"

限流组件，支持单机和分布式。

type Limiter interface {
    Allow(ctx, key string, limit Limit) (bool, error)
    AllowN(ctx, key string, limit Limit, n int) (bool, error)
    Wait(ctx, key string, limit Limit) error
    Close() error
}

type Limit struct { Rate float64; Burst int }

func New(cfg *Config, opts ...Option) (Limiter, error)
func Discard() Limiter  // No-op
```

## breaker

```go
package breaker // import "github.com/ceyewan/genesis/breaker"

熔断器组件。

type Breaker interface {
    Execute(ctx, key string, fn func() (any, error)) (any, error)
    State(key) (State, error)
}

type State int
const (
    StateClosed State = iota
    StateHalfOpen
    StateOpen
)

func New(cfg *Config, opts ...Option) (Breaker, error)
```

## registry

```go
package registry // import "github.com/ceyewan/genesis/registry"

服务注册发现组件，基于 Etcd。

type Registrar interface { Register(ctx) error; Deregister(ctx) error }
type Discovery interface { GetEndpoints(ctx, serviceName) ([]string, error); Close() }

func NewRegistrar(etcdConn EtcdConnector, svc *Service, opts ...Option) (Registrar, error)
func NewDiscovery(etcdConn EtcdConnector, opts ...Option) (Discovery, error)
```
