package idgen_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ceyewan/genesis/config"
	"github.com/ceyewan/genesis/idgen"
)

func TestConfigLoaderMapsPublishedIDGenConfigTypes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	contents := []byte(`idgen:
  generator:
    mode: single_dc
    worker_id: 513
    datacenter_id: 0
  sequencer:
    driver: redis
    key_prefix: order:sequence
    step: 7
    max_value: 900
    ttl: 750ms
  allocator:
    driver: etcd
    key_prefix: order:worker
    max_id: 64
    ttl: 3s
`)
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), contents, 0o600); err != nil {
		t.Fatal(err)
	}

	loader, err := config.New(&config.Config{Paths: []string{dir}, EnvPrefix: "IDGEN_CONTRACT"})
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
		Generator idgen.GeneratorConfig `mapstructure:"generator"`
		Sequencer idgen.SequencerConfig `mapstructure:"sequencer"`
		Allocator idgen.AllocatorConfig `mapstructure:"allocator"`
	}
	if err := loader.UnmarshalKey("idgen", &got); err != nil {
		t.Fatal(err)
	}

	if got.Generator.Mode != idgen.GeneratorModeSingleDC || got.Generator.WorkerID != 513 || got.Generator.DatacenterID != 0 {
		t.Fatalf("generator config = %+v", got.Generator)
	}
	if got.Sequencer.Driver != idgen.DriverRedis || got.Sequencer.KeyPrefix != "order:sequence" || got.Sequencer.Step != 7 || got.Sequencer.MaxValue != 900 || got.Sequencer.TTL != 750*time.Millisecond {
		t.Fatalf("sequencer config = %+v", got.Sequencer)
	}
	if got.Allocator.Driver != idgen.DriverEtcd || got.Allocator.KeyPrefix != "order:worker" || got.Allocator.MaxID != 64 || got.Allocator.TTL != 3*time.Second {
		t.Fatalf("allocator config = %+v", got.Allocator)
	}
}

func TestConfigLoaderMapsEnvironmentUnderscoresToPublishedIDGenConfigTypes(t *testing.T) {
	t.Setenv("IDGEN_ENV_CONTRACT_IDGEN_GENERATOR_WORKER_ID", "777")
	t.Setenv("IDGEN_ENV_CONTRACT_IDGEN_SEQUENCER_MAX_VALUE", "901")
	t.Setenv("IDGEN_ENV_CONTRACT_IDGEN_ALLOCATOR_MAX_ID", "65")

	loader, err := config.New(&config.Config{Paths: []string{t.TempDir()}, EnvPrefix: "IDGEN_ENV_CONTRACT"})
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
		Generator idgen.GeneratorConfig `mapstructure:"generator"`
		Sequencer idgen.SequencerConfig `mapstructure:"sequencer"`
		Allocator idgen.AllocatorConfig `mapstructure:"allocator"`
	}
	if err := loader.UnmarshalKey("idgen", &got); err != nil {
		t.Fatal(err)
	}
	if got.Generator.WorkerID != 777 || got.Sequencer.MaxValue != 901 || got.Allocator.MaxID != 65 {
		t.Fatalf("environment-mapped config = %+v", got)
	}
}
