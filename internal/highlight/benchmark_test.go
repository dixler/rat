package highlight

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func BenchmarkAnalyze(b *testing.B) {
	fixture := filepath.Join("..", "..", "testdata", "go", "default", "control_flow_gutters_showcase.go")
	fixture, err := filepath.Abs(fixture)
	require.NoError(b, err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := Analyze(fixture)
		require.NoError(b, err)
	}
}
