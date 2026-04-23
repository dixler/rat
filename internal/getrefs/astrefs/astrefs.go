package astrefs

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"notectl/internal/getrefs/refs"
)

type FuncRef struct {
	Name     string
	Def      *refs.Location
	Refs     []refs.Location
	Reassign []refs.Location
	Children map[string]*FuncRef
}

type NamedLoc struct {
	Name      string
	Loc       refs.Location
	FuncStart int
	FuncEnd   int
}

type Mark struct {
	Line       int
	Start, End int
	Name       string
	Loc        refs.Location
	FuncStart  int
	FuncEnd    int
	Definition bool
	PackageRef bool
	Package    string
}

func FileIdentifierMarks(file string) ([]Mark, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		return nil, err
	}
	var out []Mark
	type fr struct{ s, e int }
	var funcs []fr
	ast.Inspect(f, func(n ast.Node) bool {
		switch t := n.(type) {
		case *ast.FuncDecl:
			funcs = append(funcs, fr{s: fset.Position(t.Pos()).Line, e: fset.Position(t.End()).Line})
		case *ast.FuncLit:
			funcs = append(funcs, fr{s: fset.Position(t.Pos()).Line, e: fset.Position(t.End()).Line})
		}
		return true
	})
	imports := map[string]string{}
	for _, im := range f.Imports {
		if im.Name != nil && (im.Name.Name == "_" || im.Name.Name == ".") {
			continue
		}
		path := strings.Trim(im.Path.Value, "\"")
		name := path[strings.LastIndex(path, "/")+1:]
		if im.Name != nil && im.Name.Name != "" {
			name = im.Name.Name
		}
		imports[name] = path
	}
	pkgRefs := map[token.Pos]string{}
	defs := map[token.Pos]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		switch t := n.(type) {
		case *ast.AssignStmt:
			if t.Tok != token.DEFINE {
				return true
			}
			for _, lhs := range t.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && id.Name != "_" {
					defs[id.Pos()] = true
				}
			}
		case *ast.RangeStmt:
			if t.Tok != token.DEFINE {
				return true
			}
			if id, ok := t.Key.(*ast.Ident); ok && id.Name != "_" {
				defs[id.Pos()] = true
			}
			if id, ok := t.Value.(*ast.Ident); ok && id.Name != "_" {
				defs[id.Pos()] = true
			}
		case *ast.ValueSpec:
			for _, id := range t.Names {
				if id.Name != "_" {
					defs[id.Pos()] = true
				}
			}
		case *ast.Field:
			for _, id := range t.Names {
				if id.Name != "_" {
					defs[id.Pos()] = true
				}
			}
		case *ast.TypeSpec:
			if t.Name != nil {
				defs[t.Name.Pos()] = true
			}
		case *ast.FuncDecl:
			if t.Name != nil {
				defs[t.Name.Pos()] = true
			}
		}
		return true
	})
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		if path, ok := imports[id.Name]; ok {
			pkgRefs[id.Pos()] = path
		}
		return true
	})
	ast.Inspect(f, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || id.Name == "_" {
			return true
		}
		p := fset.Position(id.Pos())
		fs, fe := 0, 0
		for _, fn := range funcs {
			if p.Line >= fn.s && p.Line <= fn.e {
				fs, fe = fn.s, fn.e
				break
			}
		}
		path, isPkg := pkgRefs[id.Pos()]
		out = append(out, Mark{
			Line:       p.Line,
			Start:      p.Column - 1,
			End:        p.Column - 1 + len(id.Name),
			Name:       id.Name,
			Loc:        identLoc(file, fset, id),
			FuncStart:  fs,
			FuncEnd:    fe,
			Definition: defs[id.Pos()],
			PackageRef: isPkg,
			Package:    path,
		})
		return true
	})
	return out, nil
}

func CapturedRefsInFunction(ref refs.Location) ([]*FuncRef, []NamedLoc) {
	file, line, _ := refs.ToFileLine(ref)
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		return nil, nil
	}
	var target ast.Node
	funcStart, funcEnd := 0, 0
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok {
			continue
		}
		start, end := fset.Position(fd.Pos()).Line, fset.Position(fd.End()).Line
		if line >= start && line <= end {
			target, funcStart, funcEnd = fd, start, end
			break
		}
	}
	if target == nil {
		return nil, nil
	}
	roots, declPos := map[string]*FuncRef{}, map[token.Pos]bool{}
	var externals []NamedLoc
	addRoot := func(name string) *FuncRef {
		if roots[name] == nil {
			roots[name] = &FuncRef{Name: name, Children: map[string]*FuncRef{}}
		}
		return roots[name]
	}
	ast.Inspect(target, func(n ast.Node) bool {
		switch t := n.(type) {
		case *ast.AssignStmt:
			for _, lhs := range t.Lhs {
				id, ok := lhs.(*ast.Ident)
				if !ok || id.Name == "_" {
					continue
				}
				r, loc := addRoot(id.Name), identLoc(file, fset, id)
				if t.Tok == token.DEFINE && r.Def == nil {
					r.Def, declPos[id.Pos()] = &loc, true
				} else {
					r.Reassign = append(r.Reassign, loc)
				}
			}
		case *ast.ValueSpec:
			for _, id := range t.Names {
				if id.Name != "_" {
					r, loc := addRoot(id.Name), identLoc(file, fset, id)
					r.Def, declPos[id.Pos()] = &loc, true
				}
			}
		case *ast.Field:
			for _, id := range t.Names {
				if id.Name != "_" {
					r, loc := addRoot(id.Name), identLoc(file, fset, id)
					r.Def, declPos[id.Pos()] = &loc, true
				}
			}
		}
		return true
	})
	ast.Inspect(target, func(n ast.Node) bool {
		switch t := n.(type) {
		case *ast.SelectorExpr:
			parts := selectorChain(t)
			if len(parts) < 2 {
				return true
			}
			curr := addRoot(parts[0])
			for _, p := range parts[1:] {
				if curr.Children[p] == nil {
					curr.Children[p] = &FuncRef{Name: p, Children: map[string]*FuncRef{}}
				}
				curr = curr.Children[p]
			}
			curr.Refs = append(curr.Refs, identLoc(file, fset, t.Sel))
		case *ast.Ident:
			if t.Name == "_" || declPos[t.Pos()] {
				return true
			}
			if _, ok := roots[t.Name]; ok {
				roots[t.Name].Refs = append(roots[t.Name].Refs, identLoc(file, fset, t))
			} else {
				externals = append(externals, NamedLoc{Name: t.Name, Loc: identLoc(file, fset, t), FuncStart: funcStart, FuncEnd: funcEnd})
			}
		}
		return true
	})
	out := make([]*FuncRef, 0, len(roots))
	for _, r := range roots {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, externals
}

func ClassifyExternal(repoRoot string, def, ref refs.Location, funcStart, funcEnd int) (string, string, bool) {
	df, dl, _ := refs.ToFileLine(def)
	if strings.HasPrefix(filepath.Clean(df), "/usr/lib/go/src/builtin") {
		return "", "", false
	}
	rf, _, _ := refs.ToFileLine(ref)
	if df == rf {
		if dl >= funcStart && dl <= funcEnd {
			return "", "", false
		}
		rel, _ := filepath.Rel(repoRoot, df)
		return "same file", rel, true
	}
	cleanRoot := filepath.Clean(repoRoot) + string(os.PathSeparator)
	inRepoDef := strings.HasPrefix(filepath.Clean(df), cleanRoot)
	inRepoRef := strings.HasPrefix(filepath.Clean(rf), cleanRoot)
	if inRepoDef && inRepoRef {
		if filepath.Dir(df) == filepath.Dir(rf) {
			rel, _ := filepath.Rel(repoRoot, df)
			return "same package", rel, true
		}
		pkg, _ := filepath.Rel(repoRoot, filepath.Dir(df))
		return "same repository", pkg, true
	}
	return "external repositories", filepath.Dir(df), true
}

func selectorChain(s *ast.SelectorExpr) []string {
	var out []string
	var walk func(ast.Expr)
	walk = func(e ast.Expr) {
		switch t := e.(type) {
		case *ast.Ident:
			out = append(out, t.Name)
		case *ast.SelectorExpr:
			walk(t.X)
			out = append(out, t.Sel.Name)
		}
	}
	walk(s)
	return out
}

func identLoc(file string, fset *token.FileSet, id *ast.Ident) refs.Location {
	p := fset.Position(id.Pos())
	l := refs.Location{URI: refs.PathToURI(file)}
	l.Range.Start.Line, l.Range.Start.Character = p.Line-1, p.Column-1
	l.Range.End = l.Range.Start
	return l
}
