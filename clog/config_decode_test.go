package clog_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ceyewan/genesis/clog"
	genesisconfig "github.com/ceyewan/genesis/config"
)

func TestConfigDecodesNestedSnakeCaseYAML(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(`
observability:
  logging:
    level: debug
    format: json
    output: stderr
    enable_color: true
    add_source: true
    source_root: /srv/example
    service_name: api
    version: 1.2.3
    instance_id: api-7
    environment: staging
`), 0o600))

	loader, err := genesisconfig.New(&genesisconfig.Config{
		Paths:     []string{dir},
		EnvPrefix: "CLOG_CONFIG_CONTRACT",
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, loader.Close()) })
	require.NoError(t, loader.Load(context.Background()))

	var got clog.Config
	require.NoError(t, loader.UnmarshalKey("observability.logging", &got))
	require.Equal(t, clog.Config{
		Level:       "debug",
		Format:      "json",
		Output:      "stderr",
		EnableColor: true,
		AddSource:   true,
		SourceRoot:  "/srv/example",
		ServiceName: "api",
		Version:     "1.2.3",
		InstanceID:  "api-7",
		Environment: "staging",
	}, got)
}
