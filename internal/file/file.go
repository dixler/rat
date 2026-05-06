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
	HasTerminalControlFlowStatement() bool
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
	Statements() []ControlFlowStatement
	HasTerminalControlFlowStatement() bool
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
	MayBreak() bool
	MayReturn() bool
}

type SwitchBlock interface {
	Block
	SwitchKind() string
	CaseCount() int
	HasDefault() bool
}

type CaseBlock interface {
	Block
	IsDefault() bool
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
	location                        location
	blocks                          []Block
	statements                      []ControlFlowStatement
	hasTerminalControlFlowStatement bool
}

type ifBlock struct {
	blockBase
	ifChainID string
	branches  []IfBranch
}

type ifBranch struct {
	ifBranchBase
	elseIf bool
}

type elseBranch struct {
	ifBranchBase
}

type ifBranchBase struct {
	location                        location
	step                            int
	blocks                          []Block
	statements                      []ControlFlowStatement
	hasTerminalControlFlowStatement bool
}

type loopBlock struct {
	blockBase
	kind      string
	mayBreak  bool
	mayReturn bool
}

type switchBlock struct {
	blockBase
	kind       string
	caseCount  int
	hasDefault bool
}

type caseBlock struct {
	blockBase
	isDefault bool
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

type namedLocation struct {
	location location
	text     string
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
func (b *blockBase) HasTerminalControlFlowStatement() bool { return b.hasTerminalControlFlowStatement }

func (b *ifBlock) IfChainID() string { return b.ifChainID }
func (b *ifBlock) Branches() []IfBranch {
	return append([]IfBranch(nil), b.branches...)
}
func (b *ifBranchBase) Location() Location { return b.location }
func (b *ifBranchBase) Step() int          { return b.step }
func (b *ifBranchBase) Blocks() []Block    { return append([]Block(nil), b.blocks...) }
func (b *ifBranchBase) Statements() []ControlFlowStatement {
	return append([]ControlFlowStatement(nil), b.statements...)
}
func (b *ifBranchBase) HasTerminalControlFlowStatement() bool {
	return b.hasTerminalControlFlowStatement
}
func (b *ifBranch) IsElseIf() bool { return b.elseIf }

func (b *elseBranch) IsElse() bool        { return true }
func (b *loopBlock) LoopKind() string     { return b.kind }
func (b *loopBlock) MayBreak() bool       { return b.mayBreak }
func (b *loopBlock) MayReturn() bool      { return b.mayReturn }
func (b *switchBlock) SwitchKind() string { return b.kind }
func (b *switchBlock) CaseCount() int     { return b.caseCount }
func (b *switchBlock) HasDefault() bool   { return b.hasDefault }
func (b *caseBlock) IsDefault() bool      { return b.isDefault }

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

func (n namedLocation) Location() Location { return n.location }
func (n namedLocation) Text() string       { return n.text }

func TopLevelNamedFields(f File) []NamedLocation {
	if f == nil {
		return nil
	}

	var out []NamedLocation
	for _, field := range scan.TopLevelNamedFields(f.Name(), f.Source()) {
		out = append(out, namedLocation{location: location{file: field.File, line: field.Line, column: field.Column}, text: field.Text})
	}

	return out
}

type indirectCall struct {
	location location
	text     string
}

func (c *indirectCall) Location() Location { return c.location }
func (c *indirectCall) Text() string       { return c.text }

func ProjectRoot(path string) string {
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
		if pathExists(filepath.Join(dir, ".git")) || pathExists(filepath.Join(dir, "go.mod")) {
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
