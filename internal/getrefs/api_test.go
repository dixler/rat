package getrefs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseQueryNameOnly(t *testing.T) {
	q, err := parseQuery("/repo", "target")
	if err != nil {
		t.Fatalf("parseQuery returned error: %v", err)
	}
	if q.name != "target" {
		t.Fatalf("name = %q, want %q", q.name, "target")
	}
	if !q.inScope("/repo/any/file.go") {
		t.Fatal("name-only query should allow any file")
	}
}

func TestParseQueryNameOnlyNoRepo(t *testing.T) {
	t.Parallel()
	q, err := parseQuery(t.TempDir(), "myIdent")
	require.NoError(t, err)
	require.Equal(t, "myIdent", q.name)
	assert.True(t, q.inScope("anything.go"))
}

func TestParseQueryFileAndDirScope(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	pkgDir := filepath.Join(root, "pkg")
	nested := filepath.Join(pkgDir, "nested")
	require.NoError(t, os.MkdirAll(nested, 0o755))
	file := filepath.Join(pkgDir, "a.go")
	other := filepath.Join(nested, "b.go")
	require.NoError(t, os.WriteFile(file, []byte("package p\n"), 0o644))
	require.NoError(t, os.WriteFile(other, []byte("package p\n"), 0o644))

	fileQ, err := parseQuery(root, filepath.Join("pkg", "a.go")+":Thing")
	require.NoError(t, err)
	assert.True(t, fileQ.inScope(file))
	assert.False(t, fileQ.inScope(other))

	dirQ, err := parseQuery(root, "pkg:Thing")
	require.NoError(t, err)
	assert.True(t, dirQ.inScope(file))
	assert.True(t, dirQ.inScope(other))
}

func TestParseQueryErrors(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, arg := range []string{"pkg:", ":thing"} {
		_, err := parseQuery(root, arg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "usage: getrefs")
	}
	_, err := parseQuery(root, "missing:thing")
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "invalid scope"))
}

func TestGroupRefsByReassign(t *testing.T) {
	t.Parallel()
	refs := []Location{mkLoc("/tmp/x.go", 2, 1), mkLoc("/tmp/x.go", 4, 1), mkLoc("/tmp/x.go", 8, 1)}
	reassigns := []Location{mkLoc("/tmp/x.go", 3, 1), mkLoc("/tmp/x.go", 7, 1)}

	base, byAssign := groupRefsByReassign(refs, reassigns)
	require.Len(t, base, 1)
	assert.Equal(t, 2, locLine(base[0]))
	require.Len(t, byAssign[0], 1)
	assert.Equal(t, 4, locLine(byAssign[0][0]))
	require.Len(t, byAssign[1], 1)
	assert.Equal(t, 8, locLine(byAssign[1][0]))
}

func TestClassifyExternal(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	samePkgDef := filepath.Join(root, "pkg", "a.go")
	sameFile := filepath.Join(root, "pkg", "b.go")
	sameRepoDef := filepath.Join(root, "other", "c.go")
	extDef := filepath.Join(t.TempDir(), "ext", "d.go")
	for _, p := range []string{samePkgDef, sameFile, sameRepoDef, extDef} {
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte("package p\n"), 0o644))
	}
	ref := mkLoc(sameFile, 15, 1)

	cat, grp, ok := classifyExternal(root, mkLoc("/usr/lib/go/src/builtin/builtin.go", 1, 1), ref, 10, 20)
	assert.False(t, ok)
	assert.Equal(t, "", cat)
	assert.Equal(t, "", grp)

	_, _, ok = classifyExternal(root, mkLoc(sameFile, 12, 1), ref, 10, 20)
	assert.False(t, ok)

	cat, grp, ok = classifyExternal(root, mkLoc(sameFile, 2, 1), ref, 10, 20)
	require.True(t, ok)
	assert.Equal(t, "same file", cat)
	assert.Equal(t, filepath.Join("pkg", "b.go"), grp)

	cat, grp, ok = classifyExternal(root, mkLoc(samePkgDef, 2, 1), ref, 10, 20)
	require.True(t, ok)
	assert.Equal(t, "same package", cat)
	assert.Equal(t, filepath.Join("pkg", "a.go"), grp)

	cat, grp, ok = classifyExternal(root, mkLoc(sameRepoDef, 2, 1), ref, 10, 20)
	require.True(t, ok)
	assert.Equal(t, "same repository", cat)
	assert.Equal(t, "other", grp)

	cat, grp, ok = classifyExternal(root, mkLoc(extDef, 2, 1), ref, 10, 20)
	require.True(t, ok)
	assert.Equal(t, "external repositories", cat)
	assert.Equal(t, filepath.Dir(extDef), grp)
}
