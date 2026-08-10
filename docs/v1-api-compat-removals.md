# Genesis v1.0.0-rc.2 approved API removals

This file is consumed by `make api-compat-check`. Every entry is an exact RC1
API that the approved RC2 surface decision removes without a replacement. The
check rejects entries that are absent from the immutable RC1 baseline, remain
in the current candidate, disguise a signature replacement, or overlap the
paired replacement files.

`testkit` moves to `internal/testkit`: it is repository test infrastructure,
has no Resonance consumer, and exposes concrete Testcontainers/backend types
that are not suitable for the stable public module contract. External tests
must own their fixtures.

The two standard-library HTTP wrappers are removed from `trace`. They were
zero-logic aliases with no known consumer and exposed a pinned
`otelhttp.Option` / `*otelhttp.Transport` contract. Callers migrate directly to
`otelhttp.NewHandler` and `otelhttp.NewTransport`.

## `testkit`

- func: `func NewContext(*testing.T, time.Duration) (context.Context, context.CancelFunc)`
- func: `func NewEtcdContainer(*testing.T) (*github.com/testcontainers/testcontainers-go/modules/etcd.EtcdContainer, *github.com/ceyewan/genesis/connector.EtcdConfig)`
- func: `func NewEtcdContainerClient(*testing.T) *go.etcd.io/etcd/client/v3.Client`
- func: `func NewEtcdContainerConfig(*testing.T) *github.com/ceyewan/genesis/connector.EtcdConfig`
- func: `func NewEtcdContainerConnector(*testing.T) github.com/ceyewan/genesis/connector.EtcdConnector`
- func: `func NewID() string`
- func: `func NewKafkaContainerClient(*testing.T) *github.com/twmb/franz-go/pkg/kgo.Client`
- func: `func NewKafkaContainerConfig(*testing.T) *github.com/ceyewan/genesis/connector.KafkaConfig`
- func: `func NewKafkaContainerConnector(*testing.T) github.com/ceyewan/genesis/connector.KafkaConnector`
- func: `func NewKit(*testing.T) *Kit`
- func: `func NewLogger() github.com/ceyewan/genesis/clog.Logger`
- func: `func NewMeter() github.com/ceyewan/genesis/metrics.Meter`
- func: `func NewMySQLConnector(*testing.T) github.com/ceyewan/genesis/connector.MySQLConnector`
- func: `func NewMySQLContainerConfig(*testing.T) *github.com/ceyewan/genesis/connector.MySQLConfig`
- func: `func NewMySQLDB(*testing.T) *gorm.io/gorm.DB`
- func: `func NewNATSContainer(*testing.T) (*github.com/testcontainers/testcontainers-go/modules/nats.NATSContainer, *github.com/ceyewan/genesis/connector.NATSConfig)`
- func: `func NewNATSContainerConfig(*testing.T) *github.com/ceyewan/genesis/connector.NATSConfig`
- func: `func NewNATSContainerConn(*testing.T) *github.com/nats-io/nats.go.Conn`
- func: `func NewNATSContainerConnector(*testing.T) github.com/ceyewan/genesis/connector.NATSConnector`
- func: `func NewPersistentSQLiteConfig(*testing.T) *github.com/ceyewan/genesis/connector.SQLiteConfig`
- func: `func NewPersistentSQLiteConnector(*testing.T) github.com/ceyewan/genesis/connector.SQLiteConnector`
- func: `func NewPostgreSQLConnector(*testing.T) github.com/ceyewan/genesis/connector.PostgreSQLConnector`
- func: `func NewPostgreSQLContainerConfig(*testing.T) *github.com/ceyewan/genesis/connector.PostgreSQLConfig`
- func: `func NewPostgreSQLDB(*testing.T) *gorm.io/gorm.DB`
- func: `func NewRedisContainerClient(*testing.T) *github.com/redis/go-redis/v9.Client`
- func: `func NewRedisContainerConfig(*testing.T) *github.com/ceyewan/genesis/connector.RedisConfig`
- func: `func NewRedisContainerConnector(*testing.T) github.com/ceyewan/genesis/connector.RedisConnector`
- func: `func NewSQLiteConfig() *github.com/ceyewan/genesis/connector.SQLiteConfig`
- func: `func NewSQLiteConnector(*testing.T) github.com/ceyewan/genesis/connector.SQLiteConnector`
- func: `func NewSQLiteDB(*testing.T) *gorm.io/gorm.DB`
- func: `func RequireDocker(*testing.T)`
- type: `type Kit struct{Ctx context.Context; Logger github.com/ceyewan/genesis/clog.Logger; Meter github.com/ceyewan/genesis/metrics.Meter}`

## `trace`

- func: `func HTTPHandler(net/http.Handler, string, ...go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp.Option) net/http.Handler`
- func: `func HTTPTransport(net/http.RoundTripper, ...go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp.Option) *go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp.Transport`
