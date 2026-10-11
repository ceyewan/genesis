package cache

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func BenchmarkLocalKV(b *testing.B) {
	for _, operation := range []string{"Set", "Get"} {
		b.Run(operation, func(b *testing.B) {
			local, err := NewLocal(&LocalConfig{MaxEntries: 1000})
			require.NoError(b, err)
			b.Cleanup(func() { require.NoError(b, local.Close()) })
			ctx := context.Background()
			value := map[string]any{"id": 42, "name": "product"}
			require.NoError(b, local.Set(ctx, "key", value, time.Minute))
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if operation == "Set" {
					if err := local.Set(ctx, "key", value, time.Minute); err != nil {
						b.Fatal(err)
					}
				} else {
					var dest map[string]any
					if err := local.Get(ctx, "key", &dest); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}

func BenchmarkMultiLocalHit(b *testing.B) {
	local, err := NewLocal(&LocalConfig{MaxEntries: 1000})
	require.NoError(b, err)
	b.Cleanup(func() { require.NoError(b, local.Close()) })
	multi, err := NewMulti(local, newMockKVForMulti(), nil)
	require.NoError(b, err)
	ctx := context.Background()
	require.NoError(b, multi.Set(ctx, "key", "value", time.Minute))
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			var value string
			if err := multi.Get(ctx, "key", &value); err != nil {
				b.Error(err)
			}
		}
	})
}
