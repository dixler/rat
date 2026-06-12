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
	decls         []Declaration
	namedFields   []NamedLocation
	indirectCalls []IndirectCall
}

type location struct {
	file   string
	line   int
	column int
}

type declaration struct {
	name          string
	kind          Kind
	location      location
	referenceType bool
	references    []Reference
	declarations  []Declaration
	parent        Declaration
}

type reference struct {
	parent        Declaration
	declaration   Declaration
	location      location
	text          string
	kind          Kind
	referenceType bool
}

type packageReference struct {
	*reference
	pkg PackageDeclaration
}

type packageDeclaration struct {
	name     string
	location location
	files    []Declaration
}

type namedLocation struct {
	location             location
	text                 string
	inline               bool
	referenceType        bool
	distanceLocation     *location
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
	raw, err := scan.Build(abs, src)
	if err != nil {
		return nil, err
	}
	return buildTree(abs, string(src), raw)
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
func (f *file) Declarations() []Declaration { return clone(f.decls) }
func (f *file) TopLevelNamedFields() []NamedLocation {
	return clone(f.namedFields)
}
func (f *file) IndirectCalls() []IndirectCall { return clone(f.indirectCalls) }

func (l location) File() string { return l.file }
func (l location) Line() int    { return l.line }
func (l location) Column() int  { return l.column }

func (d *declaration) Name() string            { return d.name }
func (d *declaration) Kind() Kind              { return d.kind }
func (d *declaration) Location() Location      { return d.location }
func (d *declaration) References() []Reference { return clone(d.references) }
func (d *declaration) Declarations() []Declaration {
	return clone(d.declarations)
}
func (d *declaration) Parent() Declaration { return d.parent }
func (d *declaration) ReferenceType() bool { return d.referenceType }

func (r *reference) Parent() Declaration      { return r.parent }
func (r *reference) Declaration() Declaration { return r.declaration }
func (r *reference) Location() Location       { return r.location }
func (r *reference) Text() string             { return r.text }
func (r *reference) Kind() Kind               { return r.kind }
func (r *reference) ReferenceType() bool      { return r.referenceType }

func (r *packageReference) Package() PackageDeclaration { return r.pkg }

func (p *packageDeclaration) Name() string         { return p.name }
func (p *packageDeclaration) Location() Location   { return p.location }
func (p *packageDeclaration) Files() []Declaration { return clone(p.files) }

func (n namedLocation) Location() Location  { return n.location }
func (n namedLocation) Text() string        { return n.text }
func (n namedLocation) ReferenceType() bool { return n.referenceType }
func (n namedLocation) DeclarationLocations() []Location {
	return clone(n.declarationLocations)
}
func (n namedLocation) DistanceLocation() Location {
	if n.distanceLocation == nil {
		return nil
	}
	return *n.distanceLocation
}
func (n namedLocation) Inline() bool { return n.inline }

func buildNamedFields(fields []scan.NamedField) []NamedLocation {
	out := make([]NamedLocation, 0, len(fields))
	for _, field := range fields {
		named := namedLocation{location: fromScanLocation(field.Location), text: field.Text, inline: field.Inline, referenceType: field.ReferenceType}
		if loc, ok := optionalLocation(field.StructDecl); ok {
			named.distanceLocation = &loc
		}
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

type indirectCall struct {
	location location
	text     string
}

func (c *indirectCall) Location() Location { return c.location }
func (c *indirectCall) Text() string       { return c.text }

func clone[T any](in []T) []T { return append([]T(nil), in...) }

func fromScanLocation(loc scan.Location) location {
	return location{file: loc.File, line: loc.Line, column: loc.Column}
}

func optionalLocation(loc scan.Location) (location, bool) {
	if loc.Line < 1 || loc.Column < 1 {
		return location{}, false
	}
	return fromScanLocation(loc), true
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
	markers := [...]string{".git", "go.mod", "package.json"}
	for {
		if hasMarker(dir, markers[:]...) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func pathExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func hasMarker(dir string, names ...string) bool {
	for _, name := range names {
		if pathExists(filepath.Join(dir, name)) {
			return true
		}
	}
	return false
}
