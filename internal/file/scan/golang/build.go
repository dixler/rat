package golang

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"hash/crc32"
	"path/filepath"
	"sync"

	"rat/internal/file/scan"
	"rat/internal/file/scan/golang/goplsclient"
)

type scanner struct{}

// Keep an overlay and its workspace queries together. Stripes bound lock
// storage while allowing unrelated documents to build concurrently.
var documentLocks [64]sync.Mutex

func init()                          { scan.Register(scanner{}) }
func (scanner) Extensions() []string { return []string{".go"} }

// A build owns its AST, type information and output records. The workspace
// client supplies locations that a single-file type check cannot resolve.
type build struct {
	file            string
	source          []byte
	fset            *token.FileSet
	ast             *ast.File
	info            *types.Info
	pkg             *types.Package
	client          *goplsclient.Client
	result          scan.Result
	next            int
	owners          map[ast.Node]string
	decls           map[string]*scan.Declaration
	objects         map[types.Object]string
	positions       map[token.Pos]string
	definitions     map[token.Pos]scan.Location
	objectLocations map[types.Object]scan.Location
	imports         map[string]scan.Package
	implicit        map[token.Pos]bool
	indirect        map[*ast.CallExpr]bool
	callKinds       map[scan.Location]bool
	workspace       map[string]parsedFile
}

type parsedFile struct {
	file      *ast.File
	positions *token.FileSet
}

func (scanner) Build(file string, source []byte) (*scan.Result, error) {
	file, err := filepath.Abs(file)
	if err != nil {
		return nil, err
	}
	b := &build{file: file, source: source, fset: token.NewFileSet(),
		owners: map[ast.Node]string{}, decls: map[string]*scan.Declaration{}, objects: map[types.Object]string{}, positions: map[token.Pos]string{},
		definitions: map[token.Pos]scan.Location{}, objectLocations: map[types.Object]scan.Location{}, imports: map[string]scan.Package{}, implicit: map[token.Pos]bool{}, indirect: map[*ast.CallExpr]bool{}, callKinds: map[scan.Location]bool{}, workspace: map[string]parsedFile{}}
	b.result.File = file
	b.ast, err = parser.ParseFile(b.fset, file, source, parser.ParseComments|parser.AllErrors)
	if b.ast == nil || b.ast.Name == nil || !b.ast.Package.IsValid() {
		return &b.result, nil
	}
	b.workspace[file] = parsedFile{b.ast, b.fset}
	b.info = &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}, Implicits: map[ast.Node]types.Object{}, Selections: map[*ast.SelectorExpr]*types.Selection{}}
	config := types.Config{Importer: importer.Default(), Error: func(error) {}}
	b.pkg, _ = config.Check(b.ast.Name.Name, b.fset, []*ast.File{b.ast}, b.info)
	lock := &documentLocks[crc32.ChecksumIEEE([]byte(file))%uint32(len(documentLocks))]
	lock.Lock()
	defer lock.Unlock()
	b.client, err = goplsclient.Default()
	if err != nil {
		return nil, err
	}
	if err := b.client.SyncDocumentContent(file, string(source)); err != nil {
		return nil, err
	}
	b.packages()
	b.index(b.ast, &b.result.Declarations, "", "")
	var bind func([]scan.Declaration)
	bind = func(ds []scan.Declaration) {
		for i := range ds {
			b.decls[ds[i].ID] = &ds[i]
			bind(ds[i].Declarations)
		}
	}
	bind(b.result.Declarations)
	b.references(b.ast, "")
	b.fieldsAndCalls()
	b.syntax()
	return &b.result, nil
}

func (b *build) parseWorkspace(file string) parsedFile {
	if parsed, ok := b.workspace[file]; ok {
		return parsed
	}
	fset := token.NewFileSet()
	parsed, _ := parser.ParseFile(fset, file, nil, 0)
	result := parsedFile{parsed, fset}
	b.workspace[file] = result
	return result
}

func (b *build) id(kind string) string { b.next++; return fmt.Sprintf("%s-%d", kind, b.next) }
func (b *build) location(pos token.Pos) scan.Location {
	if !pos.IsValid() {
		return scan.Location{}
	}
	p := b.fset.Position(pos)
	return scan.Location{File: p.Filename, Line: p.Line, Column: p.Column}
}
func (b *build) span(pos token.Pos, length int) scan.Span {
	p := b.location(pos)
	return scan.Span{Line: p.Line, Column: p.Column, Length: length}
}
func (b *build) spans(positions ...token.Pos) scan.NodeSpans {
	var spans scan.NodeSpans
	for _, pos := range positions {
		spans = append(spans, b.span(pos, 1))
	}
	return spans
}
func (b *build) definition(id *ast.Ident) scan.Location {
	if loc, ok := b.definitions[id.Pos()]; ok {
		return loc
	}
	var loc scan.Location
	obj := b.info.ObjectOf(id)
	if obj != nil {
		if loc, ok := b.objectLocations[obj]; ok {
			return loc
		}
	}
	if obj != nil && obj.Pos().IsValid() && obj.Pkg() == b.pkg {
		loc = b.location(obj.Pos())
	}
	if !scan.HasLocation(loc) {
		if found, ok, err := b.client.DefinitionForPosition(b.fset.Position(id.Pos())); err == nil && ok {
			loc = scan.Location{File: found.File, Line: found.Line, Column: found.Column}
		}
	}
	b.definitions[id.Pos()] = loc
	if obj != nil {
		b.objectLocations[obj] = loc
	}
	return loc
}

func referenceType(t types.Type) bool {
	seen := map[types.Type]bool{}
	var visit func(types.Type) bool
	visit = func(t types.Type) bool {
		if t == nil || seen[t] {
			return false
		}
		seen[t] = true
		switch t := t.(type) {
		case *types.Pointer, *types.Slice, *types.Map, *types.Chan, *types.Signature, *types.Interface:
			return true
		case *types.Named:
			return visit(t.Underlying())
		case *types.Alias:
			return visit(types.Unalias(t))
		case *types.Array:
			return visit(t.Elem())
		case *types.Struct:
			for i := 0; i < t.NumFields(); i++ {
				if visit(t.Field(i).Type()) {
					return true
				}
			}
		}
		return false
	}
	return visit(t)
}

func objectKind(obj types.Object) string {
	switch obj.(type) {
	case *types.TypeName:
		return scan.KindType
	case *types.Func:
		return scan.KindFunction
	case *types.PkgName:
		return scan.KindPackage
	}
	return scan.KindVariable
}
