package metrics

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		key      string
		value    string
		expected Label
	}{
		{
			name:     "normal key and value",
			key:      "status",
			value:    "success",
			expected: Label{Key: "status", Value: "success"},
		},
		{
			name:     "empty key",
			key:      "",
			value:    "success",
			expected: Label{Key: "", Value: "success"},
		},
		{
			name:     "empty value",
			key:      "status",
			value:    "",
			expected: Label{Key: "status", Value: ""},
		},
		{
			name:     "empty key and value",
			key:      "",
			value:    "",
			expected: Label{Key: "", Value: ""},
		},
		{
			name:     "whitespace characters",
			key:      "  status  ",
			value:    "  success  ",
			expected: Label{Key: "  status  ", Value: "  success  "},
		},
		{
			name:     "special characters",
			key:      "status_code:200",
			value:    "OK!",
			expected: Label{Key: "status_code:200", Value: "OK!"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := L(tt.key, tt.value)
			require.Equal(t, tt.expected, got)
		})
	}
}
