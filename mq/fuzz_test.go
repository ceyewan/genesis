package mq

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ceyewan/genesis/clog"
)

func FuzzRedisHeaders(f *testing.F) {
	for _, value := range []string{`{"traceparent":"value"}`, `null`, `[]`, `{"x":1}`, `{`} {
		f.Add(value)
	}
	f.Fuzz(func(t *testing.T, value string) {
		transport := &redisStreamTransport{logger: clog.Discard()}
		var expected Headers
		err := json.Unmarshal([]byte(value), &expected)
		actual := transport.decodeHeaders(value)
		if err != nil {
			require.Nil(t, actual)
			return
		}
		require.Equal(t, expected, actual)
		cloned := actual.Clone()
		if cloned != nil {
			cloned.Set("diagnostic", "changed")
			require.Equal(t, expected, actual)
		}
	})
}
