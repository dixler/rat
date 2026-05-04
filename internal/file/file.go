package file

import (
	"fmt"
	"os"
	"path/filepath"

	"rat/internal/file/scan"
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
	Escapes() bool
}

type Declaration interface {
	Name() string
	Kind() Kind
	Location() Location
	References() []Reference
	Declarations() []Declaration
	Blocks() []Block
	Parent() Declaration
	Escapes() bool
}

type ControlFlowStatement interface {
	Kind() string
	Location() Location
}

type Block interface {
	Location() Location
	Blocks() []Block
	Statements() []ControlFlowStatement
	ControlFlowStatements() []ControlFlowStatement
}

type IfBlock interface {
	Block
	IfChainID() string
	Branches() []IfBranch
}

type IfBranch interface {
	Location() Location
	Step() int
	Blocks() []Block
}

type ConditionalBranch interface {
	IfBranch
	IsElseIf() bool
}

type ElseBranch interface {
	IfBranch
	IsElse() bool
}

type LoopBlock interface {
	Block
	LoopKind() string
	HasBreak() bool
}

type SwitchBlock interface {
	Block
	SwitchKind() string
	CaseCount() int
	HasDefault() bool
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
	Returns() []Location
	IndirectCalls() []IndirectCall
}

type file struct {
	name          string
	source        string
	root          *declaration
	packageRefs   []PackageReference
	decls         []Declaration
	returns       []Location
	indirectCalls []IndirectCall
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
	blocks       []Block
	parent       Declaration
	escapes      bool
}

type controlFlowStatement struct {
	kind     string
	location location
}

type blockBase struct {
	location   location
	blocks     []Block
	statements []ControlFlowStatement
}

type ifBlock struct {
	blockBase
	ifChainID string
	branches  []IfBranch
}

type ifBranch struct {
	location location
	step     int
	blocks   []Block
	elseIf   bool
}

type elseBranch struct {
	location location
	step     int
	blocks   []Block
}

type loopBlock struct {
	blockBase
	kind     string
	hasBreak bool
}

type switchBlock struct {
	blockBase
	kind       string
	caseCount  int
	hasDefault bool
}

type caseBlock struct {
	blockBase
}

type anonymousBlock struct {
	blockBase
}

type reference struct {
	parent      Declaration
	declaration Declaration
	location    location
	text        string
	kind        Kind
	escapes     bool
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
	root, pkgRefs, decls, returns, indirectCalls, err := buildTree(raw)
	if err != nil {
		return nil, fmt.Errorf("build file tree: %w", err)
	}
	f.root = root
	f.packageRefs = pkgRefs
	f.decls = decls
	f.returns = returns
	f.indirectCalls = indirectCalls
	return f, nil
}

func (f *file) Name() string      { return f.name }
func (f *file) Source() string    { return f.source }
func (f *file) Tree() Declaration { return f.root }
func (f *file) PackageReferences() []PackageReference {
	return append([]PackageReference(nil), f.packageRefs...)
}
func (f *file) Declarations() []Declaration   { return append([]Declaration(nil), f.decls...) }
func (f *file) Returns() []Location           { return append([]Location(nil), f.returns...) }
func (f *file) IndirectCalls() []IndirectCall { return append([]IndirectCall(nil), f.indirectCalls...) }

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
func (d *declaration) Blocks() []Block     { return append([]Block(nil), d.blocks...) }
func (d *declaration) Parent() Declaration { return d.parent }
func (d *declaration) Escapes() bool       { return d.escapes }

func (s *controlFlowStatement) Kind() string       { return s.kind }
func (s *controlFlowStatement) Location() Location { return s.location }

func (b *blockBase) Location() Location { return b.location }
func (b *blockBase) Blocks() []Block {
	return append([]Block(nil), b.blocks...)
}
func (b *blockBase) Statements() []ControlFlowStatement {
	return append([]ControlFlowStatement(nil), b.statements...)
}
func (b *blockBase) ControlFlowStatements() []ControlFlowStatement {
	out := append([]ControlFlowStatement(nil), b.statements...)
	for _, child := range b.blocks {
		out = append(out, child.ControlFlowStatements()...)
	}
	return out
}

func (b *ifBlock) IfChainID() string { return b.ifChainID }
func (b *ifBlock) Branches() []IfBranch {
	return append([]IfBranch(nil), b.branches...)
}
func (b *ifBranch) Location() Location { return b.location }
func (b *ifBranch) Step() int          { return b.step }
func (b *ifBranch) Blocks() []Block    { return append([]Block(nil), b.blocks...) }
func (b *ifBranch) IsElseIf() bool     { return b.elseIf }

func (b *elseBranch) Location() Location  { return b.location }
func (b *elseBranch) Step() int           { return b.step }
func (b *elseBranch) Blocks() []Block     { return append([]Block(nil), b.blocks...) }
func (b *elseBranch) IsElse() bool        { return true }
func (b *loopBlock) LoopKind() string     { return b.kind }
func (b *loopBlock) HasBreak() bool       { return b.hasBreak }
func (b *switchBlock) SwitchKind() string { return b.kind }
func (b *switchBlock) CaseCount() int     { return b.caseCount }
func (b *switchBlock) HasDefault() bool   { return b.hasDefault }

func (r *reference) Parent() Declaration      { return r.parent }
func (r *reference) Declaration() Declaration { return r.declaration }
func (r *reference) Location() Location       { return r.location }
func (r *reference) Text() string             { return r.text }
func (r *reference) Kind() Kind               { return r.kind }
func (r *reference) Escapes() bool            { return r.escapes }

func (r *packageReference) Package() PackageDeclaration { return r.pkg }

func (p *packageDeclaration) Name() string         { return p.name }
func (p *packageDeclaration) Location() Location   { return p.location }
func (p *packageDeclaration) Files() []Declaration { return append([]Declaration(nil), p.files...) }

type indirectCall struct {
	location location
	text     string
}

func (c *indirectCall) Location() Location { return c.location }
func (c *indirectCall) Text() string       { return c.text }
