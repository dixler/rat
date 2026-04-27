package file

import (
	"fmt"
	"os"
	"path/filepath"

	"notectl/internal/file/scan"
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

type Reference interface {
	Parent() Declaration
	Declaration() Declaration
	Location() Location
	Text() string
	Kind() Kind
}

type Declaration interface {
	Name() string
	Kind() Kind
	Location() Location
	References() []Reference
	Declarations() []Declaration
	Parent() Declaration
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

type File interface {
	Name() string
	Source() string
	Tree() Declaration
	PackageReferences() []PackageReference
	Declarations() []Declaration
}

type file struct {
	name        string
	source      string
	root        *declaration
	packageRefs []PackageReference
	decls       []Declaration
}

type location struct {
	file   string
	line   int
	column int
}

type declaration struct {
	name         string
	kind         Kind
	location     location
	references   []Reference
	declarations []Declaration
	parent       Declaration
}

type reference struct {
	parent      Declaration
	declaration Declaration
	location    location
	text        string
	kind        Kind
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

func Analyze(name string) (File, error) {
	return New(name)
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
	raw, err := scan.Build(abs)
	if err != nil {
		return nil, err
	}
	f := &file{name: abs, source: string(src)}
	root, pkgRefs, decls, err := buildTree(raw)
	if err != nil {
		return nil, fmt.Errorf("build file tree: %w", err)
	}
	f.root = root
	f.packageRefs = pkgRefs
	f.decls = decls
	return f, nil
}

func (f *file) Name() string      { return f.name }
func (f *file) Source() string    { return f.source }
func (f *file) Tree() Declaration { return f.root }
func (f *file) PackageReferences() []PackageReference {
	return append([]PackageReference(nil), f.packageRefs...)
}
func (f *file) Declarations() []Declaration { return append([]Declaration(nil), f.decls...) }

func (l location) File() string { return l.file }
func (l location) Line() int    { return l.line }
func (l location) Column() int  { return l.column }

func (d *declaration) Name() string            { return d.name }
func (d *declaration) Kind() Kind              { return d.kind }
func (d *declaration) Location() Location      { return d.location }
func (d *declaration) References() []Reference { return append([]Reference(nil), d.references...) }
func (d *declaration) Declarations() []Declaration {
	return append([]Declaration(nil), d.declarations...)
}
func (d *declaration) Parent() Declaration { return d.parent }

func (r *reference) Parent() Declaration      { return r.parent }
func (r *reference) Declaration() Declaration { return r.declaration }
func (r *reference) Location() Location       { return r.location }
func (r *reference) Text() string             { return r.text }
func (r *reference) Kind() Kind               { return r.kind }

func (r *packageReference) Package() PackageDeclaration { return r.pkg }

func (p *packageDeclaration) Name() string         { return p.name }
func (p *packageDeclaration) Location() Location   { return p.location }
func (p *packageDeclaration) Files() []Declaration { return append([]Declaration(nil), p.files...) }
