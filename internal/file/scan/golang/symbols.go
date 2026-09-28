package golang

import (
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"rat/internal/file/scan"
)

// Index records before collecting references, so forward references and nested
// functions use the same object-to-declaration lookup as ordinary locals.
func (b *build) index(node ast.Node, out *[]scan.Declaration, owner, fieldKind string) {
	if node == nil {
		return
	}
	add := func(id *ast.Ident, kind string, pos token.Pos) *scan.Declaration {
		name := ""
		var obj types.Object
		if id != nil {
			name = id.Name
			obj = b.info.Defs[id]
		}
		d := scan.Declaration{Location: b.location(pos), ID: b.id(kind), Name: name, Kind: kind}
		if obj != nil {
			d.ReferenceType = referenceType(obj.Type())
			b.objects[obj] = d.ID
		}
		if kind == scan.KindFunction && id != nil {
			d.ReferenceType = true
		}
		if _, exists := b.positions[pos]; !exists {
			b.positions[pos] = d.ID
		}
		*out = append(*out, d)
		return &(*out)[len(*out)-1]
	}
	switch n := node.(type) {
	case *ast.FuncDecl:
		d := add(n.Name, scan.KindFunction, n.Name.Pos())
		b.owners[n] = d.ID
		if n.Recv != nil {
			b.index(n.Recv, &d.Declarations, d.ID, scan.KindParameter)
		}
		b.index(n.Type, &d.Declarations, d.ID, scan.KindParameter)
		if n.Body != nil {
			b.index(n.Body, &d.Declarations, d.ID, "")
			d.ControlFlow = b.flowList(n.Body.List, n.Type, flowContext{})
		}
		return
	case *ast.FuncLit:
		d := add(nil, scan.KindFunction, n.Pos())
		b.owners[n] = d.ID
		b.index(n.Type, &d.Declarations, d.ID, scan.KindParameter)
		b.index(n.Body, &d.Declarations, d.ID, "")
		d.ControlFlow = b.flowList(n.Body.List, n.Type, flowContext{})
		return
	case *ast.TypeSpec:
		d := add(n.Name, scan.KindType, n.Name.Pos())
		b.owners[n] = d.ID
		if n.TypeParams != nil {
			b.index(n.TypeParams, &d.Declarations, d.ID, scan.KindParameter)
		}
		b.index(n.Type, &d.Declarations, d.ID, "")
		return
	case *ast.ValueSpec:
		for _, id := range n.Names {
			if id.Name == "_" {
				continue
			}
			d := add(id, scan.KindVariable, id.Pos())
			b.owners[id] = d.ID
		}
		// Value declarations own their type/initializer references as well as the
		// enclosing declaration, matching the shared record contract.
	case *ast.AssignStmt:
		if n.Tok == token.DEFINE {
			for _, expr := range n.Lhs {
				if id, ok := expr.(*ast.Ident); ok && id.Name != "_" {
					add(id, scan.KindVariable, id.Pos())
				}
			}
		}
	case *ast.RangeStmt:
		if n.Tok == token.DEFINE {
			for _, expr := range []ast.Expr{n.Key, n.Value} {
				if id, ok := expr.(*ast.Ident); ok && id.Name != "_" {
					add(id, scan.KindVariable, id.Pos())
				}
			}
		}
	case *ast.TypeSwitchStmt:
		if a, ok := n.Assign.(*ast.AssignStmt); ok && a.Tok == token.DEFINE {
			for _, expr := range a.Lhs {
				if id, ok := expr.(*ast.Ident); ok && id.Name != "_" {
					add(id, scan.KindVariable, id.Pos())
					b.implicit[id.Pos()] = true
				}
			}
		}
	case *ast.FuncType:
		if n.TypeParams != nil {
			b.index(n.TypeParams, out, owner, scan.KindParameter)
		}
		if n.Params != nil {
			b.index(n.Params, out, owner, scan.KindParameter)
		}
		return
	case *ast.StructType:
		b.index(n.Fields, out, owner, "")
		return
	case *ast.InterfaceType:
		if strings.HasPrefix(owner, "type-") {
			b.index(n.Methods, out, owner, scan.KindFunction)
		}
		return
	case *ast.Field:
		if fieldKind != "" {
			for _, id := range n.Names {
				if id.Name != "_" {
					add(id, fieldKind, id.Pos())
				}
			}
		}
		// Parameters inside function *types* are not nested declarations.
		if _, ok := n.Type.(*ast.FuncType); ok {
			return
		}
		b.index(n.Type, out, owner, "")
		return
	}
	ast.Inspect(node, func(child ast.Node) bool {
		if child == node {
			return true
		}
		if child != nil {
			b.index(child, out, owner, fieldKind)
		}
		return false
	})
}

func (b *build) references(node ast.Node, owner string) {
	if node == nil {
		return
	}
	if id := b.owners[node]; id != "" {
		owner = id
	}
	if n, ok := node.(*ast.TypeSpec); ok {
		b.references(n.Type, owner)
		return
	}
	if n, ok := node.(*ast.ValueSpec); ok {
		for _, id := range n.Names {
			if d := b.owners[id]; d != "" {
				if n.Type != nil {
					b.references(n.Type, d)
				}
				for _, value := range n.Values {
					b.references(value, d)
				}
			}
		}
	}
	if id, ok := node.(*ast.Ident); ok {
		if owner == "" || id.Name == "_" {
			return
		}
		if obj, exists := b.info.Defs[id]; exists && obj != nil {
			return
		}
		if _, exists := b.owners[id]; exists {
			return
		}
		obj := b.info.Uses[id]
		ref := scan.Reference{Location: b.location(id.Pos()), Text: id.Name, Kind: objectKind(obj)}
		if obj != nil {
			ref.ReferenceType = referenceType(obj.Type())
			ref.DeclarationID = b.objects[obj]
		}
		if ref.DeclarationID == "" && obj != nil && b.implicit[obj.Pos()] {
			ref.DeclarationID = b.positions[obj.Pos()]
		}
		if obj != nil && obj.Pos() > id.Pos() {
			ref.DeclarationID = ""
		}
		if obj != nil {
			if _, ok := obj.Type().(*types.TypeParam); ok {
				ref.Kind = scan.KindParameter
			}
		}
		if d := b.decls[ref.DeclarationID]; d != nil {
			ref.Kind = d.Kind
		} else {
			ref.Declaration = b.definition(id)
		}
		if _, ok := obj.(*types.PkgName); ok || obj == nil {
			if pkg, ok := b.imports[id.Name]; ok {
				ref.Declaration = pkg.Location
				ref.Kind = scan.KindPackage
			}
		}
		b.decls[owner].References = append(b.decls[owner].References, ref)
		return
	}
	ast.Inspect(node, func(child ast.Node) bool {
		if child == node {
			return true
		}
		if child != nil {
			b.references(child, owner)
		}
		return false
	})
}

func (b *build) packages() {
	for _, imp := range b.ast.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		pkg := scan.Package{ID: b.id("pkg"), Name: path}
		loc, ok, _ := b.client.DefinitionForPosition(b.fset.Position(imp.Path.Pos() + 1))
		if ok {
			pkg.Location = scan.Location{File: loc.File, Line: 1, Column: 1}
		}
		if pkg.File != "" {
			pkg.Files = b.packageFiles(filepath.Dir(pkg.File))
			if len(pkg.Files) > 0 {
				pkg.Location = pkg.Files[0].Location
			}
		}
		b.result.Packages = append(b.result.Packages, pkg)
		name := filepath.Base(path)
		alias := name
		if imp.Name != nil {
			alias = imp.Name.Name
		}
		b.imports[alias] = pkg
		ref := scan.PackageReference{Location: b.location(imp.Path.Pos() + token.Pos(len(imp.Path.Value)-1-len(name))), PackageID: pkg.ID, ParentID: "file", Text: name}
		if imp.Name != nil && alias != "_" && alias != "." {
			b.result.PackageReferences = append(b.result.PackageReferences, scan.PackageReference{Location: b.location(imp.Name.Pos()), PackageID: pkg.ID, ParentID: "file", Text: alias})
		}
		b.result.PackageReferences = append(b.result.PackageReferences, ref)
	}
	sort.SliceStable(b.result.PackageReferences, func(i, j int) bool { return b.result.PackageReferences[i].Text < b.result.PackageReferences[j].Text })
}

// Standard-library summaries are immutable for the running toolchain. Project
// files stay per-build so edits and overlays never reuse stale declarations.
var standardPackages sync.Map

func (b *build) packageFiles(dir string) []scan.PackageFile {
	standard := strings.HasPrefix(dir, filepath.Join(runtime.GOROOT(), "src")+string(filepath.Separator))
	if cached, ok := standardPackages.Load(dir); ok {
		return clonePackageFiles(cached.([]scan.PackageFile))
	}
	var files []scan.PackageFile
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file := filepath.Join(dir, name)
		parsed := b.parseWorkspace(file)
		if parsed.file == nil {
			continue
		}
		pf := scan.PackageFile{Location: scan.Location{File: file, Line: 1, Column: 1}}
		appendName := func(id *ast.Ident, kind string) {
			p := parsed.positions.Position(id.Pos())
			pf.Declarations = append(pf.Declarations, scan.DeclarationSummary{Location: scan.Location{File: file, Line: p.Line, Column: p.Column}, Name: id.Name, Kind: kind})
		}
		for _, decl := range parsed.file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				appendName(d.Name, scan.KindFunction)
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						appendName(s.Name, scan.KindType)
					case *ast.ValueSpec:
						for _, id := range s.Names {
							appendName(id, scan.KindVariable)
						}
					}
				}
			}
		}
		files = append(files, pf)
	}
	if standard {
		standardPackages.Store(dir, files)
		return clonePackageFiles(files)
	}
	return files
}

func clonePackageFiles(files []scan.PackageFile) []scan.PackageFile {
	files = slices.Clone(files)
	for i := range files {
		files[i].Declarations = slices.Clone(files[i].Declarations)
	}
	return files
}

func (b *build) fieldsAndCalls() {
	fields := map[token.Pos]*ast.Field{}
	declaredStructs := map[*ast.StructType]bool{}
	emitFields := func(s *ast.StructType) {
		for _, field := range s.Fields.List {
			locs := b.fieldTypes(field.Type)
			for _, id := range field.Names {
				f := scan.NamedField{Location: b.location(id.Pos()), Text: id.Name, ReferenceType: referenceType(b.info.TypeOf(field.Type)), TypeDeclarations: locs}
				if len(locs) > 0 {
					f.Declaration = locs[0]
				}
				b.result.NamedFields = append(b.result.NamedFields, f)
			}
		}
	}
	ast.Inspect(b.ast, func(node ast.Node) bool {
		if s, ok := node.(*ast.StructType); ok {
			for _, f := range s.Fields.List {
				for _, id := range f.Names {
					fields[id.Pos()] = f
				}
			}
		}
		return true
	})
	ast.Inspect(b.ast, func(node ast.Node) bool {
		if spec, ok := node.(*ast.TypeSpec); ok {
			ast.Inspect(spec.Type, func(node ast.Node) bool {
				if s, ok := node.(*ast.StructType); ok {
					emitFields(s)
					declaredStructs[s] = true
				}
				return true
			})
			return false
		}
		return true
	})
	ast.Inspect(b.ast, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.StructType:
			if !declaredStructs[n] {
				emitFields(n)
			}
		case *ast.CompositeLit:
			b.inlineFields(n, fields)
			if array, ok := n.Type.(*ast.ArrayType); ok {
				if _, ok := array.Elt.(*ast.StructType); ok {
					for _, elt := range n.Elts {
						if child, ok := elt.(*ast.CompositeLit); ok {
							b.inlineFields(child, fields)
						}
					}
				}
			}
		case *ast.CallExpr:
			var id *ast.Ident
			indirect := false
			switch fun := n.Fun.(type) {
			case *ast.Ident:
				id = fun
				if obj, ok := b.info.ObjectOf(id).(*types.Var); ok {
					_, indirect = obj.Type().Underlying().(*types.Signature)
				}
			case *ast.SelectorExpr:
				id = fun.Sel
				if obj, ok := b.info.ObjectOf(id).(*types.Var); ok {
					_, indirect = obj.Type().Underlying().(*types.Signature)
				}
				if sel := b.info.Selections[fun]; sel != nil {
					_, iface := sel.Recv().Underlying().(*types.Interface)
					indirect = indirect || iface
				}
			case *ast.IndexExpr, *ast.IndexListExpr:
				indirect = true
			case *ast.ParenExpr:
				if t := b.info.TypeOf(fun); t != nil {
					_, indirect = t.Underlying().(*types.Signature)
				}
			}
			if id != nil && !indirect {
				if obj := b.info.ObjectOf(id); obj == nil || obj.Type() == types.Typ[types.Invalid] {
					indirect = b.workspaceIndirect(id)
				}
			}
			if indirect {
				b.indirect[n] = true
				call := scan.IndirectCall{Location: b.location(n.Fun.Pos()), Text: strings.Repeat("x", int(n.Fun.End()-n.Fun.Pos()))}
				if id != nil {
					call.Location = b.location(id.Pos())
					call.Text = id.Name
				}
				b.result.IndirectCalls = append(b.result.IndirectCalls, call)
			}
		}
		return true
	})
}

// Workspace definitions distinguish interface methods and function-valued
// fields when the single-file checker has no object for a call target.
func (b *build) workspaceIndirect(id *ast.Ident) bool {
	loc := b.definition(id)
	if !scan.HasLocation(loc) {
		return false
	}
	if indirect, ok := b.callKinds[loc]; ok {
		return indirect
	}
	parsed := b.parseWorkspace(loc.File)
	if parsed.file == nil {
		return false
	}
	indirect := false
	ast.Inspect(parsed.file, func(node ast.Node) bool {
		if node == nil {
			return false
		}
		p := parsed.positions.Position(node.Pos())
		if p.Line != loc.Line || p.Column != loc.Column {
			return true
		}
		switch n := node.(type) {
		case *ast.Field:
			_, indirect = n.Type.(*ast.FuncType)
			return false
		case *ast.Ident:
			if n.Obj != nil {
				indirect = n.Obj.Kind != ast.Fun
			}
		}
		return true
	})
	b.callKinds[loc] = indirect
	return indirect
}

func (b *build) fieldTypes(expr ast.Expr) []scan.NamedFieldTypeDeclaration {
	var locs []scan.NamedFieldTypeDeclaration
	ast.Inspect(expr, func(node ast.Node) bool {
		if sel, ok := node.(*ast.SelectorExpr); ok {
			for _, loc := range b.fieldTypes(sel.Sel) {
				found := false
				for _, old := range locs {
					found = found || old == loc
				}
				if !found {
					locs = append(locs, loc)
				}
			}
			return false
		}
		if id, ok := node.(*ast.Ident); ok {
			if _, defined := b.info.Defs[id]; defined {
				return false
			}
			loc := b.definition(id)
			if scan.HasLocation(loc) {
				found := false
				for _, old := range locs {
					found = found || old.Location == loc
				}
				if !found {
					locs = append(locs, scan.NamedFieldTypeDeclaration{Location: loc})
				}
			}
		}
		return true
	})
	return locs
}

func (b *build) typeLocation(obj types.Object) scan.Location {
	if obj.Pkg() == b.pkg {
		return b.location(obj.Pos())
	}
	if obj.Pkg() != nil {
		for _, pkg := range b.result.Packages {
			if pkg.Name == obj.Pkg().Path() {
				for _, file := range pkg.Files {
					for _, decl := range file.Declarations {
						if decl.Name == obj.Name() && decl.Kind == scan.KindType {
							return decl.Location
						}
					}
				}
			}
		}
	}
	return scan.Location{Line: 1, Column: 1}
}

func (b *build) inlineFields(n *ast.CompositeLit, fields map[token.Pos]*ast.Field) {
	t := b.info.TypeOf(n)
	if t == nil {
		return
	}
	s, ok := t.Underlying().(*types.Struct)
	if !ok {
		return
	}
	var structDecl scan.Location
	if named, ok := t.(*types.Named); ok && (n.Type == nil || named.Obj().Pkg() != b.pkg) {
		structDecl = b.typeLocation(named.Obj())
	}
	for _, elt := range n.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		id, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		for i := 0; i < s.NumFields(); i++ {
			field := s.Field(i)
			if field.Name() != id.Name {
				continue
			}
			f := scan.NamedField{Location: b.location(id.Pos()), Text: id.Name, Inline: true, ReferenceType: referenceType(field.Type()), StructDecl: structDecl}
			if declared := fields[field.Pos()]; declared != nil && field.Pkg() == b.pkg {
				f.TypeDeclarations = b.fieldTypes(declared.Type)
			} else {
				loc := scan.Location{Line: 1, Column: 1}
				if named, ok := field.Type().(*types.Named); ok {
					loc = b.typeLocation(named.Obj())
				}
				f.TypeDeclarations = []scan.NamedFieldTypeDeclaration{{Location: loc}}
			}
			b.result.NamedFields = append(b.result.NamedFields, f)
		}
	}
}
