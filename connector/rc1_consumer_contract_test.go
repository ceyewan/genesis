// Package contract_test exercises the v1.0.0-rc.1 surface exactly as an
// external consumer sees it. The protected behavior is sourced from
// docs/v1-api-decisions.md and the Resonance integration call sites.
package connector_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/ceyewan/genesis/auth"
	"github.com/ceyewan/genesis/cache"
	"github.com/ceyewan/genesis/config"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/db"
	"github.com/ceyewan/genesis/dlock"
	"github.com/ceyewan/genesis/idem"
	"github.com/ceyewan/genesis/idgen"
	"github.com/ceyewan/genesis/metrics"
	"github.com/ceyewan/genesis/mq"
	"github.com/ceyewan/genesis/ratelimit"
	genesistrace "github.com/ceyewan/genesis/trace"
)

func TestRC1ConsumerConstructorsRejectNilConfiguration(t *testing.T) {
	tests := []struct {
		name      string
		construct func() error
		class     error
	}{
		{name: "auth", construct: func() error { _, err := auth.New(nil); return err }, class: auth.ErrInvalidConfig},
		{name: "cache local", construct: func() error { _, err := cache.NewLocal(nil); return err }, class: cache.ErrInvalidConfig},
		{name: "cache distributed", construct: func() error { _, err := cache.NewDistributed(nil); return err }, class: cache.ErrInvalidConfig},
		{name: "connector MySQL", construct: func() error { _, err := connector.NewMySQL(nil); return err }, class: connector.ErrConfig},
		{name: "connector PostgreSQL", construct: func() error { _, err := connector.NewPostgreSQL(nil); return err }, class: connector.ErrConfig},
		{name: "connector SQLite", construct: func() error { _, err := connector.NewSQLite(nil); return err }, class: connector.ErrConfig},
		{name: "connector Redis", construct: func() error { _, err := connector.NewRedis(nil); return err }, class: connector.ErrConfig},
		{name: "connector etcd", construct: func() error { _, err := connector.NewEtcd(nil); return err }, class: connector.ErrConfig},
		{name: "connector NATS", construct: func() error { _, err := connector.NewNATS(nil); return err }, class: connector.ErrConfig},
		{name: "connector Kafka", construct: func() error { _, err := connector.NewKafka(nil); return err }, class: connector.ErrConfig},
		{name: "dlock", construct: func() error { _, err := dlock.New(nil); return err }, class: dlock.ErrInvalidConfig},
		{name: "idem", construct: func() error { _, err := idem.New(nil); return err }, class: idem.ErrConfigNil},
		{name: "idgen allocator", construct: func() error { _, err := idgen.NewAllocator(nil); return err }, class: idgen.ErrInvalidInput},
		{name: "idgen generator", construct: func() error { _, err := idgen.NewGenerator(nil); return err }, class: idgen.ErrInvalidInput},
		{name: "idgen sequencer", construct: func() error { _, err := idgen.NewSequencer(nil); return err }, class: idgen.ErrInvalidInput},
		{name: "metrics", construct: func() error { _, err := metrics.New(nil); return err }, class: metrics.ErrInvalidConfig},
		{name: "metrics HTTP helper", construct: func() error { _, err := metrics.NewHTTPServerMetrics(metrics.Discard(), nil); return err }, class: metrics.ErrInvalidConfig},
		{name: "metrics HTTP helper meter", construct: func() error {
			_, err := metrics.NewHTTPServerMetrics(nil, metrics.DefaultHTTPServerMetricsConfig("contract"))
			return err
		}, class: metrics.ErrInvalidConfig},
		{name: "metrics gRPC helper", construct: func() error { _, err := metrics.NewGRPCServerMetrics(metrics.Discard(), nil); return err }, class: metrics.ErrInvalidConfig},
		{name: "metrics gRPC helper meter", construct: func() error {
			_, err := metrics.NewGRPCServerMetrics(nil, metrics.DefaultGRPCServerMetricsConfig("contract"))
			return err
		}, class: metrics.ErrInvalidConfig},
		{name: "mq", construct: func() error { _, err := mq.New(nil); return err }, class: mq.ErrInvalidConfig},
		{name: "ratelimit", construct: func() error { _, err := ratelimit.New(nil); return err }, class: ratelimit.ErrConfigNil},
		{name: "trace", construct: func() error { _, err := genesistrace.Init(nil); return err }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.construct()
			if err == nil {
				t.Fatal("constructor accepted a nil configuration")
			}
			if tt.class != nil && !errors.Is(err, tt.class) {
				t.Fatalf("error = %v, want errors.Is(_, %v)", err, tt.class)
			}
		})
	}
}

func TestRC1ConfigurationMappingPreservesDurationUnitsAndNestedUnderscores(t *testing.T) {
	t.Setenv("RC1_CONTRACT_DATABASE_MAX_OPEN_CONNS", "42")
	t.Setenv("RC1_CONTRACT_CACHE_DEFAULT_TTL", "750ms")

	loader, err := config.New(&config.Config{Paths: []string{t.TempDir()}, EnvPrefix: "RC1_CONTRACT"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := loader.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	})
	if err := loader.Load(context.Background()); err != nil {
		t.Fatal(err)
	}

	var got struct {
		Database struct {
			MaxOpenConns int `mapstructure:"max_open_conns"`
		} `mapstructure:"database"`
		Cache struct {
			DefaultTTL time.Duration `mapstructure:"default_ttl"`
		} `mapstructure:"cache"`
	}
	if err := loader.Unmarshal(&got); err != nil {
		t.Fatal(err)
	}
	if got.Database.MaxOpenConns != 42 {
		t.Fatalf("max_open_conns = %d, want 42", got.Database.MaxOpenConns)
	}
	if got.Cache.DefaultTTL != 750*time.Millisecond {
		t.Fatalf("default_ttl = %v, want 750ms", got.Cache.DefaultTTL)
	}
}

func TestRC1BorrowerCloseDoesNotCloseCallerOwnedConnector(t *testing.T) {
	conn, err := connector.NewSQLite(&connector.SQLiteConfig{Path: "file::memory:?cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := conn.Close(); closeErr != nil {
			t.Errorf("connector Close() error = %v", closeErr)
		}
	})

	database, err := db.New(&db.Config{Driver: "sqlite"}, db.WithSQLiteConnector(conn))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("first DB Close() error = %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("second DB Close() error = %v", err)
	}
	if err := conn.HealthCheck(context.Background()); err != nil {
		t.Fatalf("borrowed connector was closed by DB: %v", err)
	}

	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if err := conn.HealthCheck(context.Background()); !errors.Is(err, connector.ErrClientNil) {
		t.Fatalf("HealthCheck() after owner Close = %v, want ErrClientNil", err)
	}
}

func TestRC1StandaloneRateLimitCloseIsConcurrentAndTerminal(t *testing.T) {
	limiter, err := ratelimit.New(&ratelimit.Config{
		Driver: ratelimit.DriverStandalone,
		Standalone: &ratelimit.StandaloneConfig{
			CleanupInterval: time.Hour,
			IdleTimeout:     time.Hour,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	errCh := make(chan error, 8)
	for range 8 {
		go func() { errCh <- limiter.Close() }()
	}
	for range 8 {
		if err := <-errCh; err != nil {
			t.Fatalf("concurrent Close() error = %v", err)
		}
	}
	_, err = limiter.Allow(context.Background(), "consumer", ratelimit.Limit{Rate: 1, Burst: 1})
	if !errors.Is(err, ratelimit.ErrLimiterClosed) {
		t.Fatalf("Allow() after Close = %v, want ErrLimiterClosed", err)
	}
}

func TestRC1ErrorsPreservePublicClassification(t *testing.T) {
	if _, err := connector.NewRedis(&connector.RedisConfig{
		Addr:        "127.0.0.1:6379",
		DialTimeout: -time.Second,
	}); !errors.Is(err, connector.ErrConfig) {
		t.Fatalf("NewRedis() error = %v, want ErrConfig", err)
	}
	if _, err := cache.NewLocal(&cache.LocalConfig{DefaultTTL: -time.Second}); !errors.Is(err, cache.ErrInvalidTTL) {
		t.Fatalf("NewLocal() error = %v, want ErrInvalidTTL", err)
	}
	if _, err := cache.NewLocal(nil); !errors.Is(err, cache.ErrInvalidConfig) {
		t.Fatalf("NewLocal(nil) error = %v, want ErrInvalidConfig", err)
	}
	if _, err := cache.NewLocal(&cache.LocalConfig{Driver: "unknown"}); !errors.Is(err, cache.ErrInvalidConfig) {
		t.Fatalf("NewLocal(unsupported driver) error = %v, want ErrInvalidConfig", err)
	}
	if _, err := cache.NewDistributed(&cache.DistributedConfig{Driver: "unknown"}); !errors.Is(err, cache.ErrInvalidConfig) {
		t.Fatalf("NewDistributed(unsupported driver) error = %v, want ErrInvalidConfig", err)
	}
	if _, err := db.New(&db.Config{Driver: "unknown"}); !errors.Is(err, db.ErrInvalidConfig) {
		t.Fatalf("db.New() error = %v, want ErrInvalidConfig", err)
	}
	if _, err := mq.New(&mq.Config{Driver: mq.DriverNATSJetStream}); !errors.Is(err, connector.ErrClientNil) {
		t.Fatalf("mq.New(NATS without connector) error = %v, want connector.ErrClientNil", err)
	}
	if _, err := mq.New(&mq.Config{Driver: mq.DriverRedisStream}); !errors.Is(err, connector.ErrClientNil) {
		t.Fatalf("mq.New(Redis without connector) error = %v, want connector.ErrClientNil", err)
	}
}

func TestRC1TracePropagationRoundTripPreservesAssociation(t *testing.T) {
	shutdown, err := genesistrace.InstallLocalProvider("rc1-consumer-contract")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if shutdownErr := shutdown(ctx); shutdownErr != nil {
			t.Errorf("trace shutdown error = %v", shutdownErr)
		}
	})

	ctx, span := otel.Tracer("contract-test").Start(context.Background(), "produce")
	carrier := map[string]string{}
	genesistrace.Inject(ctx, carrier)
	span.End()
	if carrier["traceparent"] == "" {
		t.Fatal("Inject() did not write traceparent")
	}

	extracted := genesistrace.Extract(context.Background(), carrier)
	remote := oteltrace.SpanContextFromContext(extracted)
	if !remote.IsValid() || remote.TraceID() != span.SpanContext().TraceID() {
		t.Fatalf("extracted span context = %v, want trace ID %s", remote, span.SpanContext().TraceID())
	}
}
