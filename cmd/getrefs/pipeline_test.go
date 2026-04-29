package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenderFilePrintsSections(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := "package sample\n\nimport \"fmt\"\n\nvar count = 1\n\nfunc run(input string) {\n\tvalue := count\n\tfmt.Println(value)\n\tprintln(value, input)\n}\n"
	path := filepath.Join(dir, "sample.go")
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))

	out, err := ProcessPipeline(path)
	require.NoError(t, err)

	require.Contains(t, out, path)
	require.Contains(t, out, "Source")
	require.Contains(t, out, "\x1b[32mrun\x1b[0m")
	require.Contains(t, out, "\x1b[38;5;208mcount\x1b[0m")
	require.Contains(t, out, "\x1b[38;5;208minput\x1b[0m")
	require.Contains(t, out, "\x1b[32mrun\x1b[0m")
	require.Contains(t, out, "\x1b[97mvalue\x1b[0m")
	require.Contains(t, out, "\x1b[30m\x1b[42mcount\x1b[0m")
	require.Contains(t, out, "\x1b[30m\x1b[48;5;208minput\x1b[0m")
	require.Contains(t, out, "\x1b[30m\x1b[47mvalue\x1b[0m")
	require.Contains(t, out, "- \x1b[35mfmt\x1b[0m -> fmt")
	require.Contains(t, out, "\x1b[90mpackage sample\x1b[0m")
	require.True(t, regexp.MustCompile(`count\x1b\[0m \x1b\[90m.*sample.go:5:5`).MatchString(out))
	require.True(t, regexp.MustCompile(`run\x1b\[0m \x1b\[90m.*sample.go:7:`).MatchString(out))
	require.True(t, regexp.MustCompile(`input\x1b\[0m \x1b\[90m.*sample.go:7:10`).MatchString(out))
	require.True(t, regexp.MustCompile(`value\x1b\[0m \x1b\[90m.*sample.go:8:2`).MatchString(out))
	require.False(t, strings.Contains(out, "variable\x1b[0m"))
	require.False(t, strings.Contains(out, "function\x1b[0m"))
	require.False(t, strings.Contains(out, "println.go"))
}

func TestIndirectCalls(t *testing.T) {
	dir := t.TempDir()
	src := "package p\ntype I interface{ M() }\nfunc test(i I) {\n\ti.M()\n}\n"
	path := filepath.Join(dir, "sample_indirect.go")
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))

	escapeMode = true
	defer func() { escapeMode = false }()
	out, err := ProcessPipeline(path)
	require.NoError(t, err)

	// "M" should have a red span since it is a single-letter indirect call
	require.Contains(t, out, "\x1b[97;41mM\x1b[0m")
}
