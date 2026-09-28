package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"rat/internal/file/scan"
	_ "rat/internal/file/scan/golang"
	"rat/internal/file/scan/golang/goplsclient"
)

// These snapshots are captured from the original scanner, independently of the
// source files being rewritten. ACCEPT deliberately cannot update this contract.
func TestScannerContract(t *testing.T) {
	root := testdataRoot(t)
	for _, path := range fixtureSources(t, root) {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatal(err)
		}
		t.Run(filepath.ToSlash(rel), func(t *testing.T) {
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			checkScannerContract(t, path, source, filepath.Join(root, "scanner", rel+".json"))
		})
	}
}

func TestScannerProbes(t *testing.T) {
	cases := map[string]string{
		"overlay":         "package probe\nfunc overlay(x int) int { return x + 1 }\n",
		"incomplete":      "package probe\nfunc broken(x int) { if x > 0 {\n",
		"unresolved":      "package probe\nfunc unresolved() { missing(unknown) }\n",
		"unicode":         "package probe\n// 日本語\nfunc café(値 string) string {\n text := `一\n二`\n return 値 + text\n}\n",
		"flow":            "package probe\nfunc flow(xs []int) {\nouter: for _, x := range xs {\n switch x { case 0: continue outer; case 1: break; default: if x < 0 { break outer } }\n}\n}\n",
		"calls":           "package probe\nfunc identity[T any](v T) T { return v }\nfunc calls(f func(), fs []func(), m map[string]func()) {\n f(); (f)(); fs[0](); m[\"x\"](); identity[int](1)\n}\n",
		"embedded":        "package probe\ntype Base struct { Value int }\ntype Wrapper struct { Base; *Other; Tag string `json:\"tag\"` }\ntype Other struct { Data []byte }\nfunc embedded(w Wrapper) int { return w.Value }\n",
		"errors":          "package probe\ntype failure string\nfunc (f failure) Error() string { return string(f) }\nfunc custom() failure { return failure(\"bad\") }\nfunc named() (err error, n int) { return }\nfunc pointer() *failure { return nil }\n",
		"empty":           "",
		"invalid":         "this is not Go\n",
		"workspace-calls": "package sample\nimport \"rat/internal/file\"\nfunc workspaceCalls() {\n factory := file.New\n f, _ := factory(\"x\")\n _ = f.Source()\n}\n",
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "probe.go")
			if name == "workspace-calls" {
				path = filepath.Join(testdataRoot(t), "go", "default", "indirect.go")
				t.Cleanup(func() {
					client, err := goplsclient.Default()
					if err != nil {
						t.Error(err)
						return
					}
					if err := client.CloseDocument(path); err != nil {
						t.Error(err)
					}
				})
			} else if err := os.WriteFile(path, []byte("package probe\nvar diskOnly = 1\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			checkScannerContract(t, path, []byte(source), filepath.Join(testdataRoot(t), "scanner", "probes", name+".json"))
		})
	}
}

// A saved baseline test binary can probe additional inputs during a rewrite.
func TestScannerOracle(t *testing.T) {
	path := os.Getenv("SCANNER_ORACLE_PATH")
	if path == "" {
		t.Skip("set SCANNER_ORACLE_PATH to probe a source file")
	}
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	checkScannerContract(t, path, source, os.Getenv("SCANNER_ORACLE_GOLDEN"))
}

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

func checkScannerContract(t *testing.T, path string, source []byte, golden string) {
	t.Helper()
	result, buildErr := scan.Build(path, source)
	var nodes []any
	if result != nil {
		for _, node := range result.Nodes {
			nodes = append(nodes, struct {
				Type  string
				Value scan.Node
			}{fmt.Sprintf("%T", node), node})
		}
		result.Nodes = nil
	}
	errorText := ""
	if buildErr != nil {
		errorText = buildErr.Error()
	}
	data, err := json.MarshalIndent(struct {
		Result *scan.Result
		Nodes  []any
		Error  string
	}{result, nodes, errorText}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data) + "\n"
	for _, replacement := range [][2]string{{filepath.Clean(repoRoot(t)), "$REPO"}, {runtime.GOROOT(), "$GOROOT"}} {
		text = strings.ReplaceAll(text, filepath.ToSlash(replacement[0]), replacement[1])
	}
	if !strings.HasPrefix(filepath.Clean(path), filepath.Clean(repoRoot(t))+string(filepath.Separator)) {
		text = strings.ReplaceAll(text, filepath.ToSlash(filepath.Dir(path)), "$TEMP")
	}
	if os.Getenv("CAPTURE_SCANNER_CONTRACT") == "1" {
		if _, err := os.Stat(golden); !os.IsNotExist(err) {
			t.Fatalf("refusing to overwrite frozen contract %s", golden)
		}
		if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if canonicalContract(t, want) != canonicalContract(t, []byte(text)) {
		dir := t.TempDir()
		if output := os.Getenv("SCANNER_ACTUAL_DIR"); output != "" {
			dir = output
		}
		actual := filepath.Join(dir, filepath.Base(golden))
		if err := os.WriteFile(actual, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Errorf("scanner contract differs: %s (actual: %s)", golden, actual)
	}
}

func canonicalContract(t *testing.T, data []byte) string {
	t.Helper()
	var snapshot map[string]any
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	var index func(any)
	index = func(value any) {
		items, _ := value.([]any)
		for _, item := range items {
			record := item.(map[string]any)
			if id, ok := record["ID"].(string); ok && id != "" {
				if _, exists := ids[id]; !exists {
					ids[id] = fmt.Sprintf("id-%d", len(ids)+1)
				}
			}
			index(record["Declarations"])
		}
	}
	if result, ok := snapshot["Result"].(map[string]any); ok {
		index(result["Declarations"])
		index(result["Packages"])
	}
	var rewrite func(any)
	rewrite = func(value any) {
		switch value := value.(type) {
		case map[string]any:
			for key, child := range value {
				switch key {
				case "ID", "DeclarationID", "PackageID", "ParentID":
					if id, ok := child.(string); ok && ids[id] != "" {
						value[key] = ids[id]
					}
				}
				rewrite(child)
			}
		case []any:
			for _, child := range value {
				rewrite(child)
			}
		}
	}
	rewrite(snapshot)
	normalized, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return string(normalized)
}
