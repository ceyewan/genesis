# Genesis pre-v1 approved API replacements

This file is consumed by `make api-compat-check` together with
`compat-allowlist.md`. Every entry is the exact approved pre-v1
replacement for one reviewed RC1 signature. The check fails if an approved
replacement is absent or drifts again.

## `auth`

- type: `type Config struct{SecretKey string "mapstructure:\"secret_key\" json:\"secret_key\" yaml:\"secret_key\""; SigningMethod string "mapstructure:\"signing_method\" json:\"signing_method\" yaml:\"signing_method\""; Issuer string "mapstructure:\"issuer\" json:\"issuer\" yaml:\"issuer\""; Audience []string "mapstructure:\"audience\" json:\"audience\" yaml:\"audience\""; AccessTokenTTL time.Duration "mapstructure:\"access_token_ttl\" json:\"access_token_ttl\" yaml:\"access_token_ttl\""; RefreshTokenTTL time.Duration "mapstructure:\"refresh_token_ttl\" json:\"refresh_token_ttl\" yaml:\"refresh_token_ttl\""; TokenLookup string "mapstructure:\"token_lookup\" json:\"token_lookup\" yaml:\"token_lookup\""; TokenHeadName string "mapstructure:\"token_head_name\" json:\"token_head_name\" yaml:\"token_head_name\""}`

## `config`

- type: `type Config struct{Name string "json:\"name\" yaml:\"name\" mapstructure:\"name\""; Paths []string "json:\"paths\" yaml:\"paths\" mapstructure:\"paths\""; FileType string "json:\"file_type\" yaml:\"file_type\" mapstructure:\"file_type\""; EnvPrefix string "json:\"env_prefix\" yaml:\"env_prefix\" mapstructure:\"env_prefix\""}`

## `breaker`

- type: `type Config struct{MaxKeys int "json:\"max_keys\" yaml:\"max_keys\" mapstructure:\"max_keys\""; MaxRequests uint32 "json:\"max_requests\" yaml:\"max_requests\" mapstructure:\"max_requests\""; Interval time.Duration "json:\"interval\" yaml:\"interval\" mapstructure:\"interval\""; Timeout time.Duration "json:\"timeout\" yaml:\"timeout\" mapstructure:\"timeout\""; FailureRatio float64 "json:\"failure_ratio\" yaml:\"failure_ratio\" mapstructure:\"failure_ratio\""; MinimumRequests uint32 "json:\"minimum_requests\" yaml:\"minimum_requests\" mapstructure:\"minimum_requests\""}`

## `cache`

- type: `type DistributedConfig struct{Driver DistributedDriverType "json:\"driver\" yaml:\"driver\" mapstructure:\"driver\""; KeyPrefix string "json:\"key_prefix\" yaml:\"key_prefix\" mapstructure:\"key_prefix\""; Serializer string "json:\"serializer\" yaml:\"serializer\" mapstructure:\"serializer\""; DefaultTTL time.Duration "json:\"default_ttl\" yaml:\"default_ttl\" mapstructure:\"default_ttl\""}`
- type: `type LocalConfig struct{Driver LocalDriverType "json:\"driver\" yaml:\"driver\" mapstructure:\"driver\""; MaxEntries int "json:\"max_entries\" yaml:\"max_entries\" mapstructure:\"max_entries\""; Serializer string "json:\"serializer\" yaml:\"serializer\" mapstructure:\"serializer\""; DefaultTTL time.Duration "json:\"default_ttl\" yaml:\"default_ttl\" mapstructure:\"default_ttl\""}`
- type: `type MultiConfig struct{LocalTTL time.Duration "json:\"local_ttl\" yaml:\"local_ttl\" mapstructure:\"local_ttl\""; BackfillTTL time.Duration "json:\"backfill_ttl\" yaml:\"backfill_ttl\" mapstructure:\"backfill_ttl\""; FailOpenOnLocalError *bool "json:\"fail_open_on_local_error\" yaml:\"fail_open_on_local_error\" mapstructure:\"fail_open_on_local_error\""}`

## `clog`

- type: `type Config struct{Level string "json:\"level\" yaml:\"level\" mapstructure:\"level\""; Format string "json:\"format\" yaml:\"format\" mapstructure:\"format\""; Output string "json:\"output\" yaml:\"output\" mapstructure:\"output\""; EnableColor bool "json:\"enable_color\" yaml:\"enable_color\" mapstructure:\"enable_color\""; AddSource bool "json:\"add_source\" yaml:\"add_source\" mapstructure:\"add_source\""; SourceRoot string "json:\"source_root\" yaml:\"source_root\" mapstructure:\"source_root\""; ServiceName string "json:\"service_name\" yaml:\"service_name\" mapstructure:\"service_name\""; Version string "json:\"version\" yaml:\"version\" mapstructure:\"version\""; InstanceID string "json:\"instance_id\" yaml:\"instance_id\" mapstructure:\"instance_id\""; Environment string "json:\"environment\" yaml:\"environment\" mapstructure:\"environment\""}`

## `connector`

- type: `type RedisConfig struct{Name string "mapstructure:\"name\" json:\"name\" yaml:\"name\""; Addr string "mapstructure:\"addr\" json:\"addr\" yaml:\"addr\""; Username string "mapstructure:\"username\" json:\"username\" yaml:\"username\""; Password string "mapstructure:\"password\" json:\"password\" yaml:\"password\""; DB int "mapstructure:\"db\" json:\"db\" yaml:\"db\""; PoolSize int "mapstructure:\"pool_size\" json:\"pool_size\" yaml:\"pool_size\""; MinIdleConns int "mapstructure:\"min_idle_conns\" json:\"min_idle_conns\" yaml:\"min_idle_conns\""; DialTimeout time.Duration "mapstructure:\"dial_timeout\" json:\"dial_timeout\" yaml:\"dial_timeout\""; ReadTimeout time.Duration "mapstructure:\"read_timeout\" json:\"read_timeout\" yaml:\"read_timeout\""; WriteTimeout time.Duration "mapstructure:\"write_timeout\" json:\"write_timeout\" yaml:\"write_timeout\""; EnableTracing bool "mapstructure:\"enable_tracing\" json:\"enable_tracing\" yaml:\"enable_tracing\""}`

## `dlock`

- type: `type Config struct{Driver DriverType "json:\"driver\" yaml:\"driver\" mapstructure:\"driver\""; Prefix string "json:\"prefix\" yaml:\"prefix\" mapstructure:\"prefix\""; DefaultTTL time.Duration "json:\"default_ttl\" yaml:\"default_ttl\" mapstructure:\"default_ttl\""; RetryInterval time.Duration "json:\"retry_interval\" yaml:\"retry_interval\" mapstructure:\"retry_interval\""}`

## `idem`

- type: `type Config struct{Driver DriverType "json:\"driver\" yaml:\"driver\" mapstructure:\"driver\""; Prefix string "json:\"prefix\" yaml:\"prefix\" mapstructure:\"prefix\""; DefaultTTL time.Duration "json:\"default_ttl\" yaml:\"default_ttl\" mapstructure:\"default_ttl\""; LockTTL time.Duration "json:\"lock_ttl\" yaml:\"lock_ttl\" mapstructure:\"lock_ttl\""; WaitTimeout time.Duration "json:\"wait_timeout\" yaml:\"wait_timeout\" mapstructure:\"wait_timeout\""; WaitInterval time.Duration "json:\"wait_interval\" yaml:\"wait_interval\" mapstructure:\"wait_interval\""}`

## `idgen`

- type: `type AllocatorConfig struct{Driver DriverType "mapstructure:\"driver\" yaml:\"driver\" json:\"driver\""; KeyPrefix string "mapstructure:\"key_prefix\" yaml:\"key_prefix\" json:\"key_prefix\""; MaxID int "mapstructure:\"max_id\" yaml:\"max_id\" json:\"max_id\""; TTL time.Duration "mapstructure:\"ttl\" yaml:\"ttl\" json:\"ttl\""}`
- type: `type GeneratorConfig struct{Mode GeneratorMode "mapstructure:\"mode\" yaml:\"mode\" json:\"mode\""; WorkerID int64 "mapstructure:\"worker_id\" yaml:\"worker_id\" json:\"worker_id\""; DatacenterID int64 "mapstructure:\"datacenter_id\" yaml:\"datacenter_id\" json:\"datacenter_id\""}`
- type: `type SequencerConfig struct{Driver DriverType "mapstructure:\"driver\" yaml:\"driver\" json:\"driver\""; KeyPrefix string "mapstructure:\"key_prefix\" yaml:\"key_prefix\" json:\"key_prefix\""; Step int64 "mapstructure:\"step\" yaml:\"step\" json:\"step\""; MaxValue int64 "mapstructure:\"max_value\" yaml:\"max_value\" json:\"max_value\""; TTL time.Duration "mapstructure:\"ttl\" yaml:\"ttl\" json:\"ttl\""}`

## `metrics`

- func: `func New(*Config, ...Option) (Meter, error)`
- type: `type Config struct{ServiceName string "mapstructure:\"service_name\" json:\"service_name\" yaml:\"service_name\""; Version string "mapstructure:\"version\" json:\"version\" yaml:\"version\""; InstanceID string "mapstructure:\"instance_id\" json:\"instance_id\" yaml:\"instance_id\""; Environment string "mapstructure:\"environment\" json:\"environment\" yaml:\"environment\""; ListenAddress string "mapstructure:\"listen_address\" json:\"listen_address\" yaml:\"listen_address\""; Port int "mapstructure:\"port\" json:\"port\" yaml:\"port\""; Path string "mapstructure:\"path\" json:\"path\" yaml:\"path\""; EnableRuntime bool "mapstructure:\"enable_runtime\" json:\"enable_runtime\" yaml:\"enable_runtime\""; CardinalityLimit int "json:\"cardinality_limit\" yaml:\"cardinality_limit\" mapstructure:\"cardinality_limit\""; ServerErrors chan<- error "mapstructure:\"-\" json:\"-\" yaml:\"-\""}`
- type: `type GRPCServerMetricsConfig struct{Service string "json:\"service\" yaml:\"service\" mapstructure:\"service\""; RequestTotalName string "json:\"request_total_name\" yaml:\"request_total_name\" mapstructure:\"request_total_name\""; RequestDurationName string "json:\"request_duration_name\" yaml:\"request_duration_name\" mapstructure:\"request_duration_name\""; DurationBuckets []float64 "json:\"duration_buckets\" yaml:\"duration_buckets\" mapstructure:\"duration_buckets\""; StaticLabels []Label "json:\"static_labels\" yaml:\"static_labels\" mapstructure:\"static_labels\""}`
- type: `type HTTPServerMetricsConfig struct{Service string "json:\"service\" yaml:\"service\" mapstructure:\"service\""; RequestTotalName string "json:\"request_total_name\" yaml:\"request_total_name\" mapstructure:\"request_total_name\""; RequestDurationName string "json:\"request_duration_name\" yaml:\"request_duration_name\" mapstructure:\"request_duration_name\""; DurationBuckets []float64 "json:\"duration_buckets\" yaml:\"duration_buckets\" mapstructure:\"duration_buckets\""; StaticLabels []Label "json:\"static_labels\" yaml:\"static_labels\" mapstructure:\"static_labels\""}`
- type: `type Gauge interface{Set(context.Context, float64, ...Label)}`
- type: `type Meter interface{Counter(string, string, ...MetricOption) (Counter, error); Gauge(string, string, ...MetricOption) (Gauge, error); Histogram(string, string, ...MetricOption) (Histogram, error); Shutdown(context.Context) error; UpDownCounter(string, string, ...MetricOption) (UpDownCounter, error)}`

## `mq`

- type: `type RetryConfig struct{MaxRetries int "json:\"max_retries\" yaml:\"max_retries\" mapstructure:\"max_retries\""; InitialBackoff time.Duration "json:\"initial_backoff\" yaml:\"initial_backoff\" mapstructure:\"initial_backoff\""; MaxBackoff time.Duration "json:\"max_backoff\" yaml:\"max_backoff\" mapstructure:\"max_backoff\""; Multiplier float64 "json:\"multiplier\" yaml:\"multiplier\" mapstructure:\"multiplier\""}`

## `ratelimit`

- type: `type Limit struct{Rate float64 "json:\"rate\" yaml:\"rate\" mapstructure:\"rate\""; Burst int "json:\"burst\" yaml:\"burst\" mapstructure:\"burst\""}`

- type: `type Config struct{Driver DriverType "json:\"driver\" yaml:\"driver\" mapstructure:\"driver\""; Standalone *StandaloneConfig "json:\"standalone\" yaml:\"standalone\" mapstructure:\"standalone\""; Distributed *DistributedConfig "json:\"distributed\" yaml:\"distributed\" mapstructure:\"distributed\""}`
- type: `type DistributedConfig struct{Prefix string "json:\"prefix\" yaml:\"prefix\" mapstructure:\"prefix\""}`
- type: `type StandaloneConfig struct{CleanupInterval time.Duration "json:\"cleanup_interval\" yaml:\"cleanup_interval\" mapstructure:\"cleanup_interval\""; IdleTimeout time.Duration "json:\"idle_timeout\" yaml:\"idle_timeout\" mapstructure:\"idle_timeout\""; MaxKeys int "json:\"max_keys\" yaml:\"max_keys\" mapstructure:\"max_keys\""}`

## `registry`

- func: `func New(*Config, ...Option) (Registry, error)`

- type: `type Config struct{Namespace string "mapstructure:\"namespace\" yaml:\"namespace\" json:\"namespace\""; DefaultTTL time.Duration "mapstructure:\"default_ttl\" yaml:\"default_ttl\" json:\"default_ttl\""; RetryInterval time.Duration "mapstructure:\"retry_interval\" yaml:\"retry_interval\" json:\"retry_interval\""; LeaseFailureBuffer int "mapstructure:\"lease_failure_buffer\" yaml:\"lease_failure_buffer\" json:\"lease_failure_buffer\""}`

## `trace`

- const: `const AttrMessagingConsumerGroup untyped string = "messaging.consumer.group.name"`
- const: `const AttrMessagingDestination untyped string = "messaging.destination.name"`
- const: `const AttrMessagingOperation untyped string = "messaging.operation.name"`
