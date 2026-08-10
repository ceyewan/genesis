# Genesis v1.0.0-rc.2 approved API replacements

This file is consumed by `make api-compat-check` together with
`v1-api-compat-allowlist.md`. Every entry is the exact RC2 replacement for one
reviewed RC1 signature. The check fails if an approved replacement is absent or
drifts again.

## `breaker`

- type: `type Config struct{MaxKeys int "json:\"max_keys\" yaml:\"max_keys\" mapstructure:\"max_keys\""; MaxRequests uint32 "json:\"max_requests\" yaml:\"max_requests\" mapstructure:\"max_requests\""; Interval time.Duration "json:\"interval\" yaml:\"interval\" mapstructure:\"interval\""; Timeout time.Duration "json:\"timeout\" yaml:\"timeout\" mapstructure:\"timeout\""; FailureRatio float64 "json:\"failure_ratio\" yaml:\"failure_ratio\" mapstructure:\"failure_ratio\""; MinimumRequests uint32 "json:\"minimum_requests\" yaml:\"minimum_requests\" mapstructure:\"minimum_requests\""}`

## `cache`

- type: `type DistributedConfig struct{Driver DistributedDriverType "json:\"driver\" yaml:\"driver\" mapstructure:\"driver\""; KeyPrefix string "json:\"key_prefix\" yaml:\"key_prefix\" mapstructure:\"key_prefix\""; Serializer string "json:\"serializer\" yaml:\"serializer\" mapstructure:\"serializer\""; DefaultTTL time.Duration "json:\"default_ttl\" yaml:\"default_ttl\" mapstructure:\"default_ttl\""}`
- type: `type LocalConfig struct{Driver LocalDriverType "json:\"driver\" yaml:\"driver\" mapstructure:\"driver\""; MaxEntries int "json:\"max_entries\" yaml:\"max_entries\" mapstructure:\"max_entries\""; Serializer string "json:\"serializer\" yaml:\"serializer\" mapstructure:\"serializer\""; DefaultTTL time.Duration "json:\"default_ttl\" yaml:\"default_ttl\" mapstructure:\"default_ttl\""}`
- type: `type MultiConfig struct{LocalTTL time.Duration "json:\"local_ttl\" yaml:\"local_ttl\" mapstructure:\"local_ttl\""; BackfillTTL time.Duration "json:\"backfill_ttl\" yaml:\"backfill_ttl\" mapstructure:\"backfill_ttl\""; FailOpenOnLocalError *bool "json:\"fail_open_on_local_error\" yaml:\"fail_open_on_local_error\" mapstructure:\"fail_open_on_local_error\""}`

## `clog`

- type: `type Config struct{Level string "json:\"level\" yaml:\"level\" mapstructure:\"level\""; Format string "json:\"format\" yaml:\"format\" mapstructure:\"format\""; Output string "json:\"output\" yaml:\"output\" mapstructure:\"output\""; EnableColor bool "json:\"enable_color\" yaml:\"enable_color\" mapstructure:\"enable_color\""; AddSource bool "json:\"add_source\" yaml:\"add_source\" mapstructure:\"add_source\""; SourceRoot string "json:\"source_root\" yaml:\"source_root\" mapstructure:\"source_root\""; ServiceName string "json:\"service_name\" yaml:\"service_name\" mapstructure:\"service_name\""; Version string "json:\"version\" yaml:\"version\" mapstructure:\"version\""; InstanceID string "json:\"instance_id\" yaml:\"instance_id\" mapstructure:\"instance_id\""; Environment string "json:\"environment\" yaml:\"environment\" mapstructure:\"environment\""}`

## `dlock`

- type: `type Config struct{Driver DriverType "json:\"driver\" yaml:\"driver\" mapstructure:\"driver\""; Prefix string "json:\"prefix\" yaml:\"prefix\" mapstructure:\"prefix\""; DefaultTTL time.Duration "json:\"default_ttl\" yaml:\"default_ttl\" mapstructure:\"default_ttl\""; RetryInterval time.Duration "json:\"retry_interval\" yaml:\"retry_interval\" mapstructure:\"retry_interval\""}`

## `idem`

- type: `type Config struct{Driver DriverType "json:\"driver\" yaml:\"driver\" mapstructure:\"driver\""; Prefix string "json:\"prefix\" yaml:\"prefix\" mapstructure:\"prefix\""; DefaultTTL time.Duration "json:\"default_ttl\" yaml:\"default_ttl\" mapstructure:\"default_ttl\""; LockTTL time.Duration "json:\"lock_ttl\" yaml:\"lock_ttl\" mapstructure:\"lock_ttl\""; WaitTimeout time.Duration "json:\"wait_timeout\" yaml:\"wait_timeout\" mapstructure:\"wait_timeout\""; WaitInterval time.Duration "json:\"wait_interval\" yaml:\"wait_interval\" mapstructure:\"wait_interval\""}`

## `idgen`

- type: `type AllocatorConfig struct{Driver DriverType "mapstructure:\"driver\" yaml:\"driver\" json:\"driver\""; KeyPrefix string "mapstructure:\"key_prefix\" yaml:\"key_prefix\" json:\"key_prefix\""; MaxID int "mapstructure:\"max_id\" yaml:\"max_id\" json:\"max_id\""; TTL time.Duration "mapstructure:\"ttl\" yaml:\"ttl\" json:\"ttl\""}`
- type: `type GeneratorConfig struct{Mode GeneratorMode "mapstructure:\"mode\" yaml:\"mode\" json:\"mode\""; WorkerID int64 "mapstructure:\"worker_id\" yaml:\"worker_id\" json:\"worker_id\""; DatacenterID int64 "mapstructure:\"datacenter_id\" yaml:\"datacenter_id\" json:\"datacenter_id\""}`
- type: `type SequencerConfig struct{Driver DriverType "mapstructure:\"driver\" yaml:\"driver\" json:\"driver\""; KeyPrefix string "mapstructure:\"key_prefix\" yaml:\"key_prefix\" json:\"key_prefix\""; Step int64 "mapstructure:\"step\" yaml:\"step\" json:\"step\""; MaxValue int64 "mapstructure:\"max_value\" yaml:\"max_value\" json:\"max_value\""; TTL time.Duration "mapstructure:\"ttl\" yaml:\"ttl\" json:\"ttl\""}`

## `ratelimit`

- type: `type Config struct{Driver DriverType "json:\"driver\" yaml:\"driver\" mapstructure:\"driver\""; Standalone *StandaloneConfig "json:\"standalone\" yaml:\"standalone\" mapstructure:\"standalone\""; Distributed *DistributedConfig "json:\"distributed\" yaml:\"distributed\" mapstructure:\"distributed\""}`
- type: `type DistributedConfig struct{Prefix string "json:\"prefix\" yaml:\"prefix\" mapstructure:\"prefix\""}`
- type: `type StandaloneConfig struct{CleanupInterval time.Duration "json:\"cleanup_interval\" yaml:\"cleanup_interval\" mapstructure:\"cleanup_interval\""; IdleTimeout time.Duration "json:\"idle_timeout\" yaml:\"idle_timeout\" mapstructure:\"idle_timeout\""; MaxKeys int "json:\"max_keys\" yaml:\"max_keys\" mapstructure:\"max_keys\""}`

## `registry`

- type: `type Config struct{Namespace string "mapstructure:\"namespace\" yaml:\"namespace\" json:\"namespace\""; DefaultTTL time.Duration "mapstructure:\"default_ttl\" yaml:\"default_ttl\" json:\"default_ttl\""; RetryInterval time.Duration "mapstructure:\"retry_interval\" yaml:\"retry_interval\" json:\"retry_interval\""; LeaseFailureBuffer int "mapstructure:\"lease_failure_buffer\" yaml:\"lease_failure_buffer\" json:\"lease_failure_buffer\""}`
