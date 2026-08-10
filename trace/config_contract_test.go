package trace_test

import (
	"errors"
	"testing"
	"time"

	genesistrace "github.com/ceyewan/genesis/trace"
)

func TestInitConfigurationErrorsArePubliclyClassifiable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  *genesistrace.Config
	}{
		{name: "nil config"},
		{name: "missing service name", cfg: &genesistrace.Config{Endpoint: "localhost:4317"}},
		{name: "missing endpoint", cfg: &genesistrace.Config{ServiceName: "svc"}},
		{name: "invalid sampler", cfg: &genesistrace.Config{ServiceName: "svc", Endpoint: "localhost:4317", Sampler: 2}},
		{name: "invalid batcher", cfg: &genesistrace.Config{ServiceName: "svc", Endpoint: "localhost:4317", Batcher: "sync"}},
		{name: "negative exporter timeout", cfg: &genesistrace.Config{ServiceName: "svc", Endpoint: "localhost:4317", ExporterTimeout: -time.Second}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := genesistrace.Init(tt.cfg)
			if !errors.Is(err, genesistrace.ErrInvalidConfig) {
				t.Fatalf("Init() error = %v, want ErrInvalidConfig", err)
			}
		})
	}
}
