package getrefs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCapturedRefsInFunctionParsesDefinitionsAndReferences(t *testing.T) {
	t.Parallel()
	src := `package p

func demo() {
	a := 1
	a = a + 1
	b := a
	_ = b
	obj := struct{ field struct{ leaf int } }{}
	_ = obj.field.leaf
}
`
	dir := t.TempDir()
	file := filepath.Join(dir, "demo.go")
	require.NoError(t, os.WriteFile(file, []byte(src), 0o644))
	anchor := Location{URI: pathToURI(file)}
	anchor.Range.Start.Line = lineOf(src, "_ = b") - 1

	roots, _ := (analysisClient{}).capturedRefsInFunction(anchor)

	a := findFuncRef(roots, "a")
	require.NotNil(t, a)
	require.NotNil(t, a.Def)
	require.Len(t, a.Reassign, 1)

	b := findFuncRef(roots, "b")
	require.NotNil(t, b)
	require.NotNil(t, b.Def)
	assert.True(t, len(b.Refs) > 0)

	obj := findFuncRef(roots, "obj")
	require.NotNil(t, obj)
	require.NotNil(t, obj.Def)
	field := obj.Children["field"]
	require.NotNil(t, field)
	leaf := field.Children["leaf"]
	require.NotNil(t, leaf)
	assert.True(t, len(leaf.Refs) > 0)
}

func findFuncRef(roots []*funcRef, name string) *funcRef {
	for _, r := range roots {
		if r.Name == name {
			return r
		}
	}
	return nil
}

func lineOf(src, needle string) int {
	lines := strings.Split(src, "\n")
	for i, line := range lines {
		if strings.Contains(line, needle) {
			return i + 1
		}
	}
	return 1
}
