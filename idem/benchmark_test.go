package idem

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func BenchmarkIdemMemoryHit(b *testing.B) {
	component, err := New(&Config{Driver: DriverMemory})
	require.NoError(b, err)
	b.Cleanup(func() { require.NoError(b, component.Close()) })
	ctx := context.Background()
	fn := func(context.Context) (any, error) { return map[string]any{"id": 42}, nil }
	_, err = component.Execute(ctx, "key", fn)
	require.NoError(b, err)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := component.Execute(ctx, "key", fn); err != nil {
				b.Error(err)
			}
		}
	})
}
