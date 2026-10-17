package connector_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/ceyewan/genesis/auth"
	"github.com/ceyewan/genesis/config"
	"github.com/ceyewan/genesis/metrics"
	"github.com/ceyewan/genesis/mq"
	"github.com/ceyewan/genesis/ratelimit"
	"github.com/ceyewan/genesis/trace"
)

func TestPublicConfigSerializationTagsAreConsistent(t *testing.T) {
	t.Parallel()
	for _, value := range []any{auth.Config{}, config.Config{}, mq.RetryConfig{}, ratelimit.Limit{}, metrics.HTTPServerMetricsConfig{}, metrics.GRPCServerMetricsConfig{}} {
		typeOf := reflect.TypeOf(value)
		t.Run(typeOf.PkgPath()+"/"+typeOf.Name(), func(t *testing.T) {
			encodedJSON, err := json.Marshal(value)
			require.NoError(t, err)
			var jsonMap map[string]any
			require.NoError(t, json.Unmarshal(encodedJSON, &jsonMap))
			encodedYAML, err := yaml.Marshal(value)
			require.NoError(t, err)
			var yamlMap map[string]any
			require.NoError(t, yaml.Unmarshal(encodedYAML, &yamlMap))
			for field := range typeOf.Fields() {
				name := field.Tag.Get("mapstructure")
				require.NotEmpty(t, name, field.Name)
				require.Equal(t, strings.ToLower(name), name)
				require.Equal(t, name, field.Tag.Get("json"))
				require.Equal(t, name, field.Tag.Get("yaml"))
				require.Contains(t, jsonMap, name)
				require.Contains(t, yamlMap, name)
			}
		})
	}
	for _, value := range []any{metrics.Config{}, trace.Config{}} {
		for field := range reflect.TypeOf(value).Fields() {
			if field.Type.Kind() != reflect.Chan {
				continue
			}
			for _, tag := range []string{"json", "yaml", "mapstructure"} {
				require.Equal(t, "-", field.Tag.Get(tag))
			}
		}
	}
}
