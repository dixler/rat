package scan

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"rat/internal/goplsclient"
)

var (
	escapeAnalysisCache sync.Map
	packageFilesCache   sync.Map
)

type Result struct {
	File              string
	Declarations      []Declaration
	PackageReferences []PackageReference
	Packages          []Package
	NamedFields       []NamedField
	Returns           []Return
	IndirectCalls     []IndirectCall
	Comments          []Comment
}

type Comment struct {
	StartLine   int
	StartColumn int
	EndLine     int
	EndColumn   int
}

type IndirectCall struct {
	File   string
	Line   int
	Column int
	Text   string
}

type Return struct {
	File   string
	Line   int
	Column int
}

type Declaration struct {
	ID           string
	Name         string
	Kind         string
	File         string
	Line         int
	Column       int
	Escapes      bool
	References   []Reference
	Declarations []Declaration
	ControlFlow  []ControlFlowBlock
}

type ControlFlowStatement struct {
	Kind         string
	File         string
	Line         int
	Column       int
	ReturnsError bool
}

type ControlFlowBlock struct {
	Kind                    string
	File                    string
	Line                    int
	Column                  int
	OpenBraceLine           int
	OpenBraceColumn         int
	CloseBraceLine          int
	CloseBraceColumn        int
	HasControlFlowStatement bool
	IfChainID               string
	IfStep                  int
	Statements              []ControlFlowStatement
	Blocks                  []ControlFlowBlock
	CaseCount               int
	HasDefault              bool
	MayBreak                bool
	MayReturn               bool
}

func (b ControlFlowBlock) HasTerminalControlFlowStatement() bool {
	return b.HasControlFlowStatement
}

type Reference struct {
	DeclarationID     string
	DeclarationFile   string
	DeclarationLine   int
	DeclarationColumn int
	Text              string
	Kind              string
	File              string
	Line              int
	Column            int
	Escapes           bool
}

type PackageReference struct {
	PackageID string
	ParentID  string
	Text      string
	File      string
	Line      int
	Column    int
}

type Package struct {
	ID     string
	Name   string
	File   string
	Line   int
	Column int
	Files  []PackageFile
}

type PackageFile struct {
	File         string
	Line         int
	Column       int
	Declarations []DeclarationSummary
}

type DeclarationSummary struct {
	Name   string
	Kind   string
	File   string
	Line   int
	Column int
}

type NamedField struct {
	File              string
	Line              int
	Column            int
	Text              string
	Inline            bool
	DistanceFile      string
	DistanceLine      int
	DistanceColumn    int
	DeclarationFile   string
	DeclarationLine   int
	DeclarationColumn int
	TypeDeclarations  []NamedFieldTypeDeclaration
}

type NamedFieldTypeDeclaration struct {
	File   string
	Line   int
	Column int
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
	BlockKindBase   = "block"
	BlockKindIf     = "if"
	BlockKindElseIf = "elseif"
	BlockKindElse   = "else"
	BlockKindFor    = "for"
	BlockKindSwitch = "switch"
	BlockKindSelect = "select"
	BlockKindCase   = "case"
)

const (
	StatementKindPanic = "panic"
)

func Build(file string) (*Result, error) {
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	client, err := goplsclient.Default()
	if err != nil {
		return nil, err
	}
	info := &types.Info{
		Defs:      map[*ast.Ident]types.Object{},
		Uses:      map[*ast.Ident]types.Object{},
		Implicits: map[ast.Node]types.Object{},
		Types:     map[ast.Expr]types.TypeAndValue{},
	}
	conf := &types.Config{Importer: importer.Default(), Error: func(error) {}}
	_, _ = conf.Check(filepath.Dir(file), fset, []*ast.File{parsed}, info)
	returnErrors := collectReturnErrorClassifications(parsed, info)
	b := builder{
		file:          file,
		fset:          fset,
		info:          info,
		returnErrors:  returnErrors,
		client:        client,
		declByObj:     map[types.Object]string{},
		kindByObj:     map[types.Object]string{},
		pkgPathByName: map[string]string{},
		pkgByPath:     map[string]string{},
		pkgDefByPath:  map[string]definitionLocation{},
		goplsByPos:    map[string]definitionLocation{},
		escapes:       getEscapeAnalysis(file),
	}
	res := &Result{File: file}
	for _, imp := range parsed.Imports {
		pkgRef, pkgDecl := b.buildImport(imp)
		if pkgRef.PackageID == "" {
			continue
		}
		res.PackageReferences = append(res.PackageReferences, pkgRef)
		res.Packages = append(res.Packages, pkgDecl)
		if name := importedPackageName(imp); name != "" && name != "." && name != "_" {
			b.pkgPathByName[name] = strings.Trim(imp.Path.Value, "\"")
		}
	}
	ast.Inspect(parsed, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.ReturnStmt:
			pos := fset.Position(node.Return)
			res.Returns = append(res.Returns, Return{
				File:   file,
				Line:   pos.Line,
				Column: pos.Column,
			})
		case *ast.CallExpr:
			name, startPos := indirectCallTarget(node.Fun, fset)
			if name == "" || startPos == token.NoPos {
				break
			}

			pos := fset.Position(startPos)
			if !isIndirectCall(node.Fun, info, client, pos) {
				break
			}

			if name != "" {
				res.IndirectCalls = append(res.IndirectCalls, IndirectCall{
					File:   file,
					Line:   pos.Line,
					Column: pos.Column,
					Text:   name,
				})
			}
		}
		return true
	})
	for _, decl := range parsed.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				for _, built := range b.buildSpecs(spec) {
					if built.ID != "" {
						res.Declarations = append(res.Declarations, built)
					}
				}
			}
		case *ast.FuncDecl:
			res.Declarations = append(res.Declarations, b.buildFunc(d))
		}
	}
	res.NamedFields = b.collectTopLevelNamedFields(parsed)
	for _, group := range parsed.Comments {
		for _, comment := range group.List {
			if comment == nil {
				continue
			}
			start := fset.Position(comment.Pos())
			end := fset.Position(comment.End())
			if start.Line < 1 || end.Line < 1 {
				continue
			}
			res.Comments = append(res.Comments, Comment{
				StartLine:   start.Line,
				StartColumn: start.Column,
				EndLine:     end.Line,
				EndColumn:   end.Column,
			})
		}
	}
	sortDeclarations(res.Declarations)
	sort.Slice(res.PackageReferences, func(i, j int) bool { return res.PackageReferences[i].Text < res.PackageReferences[j].Text })
	return res, nil
}

type builder struct {
	file          string
	fset          *token.FileSet
	info          *types.Info
	returnErrors  map[token.Pos]bool
	client        *goplsclient.Client
	declByObj     map[types.Object]string
	kindByObj     map[types.Object]string
	pkgPathByName map[string]string
	pkgByPath     map[string]string
	pkgDefByPath  map[string]definitionLocation
	goplsByPos    map[string]definitionLocation
	escapes       map[string]bool
	seq           int
}

func collectReturnErrorClassifications(parsed *ast.File, info *types.Info) map[token.Pos]bool {
	out := map[token.Pos]bool{}
	if parsed == nil || info == nil {
		return out
	}
	ast.Inspect(parsed, func(n ast.Node) bool {
		switch node := n.(type) {
		case nil:
			return true
		case *ast.FuncDecl:
			if node.Body != nil {
				if obj, ok := info.Defs[node.Name].(*types.Func); ok {
					if sig, ok := obj.Type().(*types.Signature); ok {
						collectReturnErrorClassificationsInBody(node.Body, sig, info, out)
					}
				}
			}
			return false
		case *ast.FuncLit:
			if tv, ok := info.Types[node]; ok {
				if sig, ok := tv.Type.(*types.Signature); ok {
					collectReturnErrorClassificationsInBody(node.Body, sig, info, out)
				}
			}
			return false
		}
		return true
	})
	return out
}

func collectReturnErrorClassificationsInBody(body *ast.BlockStmt, sig *types.Signature, info *types.Info, out map[token.Pos]bool) {
	if body == nil || sig == nil {
		return
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch node := n.(type) {
		case nil:
			return true
		case *ast.ReturnStmt:
			out[node.Return] = returnStmtReturnsError(node, sig, info)
		case *ast.FuncLit:
			if tv, ok := info.Types[node]; ok {
				if nestedSig, ok := tv.Type.(*types.Signature); ok {
					collectReturnErrorClassificationsInBody(node.Body, nestedSig, info, out)
				}
			}
			return false
		}
		return true
	})
}

func returnStmtReturnsError(stmt *ast.ReturnStmt, sig *types.Signature, info *types.Info) bool {
	results := sig.Results()
	if stmt == nil || results == nil || results.Len() == 0 {
		return false
	}
	if len(stmt.Results) == 0 {
		return tupleHasError(results)
	}

	resultIndex := 0
	for _, expr := range stmt.Results {
		exprTypes := returnExprTypes(expr, info)
		if len(exprTypes) == 0 {
			exprTypes = []types.Type{nil}
		}
		for range exprTypes {
			if resultIndex >= results.Len() {
				break
			}
			if isErrorType(results.At(resultIndex).Type()) && !isNilExpr(expr) {
				return true
			}
			resultIndex++
		}
	}
	return false
}

func returnExprTypes(expr ast.Expr, info *types.Info) []types.Type {
	if expr == nil || info == nil {
		return nil
	}
	tv, ok := info.Types[expr]
	if !ok || tv.Type == nil {
		return nil
	}
	if tuple, ok := tv.Type.(*types.Tuple); ok {
		out := make([]types.Type, 0, tuple.Len())
		for i := 0; i < tuple.Len(); i++ {
			out = append(out, tuple.At(i).Type())
		}
		return out
	}
	return []types.Type{tv.Type}
}

func tupleHasError(tuple *types.Tuple) bool {
	if tuple == nil {
		return false
	}
	for i := 0; i < tuple.Len(); i++ {
		if isErrorType(tuple.At(i).Type()) {
			return true
		}
	}
	return false
}

func isErrorType(t types.Type) bool {
	errObj := types.Universe.Lookup("error")
	if errObj == nil || t == nil {
		return false
	}
	errIface, ok := errObj.Type().Underlying().(*types.Interface)
	if !ok {
		return false
	}
	return types.Implements(t, errIface)
}

func isNilExpr(expr ast.Expr) bool {
	id, ok := expr.(*ast.Ident)
	return ok && id.Name == "nil"
}

func getEscapeAnalysis(file string) map[string]bool {
	dir := filepath.Dir(file)
	if cached, ok := escapeAnalysisCache.Load(dir); ok {
		return cloneStringBoolMap(cached.(map[string]bool))
	}
	cmd := exec.Command("go", "build", "-gcflags=all=-m=1", "./...")
	cmd.Dir = dir
	out, _ := cmd.CombinedOutput()
	lines := strings.Split(string(out), "\n")

	escapes := make(map[string]bool)
	for _, l := range lines {
		if !strings.Contains(l, "escapes to heap") && !strings.Contains(l, "moved to heap") && !strings.Contains(l, "leaking param") {
			continue
		}
		parts := strings.SplitN(l, ":", 4)
		if len(parts) >= 4 {
			filePart := parts[0]
			line := parts[1]
			col := parts[2]

			if strings.HasPrefix(filePart, "./") {
				filePart = filePart[2:]
			}
			absPath, err := filepath.Abs(filepath.Join(dir, filePart))
			if err == nil {
				filePart = absPath
			}

			key := fmt.Sprintf("%s:%s:%s", filepath.Clean(filePart), line, col)
			escapes[key] = true
		}
	}
	escapeAnalysisCache.Store(dir, escapes)
	return escapes
}

type definitionLocation struct {
	file   string
	line   int
	column int
	ok     bool
}

func (b *builder) nextID(prefix string) string {
	b.seq++
	return fmt.Sprintf("%s-%d", prefix, b.seq)
}

func (b *builder) buildSpecs(spec ast.Spec) []Declaration {
	switch s := spec.(type) {
	case *ast.TypeSpec:
		decl := b.newDeclaration(s.Name, KindType)
		b.appendFieldDeclarations(&decl, s.TypeParams, KindParameter)
		b.appendInterfaceMethodDeclarations(&decl, s.Type)
		b.collectReferences(s.Type, &decl)
		return []Declaration{decl}
	case *ast.ValueSpec:
		if len(s.Names) == 0 {
			return nil
		}
		var decls []Declaration
		for i, name := range s.Names {
			decl := b.newDeclaration(name, KindVariable)
			if i == 0 {
				if s.Type != nil {
					b.collectReferences(s.Type, &decl)
				}
				for _, value := range s.Values {
					b.collectReferences(value, &decl)
				}
			}
			decls = append(decls, decl)
		}
		return decls
	default:
		return nil
	}
}

func (b *builder) buildFunc(fn *ast.FuncDecl) Declaration {
	decl := b.newDeclaration(fn.Name, KindFunction)
	if fn.Recv != nil {
		b.appendFieldDeclarations(&decl, fn.Recv, KindParameter)
		b.collectReferences(fn.Recv, &decl)
	}
	if fn.Type != nil {
		b.appendFieldDeclarations(&decl, fn.Type.TypeParams, KindParameter)
		b.appendFieldDeclarations(&decl, fn.Type.Params, KindParameter)
		b.collectReferences(fn.Type, &decl)
	}
	if fn.Body == nil {
		return decl
	}
	decl.ControlFlow = b.buildControlFlowBlocks(fn.Body.List)
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.DeclStmt:
			if gd, ok := x.Decl.(*ast.GenDecl); ok {
				for _, spec := range gd.Specs {
					for _, child := range b.buildSpecs(spec) {
						if child.ID != "" {
							decl.Declarations = append(decl.Declarations, child)
						}
					}
				}
			}
		case *ast.AssignStmt:
			if x.Tok == token.DEFINE {
				for _, lhs := range x.Lhs {
					id, ok := lhs.(*ast.Ident)
					if !ok || id.Name == "_" {
						continue
					}
					child := b.newDeclaration(id, KindVariable)
					decl.Declarations = append(decl.Declarations, child)
				}
			}
		case *ast.RangeStmt:
			for _, expr := range []ast.Expr{x.Key, x.Value} {
				id, ok := expr.(*ast.Ident)
				if !ok || id.Name == "_" {
					continue
				}
				child := b.newDeclaration(id, KindVariable)
				decl.Declarations = append(decl.Declarations, child)
			}
		case *ast.TypeSwitchStmt:
			assign, ok := x.Assign.(*ast.AssignStmt)
			if !ok || assign.Tok != token.DEFINE || len(assign.Lhs) != 1 {
				break
			}
			id, ok := assign.Lhs[0].(*ast.Ident)
			if !ok || id.Name == "_" {
				break
			}
			child := b.newDeclaration(id, KindVariable)
			decl.Declarations = append(decl.Declarations, child)
			for _, stmt := range x.Body.List {
				clause, ok := stmt.(*ast.CaseClause)
				if !ok {
					continue
				}
				obj := b.info.Implicits[clause]
				if obj == nil {
					continue
				}
				b.declByObj[obj] = child.ID
				b.kindByObj[obj] = KindVariable
			}
		}
		return true
	})
	b.collectReferences(fn.Body, &decl)
	sortDeclarations(decl.Declarations)
	return decl
}

type controlFlowBuilder struct {
	fset         *token.FileSet
	file         string
	returnErrors map[token.Pos]bool
	labels       map[string]*ControlFlowBlock
	breakStack   []*ControlFlowBlock
	ifChainSeq   int
}

func (b *builder) buildControlFlowBlocks(stmts []ast.Stmt) []ControlFlowBlock {
	cfb := controlFlowBuilder{fset: b.fset, file: b.file, returnErrors: b.returnErrors, labels: map[string]*ControlFlowBlock{}}
	return cfb.buildBlocks(stmts)
}

func (b *controlFlowBuilder) buildBlocks(stmts []ast.Stmt) []ControlFlowBlock {
	out := make([]ControlFlowBlock, 0, len(stmts))
	for _, stmt := range stmts {
		out = append(out, b.buildBlock(stmt))
	}
	return out
}

func (b *controlFlowBuilder) buildBlock(stmt ast.Stmt) ControlFlowBlock {
	if labeled, ok := stmt.(*ast.LabeledStmt); ok {
		if labeled.Label != nil {
			switch s := labeled.Stmt.(type) {
			case *ast.ForStmt:
				return b.buildForBlock(s.For, s.Body, labeled.Label.Name)
			case *ast.RangeStmt:
				return b.buildForBlock(s.For, s.Body, labeled.Label.Name)
			}
		}
		return b.buildBlock(labeled.Stmt)
	}

	pos := b.fset.Position(stmt.Pos())
	block := ControlFlowBlock{Kind: BlockKindBase, File: b.file, Line: pos.Line, Column: pos.Column}
	switch s := stmt.(type) {
	case *ast.BlockStmt:
		block.Blocks = b.buildBlocks(s.List)
	case *ast.IfStmt:
		block = b.buildIfBlock(s)
	case *ast.ForStmt:
		block = b.buildForBlock(s.Pos(), s.Body, "")
	case *ast.RangeStmt:
		block = b.buildForBlock(s.Pos(), s.Body, "")
	case *ast.SwitchStmt:
		block = b.buildSwitchBlock(s.Pos(), s.Body)
	case *ast.TypeSwitchStmt:
		block = b.buildSwitchBlock(s.Pos(), s.Body)
	case *ast.SelectStmt:
		block = b.buildSelectBlock(s)
	default:
		block.Statements = b.collectControlFlowStatements(stmt)
	}
	block.HasControlFlowStatement = controlFlowBlockHasTerminalStatement(block)
	return block
}
func (b *controlFlowBuilder) buildIfBlock(stmt *ast.IfStmt) ControlFlowBlock {
	b.ifChainSeq++
	chainID := fmt.Sprintf("if-chain-%d", b.ifChainSeq)
	return b.buildIfChain(stmt, chainID, 1, BlockKindIf, stmt.If)
}

func (b *controlFlowBuilder) buildIfChain(stmt *ast.IfStmt, chainID string, step int, kind string, keywordPos token.Pos) ControlFlowBlock {
	pos := b.fset.Position(keywordPos)
	block := ControlFlowBlock{Kind: kind, File: b.file, Line: pos.Line, Column: pos.Column, IfChainID: chainID, IfStep: step}
	block.Statements = b.collectControlFlowStatements(stmt.Init, stmt.Cond)
	if stmt.Body != nil {
		setBlockBracesFromStmt(b.fset, &block, stmt.Body)
		block.Blocks = append(block.Blocks, b.buildBlocks(stmt.Body.List)...)
	}
	if stmt.Else != nil {
		elsePos := stmt.Else.Pos()
		switch e := stmt.Else.(type) {
		case *ast.IfStmt:
			block.Blocks = append(block.Blocks, b.buildIfChain(e, chainID, step+1, BlockKindElseIf, elsePos))
		case *ast.BlockStmt:
			elseLoc := b.fset.Position(elsePos)
			elseBlock := ControlFlowBlock{Kind: BlockKindElse, File: b.file, Line: elseLoc.Line, Column: elseLoc.Column, IfChainID: chainID, IfStep: step + 1}
			setBlockBracesFromStmt(b.fset, &elseBlock, e)
			elseBlock.Blocks = b.buildBlocks(e.List)
			block.Blocks = append(block.Blocks, elseBlock)
		default:
			block.Blocks = append(block.Blocks, b.buildBlock(e))
		}
	}
	block.HasControlFlowStatement = controlFlowBlockHasTerminalStatement(block)
	return block
}

func (b *controlFlowBuilder) buildForBlock(pos token.Pos, body *ast.BlockStmt, label string) ControlFlowBlock {
	p := b.fset.Position(pos)
	block := ControlFlowBlock{Kind: BlockKindFor, File: b.file, Line: p.Line, Column: p.Column}
	if label != "" {
		b.labels[label] = &block
		defer delete(b.labels, label)
	}
	b.breakStack = append(b.breakStack, &block)
	defer func() { b.breakStack = b.breakStack[:len(b.breakStack)-1] }()
	if body != nil {
		setBlockBracesFromStmt(b.fset, &block, body)
		block.Blocks = b.buildBlocks(body.List)
	}
	if controlFlowBlockHasStatementKind(block, "return") {
		block.MayReturn = true
	}
	block.HasControlFlowStatement = controlFlowBlockHasTerminalStatement(block)
	return block
}

func controlFlowBlockHasStatementKind(block ControlFlowBlock, kind string) bool {
	for _, stmt := range block.Statements {
		if stmt.Kind == kind {
			return true
		}
	}
	for _, child := range block.Blocks {
		if controlFlowBlockHasStatementKind(child, kind) {
			return true
		}
	}
	return false
}

func controlFlowBlockHasTerminalStatement(block ControlFlowBlock) bool {
	for _, stmt := range block.Statements {
		if isTerminalControlFlowKind(stmt.Kind) {
			return true
		}
	}
	for _, child := range block.Blocks {
		if controlFlowBlockHasTerminalStatement(child) {
			return true
		}
	}
	return false
}

func isTerminalControlFlowKind(kind string) bool {
	switch kind {
	case "return", "continue", "break", "goto", "panic":
		return true
	default:
		return false
	}
}

func (b *controlFlowBuilder) buildSwitchBlock(pos token.Pos, body *ast.BlockStmt) ControlFlowBlock {
	p := b.fset.Position(pos)
	block := ControlFlowBlock{Kind: BlockKindSwitch, File: b.file, Line: p.Line, Column: p.Column}
	setBlockBracesFromStmt(b.fset, &block, body)
	b.breakStack = append(b.breakStack, &block)
	for _, stmt := range body.List {
		clause, ok := stmt.(*ast.CaseClause)
		if !ok {
			continue
		}
		b.appendCaseBlock(&block, clause.Case, clause.List == nil, clause.Body)
	}
	b.breakStack = b.breakStack[:len(b.breakStack)-1]
	block.HasControlFlowStatement = controlFlowBlockHasTerminalStatement(block)
	return block
}

func (b *controlFlowBuilder) buildSelectBlock(stmt *ast.SelectStmt) ControlFlowBlock {
	p := b.fset.Position(stmt.Select)
	block := ControlFlowBlock{Kind: BlockKindSelect, File: b.file, Line: p.Line, Column: p.Column}
	setBlockBracesFromStmt(b.fset, &block, stmt.Body)
	b.breakStack = append(b.breakStack, &block)
	if stmt.Body != nil {
		for _, entry := range stmt.Body.List {
			clause, ok := entry.(*ast.CommClause)
			if !ok {
				continue
			}
			b.appendCaseBlock(&block, clause.Case, clause.Comm == nil, clause.Body)
		}
	}
	b.breakStack = b.breakStack[:len(b.breakStack)-1]
	block.HasControlFlowStatement = controlFlowBlockHasTerminalStatement(block)
	return block
}

func setBlockBracesFromStmt(fset *token.FileSet, block *ControlFlowBlock, body *ast.BlockStmt) {
	if fset == nil || block == nil || body == nil {
		return
	}
	open := fset.Position(body.Lbrace)
	close := fset.Position(body.Rbrace)
	block.OpenBraceLine = open.Line
	block.OpenBraceColumn = open.Column
	block.CloseBraceLine = close.Line
	block.CloseBraceColumn = close.Column
}

func (b *controlFlowBuilder) appendCaseBlock(parent *ControlFlowBlock, casePos token.Pos, hasDefault bool, body []ast.Stmt) {
	if parent == nil {
		return
	}
	if hasDefault {
		parent.HasDefault = true
	}
	parent.CaseCount++
	p := b.fset.Position(casePos)
	caseBlock := ControlFlowBlock{Kind: BlockKindCase, File: b.file, Line: p.Line, Column: p.Column, HasDefault: hasDefault}
	caseNodes := make([]ast.Node, 0, len(body))
	for _, stmt := range body {
		caseNodes = append(caseNodes, stmt)
	}
	caseBlock.Statements = b.collectControlFlowStatements(caseNodes...)
	caseBlock.Blocks = b.buildBlocks(body)
	caseBlock.HasControlFlowStatement = controlFlowBlockHasTerminalStatement(caseBlock)
	parent.Blocks = append(parent.Blocks, caseBlock)
}

func (b *controlFlowBuilder) collectControlFlowStatements(nodes ...ast.Node) []ControlFlowStatement {
	var out []ControlFlowStatement
	for _, node := range nodes {
		if node == nil {
			continue
		}
		ast.Inspect(node, func(n ast.Node) bool {
			if n == nil {
				return true
			}
			if _, ok := n.(*ast.FuncLit); ok && n != node {
				return false
			}
			switch s := n.(type) {
			case *ast.ReturnStmt:
				p := b.fset.Position(s.Return)
				out = append(out, ControlFlowStatement{Kind: "return", File: b.file, Line: p.Line, Column: p.Column, ReturnsError: b.returnErrors[s.Return]})
			case *ast.BranchStmt:
				if s.Tok == token.BREAK {
					b.markBreakTarget(s)
				}
				kind := strings.ToLower(s.Tok.String())
				p := b.fset.Position(s.TokPos)
				out = append(out, ControlFlowStatement{Kind: kind, File: b.file, Line: p.Line, Column: p.Column})
			case *ast.CallExpr:
				id, ok := s.Fun.(*ast.Ident)
				if !ok || id.Name != StatementKindPanic {
					return true
				}
				p := b.fset.Position(id.NamePos)
				out = append(out, ControlFlowStatement{Kind: StatementKindPanic, File: b.file, Line: p.Line, Column: p.Column})
			}
			return true
		})
	}
	return out
}

func (b *controlFlowBuilder) markBreakTarget(stmt *ast.BranchStmt) {
	if stmt == nil || stmt.Tok != token.BREAK {
		return
	}
	if stmt.Label != nil {
		target := b.labels[stmt.Label.Name]
		if target != nil && target.Kind == BlockKindFor {
			target.MayBreak = true
		}
		return
	}
	if len(b.breakStack) == 0 {
		return
	}
	target := b.breakStack[len(b.breakStack)-1]
	if target != nil && target.Kind == BlockKindFor {
		target.MayBreak = true
	}
}

func (b *builder) appendInterfaceMethodDeclarations(parent *Declaration, expr ast.Expr) {
	iface, ok := expr.(*ast.InterfaceType)
	if !ok || iface.Methods == nil {
		return
	}
	b.appendFieldDeclarations(parent, iface.Methods, KindFunction)
}

func (b *builder) appendFieldDeclarations(parent *Declaration, fields *ast.FieldList, kind string) {
	if fields == nil {
		return
	}
	for _, field := range fields.List {
		for _, name := range field.Names {
			if name == nil || name.Name == "_" {
				continue
			}
			parent.Declarations = append(parent.Declarations, b.newDeclaration(name, kind))
		}
	}
}

func (b *builder) collectTopLevelNamedFields(node *ast.File) []NamedField {
	var out []NamedField
	topLevelStructs := map[token.Pos]bool{}
	structFieldsByType := map[string]map[string][]NamedFieldTypeDeclaration{}
	for _, decl := range node.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			switch t := ts.Type.(type) {
			case *ast.StructType:
				topLevelStructs[t.Pos()] = true
				structFieldsByType[ts.Name.Name] = b.structFieldTypes(t.Fields)
				b.collectNamedFields(t.Fields, false, &out)
			}
		}
	}
	ast.Inspect(node, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.StructType:
			if topLevelStructs[n.Pos()] {
				return true
			}
			b.collectNamedFields(n.Fields, false, &out)
		case *ast.CompositeLit:
			if b.collectInlineStructLiteralFields(n, &out) {
				return true
			}
			if b.collectNamedStructLiteralFields(n, structFieldsByType, &out) {
				return true
			}
			b.collectTypedStructLiteralFields(n, &out)
		}
		return true
	})
	return out
}

func (b *builder) structFieldTypes(fields *ast.FieldList) map[string][]NamedFieldTypeDeclaration {
	byName := map[string][]NamedFieldTypeDeclaration{}
	if fields == nil {
		return byName
	}
	for _, field := range fields.List {
		decls := b.namedFieldTypeDeclarations(field.Type)
		for _, name := range field.Names {
			if name != nil {
				byName[name.Name] = decls
			}
		}
	}
	return byName
}

func (b *builder) collectNamedFields(fields *ast.FieldList, inline bool, out *[]NamedField) {
	if fields == nil {
		return
	}
	for _, field := range fields.List {
		for _, name := range field.Names {
			pos := b.fset.Position(name.Pos())
			if pos.Line < 1 || pos.Column < 1 {
				continue
			}
			named := NamedField{File: pos.Filename, Line: pos.Line, Column: pos.Column, Text: name.Name, Inline: inline}
			named.TypeDeclarations = b.namedFieldTypeDeclarations(field.Type)
			if len(named.TypeDeclarations) > 0 {
				loc := named.TypeDeclarations[0]
				named.DeclarationFile = loc.File
				named.DeclarationLine = loc.Line
				named.DeclarationColumn = loc.Column
			}
			*out = append(*out, named)
		}
	}
}

func (b *builder) collectTypedStructLiteralFields(lit *ast.CompositeLit, out *[]NamedField) bool {
	tv, ok := b.info.Types[lit.Type]
	if !ok || tv.Type == nil {
		return false
	}
	st, ok := underlyingStruct(tv.Type)
	if !ok {
		return false
	}
	byName := map[string][]NamedFieldTypeDeclaration{}
	for field := range st.Fields() {
		if field != nil {
			byName[field.Name()] = b.namedFieldTypeDeclarationsForType(field.Type())
		}
	}
	distanceLoc, hasDistanceLoc := b.typeDeclarationLocationForType(tv.Type)
	return b.collectStructLiteralFields(lit, byName, distanceLoc, hasDistanceLoc, out)
}

func underlyingStruct(t types.Type) (*types.Struct, bool) {
	switch e := t.(type) {
	case nil:
		return nil, false
	case *types.Pointer:
		t = e.Elem()
	case *types.Named:
		t = e.Underlying()
	}
	st, ok := t.(*types.Struct)
	return st, ok
}

func (b *builder) typeDeclarationLocationForType(t types.Type) (definitionLocation, bool) {
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	if named, ok := t.(*types.Named); ok {
		return b.typeNameLocation(named.Obj())
	}
	return definitionLocation{}, false
}

func (b *builder) collectInlineStructLiteralFields(lit *ast.CompositeLit, out *[]NamedField) bool {
	st, ok := lit.Type.(*ast.StructType)
	if !ok || st.Fields == nil {
		return false
	}
	byName := b.structFieldTypes(st.Fields)
	return b.collectStructLiteralFields(lit, byName, definitionLocation{}, false, out)
}

func (b *builder) collectNamedStructLiteralFields(lit *ast.CompositeLit, structFieldsByType map[string]map[string][]NamedFieldTypeDeclaration, out *[]NamedField) bool {
	typeName, ok := compositeLiteralTypeName(lit.Type)
	if !ok {
		return false
	}
	byName := structFieldsByType[typeName]
	if len(byName) == 0 {
		return false
	}
	return b.collectStructLiteralFields(lit, byName, definitionLocation{}, false, out)
}

func compositeLiteralTypeName(expr ast.Expr) (string, bool) {
	switch n := expr.(type) {
	case *ast.Ident:
		return n.Name, true
	case *ast.IndexExpr:
		return compositeLiteralTypeName(n.X)
	case *ast.IndexListExpr:
		return compositeLiteralTypeName(n.X)
	}
	return "", false
}

func (b *builder) collectStructLiteralFields(lit *ast.CompositeLit, byName map[string][]NamedFieldTypeDeclaration, distanceLoc definitionLocation, hasDistanceLoc bool, out *[]NamedField) bool {
	collected := false
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		decls, ok := byName[key.Name]
		if !ok {
			continue
		}
		pos := b.fset.Position(key.Pos())
		if pos.Line < 1 || pos.Column < 1 {
			continue
		}
		named := NamedField{File: pos.Filename, Line: pos.Line, Column: pos.Column, Text: key.Name, Inline: true, TypeDeclarations: decls}
		if hasDistanceLoc {
			named.DistanceFile = distanceLoc.file
			named.DistanceLine = distanceLoc.line
			named.DistanceColumn = distanceLoc.column
		}
		*out = append(*out, named)
		collected = true
	}
	return collected
}

func (b *builder) namedFieldTypeDeclarations(expr ast.Expr) []NamedFieldTypeDeclaration {
	var out []NamedFieldTypeDeclaration
	for _, loc := range b.typeDeclarationsFor(expr) {
		out = append(out, NamedFieldTypeDeclaration{File: loc.file, Line: loc.line, Column: loc.column})
	}
	return out
}

func (b *builder) namedFieldTypeDeclarationsForType(t types.Type) []NamedFieldTypeDeclaration {
	var out []NamedFieldTypeDeclaration
	seen := map[string]bool{}
	b.appendTypeDeclarations(t, &out, seen)
	return out
}

func (b *builder) appendTypeDeclarations(t types.Type, out *[]NamedFieldTypeDeclaration, seen map[string]bool) {
	switch t := t.(type) {
	case nil, *types.Basic:
		return
	case *types.Named:
		if loc, ok := b.typeNameLocation(t.Obj()); ok {
			appendNamedFieldTypeDeclaration(out, seen, loc)
		}
		if typeArgs := t.TypeArgs(); typeArgs != nil {
			for i := 0; i < typeArgs.Len(); i++ {
				b.appendTypeDeclarations(typeArgs.At(i), out, seen)
			}
		}
	case *types.Pointer:
		b.appendTypeDeclarations(t.Elem(), out, seen)
	case *types.Slice:
		b.appendTypeDeclarations(t.Elem(), out, seen)
	case *types.Array:
		b.appendTypeDeclarations(t.Elem(), out, seen)
	case *types.Map:
		b.appendTypeDeclarations(t.Key(), out, seen)
		b.appendTypeDeclarations(t.Elem(), out, seen)
	case *types.Chan:
		b.appendTypeDeclarations(t.Elem(), out, seen)
	case *types.Signature:
		b.appendTupleTypeDeclarations(t.Params(), out, seen)
		b.appendTupleTypeDeclarations(t.Results(), out, seen)
	case *types.Struct:
		for field := range t.Fields() {
			b.appendTypeDeclarations(field.Type(), out, seen)
		}
	case *types.Interface:
		for etyp := range t.EmbeddedTypes() {
			b.appendTypeDeclarations(etyp, out, seen)
		}
	}
}

func (b *builder) appendTupleTypeDeclarations(tuple *types.Tuple, out *[]NamedFieldTypeDeclaration, seen map[string]bool) {
	if tuple == nil {
		return
	}
	for v := range tuple.Variables() {
		b.appendTypeDeclarations(v.Type(), out, seen)
	}
}

func (b *builder) typeNameLocation(obj *types.TypeName) (definitionLocation, bool) {
	if obj == nil {
		return definitionLocation{}, false
	}
	if obj.Pkg() == nil {
		return definitionLocation{file: "", line: 1, column: 1, ok: true}, true
	}
	if obj.Pkg().Path() != filepath.Dir(b.file) {
		if loc, ok := b.packageTypeDefinition(obj.Pkg().Path(), obj.Name()); ok {
			return loc, true
		}
	}
	if obj.Pos() != token.NoPos {
		pos := b.fset.Position(obj.Pos())
		if pos.Filename != "" && pos.Line > 0 && pos.Column > 0 {
			return definitionLocation{file: pos.Filename, line: pos.Line, column: pos.Column, ok: true}, true
		}
	}
	return b.packageTypeDefinition(obj.Pkg().Path(), obj.Name())
}

func (b *builder) packageTypeDefinition(importPath, name string) (definitionLocation, bool) {
	for _, file := range loadPackageFiles(importPath) {
		for _, decl := range file.Declarations {
			if decl.Kind == KindType && decl.Name == name {
				return definitionLocation{file: decl.File, line: decl.Line, column: decl.Column, ok: true}, true
			}
		}
	}
	return definitionLocation{}, false
}

func appendNamedFieldTypeDeclaration(out *[]NamedFieldTypeDeclaration, seen map[string]bool, loc definitionLocation) {
	key := fmt.Sprintf("%s:%d:%d", loc.file, loc.line, loc.column)
	if seen[key] {
		return
	}
	seen[key] = true
	*out = append(*out, NamedFieldTypeDeclaration{File: loc.file, Line: loc.line, Column: loc.column})
}

func (b *builder) collectNamedFieldsInExpr(expr ast.Expr, out *[]NamedField) {
	switch n := expr.(type) {
	case *ast.StructType:
		b.collectNamedFields(n.Fields, false, out)
		for _, field := range n.Fields.List {
			b.collectNamedFieldsInExpr(field.Type, out)
		}
	case *ast.FuncType:
		if n.Params != nil {
			for _, p := range n.Params.List {
				b.collectNamedFieldsInExpr(p.Type, out)
			}
		}
		if n.Results != nil {
			for _, r := range n.Results.List {
				b.collectNamedFieldsInExpr(r.Type, out)
			}
		}
	case *ast.ArrayType:
		b.collectNamedFieldsInExpr(n.Elt, out)
	case *ast.MapType:
		b.collectNamedFieldsInExpr(n.Key, out)
		b.collectNamedFieldsInExpr(n.Value, out)
	case *ast.StarExpr:
		b.collectNamedFieldsInExpr(n.X, out)
	case *ast.ChanType:
		b.collectNamedFieldsInExpr(n.Value, out)
	}
}

func (b *builder) typeDeclarationsFor(expr ast.Expr) []definitionLocation {
	ids := typeReferenceIdents(expr)
	out := make([]definitionLocation, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		if id == nil || id.Pos() == token.NoPos {
			continue
		}
		loc, ok := b.typeDeclarationForIdent(id)
		if !ok {
			continue
		}
		key := fmt.Sprintf("%s:%d:%d", loc.file, loc.line, loc.column)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, loc)
	}
	return out
}

func (b *builder) typeDeclarationForIdent(id *ast.Ident) (definitionLocation, bool) {
	if loc, ok := b.definitionFor(id.Pos()); ok {
		return loc, true
	}
	if obj := b.info.Uses[id]; obj != nil {
		if obj.Parent() == types.Universe {
			return definitionLocation{file: "", line: 1, column: 1, ok: true}, true
		}
		objPos := b.fset.Position(obj.Pos())
		if objPos.Filename != "" && objPos.Line > 0 && objPos.Column > 0 {
			return definitionLocation{file: objPos.Filename, line: objPos.Line, column: objPos.Column, ok: true}, true
		}
	}
	return definitionLocation{}, false
}

func typeReferenceIdents(expr ast.Expr) []*ast.Ident {
	var out []*ast.Ident
	appendTypeReferenceIdents(expr, &out)
	return out
}

func appendTypeReferenceIdents(expr ast.Expr, out *[]*ast.Ident) {
	switch n := expr.(type) {
	case *ast.Ident:
		*out = append(*out, n)
	case *ast.SelectorExpr:
		*out = append(*out, n.Sel)
	case *ast.StarExpr:
		appendTypeReferenceIdents(n.X, out)
	case *ast.ArrayType:
		appendTypeReferenceIdents(n.Elt, out)
	case *ast.MapType:
		appendTypeReferenceIdents(n.Key, out)
		appendTypeReferenceIdents(n.Value, out)
	case *ast.ChanType:
		appendTypeReferenceIdents(n.Value, out)
	case *ast.IndexExpr:
		appendTypeReferenceIdents(n.X, out)
		appendTypeReferenceIdents(n.Index, out)
	case *ast.IndexListExpr:
		appendTypeReferenceIdents(n.X, out)
		for _, idx := range n.Indices {
			appendTypeReferenceIdents(idx, out)
		}
	case *ast.ParenExpr:
		appendTypeReferenceIdents(n.X, out)
	case *ast.FuncType:
		appendFieldListTypeReferenceIdents(n.Params, out)
		appendFieldListTypeReferenceIdents(n.Results, out)
	case *ast.InterfaceType:
		appendFieldListTypeReferenceIdents(n.Methods, out)
	case *ast.StructType:
		appendFieldListTypeReferenceIdents(n.Fields, out)
	}
}

func appendFieldListTypeReferenceIdents(fields *ast.FieldList, out *[]*ast.Ident) {
	if fields == nil {
		return
	}
	for _, field := range fields.List {
		appendTypeReferenceIdents(field.Type, out)
	}
}

func (b *builder) newDeclaration(id *ast.Ident, kind string) Declaration {
	pos := b.fset.Position(id.Pos())
	key := fmt.Sprintf("%s:%d:%d", filepath.Clean(b.file), pos.Line, pos.Column)
	decl := Declaration{
		ID:      b.nextID(kind),
		Name:    id.Name,
		Kind:    kind,
		File:    b.file,
		Line:    pos.Line,
		Column:  pos.Column,
		Escapes: b.escapes[key],
	}
	if obj := b.info.Defs[id]; obj != nil {
		b.declByObj[obj] = decl.ID
		b.kindByObj[obj] = kind
	}
	return decl
}

func (b *builder) collectReferences(node ast.Node, decl *Declaration) {
	ast.Inspect(node, func(n ast.Node) bool {
		if selector, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := selector.X.(*ast.Ident); ok && b.info.Uses[id] == nil {
				if importPath := b.pkgPathByName[id.Name]; importPath != "" {
					b.appendReferenceForIdent(id, decl, importPath)
					b.appendReferenceForIdent(selector.Sel, decl, "")
					return false
				}
			}
		}
		id, ok := n.(*ast.Ident)
		if !ok || id.Name == "_" {
			return true
		}
		b.appendReferenceForIdent(id, decl, "")
		return true
	})
	sortReferences(decl.References)
}

func (b *builder) appendReferenceForIdent(id *ast.Ident, decl *Declaration, importPath string) {
	if id == nil || id.Name == "_" || b.info.Defs[id] != nil {
		return
	}
	pos := b.fset.Position(id.Pos())
	key := fmt.Sprintf("%s:%d:%d", filepath.Clean(b.file), pos.Line, pos.Column)
	ref := Reference{
		Text:    id.Name,
		File:    b.file,
		Line:    pos.Line,
		Column:  pos.Column,
		Kind:    b.classifyObject(b.info.Uses[id]),
		Escapes: b.escapes[key],
	}
	if importPath != "" {
		ref.Kind = KindPackage
		if loc, ok := b.packageDefinitionForImportPath(importPath); ok {
			ref.DeclarationFile = loc.file
			ref.DeclarationLine = loc.line
			ref.DeclarationColumn = loc.column
		}
	} else if obj := b.info.Uses[id]; obj != nil {
		ref.DeclarationID = b.declByObj[obj]
		if pkgName, ok := obj.(*types.PkgName); ok && pkgName.Imported() != nil {
			if loc, ok := b.packageDefinitionForImportPath(pkgName.Imported().Path()); ok {
				ref.DeclarationFile = loc.file
				ref.DeclarationLine = loc.line
				ref.DeclarationColumn = loc.column
			}
		}
	}
	if ref.DeclarationFile == "" {
		if target, ok := b.definitionFor(id.Pos()); ok {
			ref.DeclarationFile = target.file
			ref.DeclarationLine = target.line
			ref.DeclarationColumn = target.column
		}
	}
	decl.References = append(decl.References, ref)
}

func (b *builder) definitionFor(pos token.Pos) (definitionLocation, bool) {
	position := b.fset.Position(pos)
	key := fmt.Sprintf("%s:%d:%d", position.Filename, position.Line, position.Column)
	if cached, ok := b.goplsByPos[key]; ok {
		return cached, cached.ok
	}
	if b.client == nil {
		b.goplsByPos[key] = definitionLocation{}
		return definitionLocation{}, false
	}
	target, ok, err := b.client.Definition(position.Filename, position.Line, position.Column)
	if err != nil || !ok {
		b.goplsByPos[key] = definitionLocation{}
		return definitionLocation{}, false
	}
	loc := definitionLocation{file: target.File, line: target.Line, column: target.Column, ok: target.File != "" && target.Line > 0 && target.Column > 0}
	b.goplsByPos[key] = loc
	return loc, loc.ok
}

func (b *builder) buildImport(imp *ast.ImportSpec) (PackageReference, Package) {
	path := strings.Trim(imp.Path.Value, "\"")
	name := importedPackageName(imp)
	pos := b.fset.Position(imp.Pos())
	pkgID := b.pkgByPath[path]
	if pkgID == "" {
		pkgID = b.nextID("pkg")
		b.pkgByPath[path] = pkgID
	}
	pkgRef := PackageReference{PackageID: pkgID, ParentID: KindFile, Text: name, File: b.file, Line: pos.Line, Column: pos.Column}
	pkgFiles := loadPackageFiles(path)
	pkgFile := b.file
	pkgLine, pkgColumn := pos.Line, pos.Column
	if len(pkgFiles) > 0 {
		pkgFile = pkgFiles[0].File
		pkgLine, pkgColumn = pkgFiles[0].Line, pkgFiles[0].Column
	}
	return pkgRef, Package{ID: pkgID, Name: path, File: pkgFile, Line: pkgLine, Column: pkgColumn, Files: pkgFiles}
}

func importedPackageName(imp *ast.ImportSpec) string {
	if imp == nil {
		return ""
	}
	path := strings.Trim(imp.Path.Value, "\"")
	name := filepath.Base(path)
	if imp.Name != nil {
		name = imp.Name.Name
	}
	return name
}

func loadPackageFiles(importPath string) []PackageFile {
	if cached, ok := packageFilesCache.Load(importPath); ok {
		return clonePackageFiles(cached.([]PackageFile))
	}
	out, err := exec.Command("go", "list", "-f", "{{.Dir}}", importPath).Output()
	if err != nil {
		return nil
	}
	dir := strings.TrimSpace(string(out))
	entries, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil
	}
	files := make([]PackageFile, 0, len(entries))
	for _, file := range entries {
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			continue
		}
		pf := PackageFile{File: file, Line: 1, Column: 1}
		for _, decl := range parsed.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				pos := fset.Position(d.Name.Pos())
				pf.Declarations = append(pf.Declarations, DeclarationSummary{Name: d.Name.Name, Kind: KindFunction, File: file, Line: pos.Line, Column: pos.Column})
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						pos := fset.Position(s.Name.Pos())
						pf.Declarations = append(pf.Declarations, DeclarationSummary{Name: s.Name.Name, Kind: KindType, File: file, Line: pos.Line, Column: pos.Column})
					case *ast.ValueSpec:
						for _, name := range s.Names {
							pos := fset.Position(name.Pos())
							pf.Declarations = append(pf.Declarations, DeclarationSummary{Name: name.Name, Kind: KindVariable, File: file, Line: pos.Line, Column: pos.Column})
						}
					}
				}
			}
		}
		sort.Slice(pf.Declarations, func(i, j int) bool { return pf.Declarations[i].Line < pf.Declarations[j].Line })
		files = append(files, pf)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].File < files[j].File })
	packageFilesCache.Store(importPath, clonePackageFiles(files))
	return files
}

func indirectCallTarget(fun ast.Expr, fset *token.FileSet) (string, token.Pos) {
	switch expr := fun.(type) {
	case *ast.Ident:
		return expr.Name, expr.Pos()
	case *ast.SelectorExpr:
		return expr.Sel.Name, expr.Sel.Pos()
	default:
		startPos := fun.Pos()
		endPos := fun.End()
		posStart := fset.Position(startPos)
		posEnd := fset.Position(endPos)
		if posStart.Line == posEnd.Line && posEnd.Column > posStart.Column {
			return strings.Repeat("x", posEnd.Column-posStart.Column), startPos
		}
		return "call", startPos
	}
}

func isIndirectCall(fun ast.Expr, info *types.Info, client *goplsclient.Client, pos token.Position) bool {
	switch expr := fun.(type) {
	case *ast.Ident:
		if indirect, ok := indirectObject(info.Uses[expr]); ok {
			return indirect
		}
		if indirect, ok := isIndirectCallByDefinition(client, pos); ok {
			return indirect
		}
		return isIndirectCallByHover(client, pos)
	case *ast.SelectorExpr:
		if isPackageQualifier(expr.X, info) {
			return false
		}
		if selection := info.Selections[expr]; selection != nil {
			if indirect, ok := indirectObject(selection.Obj()); ok && !indirect {
				if isInterfaceType(selection.Recv()) {
					if indirect, ok := isIndirectCallByDefinition(client, pos); ok {
						return indirect
					}
					return true
				}
				return false
			}
			if indirect, ok := isIndirectCallByDefinition(client, pos); ok {
				return indirect
			}
			return isIndirectCallByHover(client, pos)
		}
		if indirect, ok := isIndirectCallByDefinition(client, pos); ok {
			return indirect
		}
		return isIndirectCallByHover(client, pos)
	case *ast.FuncLit:
		return false
	case *ast.IndexExpr, *ast.IndexListExpr:
		return true
	case *ast.ParenExpr:
		return isIndirectCall(expr.X, info, client, pos)
	}

	if indirect, ok := isIndirectCallByDefinition(client, pos); ok {
		return indirect
	}
	return isIndirectCallByHover(client, pos)
}

func indirectObject(obj types.Object) (bool, bool) {
	switch obj.(type) {
	case *types.Var:
		return true, true
	case *types.Func, *types.Builtin, *types.TypeName:
		return false, true
	default:
		return false, false
	}
}

func isPackageQualifier(expr ast.Expr, info *types.Info) bool {
	id, ok := expr.(*ast.Ident)
	if !ok {
		return false
	}
	_, ok = info.Uses[id].(*types.PkgName)
	return ok
}

func isInterfaceType(t types.Type) bool {
	if t == nil {
		return false
	}
	_, ok := types.Unalias(t).Underlying().(*types.Interface)
	return ok
}

func isIndirectCallByHover(client *goplsclient.Client, pos token.Position) bool {
	if client == nil {
		return false
	}
	hoverRaw, err := client.Hover(pos.Filename, pos.Line, pos.Column)
	if err != nil || hoverRaw == "" {
		return false
	}
	var h struct {
		Contents struct {
			Value string `json:"value"`
		} `json:"contents"`
	}
	_ = json.Unmarshal([]byte(hoverRaw), &h)
	val := h.Contents.Value
	return strings.Contains(val, "```go\nvar ") || strings.Contains(val, "```go\nfield ") || (strings.HasPrefix(val, "```go\ntype ") && strings.Contains(val, "interface"))
}

func isIndirectCallByDefinition(client *goplsclient.Client, pos token.Position) (bool, bool) {
	if client == nil {
		return false, false
	}
	loc, ok, err := client.Definition(pos.Filename, pos.Line, pos.Column)
	if err != nil || !ok || loc.File == "" || loc.Line < 1 {
		return false, false
	}
	targetSrc, err := os.ReadFile(loc.File)
	if err != nil {
		return false, false
	}
	lines := strings.Split(string(targetSrc), "\n")
	if len(lines) < loc.Line {
		return false, false
	}
	line := strings.TrimSpace(lines[loc.Line-1])
	return !strings.HasPrefix(line, "func "), true
}

func cloneStringBoolMap(src map[string]bool) map[string]bool {
	if src == nil {
		return nil
	}
	dst := make(map[string]bool, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func clonePackageFiles(src []PackageFile) []PackageFile {
	if src == nil {
		return nil
	}
	dst := make([]PackageFile, len(src))
	for i, file := range src {
		dst[i] = file
		if len(file.Declarations) > 0 {
			dst[i].Declarations = append([]DeclarationSummary(nil), file.Declarations...)
		}
	}
	return dst
}

func (b *builder) packageDefinitionForImportPath(importPath string) (definitionLocation, bool) {
	if cached, ok := b.pkgDefByPath[importPath]; ok {
		return cached, cached.ok
	}
	files := loadPackageFiles(importPath)
	if len(files) == 0 {
		b.pkgDefByPath[importPath] = definitionLocation{}
		return definitionLocation{}, false
	}
	loc := definitionLocation{file: files[0].File, line: files[0].Line, column: files[0].Column, ok: files[0].File != "" && files[0].Line > 0 && files[0].Column > 0}
	b.pkgDefByPath[importPath] = loc
	return loc, loc.ok
}

func (b *builder) classifyObject(obj types.Object) string {
	if kind := b.kindByObj[obj]; kind != "" {
		return kind
	}
	switch obj.(type) {
	case *types.PkgName:
		return KindPackage
	case *types.TypeName:
		if _, ok := obj.Type().(*types.TypeParam); ok {
			return KindParameter
		}
		return KindType
	case *types.Func:
		return KindFunction
	case *types.Var:
		if _, ok := obj.Type().(*types.TypeParam); ok {
			return KindParameter
		}
		return KindVariable
	default:
		return KindVariable
	}
}

func sortDeclarations(decls []Declaration) {
	sort.Slice(decls, func(i, j int) bool {
		if decls[i].Line != decls[j].Line {
			return decls[i].Line < decls[j].Line
		}
		return decls[i].Column < decls[j].Column
	})
}

func sortReferences(refs []Reference) {
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Line != refs[j].Line {
			return refs[i].Line < refs[j].Line
		}
		return refs[i].Column < refs[j].Column
	})
}

func TopLevelNamedFields(name, source string) []NamedField {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, name, source, parser.SkipObjectResolution)
	if err != nil || node == nil {
		return nil
	}

	var out []NamedField
	topLevelStructs := map[token.Pos]bool{}
	for _, decl := range node.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			if t, ok := ts.Type.(*ast.StructType); ok {
				topLevelStructs[t.Pos()] = true
				collectNamedFields(fset, t.Fields, false, &out)
			}
		}
	}
	ast.Inspect(node, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.StructType:
			if topLevelStructs[n.Pos()] {
				return true
			}
			collectNamedFields(fset, n.Fields, false, &out)
		case *ast.CompositeLit:
			collectInlineStructLiteralFields(fset, n, &out)
		}
		return true
	})

	return out
}

func collectNamedFields(fset *token.FileSet, fields *ast.FieldList, inline bool, out *[]NamedField) {
	if fields == nil {
		return
	}
	for _, field := range fields.List {
		for _, name := range field.Names {
			pos := fset.Position(name.Pos())
			if pos.Line < 1 || pos.Column < 1 {
				continue
			}
			*out = append(*out, NamedField{File: pos.Filename, Line: pos.Line, Column: pos.Column, Text: name.Name, Inline: inline})
		}
	}
}

func collectInlineStructLiteralFields(fset *token.FileSet, lit *ast.CompositeLit, out *[]NamedField) {
	st, ok := lit.Type.(*ast.StructType)
	if !ok || st.Fields == nil {
		return
	}
	fieldNames := map[string]bool{}
	for _, field := range st.Fields.List {
		for _, name := range field.Names {
			if name != nil {
				fieldNames[name.Name] = true
			}
		}
	}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok || !fieldNames[key.Name] {
			continue
		}
		pos := fset.Position(key.Pos())
		if pos.Line < 1 || pos.Column < 1 {
			continue
		}
		*out = append(*out, NamedField{File: pos.Filename, Line: pos.Line, Column: pos.Column, Text: key.Name, Inline: true})
	}
}

func collectNamedFieldsInExpr(fset *token.FileSet, expr ast.Expr, out *[]NamedField) {
	switch n := expr.(type) {
	case *ast.StructType:
		collectNamedFields(fset, n.Fields, false, out)
		for _, field := range n.Fields.List {
			collectNamedFieldsInExpr(fset, field.Type, out)
		}
	case *ast.FuncType:
		if n.Params != nil {
			for _, p := range n.Params.List {
				collectNamedFieldsInExpr(fset, p.Type, out)
			}
		}
		if n.Results != nil {
			for _, r := range n.Results.List {
				collectNamedFieldsInExpr(fset, r.Type, out)
			}
		}
	case *ast.ArrayType:
		collectNamedFieldsInExpr(fset, n.Elt, out)
	case *ast.MapType:
		collectNamedFieldsInExpr(fset, n.Key, out)
		collectNamedFieldsInExpr(fset, n.Value, out)
	case *ast.StarExpr:
		collectNamedFieldsInExpr(fset, n.X, out)
	case *ast.ChanType:
		collectNamedFieldsInExpr(fset, n.Value, out)
	}
}
