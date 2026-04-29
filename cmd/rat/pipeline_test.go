package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderFixtures(t *testing.T) {
	t.Parallel()

	root := fixtureRoot(t)
	fixturesDir := root

	cases := fixtureSources(t, fixturesDir)
	require.True(t, len(cases) > 0, "no fixture source files found")

	accept := os.Getenv("ACCEPT") == "1"
	for _, sourcePath := range cases {
		rel, err := filepath.Rel(fixturesDir, sourcePath)
		require.NoError(t, err)

		t.Run(rel, func(t *testing.T) {
			t.Parallel()
			escapeMode = filepath.Dir(rel) == "escapes"
			defer func() { escapeMode = false }()

			out, err := ProcessPipeline(sourcePath)
			require.NoError(t, err)

			normalized := normalizeOutput(out, sourcePath, rel)
			expectedPath := sourcePath + ".out"

			if accept {
				require.NoError(t, os.MkdirAll(filepath.Dir(expectedPath), 0o755))
				require.NoError(t, os.WriteFile(expectedPath, []byte(normalized), 0o644))
				return
			}

			expected, err := os.ReadFile(expectedPath)
			assert.NoError(t, err, "expected output not found; run with ACCEPT=1 to create it")
			assert.Equal(t, string(expected), normalized)
		})
	}
}

func fixtureRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	return filepath.Join(wd, "..", "..", "testdata", "rat")
}

func fixtureSources(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(root)
	require.NoError(t, err)

	out := make([]string, 0, len(entries))
	var walk func(string)
	walk = func(dir string) {
		dirEntries, err := os.ReadDir(dir)
		require.NoError(t, err)
		for _, entry := range dirEntries {
			path := filepath.Join(dir, entry.Name())
			if entry.IsDir() {
				walk(path)
				continue
			}
			if filepath.Ext(path) == ".go" {
				out = append(out, path)
			}
		}
	}
	walk(root)
	sort.Strings(out)
	return out
}

func normalizeOutput(output, sourcePath, rel string) string {
	relPath := filepath.ToSlash(filepath.Join("testdata", "rat", rel))
	absPath := filepath.ToSlash(sourcePath)
	normalized := filepath.ToSlash(output)
	normalized = replaceAll(normalized, absPath, relPath)
	normalized = replaceAll(normalized, filepath.ToSlash(filepath.Clean(sourcePath)), relPath)
	return normalized
}

func replaceAll(s, old, new string) string {
	if old == "" {
		return s
	}
	return strings.ReplaceAll(s, old, new)
}
