package getrefs

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"notectl/internal/getrefs/lsp"
	"notectl/internal/getrefs/refs"
	"notectl/internal/getrefs/view"
)

type query struct {
	name    string
	inScope func(string) bool
}

func Run(arg string) error {
	root, err := repoRoot()
	if err != nil {
		return err
	}
	q, err := parseQuery(root, arg)
	if err != nil {
		return err
	}
	c, err := lsp.New(root)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.Init(root); err != nil {
		return err
	}
	matches, err := c.Find(lsp.Query{Name: q.name, InScope: q.inScope})
	if err != nil {
		return err
	}
	return view.Render(c, analyzer{}, root, q.name, matches)
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

type Location = refs.Location
type Pos = refs.Pos
type Match = refs.Match
