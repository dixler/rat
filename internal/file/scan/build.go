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
	return scanner.Build(file)
}

type Result struct {
	File              string
	Declarations      []Declaration
	PackageReferences []PackageReference
	Packages          []Package
	NamedFields       []NamedField
	Returns           []Return
	IndirectCalls     []IndirectCall
	Comments          []Comment
	Tokens            []Token
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

type Token struct {
	Location
	Text         string
	Kind         string
	AnchorLine   int
	AnchorColumn int
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

const (
	TokenKindDeclarationKeyword = "declaration-keyword"
	TokenKindControlKeyword     = "control-keyword"
	TokenKindEscapeKeyword      = "escape-keyword"
	TokenKindLiteral            = "literal"
	TokenKindPackageName        = "package-name"
	TokenKindLoopOperator       = "loop-operator"
	TokenKindBuiltin            = "builtin"
)

func IsBuiltinFile(file string) bool {
	p := filepath.Clean(file)
	return p == "" || strings.Contains(p, BuiltinFile)
}
