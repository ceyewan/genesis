# Genesis pre-v1 compatibility exceptions

This file is consumed by `make api-compat-check`. Every entry is the exact
RC1 signature that a reviewed pre-v1 contract change replaces. Stale entries
fail the check and must be removed.

After RC2, `metrics.New` gains the same functional-option shape used by the
other Genesis component constructors so applications can inject the shared
logger. The direct `metrics.New(cfg)` call remains valid, but the exact Go
function type changes and is therefore recorded as an approved pre-v1 break.

The three exported messaging attribute constants move from the retired
OpenTelemetry messaging keys to the current semantic-convention names. Their
constant values are part of the exported API, so the replacements are recorded
explicitly instead of being hidden by a regenerated inventory.

The `breaker`, `cache`, `clog`, `dlock`, `idem`, `idgen`, `ratelimit`, and `registry` config
types below gain explicit `mapstructure` tags. This makes the documented
`config.Loader` integration work for snake_case keys. Field order and existing
JSON/YAML tags remain unchanged.

`ratelimit.StandaloneConfig` additionally gains `MaxKeys`. The bounded default
prevents attacker-controlled keys from growing the in-memory limiter without a
hard ceiling. Consumers using positional struct literals must migrate to keyed
literals.

The audit follow-up approves the config tags below, the unified Registry
constructor, and the Gauge/UpDownCounter split. Gauge increment/decrement methods
are removed explicitly in compat-removals.md; Meter implementers must add
UpDownCounter. Metrics also gains a bounded cardinality configuration.

## `auth`

- type: `type Config struct{SecretKey string "mapstructure:\"secret_key\""; SigningMethod string "mapstructure:\"signing_method\""; Issuer string "mapstructure:\"issuer\""; Audience []string "mapstructure:\"audience\""; AccessTokenTTL time.Duration "mapstructure:\"access_token_ttl\""; RefreshTokenTTL time.Duration "mapstructure:\"refresh_token_ttl\""; TokenLookup string "mapstructure:\"token_lookup\""; TokenHeadName string "mapstructure:\"token_head_name\""}`

## `config`

- type: `type Config struct{Name string; Paths []string; FileType string; EnvPrefix string}`

## `breaker`

- type: `type Config struct{MaxKeys int "json:\"max_keys\" yaml:\"max_keys\""; MaxRequests uint32 "json:\"max_requests\" yaml:\"max_requests\""; Interval time.Duration "json:\"interval\" yaml:\"interval\""; Timeout time.Duration "json:\"timeout\" yaml:\"timeout\""; FailureRatio float64 "json:\"failure_ratio\" yaml:\"failure_ratio\""; MinimumRequests uint32 "json:\"minimum_requests\" yaml:\"minimum_requests\""}`

## `cache`

- type: `type DistributedConfig struct{Driver DistributedDriverType "json:\"driver\" yaml:\"driver\""; KeyPrefix string "json:\"key_prefix\" yaml:\"key_prefix\""; Serializer string "json:\"serializer\" yaml:\"serializer\""; DefaultTTL time.Duration "json:\"default_ttl\" yaml:\"default_ttl\""}`
- type: `type LocalConfig struct{Driver LocalDriverType "json:\"driver\" yaml:\"driver\""; MaxEntries int "json:\"max_entries\" yaml:\"max_entries\""; Serializer string "json:\"serializer\" yaml:\"serializer\""; DefaultTTL time.Duration "json:\"default_ttl\" yaml:\"default_ttl\""}`
- type: `type MultiConfig struct{LocalTTL time.Duration "json:\"local_ttl\" yaml:\"local_ttl\""; BackfillTTL time.Duration "json:\"backfill_ttl\" yaml:\"backfill_ttl\""; FailOpenOnLocalError *bool "json:\"fail_open_on_local_error\" yaml:\"fail_open_on_local_error\""}`

## `clog`

- type: `type Config struct{Level string "json:\"level\" yaml:\"level\""; Format string "json:\"format\" yaml:\"format\""; Output string "json:\"output\" yaml:\"output\""; EnableColor bool "json:\"enable_color\" yaml:\"enable_color\""; AddSource bool "json:\"add_source\" yaml:\"add_source\""; SourceRoot string "json:\"source_root\" yaml:\"source_root\""; ServiceName string "json:\"service_name\" yaml:\"service_name\""; Version string "json:\"version\" yaml:\"version\""; InstanceID string "json:\"instance_id\" yaml:\"instance_id\""; Environment string "json:\"environment\" yaml:\"environment\""}`

## `connector`

Redis ACL support adds `RedisConfig.Username` before stable v1. Keyed literals
keep their behavior; positional literals must migrate to keyed fields. The
new transport option does not alter the constructor or Connector interfaces.

- type: `type RedisConfig struct{Name string "mapstructure:\"name\" json:\"name\" yaml:\"name\""; Addr string "mapstructure:\"addr\" json:\"addr\" yaml:\"addr\""; Password string "mapstructure:\"password\" json:\"password\" yaml:\"password\""; DB int "mapstructure:\"db\" json:\"db\" yaml:\"db\""; PoolSize int "mapstructure:\"pool_size\" json:\"pool_size\" yaml:\"pool_size\""; MinIdleConns int "mapstructure:\"min_idle_conns\" json:\"min_idle_conns\" yaml:\"min_idle_conns\""; DialTimeout time.Duration "mapstructure:\"dial_timeout\" json:\"dial_timeout\" yaml:\"dial_timeout\""; ReadTimeout time.Duration "mapstructure:\"read_timeout\" json:\"read_timeout\" yaml:\"read_timeout\""; WriteTimeout time.Duration "mapstructure:\"write_timeout\" json:\"write_timeout\" yaml:\"write_timeout\""; EnableTracing bool "mapstructure:\"enable_tracing\" json:\"enable_tracing\" yaml:\"enable_tracing\""}`

## `dlock`

- type: `type Config struct{Driver DriverType "json:\"driver\" yaml:\"driver\""; Prefix string "json:\"prefix\" yaml:\"prefix\""; DefaultTTL time.Duration "json:\"default_ttl\" yaml:\"default_ttl\""; RetryInterval time.Duration "json:\"retry_interval\" yaml:\"retry_interval\""}`

## `idem`

- type: `type Config struct{Driver DriverType "json:\"driver\" yaml:\"driver\""; Prefix string "json:\"prefix\" yaml:\"prefix\""; DefaultTTL time.Duration "json:\"default_ttl\" yaml:\"default_ttl\""; LockTTL time.Duration "json:\"lock_ttl\" yaml:\"lock_ttl\""; WaitTimeout time.Duration "json:\"wait_timeout\" yaml:\"wait_timeout\""; WaitInterval time.Duration "json:\"wait_interval\" yaml:\"wait_interval\""}`

## `idgen`

- type: `type AllocatorConfig struct{Driver DriverType "yaml:\"driver\" json:\"driver\""; KeyPrefix string "yaml:\"key_prefix\" json:\"key_prefix\""; MaxID int "yaml:\"max_id\" json:\"max_id\""; TTL time.Duration "yaml:\"ttl\" json:\"ttl\""}`
- type: `type GeneratorConfig struct{Mode GeneratorMode "yaml:\"mode\" json:\"mode\""; WorkerID int64 "yaml:\"worker_id\" json:\"worker_id\""; DatacenterID int64 "yaml:\"datacenter_id\" json:\"datacenter_id\""}`
- type: `type SequencerConfig struct{Driver DriverType "yaml:\"driver\" json:\"driver\""; KeyPrefix string "yaml:\"key_prefix\" json:\"key_prefix\""; Step int64 "yaml:\"step\" json:\"step\""; MaxValue int64 "yaml:\"max_value\" json:\"max_value\""; TTL time.Duration "yaml:\"ttl\" json:\"ttl\""}`

## `metrics`

- func: `func New(*Config) (Meter, error)`
- type: `type Config struct{ServiceName string "mapstructure:\"service_name\" json:\"service_name\" yaml:\"service_name\""; Version string "mapstructure:\"version\" json:\"version\" yaml:\"version\""; InstanceID string "mapstructure:\"instance_id\" json:\"instance_id\" yaml:\"instance_id\""; Environment string "mapstructure:\"environment\" json:\"environment\" yaml:\"environment\""; ListenAddress string "mapstructure:\"listen_address\" json:\"listen_address\" yaml:\"listen_address\""; Port int "mapstructure:\"port\" json:\"port\" yaml:\"port\""; Path string "mapstructure:\"path\" json:\"path\" yaml:\"path\""; EnableRuntime bool "mapstructure:\"enable_runtime\" json:\"enable_runtime\" yaml:\"enable_runtime\""; ServerErrors chan<- error "mapstructure:\"-\" json:\"-\" yaml:\"-\""}`
- type: `type GRPCServerMetricsConfig struct{Service string; RequestTotalName string; RequestDurationName string; DurationBuckets []float64; StaticLabels []Label}`
- type: `type HTTPServerMetricsConfig struct{Service string; RequestTotalName string; RequestDurationName string; DurationBuckets []float64; StaticLabels []Label}`
- type: `type Gauge interface{Dec(context.Context, ...Label); Inc(context.Context, ...Label); Set(context.Context, float64, ...Label)}`
- type: `type Meter interface{Counter(string, string, ...MetricOption) (Counter, error); Gauge(string, string, ...MetricOption) (Gauge, error); Histogram(string, string, ...MetricOption) (Histogram, error); Shutdown(context.Context) error}`

## `mq`

- type: `type RetryConfig struct{MaxRetries int; InitialBackoff time.Duration; MaxBackoff time.Duration; Multiplier float64}`

## `ratelimit`

- type: `type Limit struct{Rate float64; Burst int}`

- type: `type Config struct{Driver DriverType "json:\"driver\" yaml:\"driver\""; Standalone *StandaloneConfig "json:\"standalone\" yaml:\"standalone\""; Distributed *DistributedConfig "json:\"distributed\" yaml:\"distributed\""}`
- type: `type DistributedConfig struct{Prefix string "json:\"prefix\" yaml:\"prefix\""}`
- type: `type StandaloneConfig struct{CleanupInterval time.Duration "json:\"cleanup_interval\" yaml:\"cleanup_interval\""; IdleTimeout time.Duration "json:\"idle_timeout\" yaml:\"idle_timeout\""}`

## `registry`

- func: `func New(github.com/ceyewan/genesis/connector.EtcdConnector, *Config, ...Option) (Registry, error)`

- type: `type Config struct{Namespace string "yaml:\"namespace\" json:\"namespace\""; DefaultTTL time.Duration "yaml:\"default_ttl\" json:\"default_ttl\""; RetryInterval time.Duration "yaml:\"retry_interval\" json:\"retry_interval\""; LeaseFailureBuffer int "yaml:\"lease_failure_buffer\" json:\"lease_failure_buffer\""}`

## `trace`

- const: `const AttrMessagingConsumerGroup untyped string = "messaging.consumer.group"`
- const: `const AttrMessagingDestination untyped string = "messaging.destination"`
- const: `const AttrMessagingOperation untyped string = "messaging.operation"`
