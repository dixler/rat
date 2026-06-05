package scan

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
)

type Scanner interface {
	Extensions() []string
	Build(file string) (*Result, error)
}

var scannersMu sync.RWMutex
var scannersByExtension = map[string]Scanner{}

func Register(scanner Scanner) {
	scannersMu.Lock()
	defer scannersMu.Unlock()
	for _, ext := range scanner.Extensions() {
		ext = strings.ToLower(ext)
		if ext != "" && !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		scannersByExtension[ext] = scanner
	}
}

func Build(file string) (*Result, error) {
	ext := strings.ToLower(filepath.Ext(file))
	scannersMu.RLock()
	scanner := scannersByExtension[ext]
	scannersMu.RUnlock()
	if scanner == nil {
		return nil, fmt.Errorf("unsupported file type %q", filepath.Ext(file))
	}
	result, err := scanner.Build(file)
	if result != nil {
		result.Nodes = append(result.Nodes, BuildNodes(result)...)
	}
	return result, err
}

type Result struct {
	File              string
	Nodes             []Node
	Declarations      []Declaration
	PackageReferences []PackageReference
	Packages          []Package
	NamedFields       []NamedField
	Returns           []Return
	IndirectCalls     []IndirectCall
	Comments          []Comment
}

type Span struct {
	Line   int
	Column int
	Length int
}

type Node interface {
	Spans() []Span
}

type IdentRole int

const (
	IdentRoleDeclaration IdentRole = iota + 1
	IdentRoleReference
)

type IdentNode struct {
	Span          Span
	Role          IdentRole
	Kind          string
	Escapes       bool
	ReferenceType bool
}

func (n IdentNode) Spans() []Span { return oneSpan(n.Span) }

type CondNode struct {
	NodeSpans []Span
	IsGuard   bool
}

func (n CondNode) Spans() []Span { return append([]Span(nil), n.NodeSpans...) }

type MatchNode struct {
	NodeSpans  []Span
	HasDefault bool
}

func (n MatchNode) Spans() []Span { return append([]Span(nil), n.NodeSpans...) }

type LoopNode struct {
	NodeSpans []Span
	HasExit   bool
}

func (n LoopNode) Spans() []Span { return append([]Span(nil), n.NodeSpans...) }

type JumpKind int

const (
	JumpKindExit JumpKind = iota + 1
	JumpKindErrorExit
	JumpKindContinue
	JumpKindBreak
	JumpKindEscape
	JumpKindFallthrough
)

type JumpNode struct {
	Span Span
	Kind JumpKind
}

func (n JumpNode) Spans() []Span { return oneSpan(n.Span) }

type DeclarationSyntaxNode struct{ NodeSpans []Span }

func (n DeclarationSyntaxNode) Spans() []Span { return append([]Span(nil), n.NodeSpans...) }

type ProgramSyntaxNode struct{ NodeSpans []Span }

func (n ProgramSyntaxNode) Spans() []Span { return append([]Span(nil), n.NodeSpans...) }

type EscapeSyntaxNode struct{ NodeSpans []Span }

func (n EscapeSyntaxNode) Spans() []Span { return append([]Span(nil), n.NodeSpans...) }

type LiteralNode struct{ NodeSpans []Span }

func (n LiteralNode) Spans() []Span { return append([]Span(nil), n.NodeSpans...) }

type BuiltinNode struct{ NodeSpans []Span }

func (n BuiltinNode) Spans() []Span { return append([]Span(nil), n.NodeSpans...) }

type PackageNameNode struct{ NodeSpans []Span }

func (n PackageNameNode) Spans() []Span { return append([]Span(nil), n.NodeSpans...) }

type LoopOperatorNode struct {
	Span   Span
	Anchor Span
}

func (n LoopOperatorNode) Spans() []Span { return oneSpan(n.Span) }

func oneSpan(span Span) []Span {
	if span.Line < 1 || span.Column < 1 || span.Length < 1 {
		return nil
	}
	return []Span{span}
}

func SpansForText(line, column int, text string) []Span {
	if line < 1 || column < 1 || text == "" {
		return nil
	}
	parts := strings.Split(text, "\n")
	spans := make([]Span, 0, len(parts))
	for i, part := range parts {
		if part == "" {
			continue
		}
		col := 1
		if i == 0 {
			col = column
		}
		spans = append(spans, Span{Line: line + i, Column: col, Length: len(part)})
	}
	return spans
}

type Location struct {
	File   string
	Line   int
	Column int
}

type Comment struct {
	StartLine   int
	StartColumn int
	EndLine     int
	EndColumn   int
}

type IndirectCall struct {
	Location
	Text string
}

type Return struct {
	Location
}

type Declaration struct {
	Location
	ID            string
	Name          string
	Kind          string
	Escapes       bool
	ReferenceType bool
	References    []Reference
	Declarations  []Declaration
	ControlFlow   []ControlFlowBlock
}

type ControlFlowStatement struct {
	Location
	Kind         string
	ReturnsError bool
}

type ControlFlowBlock struct {
	Location
	Kind                            string
	OpenBraceLine                   int
	OpenBraceColumn                 int
	CloseBraceLine                  int
	CloseBraceColumn                int
	HasTerminalControlFlowStatement bool
	IfChainID                       string
	IfStep                          int
	Statements                      []ControlFlowStatement
	Blocks                          []ControlFlowBlock
	CaseCount                       int
	HasDefault                      bool
	MayBreak                        bool
	MayReturn                       bool
}

type Reference struct {
	Location
	DeclarationID string
	Declaration   DefinitionLocation
	Text          string
	Kind          string
	Escapes       bool
	ReferenceType bool
}

type PackageReference struct {
	Location
	PackageID string
	ParentID  string
	Text      string
}

type Package struct {
	Location
	ID    string
	Name  string
	Files []PackageFile
}

type PackageFile struct {
	Location
	Declarations []DeclarationSummary
}

type DeclarationSummary struct {
	Location
	Name string
	Kind string
}

type NamedField struct {
	Location
	Text             string
	Inline           bool
	ReferenceType    bool
	StructDecl       DefinitionLocation
	Declaration      NamedFieldTypeDeclaration
	TypeDeclarations []NamedFieldTypeDeclaration
}

type NamedFieldTypeDeclaration struct {
	Location
}

type DefinitionLocation struct {
	File   string
	Line   int
	Column int
	OK     bool
}

func NewDefinitionLocation(file string, line, column int) DefinitionLocation {
	return DefinitionLocation{File: file, Line: line, Column: column, OK: file != "" && line > 0 && column > 0}
}

func BuiltinDefinitionLocation(language string) DefinitionLocation {
	return NewDefinitionLocation(BuiltinFile+"/"+language, 1, 1)
}

func (l DefinitionLocation) Location() Location {
	if !l.OK {
		return Location{}
	}
	return Location{File: l.File, Line: l.Line, Column: l.Column}
}

const (
	KindPackage   = "package"
	KindType      = "type"
	KindVariable  = "variable"
	KindParameter = "parameter"
	KindFunction  = "function"
	KindFile      = "file"
)

const (
	BlockKindBase    = "block"
	BlockKindIf      = "if"
	BlockKindElseIf  = "elseif"
	BlockKindElse    = "else"
	BlockKindFor     = "for"
	BlockKindWhile   = "while"
	BlockKindDo      = "do"
	BlockKindSwitch  = "switch"
	BlockKindSelect  = "select"
	BlockKindCase    = "case"
	BlockKindTry     = "try"
	BlockKindCatch   = "catch"
	BlockKindFinally = "finally"
)

const (
	ConstructKindBase              = "base"
	ConstructKindBranch            = "branch"
	ConstructKindBranchAlternative = "branch-alternative"
	ConstructKindLoop              = "loop"
	ConstructKindExhaustiveMatch   = "exhaustive-match"
	ConstructKindCase              = "case"
)

func BlockConstructKind(kind string) string {
	switch kind {
	case BlockKindIf, BlockKindTry:
		return ConstructKindBranch
	case BlockKindElseIf, BlockKindElse, BlockKindCatch, BlockKindFinally:
		return ConstructKindBranchAlternative
	case BlockKindFor, BlockKindWhile, BlockKindDo:
		return ConstructKindLoop
	case BlockKindSwitch, BlockKindSelect:
		return ConstructKindExhaustiveMatch
	case BlockKindCase:
		return ConstructKindCase
	default:
		return ConstructKindBase
	}
}

const (
	StatementKindPanic = "panic"
	BuiltinFile        = "/src/builtin"
)

func IsBuiltinFile(file string) bool {
	p := filepath.Clean(file)
	return p == "" || strings.Contains(p, BuiltinFile)
}
