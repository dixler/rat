package getrefs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveGoFileArg(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	file := filepath.Join(root, "a.go")
	require.NoError(t, os.WriteFile(file, []byte("package p\n"), 0o644))

	got, ok := resolveGoFileArg(root, "a.go")
	require.True(t, ok)
	assert.Equal(t, file, got)

	_, ok = resolveGoFileArg(root, "pkg:Thing")
	assert.False(t, ok)
	_, ok = resolveGoFileArg(root, "missing.go")
	assert.False(t, ok)
}

func TestColorLineDefinitionAndReferenceStyles(t *testing.T) {
	t.Parallel()
	out := colorLine("target = target", []lineSpan{
		{start: 0, end: 6, color: clrYellow, isDef: true},
		{start: 9, end: 15, color: clrYellow},
	})
	assert.True(t, strings.Contains(out, clrYellow+"target"+clrReset))
	assert.True(t, strings.Contains(out, clrPlain+bgFor(clrYellow)+"target"+clrReset))
}

func TestClassifyColorSkipsSameFunctionForNonVariableSymbols(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	file := filepath.Join(root, "a.go")
	require.NoError(t, os.WriteFile(file, []byte("package p\n"), 0o644))

	ref := mkLoc(file, 20, 1)
	def := mkLoc(file, 12, 1)
	cat, color := classifyColor(root, def, ref, funcScope{start: 10, end: 30}, false)
	assert.Equal(t, "same file", cat)
	assert.Equal(t, clrGreen, color)
}
