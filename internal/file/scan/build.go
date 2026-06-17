package scan

import (
	"fmt"
	"path/filepath"
	"strings"
)

type Scanner interface {
	Extensions() []string
	Build(file string, source []byte) (*Result, error)
}

var scannersByExtension = map[string]Scanner{}

func Register(scanner Scanner) {
	for _, ext := range scanner.Extensions() {
		ext = strings.ToLower(ext)
		if ext != "" && !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		scannersByExtension[ext] = scanner
	}
}

func Build(file string, source []byte) (*Result, error) {
	ext := strings.ToLower(filepath.Ext(file))
	scanner := scannersByExtension[ext]
	if scanner == nil {
		return nil, fmt.Errorf("unsupported file type %q", filepath.Ext(file))
	}
	result, err := scanner.Build(file, source)
	if result != nil {
		result.Nodes = append(result.Nodes, BuildNodes(result, source)...)
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
	IndirectCalls     []IndirectCall
}

type Span struct {
	Line   int
	Column int
	Length int
}

type Node interface {
	Spans() []Span
}

type NodeSpans []Span

func (spans NodeSpans) Spans() []Span { return append([]Span(nil), spans...) }

type CondNode struct {
	NodeSpans
	IsGuard bool
}

type PartialNode struct {
	NodeSpans
	IsComplete bool
}

type LoopNode struct {
	NodeSpans
	HasExit bool
}

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

type DeclarationSyntaxNode struct{ NodeSpans }

type MutableTypeSyntaxNode struct{ NodeSpans }

type FunctionSyntaxNode struct {
	NodeSpans
	ReturnsError bool
}

type InlineFunctionIndentNode struct{ NodeSpans }

type ProgramSyntaxNode struct{ NodeSpans }

type EscapeSyntaxNode struct{ NodeSpans }

type LiteralNode struct{ NodeSpans }

type CommentNode struct{ NodeSpans }

type PackageNameNode struct{ NodeSpans }

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

type IndirectCall struct {
	Location
	Text string
}

type Declaration struct {
	Location
	ID            string
	Name          string
	Kind          string
	ReferenceType bool
	References    []Reference
	Declarations  []Declaration
	ControlFlow   []ControlFlowBlock
}

type ControlFlowStatement struct {
	Location
	Kind         string
	ReturnsError bool
	IsAbort      bool
}

type ControlFlowBlock struct {
	Location
	Kind             string
	OpenBraceLine    int
	OpenBraceColumn  int
	CloseBraceLine   int
	CloseBraceColumn int
	HasAbort         bool
	Statements       []ControlFlowStatement
	Blocks           []ControlFlowBlock
	HasDefault       bool
	MayBreak         bool
	MayReturn        bool
}

func (b ControlFlowBlock) HasAbortStmt(recursive bool) bool {
	for _, stmt := range b.Statements {
		switch stmt.Kind {
		case "return", "throw", "continue", "goto", StatementKindPanic:
			return true
		case "break":
			if !stmt.IsAbort {
				return true
			}
		}
	}
	if recursive {
		for _, child := range b.Blocks {
			if child.HasAbortStmt(true) {
				return true
			}
		}
	}
	return false
}

type Reference struct {
	Location
	DeclarationID string
	Declaration   Location
	Text          string
	Kind          string
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
	StructDecl       Location
	Declaration      NamedFieldTypeDeclaration
	TypeDeclarations []NamedFieldTypeDeclaration
}

type NamedFieldTypeDeclaration struct {
	Location
}

func HasLocation(loc Location) bool {
	return loc.File != "" && loc.Line > 0 && loc.Column > 0
}

func BuiltinLocation(language string) Location {
	return Location{File: BuiltinFile + "/" + language, Line: 1, Column: 1}
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
