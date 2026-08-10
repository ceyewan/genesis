# Genesis v1.0.0-rc.1 compatibility exceptions

This file is consumed by `make api-compat-check`. Every entry is the exact
RC1 signature that a reviewed RC2 contract change replaces. Stale entries fail
the check and must be removed.

The `breaker`, `cache`, `clog`, `dlock`, `idem`, `idgen`, `ratelimit`, and `registry` config
types below gain explicit `mapstructure` tags. This makes the documented
`config.Loader` integration work for snake_case keys. Field order and existing
JSON/YAML tags remain unchanged.

`ratelimit.StandaloneConfig` additionally gains `MaxKeys`. The bounded default
prevents attacker-controlled keys from growing the in-memory limiter without a
hard ceiling. Consumers using positional struct literals must migrate to keyed
literals.

## `breaker`

- type: `type Config struct{MaxKeys int "json:\"max_keys\" yaml:\"max_keys\""; MaxRequests uint32 "json:\"max_requests\" yaml:\"max_requests\""; Interval time.Duration "json:\"interval\" yaml:\"interval\""; Timeout time.Duration "json:\"timeout\" yaml:\"timeout\""; FailureRatio float64 "json:\"failure_ratio\" yaml:\"failure_ratio\""; MinimumRequests uint32 "json:\"minimum_requests\" yaml:\"minimum_requests\""}`

## `cache`

- type: `type DistributedConfig struct{Driver DistributedDriverType "json:\"driver\" yaml:\"driver\""; KeyPrefix string "json:\"key_prefix\" yaml:\"key_prefix\""; Serializer string "json:\"serializer\" yaml:\"serializer\""; DefaultTTL time.Duration "json:\"default_ttl\" yaml:\"default_ttl\""}`
- type: `type LocalConfig struct{Driver LocalDriverType "json:\"driver\" yaml:\"driver\""; MaxEntries int "json:\"max_entries\" yaml:\"max_entries\""; Serializer string "json:\"serializer\" yaml:\"serializer\""; DefaultTTL time.Duration "json:\"default_ttl\" yaml:\"default_ttl\""}`
- type: `type MultiConfig struct{LocalTTL time.Duration "json:\"local_ttl\" yaml:\"local_ttl\""; BackfillTTL time.Duration "json:\"backfill_ttl\" yaml:\"backfill_ttl\""; FailOpenOnLocalError *bool "json:\"fail_open_on_local_error\" yaml:\"fail_open_on_local_error\""}`

## `clog`

- type: `type Config struct{Level string "json:\"level\" yaml:\"level\""; Format string "json:\"format\" yaml:\"format\""; Output string "json:\"output\" yaml:\"output\""; EnableColor bool "json:\"enable_color\" yaml:\"enable_color\""; AddSource bool "json:\"add_source\" yaml:\"add_source\""; SourceRoot string "json:\"source_root\" yaml:\"source_root\""; ServiceName string "json:\"service_name\" yaml:\"service_name\""; Version string "json:\"version\" yaml:\"version\""; InstanceID string "json:\"instance_id\" yaml:\"instance_id\""; Environment string "json:\"environment\" yaml:\"environment\""}`

## `dlock`

- type: `type Config struct{Driver DriverType "json:\"driver\" yaml:\"driver\""; Prefix string "json:\"prefix\" yaml:\"prefix\""; DefaultTTL time.Duration "json:\"default_ttl\" yaml:\"default_ttl\""; RetryInterval time.Duration "json:\"retry_interval\" yaml:\"retry_interval\""}`

## `idem`

- type: `type Config struct{Driver DriverType "json:\"driver\" yaml:\"driver\""; Prefix string "json:\"prefix\" yaml:\"prefix\""; DefaultTTL time.Duration "json:\"default_ttl\" yaml:\"default_ttl\""; LockTTL time.Duration "json:\"lock_ttl\" yaml:\"lock_ttl\""; WaitTimeout time.Duration "json:\"wait_timeout\" yaml:\"wait_timeout\""; WaitInterval time.Duration "json:\"wait_interval\" yaml:\"wait_interval\""}`

## `idgen`

- type: `type AllocatorConfig struct{Driver DriverType "yaml:\"driver\" json:\"driver\""; KeyPrefix string "yaml:\"key_prefix\" json:\"key_prefix\""; MaxID int "yaml:\"max_id\" json:\"max_id\""; TTL time.Duration "yaml:\"ttl\" json:\"ttl\""}`
- type: `type GeneratorConfig struct{Mode GeneratorMode "yaml:\"mode\" json:\"mode\""; WorkerID int64 "yaml:\"worker_id\" json:\"worker_id\""; DatacenterID int64 "yaml:\"datacenter_id\" json:\"datacenter_id\""}`
- type: `type SequencerConfig struct{Driver DriverType "yaml:\"driver\" json:\"driver\""; KeyPrefix string "yaml:\"key_prefix\" json:\"key_prefix\""; Step int64 "yaml:\"step\" json:\"step\""; MaxValue int64 "yaml:\"max_value\" json:\"max_value\""; TTL time.Duration "yaml:\"ttl\" json:\"ttl\""}`

## `ratelimit`

- type: `type Config struct{Driver DriverType "json:\"driver\" yaml:\"driver\""; Standalone *StandaloneConfig "json:\"standalone\" yaml:\"standalone\""; Distributed *DistributedConfig "json:\"distributed\" yaml:\"distributed\""}`
- type: `type DistributedConfig struct{Prefix string "json:\"prefix\" yaml:\"prefix\""}`
- type: `type StandaloneConfig struct{CleanupInterval time.Duration "json:\"cleanup_interval\" yaml:\"cleanup_interval\""; IdleTimeout time.Duration "json:\"idle_timeout\" yaml:\"idle_timeout\""}`

## `registry`

- type: `type Config struct{Namespace string "yaml:\"namespace\" json:\"namespace\""; DefaultTTL time.Duration "yaml:\"default_ttl\" json:\"default_ttl\""; RetryInterval time.Duration "yaml:\"retry_interval\" json:\"retry_interval\""; LeaseFailureBuffer int "yaml:\"lease_failure_buffer\" json:\"lease_failure_buffer\""}`
