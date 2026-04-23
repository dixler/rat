package getrefs

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"

	"notectl/internal/getrefs/astrefs"
	"notectl/internal/getrefs/colors"
	"notectl/internal/getrefs/lsp"
	"notectl/internal/getrefs/refs"
	"notectl/internal/getrefs/view"
)

func Cat(path string) error {
	root, err := repoRoot()
	if err != nil {
		return err
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	path = filepath.Clean(path)
	c, err := lsp.New(root)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.Init(root); err != nil {
		return err
	}
	marks, err := astrefs.FileIdentifierMarks(path)
	if err != nil {
		return err
	}
	out := make([]view.ColorMark, 0, len(marks))
	mod := modulePath(root)
	for _, m := range marks {
		def := refs.Location{}
		if !m.PackageRef {
			def = c.DefinitionAt(m.Loc)
			if isBuiltinDef(def) {
				continue
			}
		}
		cs := refColors[refKind(root, mod, m, def)]
		open := cs.bg
		if m.Definition {
			open = cs.fg
		}
		if open == "" {
			open = cs.fg
		}
		out = append(out, view.ColorMark{Line: m.Line, Start: m.Start, End: m.End, Open: open})
	}
	return view.PrintFileWithMarks(path, out)
}

type category string

const (
	catSameFunc category = "same function"
	catSameFile category = "same file"
	catSamePkg  category = "same package"
	catSameRepo category = "same repository"
	catExternal category = "external repositories"
)

type colorPair struct{ fg, bg string }

var refColors = map[category]colorPair{
	catSameFunc: {fg: colors.Yellow, bg: colors.BgYellow},
	catSameFile: {fg: colors.Green, bg: colors.BgGreen},
	catSamePkg:  {fg: colors.Cyan, bg: colors.BgCyan},
	catSameRepo: {fg: colors.Blue, bg: colors.BgBlue},
	catExternal: {fg: colors.Purple, bg: colors.BgPurple},
}

func refKind(repoRoot, module string, m astrefs.Mark, def refs.Location) category {
	if m.PackageRef {
		if module != "" && strings.HasPrefix(m.Package, module) {
			return catSameRepo
		}
		return catExternal
	}
	ref := m.Loc
	df, dl, _ := refs.ToFileLine(def)
	df = filepath.Clean(df)
	rf := filepath.Clean(refs.File(ref))
	if df == rf {
		if m.FuncStart > 0 && dl >= m.FuncStart && dl <= m.FuncEnd {
			return catSameFunc
		}
		return catSameFile
	}
	root := filepath.Clean(repoRoot) + string(os.PathSeparator)
	if strings.HasPrefix(df, root) && strings.HasPrefix(rf, root) {
		if filepath.Dir(df) == filepath.Dir(rf) {
			return catSamePkg
		}
		return catSameRepo
	}
	return catExternal
}

func isBuiltinDef(def refs.Location) bool {
	f := filepath.Clean(refs.File(def))
	return strings.HasPrefix(f, "/usr/lib/go/src/builtin")
}

func modulePath(root string) string {
	b, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	line := bytes.SplitN(b, []byte("\n"), 2)[0]
	const p = "module "
	if !bytes.HasPrefix(line, []byte(p)) {
		return ""
	}
	return strings.TrimSpace(string(line[len(p):]))
}
