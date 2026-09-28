package file

import (
	"os"
	"path/filepath"

	"rat/internal/file/scan"
	_ "rat/internal/file/scan/golang"
)

type Kind string

const (
	KindPackage   Kind = "package"
	KindType      Kind = "type"
	KindVariable  Kind = "variable"
	KindParameter Kind = "parameter"
	KindFunction  Kind = "function"
	KindFile      Kind = "file"
)

type Location interface {
	File() string
	Line() int
	Column() int
}

type IndirectCall interface {
	Location() Location
	Text() string
}

type Reference interface {
	Parent() Declaration
	Declaration() Declaration
	Location() Location
	Text() string
	Kind() Kind
	ReferenceType() bool
}

type Declaration interface {
	Name() string
	Kind() Kind
	Location() Location
	References() []Reference
	Declarations() []Declaration
	Parent() Declaration
	ReferenceType() bool
}

type PackageReference interface {
	Reference
	Package() PackageDeclaration
}

type PackageDeclaration interface {
	Name() string
	Location() Location
	Files() []Declaration
}

type NamedLocation interface {
	Location() Location
	Text() string
	DeclarationLocations() []Location
	DistanceLocation() Location
	Inline() bool
	ReferenceType() bool
}

type File interface {
	Name() string
	Source() string
	SourceLines() []string
	ProjectRoot() string
	Tree() Declaration
	Nodes() []scan.Node
	PackageReferences() []PackageReference
	Declarations() []Declaration
	TopLevelNamedFields() []NamedLocation
	IndirectCalls() []IndirectCall
}

type file struct {
	name          string
	source        string
	sourceLines   []string
	root          *declaration
	nodes         []scan.Node
	packageRefs   []PackageReference
	namedFields   []NamedLocation
	indirectCalls []IndirectCall
}

type location struct{ scan.Location }

type declaration struct {
	raw          scan.Declaration
	references   []Reference
	declarations []Declaration
	parent       Declaration
}

type reference struct {
	raw         scan.Reference
	parent      Declaration
	declaration Declaration
}

type packageReference struct {
	*reference
	pkg PackageDeclaration
}

type packageDeclaration struct {
	raw   scan.Package
	files []Declaration
}

type namedLocation struct {
	raw                  scan.NamedField
	declarationLocations []Location
}

func New(name string) (File, error) {
	abs, err := filepath.Abs(name)
	if err != nil {
		return nil, err
	}
	src, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	return NewContent(abs, src)
}

func NewContent(name string, src []byte) (File, error) {
	abs, err := filepath.Abs(name)
	if err != nil {
		return nil, err
	}
	raw, err := scan.Build(abs, src)
	if err != nil {
		return nil, err
	}
	return buildTree(abs, string(src), raw), nil
}

func (f *file) Name() string   { return f.name }
func (f *file) Source() string { return f.source }
func (f *file) SourceLines() []string {
	return clone(f.sourceLines)
}
func (f *file) ProjectRoot() string { return projectRoot(f.name) }
func (f *file) Tree() Declaration   { return f.root }
func (f *file) Nodes() []scan.Node  { return clone(f.nodes) }
func (f *file) PackageReferences() []PackageReference {
	return clone(f.packageRefs)
}
func (f *file) Declarations() []Declaration { return f.root.Declarations() }
func (f *file) TopLevelNamedFields() []NamedLocation {
	return clone(f.namedFields)
}
func (f *file) IndirectCalls() []IndirectCall { return clone(f.indirectCalls) }

func (l location) File() string { return l.Location.File }
func (l location) Line() int    { return l.Location.Line }
func (l location) Column() int  { return l.Location.Column }

func (d *declaration) Name() string            { return d.raw.Name }
func (d *declaration) Kind() Kind              { return Kind(d.raw.Kind) }
func (d *declaration) Location() Location      { return location{d.raw.Location} }
func (d *declaration) References() []Reference { return clone(d.references) }
func (d *declaration) Declarations() []Declaration {
	return clone(d.declarations)
}
func (d *declaration) Parent() Declaration { return d.parent }
func (d *declaration) ReferenceType() bool { return d.raw.ReferenceType }

func (r *reference) Parent() Declaration      { return r.parent }
func (r *reference) Declaration() Declaration { return r.declaration }
func (r *reference) Location() Location       { return location{r.raw.Location} }
func (r *reference) Text() string             { return r.raw.Text }
func (r *reference) Kind() Kind               { return Kind(r.raw.Kind) }
func (r *reference) ReferenceType() bool      { return r.raw.ReferenceType }

func (r *packageReference) Package() PackageDeclaration { return r.pkg }

func (p *packageDeclaration) Name() string         { return p.raw.Name }
func (p *packageDeclaration) Location() Location   { return location{p.raw.Location} }
func (p *packageDeclaration) Files() []Declaration { return clone(p.files) }

func (n namedLocation) Location() Location  { return location{n.raw.Location} }
func (n namedLocation) Text() string        { return n.raw.Text }
func (n namedLocation) ReferenceType() bool { return n.raw.ReferenceType }
func (n namedLocation) DeclarationLocations() []Location {
	return clone(n.declarationLocations)
}
func (n namedLocation) DistanceLocation() Location {
	if loc, ok := optionalLocation(n.raw.StructDecl); ok {
		return loc
	}
	return nil
}
func (n namedLocation) Inline() bool { return n.raw.Inline }

func buildNamedFields(fields []scan.NamedField) []NamedLocation {
	out := make([]NamedLocation, 0, len(fields))
	for _, field := range fields {
		named := namedLocation{raw: field}
		for _, decl := range field.TypeDeclarations {
			if loc, ok := optionalLocation(decl.Location); ok {
				named.declarationLocations = append(named.declarationLocations, loc)
			}
		}
		if len(named.declarationLocations) == 0 {
			if loc, ok := optionalLocation(field.Declaration.Location); ok {
				named.declarationLocations = append(named.declarationLocations, loc)
			}
		}
		out = append(out, named)
	}
	return out
}

type indirectCall struct{ raw scan.IndirectCall }

func (c *indirectCall) Location() Location { return location{c.raw.Location} }
func (c *indirectCall) Text() string       { return c.raw.Text }

func clone[T any](in []T) []T { return append([]T(nil), in...) }

func optionalLocation(loc scan.Location) (location, bool) {
	if loc.Line < 1 || loc.Column < 1 {
		return location{}, false
	}
	return location{loc}, true
}

func projectRoot(path string) string {
	path = filepath.Clean(path)
	abs, err := filepath.Abs(path)
	if err == nil {
		path = abs
	}
	dir := path
	if filepath.Ext(dir) != "" {
		dir = filepath.Dir(dir)
	}
	for {
		for _, marker := range []string{".git", "go.mod", "package.json"} {
			if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
