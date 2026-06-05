package typescript

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDefaultLSPClientDefinition(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "sample.ts")
	require.NoError(t, os.WriteFile(file, []byte("const count = 1;\nconst value = count;\n"), 0o644))

	client := defaultLSPClient(file)
	require.NotNil(t, client)
	defer client.Close()

	require.NoError(t, client.SyncDocument(file))

	start := time.Now()
	loc, ok, err := client.DefinitionInSyncedDocument(file, 2, 15)
	require.NoError(t, err)
	require.True(t, ok)
	elapsed := time.Since(start)
	require.True(t, elapsed < time.Second, "definition took "+elapsed.String())
	require.Equal(t, file, loc.File)
	require.Equal(t, 1, loc.Line)
	require.Equal(t, 7, loc.Column)
}
