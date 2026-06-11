package file_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"rat/internal/file"
)

func TestNewBuildsFileTree(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := `package sample

import "fmt"

type item struct{}

var count = 1

func run(input string) {
	value := count
	println(input)
	fmt.Println(value)
}
`
	path := filepath.Join(dir, "sample.go")
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))

	f, err := file.New(path)
	require.NoError(t, err)
	require.Equal(t, src, f.Source())
	require.Len(t, f.PackageReferences(), 1)
	require.Len(t, f.Declarations(), 3)
	require.Equal(t, "sample.go", f.Tree().Name())

	fn := f.Declarations()[2]
	require.Equal(t, file.KindFunction, fn.Kind())
	require.Len(t, fn.Declarations(), 2)
	require.True(t, len(fn.References()) > 0)
	require.Equal(t, file.KindParameter, fn.Declarations()[0].Kind())
	require.Equal(t, "input", fn.Declarations()[0].Name())
	require.Equal(t, file.KindVariable, fn.Declarations()[1].Kind())
	require.Equal(t, "value", fn.Declarations()[1].Name())
}

func BenchmarkNew(b *testing.B) {
	dir := b.TempDir()
	src := `package sample

import "fmt"

type item struct {
	name string
	data []byte
}

var count = 1

func run(input string) error {
	value := count
	println(input)
	fmt.Println(value)
	items := []item{{name: "a", data: []byte(input)}}
	for _, item := range items {
		if item.name == "" {
			continue
		}
		fmt.Println(item.name)
	}
	return nil
}
`
	path := filepath.Join(dir, "sample.go")
	require.NoError(b, os.WriteFile(path, []byte(src), 0o644))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := file.New(path)
		require.NoError(b, err)
	}
}
