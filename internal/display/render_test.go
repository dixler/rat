package display_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"notectl/internal/display"
	"notectl/internal/file"
)

func TestRenderFilePrintsSections(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := "package sample\n\nimport \"fmt\"\n\nvar count = 1\n\nfunc run() {\n\tvalue := count\n\tfmt.Println(value)\n\tprintln(value)\n}\n"
	path := filepath.Join(dir, "sample.go")
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))

	f, err := file.New(path)
	require.NoError(t, err)

	out := captureStdout(func() { display.RenderFile(f) })
	require.Contains(t, out, path)
	require.Contains(t, out, "Source")
	require.Contains(t, out, "function")
	require.Contains(t, out, "\x1b[32mrun\x1b[0m")
	require.Contains(t, out, "\x1b[97m\x1b[48;5;208mvalue\x1b[0m")
	require.Contains(t, out, "\x1b[97m\x1b[42mcount\x1b[0m")
	require.Contains(t, out, "- \x1b[35mfmt\x1b[0m -> fmt")
	require.False(t, strings.Contains(out, "print.go"))
	require.True(t, regexp.MustCompile(`count\x1b\[0m \x1b\[90m.*sample.go:5:5`).MatchString(out))
	require.True(t, regexp.MustCompile(`\x1b\[32m.*sample.go:7:`).MatchString(out))
	require.True(t, regexp.MustCompile(`\x1b\[38;5;208m.*sample.go:8:`).MatchString(out))
	require.False(t, strings.Contains(out, "println.go"))
}

func captureStdout(fn func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = old
	var b bytes.Buffer
	_, _ = io.Copy(&b, r)
	return b.String()
}
