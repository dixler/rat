package scan

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"notectl/internal/goplsclient"
)

type Result struct {
	File              string
	Declarations      []Declaration
	PackageReferences []PackageReference
	Packages          []Package
}

type Declaration struct {
	ID           string
	Name         string
	Kind         string
	File         string
	Line         int
	Column       int
	References   []Reference
	Declarations []Declaration
}

type Reference struct {
	DeclarationID     string
	DeclarationFile   string
	DeclarationLine   int
	DeclarationColumn int
	Text              string
	Kind              string
	File              string
	Line              int
	Column            int
}

type PackageReference struct {
	PackageID string
	ParentID  string
	Text      string
	File      string
	Line      int
	Column    int
}

type Package struct {
	ID     string
	Name   string
	File   string
	Line   int
	Column int
	Files  []PackageFile
}

type PackageFile struct {
	File         string
	Line         int
	Column       int
	Declarations []DeclarationSummary
}

type DeclarationSummary struct {
	Name   string
	Kind   string
	File   string
	Line   int
	Column int
}

func Build(file string) (*Result, error) {
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	info := &types.Info{Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
	conf := &types.Config{Importer: importer.Default(), Error: func(error) {}}
	_, _ = conf.Check(filepath.Dir(file), fset, []*ast.File{parsed}, info)
	b := builder{
		file:       file,
		fset:       fset,
		info:       info,
		declByObj:  map[types.Object]string{},
		kindByObj:  map[types.Object]string{},
		pkgByPath:  map[string]string{},
		goplsByPos: map[string]definitionLocation{},
	}
	res := &Result{File: file}
	for _, decl := range parsed.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				for _, built := range b.buildSpecs(spec) {
					if built.ID != "" {
						res.Declarations = append(res.Declarations, built)
					}
				}
			}
		case *ast.FuncDecl:
			res.Declarations = append(res.Declarations, b.buildFunc(d))
		}
	}
	for _, imp := range parsed.Imports {
		pkgRef, pkgDecl := b.buildImport(imp)
		if pkgRef.PackageID == "" {
			continue
		}
		res.PackageReferences = append(res.PackageReferences, pkgRef)
		res.Packages = append(res.Packages, pkgDecl)
	}
	sortDeclarations(res.Declarations)
	sort.Slice(res.PackageReferences, func(i, j int) bool { return res.PackageReferences[i].Text < res.PackageReferences[j].Text })
	return res, nil
}

type builder struct {
	file       string
	fset       *token.FileSet
	info       *types.Info
	declByObj  map[types.Object]string
	kindByObj  map[types.Object]string
	pkgByPath  map[string]string
	goplsByPos map[string]definitionLocation
	seq        int
}

type definitionLocation struct {
	file   string
	line   int
	column int
	ok     bool
}

func (b *builder) nextID(prefix string) string {
	b.seq++
	return fmt.Sprintf("%s-%d", prefix, b.seq)
}

func (b *builder) buildSpecs(spec ast.Spec) []Declaration {
	switch s := spec.(type) {
	case *ast.TypeSpec:
		decl := b.newDeclaration(s.Name, "type")
		b.collectReferences(s.Type, &decl)
		return []Declaration{decl}
	case *ast.ValueSpec:
		if len(s.Names) == 0 {
			return nil
		}
		var decls []Declaration
		for i, name := range s.Names {
			decl := b.newDeclaration(name, "variable")
			if i == 0 {
				if s.Type != nil {
					b.collectReferences(s.Type, &decl)
				}
				for _, value := range s.Values {
					b.collectReferences(value, &decl)
				}
			}
			decls = append(decls, decl)
		}
		return decls
	default:
		return nil
	}
}

func (b *builder) buildFunc(fn *ast.FuncDecl) Declaration {
	decl := b.newDeclaration(fn.Name, "function")
	if fn.Type != nil && fn.Type.Params != nil {
		for _, field := range fn.Type.Params.List {
			for _, name := range field.Names {
				if name == nil || name.Name == "_" {
					continue
				}
				decl.Declarations = append(decl.Declarations, b.newDeclaration(name, "parameter"))
			}
		}
	}
	if fn.Type != nil {
		b.collectReferences(fn.Type, &decl)
	}
	if fn.Body == nil {
		return decl
	}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.DeclStmt:
			if gd, ok := x.Decl.(*ast.GenDecl); ok {
				for _, spec := range gd.Specs {
					for _, child := range b.buildSpecs(spec) {
						if child.ID != "" {
							decl.Declarations = append(decl.Declarations, child)
						}
					}
				}
			}
		case *ast.AssignStmt:
			if x.Tok == token.DEFINE {
				for _, lhs := range x.Lhs {
					id, ok := lhs.(*ast.Ident)
					if !ok || id.Name == "_" {
						continue
					}
					child := b.newDeclaration(id, "variable")
					decl.Declarations = append(decl.Declarations, child)
				}
			}
		case *ast.RangeStmt:
			for _, expr := range []ast.Expr{x.Key, x.Value} {
				id, ok := expr.(*ast.Ident)
				if !ok || id.Name == "_" {
					continue
				}
				child := b.newDeclaration(id, "variable")
				decl.Declarations = append(decl.Declarations, child)
			}
		}
		return true
	})
	b.collectReferences(fn.Body, &decl)
	sortDeclarations(decl.Declarations)
	return decl
}

func (b *builder) newDeclaration(id *ast.Ident, kind string) Declaration {
	pos := b.fset.Position(id.Pos())
	decl := Declaration{ID: b.nextID(kind), Name: id.Name, Kind: kind, File: b.file, Line: pos.Line, Column: pos.Column}
	if obj := b.info.Defs[id]; obj != nil {
		b.declByObj[obj] = decl.ID
		b.kindByObj[obj] = kind
	}
	return decl
}

func (b *builder) collectReferences(node ast.Node, decl *Declaration) {
	ast.Inspect(node, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || id.Name == "_" {
			return true
		}
		if b.info.Defs[id] != nil {
			return true
		}
		pos := b.fset.Position(id.Pos())
		ref := Reference{Text: id.Name, File: b.file, Line: pos.Line, Column: pos.Column, Kind: b.classifyObject(b.info.Uses[id])}
		if obj := b.info.Uses[id]; obj != nil {
			ref.DeclarationID = b.declByObj[obj]
		}
		if target, ok := b.definitionFor(id.Pos()); ok {
			ref.DeclarationFile = target.file
			ref.DeclarationLine = target.line
			ref.DeclarationColumn = target.column
		}
		decl.References = append(decl.References, ref)
		return true
	})
	sortReferences(decl.References)
}

func (b *builder) definitionFor(pos token.Pos) (definitionLocation, bool) {
	position := b.fset.Position(pos)
	key := fmt.Sprintf("%s:%d:%d", position.Filename, position.Line, position.Column)
	if cached, ok := b.goplsByPos[key]; ok {
		return cached, cached.ok
	}
	client, err := goplsclient.Default()
	if err != nil {
		b.goplsByPos[key] = definitionLocation{}
		return definitionLocation{}, false
	}
	target, ok, err := client.Definition(position.Filename, position.Line, position.Column)
	if err != nil || !ok {
		b.goplsByPos[key] = definitionLocation{}
		return definitionLocation{}, false
	}
	loc := definitionLocation{file: target.File, line: target.Line, column: target.Column, ok: target.File != "" && target.Line > 0 && target.Column > 0}
	b.goplsByPos[key] = loc
	return loc, loc.ok
}

func (b *builder) buildImport(imp *ast.ImportSpec) (PackageReference, Package) {
	path := strings.Trim(imp.Path.Value, "\"")
	name := filepath.Base(path)
	if imp.Name != nil {
		name = imp.Name.Name
	}
	pos := b.fset.Position(imp.Pos())
	pkgID := b.pkgByPath[path]
	if pkgID == "" {
		pkgID = b.nextID("pkg")
		b.pkgByPath[path] = pkgID
	}
	pkgRef := PackageReference{PackageID: pkgID, ParentID: "file", Text: name, File: b.file, Line: pos.Line, Column: pos.Column}
	return pkgRef, Package{ID: pkgID, Name: path, File: b.file, Line: pos.Line, Column: pos.Column, Files: loadPackageFiles(path)}
}

func loadPackageFiles(importPath string) []PackageFile {
	out, err := exec.Command("go", "list", "-f", "{{.Dir}}", importPath).Output()
	if err != nil {
		return nil
	}
	dir := strings.TrimSpace(string(out))
	entries, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil
	}
	files := make([]PackageFile, 0, len(entries))
	for _, file := range entries {
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			continue
		}
		pf := PackageFile{File: file, Line: 1, Column: 1}
		for _, decl := range parsed.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				pos := fset.Position(d.Name.Pos())
				pf.Declarations = append(pf.Declarations, DeclarationSummary{Name: d.Name.Name, Kind: "function", File: file, Line: pos.Line, Column: pos.Column})
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						pos := fset.Position(s.Name.Pos())
						pf.Declarations = append(pf.Declarations, DeclarationSummary{Name: s.Name.Name, Kind: "type", File: file, Line: pos.Line, Column: pos.Column})
					case *ast.ValueSpec:
						for _, name := range s.Names {
							pos := fset.Position(name.Pos())
							pf.Declarations = append(pf.Declarations, DeclarationSummary{Name: name.Name, Kind: "variable", File: file, Line: pos.Line, Column: pos.Column})
						}
					}
				}
			}
		}
		sort.Slice(pf.Declarations, func(i, j int) bool { return pf.Declarations[i].Line < pf.Declarations[j].Line })
		files = append(files, pf)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].File < files[j].File })
	return files
}

func (b *builder) classifyObject(obj types.Object) string {
	if kind := b.kindByObj[obj]; kind != "" {
		return kind
	}
	switch obj.(type) {
	case *types.PkgName:
		return "package"
	case *types.TypeName:
		return "type"
	case *types.Func:
		return "function"
	default:
		return "variable"
	}
}

func sortDeclarations(decls []Declaration) {
	sort.Slice(decls, func(i, j int) bool {
		if decls[i].Line != decls[j].Line {
			return decls[i].Line < decls[j].Line
		}
		return decls[i].Column < decls[j].Column
	})
}

func sortReferences(refs []Reference) {
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Line != refs[j].Line {
			return refs[i].Line < refs[j].Line
		}
		return refs[i].Column < refs[j].Column
	})
}
