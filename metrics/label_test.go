package metrics

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		key   string
		value string
	}{
		{
			name:  "typical case",
			key:   "method",
			value: "GET",
		},
		{
			name:  "empty strings",
			key:   "",
			value: "",
		},
		{
			name:  "special characters",
			key:   "endpoint",
			value: "/api/v1/users/{id}",
		},
		{
			name:  "status code",
			key:   "status_code",
			value: "404 Not Found",
		},
		{
			name:  "underscore key",
			key:   "test_key",
			value: "test-value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			label := L(tt.key, tt.value)

			require.Equal(t, tt.key, label.Key)
			require.Equal(t, tt.value, label.Value)
		})
	}
}
