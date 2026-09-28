package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"rat/internal/file/scan"
	_ "rat/internal/file/scan/golang"
	"rat/internal/file/scan/golang/goplsclient"
)

func TestScannerConcurrentOverlays(t *testing.T) {
	path := filepath.Join(t.TempDir(), "probe.go")
	if err := os.WriteFile(path, []byte("package probe\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sources := []string{
		"package probe\nfunc first(v string) string { return v }\n",
		"package probe\nfunc other(v []byte) []byte { return v }\n",
	}
	want := make([]*scan.Result, len(sources))
	for i, source := range sources {
		var err error
		want[i], err = scan.Build(path, []byte(source))
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Run("builds", func(t *testing.T) {
		for i := range 12 {
			t.Run(fmt.Sprint(i), func(t *testing.T) {
				t.Parallel()
				index := i % len(sources)
				got, err := scan.Build(path, []byte(sources[index]))
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(want[index], got) {
					t.Fatal("concurrent overlay changed scanner results")
				}
			})
		}
	})
}

func TestScannerWorkspaceFailure(t *testing.T) {
	if os.Getenv("SCANNER_CLOSED_CLIENT") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestScannerWorkspaceFailure$")
		cmd.Env = append(os.Environ(), "SCANNER_CLOSED_CLIENT=1")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("workspace failure probe: %v\n%s", err, output)
		}
		return
	}
	client, err := goplsclient.Default()
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(testdataRoot(t), "go", "default", "same_file_refs.go")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	result, err := scan.Build(path, source)
	if result != nil || !errors.Is(err, os.ErrClosed) {
		t.Fatalf("closed workspace: result=%v, error=%v", result, err)
	}
}

func TestScannerPackageIsolation(t *testing.T) {
	path := filepath.Join(testdataRoot(t), "go", "default", "import_strings.go")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	build := func() *scan.Result {
		result, err := scan.Build(path, source)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	first, want := build(), build()
	mutated := 0
	for _, pkg := range first.Packages {
		for i := range pkg.Files {
			pkg.Files[i].File = "changed"
			for j := range pkg.Files[i].Declarations {
				pkg.Files[i].Declarations[j].Name = "changed"
				mutated++
			}
		}
	}
	if mutated == 0 {
		t.Fatal("probe did not exercise package declarations")
	}
	if got := build(); !reflect.DeepEqual(want, got) {
		t.Fatal("mutating a result changed a subsequent build")
	}
}
