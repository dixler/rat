package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderFixtures(t *testing.T) {
	t.Parallel()

	root := testdataRoot(t)

	cases := fixtureSources(t, root)
	require.True(t, len(cases) > 0, "no fixture source files found")

	accept := os.Getenv("ACCEPT") == "1"
	for _, sourcePath := range cases {
		rel, err := filepath.Rel(root, sourcePath)
		require.NoError(t, err)

		t.Run(rel, func(t *testing.T) {
			t.Parallel()

			out, err := ProcessPipeline(sourcePath, ModeANSI)
			require.NoError(t, err)

			normalized := normalizeOutput(out, sourcePath, rel, true)
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

func TestRenderInternalSources(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	internalRoot := filepath.Join(root, "internal")
	goldenRoot := filepath.Join(root, "testdata", "internal")

	sources := internalSources(t, internalRoot)
	require.True(t, len(sources) > 0, "no internal source files found")

	accept := os.Getenv("ACCEPT") == "1"
	changed := map[string]bool{}
	if !accept {
		changed = gitChangedFiles(t, root)
	}

	for _, sourcePath := range sources {
		rel, err := filepath.Rel(internalRoot, sourcePath)
		require.NoError(t, err)
		testSourcePath := sourcePath
		testRel := rel

		t.Run(testRel, func(t *testing.T) {
			t.Parallel()

			out, err := ProcessPipeline(testSourcePath, ModeANSI)
			require.NoError(t, err)

			normalized := normalizeOutput(out, testSourcePath, filepath.ToSlash(filepath.Join("internal", testRel)), false)
			expectedPath := filepath.Join(goldenRoot, testRel+".out")
			if accept || changed[filepath.ToSlash(testSourcePath)] {
				require.NoError(t, os.MkdirAll(filepath.Dir(expectedPath), 0o755))
				require.NoError(t, os.WriteFile(expectedPath, []byte(normalized), 0o644))
				if accept {
					return
				}
			}

			expected, err := os.ReadFile(expectedPath)
			assert.NoError(t, err, "expected output not found; run with ACCEPT=1 to create it")
			assert.Equal(t, string(expected), normalized)
		})
	}
}

func testdataRoot(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "testdata")
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	return filepath.Join(wd, "..", "..")
}

func fixtureSources(t *testing.T, root string) []string {
	t.Helper()
	fixtureRoots := []string{filepath.Join(root, "go")}

	out := []string{}
	for _, fixtureRoot := range fixtureRoots {
		entries, err := os.ReadDir(fixtureRoot)
		require.NoError(t, err)
		walkFixtureSources(t, fixtureRoot, entries, &out)
	}
	sort.Strings(out)
	return out
}

func walkFixtureSources(t *testing.T, dir string, entries []os.DirEntry, out *[]string) {
	t.Helper()
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			dirEntries, err := os.ReadDir(path)
			require.NoError(t, err)
			walkFixtureSources(t, path, dirEntries, out)
			continue
		}
		if isFixtureSource(path) {
			*out = append(*out, path)
		}
	}
}

func isFixtureSource(path string) bool {
	switch filepath.Ext(path) {
	case ".go":
		return true
	default:
		return false
	}
}

func internalSources(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	out := []string{}
	walkInternalSources(t, root, entries, &out)
	sort.Strings(out)
	return out
}

func walkInternalSources(t *testing.T, dir string, entries []os.DirEntry, out *[]string) {
	t.Helper()
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			dirEntries, err := os.ReadDir(path)
			require.NoError(t, err)
			walkInternalSources(t, path, dirEntries, out)
			continue
		}
		if filepath.Ext(path) == ".go" {
			*out = append(*out, path)
		}
	}
}

func gitChangedFiles(t *testing.T, root string) map[string]bool {
	t.Helper()
	cmd := exec.Command("git", "status", "--porcelain", "--untracked-files=no", "--", "internal")
	cmd.Dir = root
	out, err := cmd.Output()
	require.NoError(t, err)

	changed := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) < 4 {
			continue
		}
		path := strings.TrimSpace(line[3:])
		if strings.Contains(path, " -> ") {
			parts := strings.Split(path, " -> ")
			path = parts[len(parts)-1]
		}
		changed[filepath.ToSlash(filepath.Join(root, filepath.FromSlash(path)))] = true
	}
	return changed
}

func normalizeOutput(output, sourcePath, rel string, testdata bool) string {
	relPath := filepath.ToSlash(rel)
	if testdata {
		relPath = filepath.ToSlash(filepath.Join("testdata", rel))
	}
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
