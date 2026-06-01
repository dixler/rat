package file

import (
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
}

type Declaration interface {
	Name() string
	Kind() Kind
	Location() Location
	References() []Reference
	Declarations() []Declaration
	Blocks() []Block
	Parent() Declaration
}

type ControlFlowStatement interface {
	Kind() string
	Location() Location
	ReturnsError() bool
}

type Block interface {
	Location() Location
	OpenBrace() Location
	CloseBrace() Location
	Blocks() []Block
	Statements() []ControlFlowStatement
	ControlFlowStatements() []ControlFlowStatement
	HasTerminalControlFlowStatement() bool
	HasStatementKind(kind string) bool
	HasDirectReturn() bool
	HasFallthrough() bool
}

type IfBlock interface {
	Block
	IfChainID() string
	Branches() []IfBranch
}

type IfBranch interface {
	Location() Location
	OpenBrace() Location
	CloseBrace() Location
	Step() int
	Blocks() []Block
	Statements() []ControlFlowStatement
	HasTerminalControlFlowStatement() bool
	Keyword() string
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
	HasEscapingControlFlow() bool
}

type SwitchBlock interface {
	Block
	SwitchKind() string
	CaseCount() int
	HasDefault() bool
	IsExhaustive() bool
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
	DeclarationLocation() Location
	DeclarationLocations() []Location
	DistanceLocation() Location
	Inline() bool
}

type Comment interface {
	Start() Location
	End() Location
}

type File interface {
	Name() string
	Source() string
	Tree() Declaration
	PackageReferences() []PackageReference
	Declarations() []Declaration
	Returns() []Location
	IndirectCalls() []IndirectCall
	Comments() []Comment
}

type file struct {
	name          string
	source        string
	root          *declaration
	packageRefs   []PackageReference
	decls         []Declaration
	namedFields   []NamedLocation
	returns       []Location
	indirectCalls []IndirectCall
	comments      []Comment
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
}

type controlFlowStatement struct {
	kind         string
	location     location
	returnsError bool
}

type blockBase struct {
	location                        Location
	openBrace                       *location
	closeBrace                      *location
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
	blockBase
	step int
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
	distanceLocation     *location
	declarationLocation  *location
	declarationLocations []Location
}

type commentSpan struct {
	start location
	end   location
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
	return buildTree(abs, string(src), raw)
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
func (f *file) Comments() []Comment           { return append([]Comment(nil), f.comments...) }

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

func (s *controlFlowStatement) Kind() string       { return s.kind }
func (s *controlFlowStatement) Location() Location { return s.location }
func (s *controlFlowStatement) ReturnsError() bool { return s.returnsError }

func (b *blockBase) Location() Location { return b.location }
func (b *blockBase) OpenBrace() Location {
	if b == nil {
		return nil
	}
	return b.openBrace
}
func (b *blockBase) CloseBrace() Location {
	if b == nil {
		return nil
	}
	return b.closeBrace
}
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
func (b *blockBase) HasStatementKind(kind string) bool {
	for _, stmt := range b.statements {
		if stmt != nil && stmt.Kind() == kind {
			return true
		}
	}
	return false
}
func (b *blockBase) HasDirectReturn() bool { return b.HasStatementKind("return") }
func (b *blockBase) HasFallthrough() bool  { return b.HasStatementKind("fallthrough") }

func (b *ifBlock) IfChainID() string { return b.ifChainID }
func (b *ifBlock) Branches() []IfBranch {
	return append([]IfBranch(nil), b.branches...)
}
func (b *ifBranchBase) Location() Location { return b.location }
func (b *ifBranchBase) OpenBrace() Location {
	if b == nil {
		return nil
	}
	return b.openBrace
}
func (b *ifBranchBase) CloseBrace() Location {
	if b == nil {
		return nil
	}
	return b.closeBrace
}
func (b *ifBranchBase) Step() int       { return b.step }
func (b *ifBranchBase) Blocks() []Block { return append([]Block(nil), b.blocks...) }
func (b *ifBranchBase) Statements() []ControlFlowStatement {
	return append([]ControlFlowStatement(nil), b.statements...)
}
func (b *ifBranchBase) HasTerminalControlFlowStatement() bool {
	return b.hasTerminalControlFlowStatement
}
func (b *ifBranchBase) Keyword() string { return "if" }
func (b *ifBranch) IsElseIf() bool      { return b.elseIf }
func (b *ifBranch) Keyword() string {
	if b.elseIf {
		return "else if"
	}
	return "if"
}

func (b *elseBranch) IsElse() bool                { return true }
func (b *elseBranch) Keyword() string             { return "else" }
func (b *loopBlock) LoopKind() string             { return b.kind }
func (b *loopBlock) MayBreak() bool               { return b.mayBreak }
func (b *loopBlock) MayReturn() bool              { return b.mayReturn }
func (b *loopBlock) HasEscapingControlFlow() bool { return b.mayBreak || b.mayReturn }
func (b *switchBlock) SwitchKind() string         { return b.kind }
func (b *switchBlock) CaseCount() int             { return b.caseCount }
func (b *switchBlock) HasDefault() bool           { return b.hasDefault }
func (b *switchBlock) IsExhaustive() bool         { return b.hasDefault }
func (b *caseBlock) IsDefault() bool              { return b.isDefault }

func (r *reference) Parent() Declaration      { return r.parent }
func (r *reference) Declaration() Declaration { return r.declaration }
func (r *reference) Location() Location       { return r.location }
func (r *reference) Text() string             { return r.text }
func (r *reference) Kind() Kind               { return r.kind }

func (r *packageReference) Package() PackageDeclaration { return r.pkg }

func (p *packageDeclaration) Name() string         { return p.name }
func (p *packageDeclaration) Location() Location   { return p.location }
func (p *packageDeclaration) Files() []Declaration { return append([]Declaration(nil), p.files...) }

func (n namedLocation) Location() Location { return n.location }
func (n namedLocation) Text() string       { return n.text }
func (n namedLocation) DeclarationLocation() Location {
	if n.declarationLocation == nil {
		return nil
	}
	return *n.declarationLocation
}
func (n namedLocation) DeclarationLocations() []Location {
	return append([]Location(nil), n.declarationLocations...)
}
func (n namedLocation) DistanceLocation() Location {
	if n.distanceLocation == nil {
		return nil
	}
	return *n.distanceLocation
}
func (n namedLocation) Inline() bool { return n.inline }

func (c commentSpan) Start() Location { return c.start }
func (c commentSpan) End() Location   { return c.end }

func TopLevelNamedFields(f File) []NamedLocation {
	if f == nil {
		return nil
	}
	if built, ok := f.(*file); ok {
		return append([]NamedLocation(nil), built.namedFields...)
	}

	var out []NamedLocation
	for _, field := range scan.TopLevelNamedFields(f.Name(), f.Source()) {
		out = append(out, buildNamedField(field))
	}

	return out
}

func buildNamedFields(fields []scan.NamedField) []NamedLocation {
	out := make([]NamedLocation, 0, len(fields))
	for _, field := range fields {
		out = append(out, buildNamedField(field))
	}
	return out
}

func buildNamedField(field scan.NamedField) NamedLocation {
	named := namedLocation{location: location{file: field.File, line: field.Line, column: field.Column}, text: field.Text, inline: field.Inline}
	if field.DistanceLine > 0 && field.DistanceColumn > 0 {
		loc := location{file: field.DistanceFile, line: field.DistanceLine, column: field.DistanceColumn}
		named.distanceLocation = &loc
	}
	for _, decl := range field.TypeDeclarations {
		if decl.Line < 1 || decl.Column < 1 {
			continue
		}
		loc := location{file: decl.File, line: decl.Line, column: decl.Column}
		named.declarationLocations = append(named.declarationLocations, loc)
	}
	if len(named.declarationLocations) > 0 {
		loc := named.declarationLocations[0].(location)
		named.declarationLocation = &loc
	} else if field.Declaration.Line > 0 && field.Declaration.Column > 0 {
		loc := location{file: field.Declaration.File, line: field.Declaration.Line, column: field.Declaration.Column}
		named.declarationLocation = &loc
		named.declarationLocations = append(named.declarationLocations, loc)
	}
	return named
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
