package getrefs

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type funcRef struct {
	Name     string
	Def      *Location
	Refs     []Location
	Reassign []Location
	Children map[string]*funcRef
}

type namedLoc struct {
	Name      string
	Loc       Location
	FuncStart int
	FuncEnd   int
}

type analysisClient struct{}

func (analysisClient) capturedRefsInFunction(ref Location) ([]*funcRef, []namedLoc) {
	file, line, _ := locToFileLine(ref)
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
	roots, declPos := map[string]*funcRef{}, map[token.Pos]bool{}
	var externals []namedLoc
	addRoot := func(name string) *funcRef {
		if roots[name] == nil {
			roots[name] = &funcRef{Name: name, Children: map[string]*funcRef{}}
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
					curr.Children[p] = &funcRef{Name: p, Children: map[string]*funcRef{}}
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
				externals = append(externals, namedLoc{Name: t.Name, Loc: identLoc(file, fset, t), FuncStart: funcStart, FuncEnd: funcEnd})
			}
		}
		return true
	})
	out := make([]*funcRef, 0, len(roots))
	for _, r := range roots {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, externals
}

func classifyExternal(repoRoot string, def, ref Location, funcStart, funcEnd int) (string, string, bool) {
	df, dl, _ := locToFileLine(def)
	if strings.HasPrefix(filepath.Clean(df), "/usr/lib/go/src/builtin") {
		return "", "", false
	}
	rf, _, _ := locToFileLine(ref)
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

func identLoc(file string, fset *token.FileSet, id *ast.Ident) Location {
	p := fset.Position(id.Pos())
	l := Location{URI: pathToURI(file)}
	l.Range.Start.Line, l.Range.Start.Character = p.Line-1, p.Column-1
	l.Range.End = l.Range.Start
	return l
}
