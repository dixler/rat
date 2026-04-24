package getrefs

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type Location struct {
	URI   string `json:"uri"`
	Range struct {
		Start Pos `json:"start"`
		End   Pos `json:"end"`
	} `json:"range"`
}

type Pos struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type Match struct {
	Def  Location
	Refs []Location
}

type query struct {
	name    string
	inScope func(string) bool
}

func Run(arg string) error {
	root, err := repoRoot()
	if err != nil {
		return err
	}
	if file, ok := resolveGoFileArg(root, arg); ok {
		lsp, err := newLSPClient(root)
		if err != nil {
			return RenderFileCat(newLocalResolver(file), root, file)
		}
		defer lsp.Close()
		if err := lsp.Init(root); err != nil {
			return RenderFileCat(newLocalResolver(file), root, file)
		}
		return RenderFileCat(lsp, root, file)
	}
	q, err := parseQuery(root, arg)
	if err != nil {
		return err
	}
	lsp, err := newLSPClient(root)
	if err != nil {
		return err
	}
	defer lsp.Close()
	if err := lsp.Init(root); err != nil {
		return err
	}
	matches, err := lsp.Find(q)
	if err != nil {
		return err
	}
	return Render(lsp, root, q.name, matches)
}

func resolveGoFileArg(root, arg string) (string, bool) {
	if strings.Contains(arg, ":") || !strings.HasSuffix(arg, ".go") {
		return "", false
	}
	p := arg
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	p = filepath.Clean(p)
	info, err := os.Stat(p)
	if err != nil || info.IsDir() {
		return "", false
	}
	return p, true
}

func repoRoot() (string, error) {
	c := exec.Command("git", "rev-parse", "--show-toplevel")
	b, err := c.Output()
	if err != nil {
		return "", errors.New("run inside a git repo")
	}
	return strings.TrimSpace(string(b)), nil
}

func parseQuery(root, arg string) (query, error) {
	if !strings.Contains(arg, ":") {
		return query{name: arg, inScope: func(string) bool { return true }}, nil
	}
	scopeArg, name, ok := strings.Cut(arg, ":")
	if !ok || strings.TrimSpace(name) == "" || strings.TrimSpace(scopeArg) == "" {
		return query{}, errors.New("usage: getrefs [<file|dir>:]<identifierName>")
	}
	scopePath := scopeArg
	if !filepath.IsAbs(scopePath) {
		scopePath = filepath.Join(root, scopePath)
	}
	scopePath = filepath.Clean(scopePath)
	info, err := os.Stat(scopePath)
	if err != nil {
		return query{}, fmt.Errorf("invalid scope %q: %w", scopeArg, err)
	}
	if info.IsDir() {
		prefix := scopePath + string(os.PathSeparator)
		return query{name: name, inScope: func(file string) bool {
			f := filepath.Clean(file)
			return f == scopePath || strings.HasPrefix(f, prefix)
		}}, nil
	}
	return query{name: name, inScope: func(file string) bool { return filepath.Clean(file) == scopePath }}, nil
}

func locToFileLine(loc Location) (string, int, int) {
	u, _ := url.Parse(loc.URI)
	p := filepath.FromSlash(u.Path)
	return p, loc.Range.Start.Line + 1, loc.Range.Start.Character + 1
}

func locFile(loc Location) string { f, _, _ := locToFileLine(loc); return f }
func locLine(loc Location) int    { _, l, _ := locToFileLine(loc); return l }

func locKey(loc Location) string {
	f, l, c := locToFileLine(loc)
	return f + ":" + strconv.Itoa(l) + ":" + strconv.Itoa(c)
}

func lineText(file string, line int) string {
	b, err := os.ReadFile(file)
	if err != nil {
		return ""
	}
	ls := bytes.Split(b, []byte("\n"))
	if line < 1 || line > len(ls) {
		return ""
	}
	return strings.TrimSpace(string(ls[line-1]))
}

func uniqLocs(in []Location) []Location {
	seen := map[string]bool{}
	out := in[:0]
	for _, l := range in {
		k := locKey(l)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, l)
	}
	return out
}

func sortLocs(locs []Location) {
	sort.Slice(locs, func(i, j int) bool {
		fi, li, ci := locToFileLine(locs[i])
		fj, lj, cj := locToFileLine(locs[j])
		if fi != fj {
			return fi < fj
		}
		if li != lj {
			return li < lj
		}
		return ci < cj
	})
}
