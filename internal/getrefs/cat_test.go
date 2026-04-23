package getrefs

import (
	"bytes"
	"os"
	"testing"

	"notectl/internal/getrefs/astrefs"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRefKind(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	assert.Equal(t, catSameRepo, refKind(root, "notectl", astrefs.Mark{PackageRef: true, Package: "notectl/pkg"}, Location{}))
	assert.Equal(t, catExternal, refKind(root, "notectl", astrefs.Mark{PackageRef: true, Package: "fmt"}, Location{}))
}

func TestPrintFileWithMarks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := dir + "/x.go"
	require.NoError(t, os.WriteFile(file, []byte("abc\n"), 0o644))

	out := capturePrint(func() {
		require.NoError(t, printFileWithMarks(file, []mark{{line: 1, start: 0, end: 1, open: clrGreen}}))
	})
	assert.Contains(t, out, clrGreen+"a"+clrReset)
}

func capturePrint(fn func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = old
	var b bytes.Buffer
	_, _ = b.ReadFrom(r)
	return b.String()
}
