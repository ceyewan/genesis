package idgen

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func FuzzParseGeneratorID(f *testing.F) {
	f.Add(int64(0), "multi_dc")
	f.Add(int64(-1), "single_dc")
	f.Add(int64(1<<62), "multi_dc")
	f.Add(int64(42), "unknown")
	f.Fuzz(func(t *testing.T, id int64, mode string) {
		timestamp, dc, worker, sequence, err := ParseGeneratorID(id, GeneratorMode(mode))
		if id < 0 || (mode != "single_dc" && mode != "multi_dc") {
			require.ErrorIs(t, err, ErrInvalidInput)
			return
		}
		require.NoError(t, err)
		require.Equal(t, id, composeGeneratorID(timestamp-genesisEpochMilli, GeneratorMode(mode), dc, worker, sequence))
	})
}
