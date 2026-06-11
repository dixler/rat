package golang

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

	"rat/internal/file/scan"
	"rat/internal/file/scan/golang/goplsclient"
)

var (
	escapeAnalysisCache sync.Map
	packageFilesCache   sync.Map
)

type Result = scan.Result
type Location = scan.Location
type IndirectCall = scan.IndirectCall
type Declaration = scan.Declaration
type ControlFlowStatement = scan.ControlFlowStatement
type ControlFlowBlock = scan.ControlFlowBlock
type Reference = scan.Reference
type PackageReference = scan.PackageReference
type Package = scan.Package
type PackageFile = scan.PackageFile
type DeclarationSummary = scan.DeclarationSummary
type NamedField = scan.NamedField
type NamedFieldTypeDeclaration = scan.NamedFieldTypeDeclaration
type definitionLocation = scan.Location

type namedFieldInfo struct {
	ReferenceType    bool
	TypeDeclarations []NamedFieldTypeDeclaration
}

type pendingIndirectCall struct {
	fun      ast.Expr
	position token.Position
	text     string
}

type positionKey struct {
	file   string
	line   int
	column int
}

type definitionLookup struct {
	loc goplsclient.Location
	ok  bool
}

type indirectLookup struct {
	indirect bool
	ok       bool
}

type goplsLookupCache struct {
	client              *goplsclient.Client
	definitions         map[positionKey]definitionLookup
	hovers              map[positionKey]string
	indirectDefinitions map[positionKey]indirectLookup
	indirectCalls       map[positionKey]bool
	fileLines           map[string][]string
}

type asyncClient struct {
	ready  chan struct{}
	client *goplsclient.Client
	err    error
}

const (
	KindPackage   = scan.KindPackage
	KindType      = scan.KindType
	KindVariable  = scan.KindVariable
	KindParameter = scan.KindParameter
	KindFunction  = scan.KindFunction
	KindFile      = scan.KindFile
)

const (
	BlockKindBase      = scan.BlockKindBase
	BlockKindIf        = scan.BlockKindIf
	BlockKindElseIf    = scan.BlockKindElseIf
	BlockKindElse      = scan.BlockKindElse
	BlockKindFor       = scan.BlockKindFor
	BlockKindWhile     = scan.BlockKindWhile
	BlockKindDo        = scan.BlockKindDo
	BlockKindSwitch    = scan.BlockKindSwitch
	BlockKindSelect    = scan.BlockKindSelect
	BlockKindCase      = scan.BlockKindCase
	BlockKindTry       = scan.BlockKindTry
	BlockKindCatch     = scan.BlockKindCatch
	BlockKindFinally   = scan.BlockKindFinally
	StatementKindPanic = scan.StatementKindPanic
)

type Scanner struct{}

func init() {
	scan.Register(Scanner{})
}

func (Scanner) Extensions() []string { return []string{".go"} }

func (Scanner) Build(file string, source []byte) (*scan.Result, error) { return buildGo(file, source) }

func startDefaultClient() *asyncClient {
	started := &asyncClient{ready: make(chan struct{})}
	go func() {
		started.client, started.err = goplsclient.Default()
		close(started.ready)
	}()
	return started
}

func (c *asyncClient) wait() (*goplsclient.Client, error) {
	<-c.ready
	return c.client, c.err
}

func buildGo(file string, source []byte) (*Result, error) {
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, source, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	startedClient := startDefaultClient()
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
		declByObj:     map[types.Object]string{},
		kindByObj:     map[types.Object]string{},
		pkgPathByName: map[string]string{},
		pkgByPath:     map[string]string{},
		pkgDefByPath:  map[string]definitionLocation{},
		seen:          map[string]struct{}{},
	}
	syntaxNodes, pendingIndirectCalls := collectGoSyntaxData(fset, parsed, info)
	nodes := collectGoTokenNodes(file, source)
	nodes = append(nodes, syntaxNodes...)
	res := &Result{File: file, Nodes: nodes}
	for _, imp := range parsed.Imports {
		path := strings.Trim(imp.Path.Value, "\"")
		name := importedPackageName(imp)
		segment := importedPathSegment(path)
		pathPos := fset.Position(imp.Path.Pos())
		segmentPos := importPathSegmentLocation(pathPos, path)
		res.Nodes = append(res.Nodes, importStringCommentNodes(pathPos, path)...)
		pkgID := b.pkgByPath[path]
		if pkgID == "" {
			pkgID = b.nextID("pkg")
			b.pkgByPath[path] = pkgID
		}
		pkgFiles := loadPackageFiles(path)
		pkgFile, pkgLine, pkgColumn := b.file, pathPos.Line, pathPos.Column
		if len(pkgFiles) > 0 {
			pkgFile, pkgLine, pkgColumn = pkgFiles[0].File, pkgFiles[0].Line, pkgFiles[0].Column
		}
		if pathPos.Line > 0 && pathPos.Column > 0 && path != "" && segment != "" {
			res.PackageReferences = append(res.PackageReferences, PackageReference{PackageID: pkgID, ParentID: KindFile, Text: segment, Location: segmentPos})
		}
		if imp.Name != nil && imp.Name.Name != "." && imp.Name.Name != "_" {
			aliasPos := fset.Position(imp.Name.Pos())
			if aliasPos.Line > 0 && aliasPos.Column > 0 {
				res.PackageReferences = append(res.PackageReferences, PackageReference{
					PackageID: pkgID,
					ParentID:  KindFile,
					Text:      imp.Name.Name,
					Location:  Location{b.file, aliasPos.Line, aliasPos.Column},
				})
			}
		}
		res.Packages = append(res.Packages, Package{ID: pkgID, Name: path, Location: Location{pkgFile, pkgLine, pkgColumn}, Files: pkgFiles})
		if name != "" && name != "." && name != "_" {
			b.pkgPathByName[name] = path
		}
	}
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
	res.Nodes = append(res.Nodes, commentNodes(fset, parsed)...)
	sortDeclarations(res.Declarations)
	sort.Slice(res.PackageReferences, func(i, j int) bool { return res.PackageReferences[i].Text < res.PackageReferences[j].Text })
	client, err := startedClient.wait()
	if err != nil {
		return nil, err
	}
	if err := finalizeGoResult(res, parsed, info, &b, pendingIndirectCalls, client, source); err != nil {
		return nil, err
	}
	return res, nil
}

type builder struct {
	file             string
	fset             *token.FileSet
	info             *types.Info
	returnErrors     map[token.Pos]bool
	declByObj        map[types.Object]string
	kindByObj        map[types.Object]string
	pkgPathByName    map[string]string
	pkgByPath        map[string]string
	pkgDefByPath     map[string]definitionLocation
	definitionForPos func(token.Pos) (definitionLocation, bool)
	seq              int
	seen             map[string]struct{}
}

func collectGoSyntaxData(fset *token.FileSet, parsed *ast.File, info *types.Info) ([]scan.Node, []pendingIndirectCall) {
	if fset == nil || parsed == nil {
		return nil, nil
	}
	var nodes []scan.Node
	var pending []pendingIndirectCall
	ast.Inspect(parsed, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.ArrayType:
			if syntaxNode := arrayTypeNode(fset, node); syntaxNode != nil {
				nodes = append(nodes, syntaxNode)
			}
		case *ast.CallExpr:
			name, startPos := indirectCallTarget(node.Fun, fset)
			if name == "" || startPos == token.NoPos {
				break
			}
			pending = append(pending, pendingIndirectCall{
				fun:      node.Fun,
				position: fset.Position(startPos),
				text:     name,
			})
		case *ast.FuncDecl:
			if node.Type == nil {
				break
			}
			nodes = append(nodes, functionNode(fset, node.Type.Func, node.Body, signatureLastResultIsError(funcDeclSignature(node, info)), false)...)
		case *ast.FuncLit:
			if node.Type == nil {
				break
			}
			nodes = append(nodes, functionNode(fset, node.Type.Func, node.Body, signatureLastResultIsError(funcLitSignature(node, info)), true)...)
		}
		return true
	})
	return nodes, pending
}

func functionNode(fset *token.FileSet, funcPos token.Pos, body *ast.BlockStmt, returnsError, inline bool) []scan.Node {
	p := fset.Position(funcPos)
	spans := []scan.Span{{Line: p.Line, Column: p.Column, Length: len("func")}}
	if body != nil {
		open := fset.Position(body.Lbrace)
		close := fset.Position(body.Rbrace)
		spans = append(spans, scan.Span{Line: open.Line, Column: open.Column, Length: 1}, scan.Span{Line: close.Line, Column: close.Column, Length: 1})
	}
	out := []scan.Node{scan.FunctionSyntaxNode{NodeSpans: spans, ReturnsError: returnsError}}
	if inline && body != nil {
		if indentSpans := inlineFunctionIndentSpans(fset, body); len(indentSpans) > 0 {
			out = append(out, scan.InlineFunctionIndentNode{NodeSpans: indentSpans})
		}
	}
	return out
}

func inlineFunctionIndentSpans(fset *token.FileSet, body *ast.BlockStmt) []scan.Span {
	if fset == nil || body == nil || body.Lbrace == token.NoPos || body.Rbrace == token.NoPos {
		return nil
	}
	open := fset.Position(body.Lbrace)
	close := fset.Position(body.Rbrace)
	if open.Line < 1 || close.Line <= open.Line || close.Column < 1 {
		return nil
	}

	baseColumn := close.Column
	indentWidth := 0
	for _, stmt := range body.List {
		if stmt == nil {
			continue
		}
		pos := fset.Position(stmt.Pos())
		if pos.Column <= baseColumn {
			continue
		}
		width := pos.Column - baseColumn
		if indentWidth == 0 || width < indentWidth {
			indentWidth = width
		}
	}
	if indentWidth < 1 {
		return nil
	}

	lines := map[int]struct{}{}
	addLine := func(line int) {
		if line > open.Line && line < close.Line {
			lines[line] = struct{}{}
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch node := n.(type) {
		case nil:
			return true
		case ast.Stmt:
			addLine(fset.Position(node.Pos()).Line)
		}
		if block, ok := n.(*ast.BlockStmt); ok && block != body {
			addLine(fset.Position(block.Rbrace).Line)
		}
		return true
	})

	out := make([]scan.Span, 0, len(lines))
	for line := range lines {
		out = append(out, scan.Span{Line: line, Column: baseColumn, Length: indentWidth})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	return out
}

func funcDeclSignature(fn *ast.FuncDecl, info *types.Info) *types.Signature {
	if fn == nil || info == nil || fn.Name == nil {
		return nil
	}
	obj, _ := info.Defs[fn.Name].(*types.Func)
	if obj == nil {
		return nil
	}
	sig, _ := obj.Type().(*types.Signature)
	return sig
}

func funcLitSignature(fn *ast.FuncLit, info *types.Info) *types.Signature {
	if fn == nil || info == nil {
		return nil
	}
	tv, ok := info.Types[fn]
	if !ok {
		return nil
	}
	sig, _ := tv.Type.(*types.Signature)
	return sig
}

func signatureLastResultIsError(sig *types.Signature) bool {
	if sig == nil || sig.Results() == nil || sig.Results().Len() == 0 {
		return false
	}
	return isErrorType(sig.Results().At(sig.Results().Len() - 1).Type())
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
		for v := range tuple.Variables() {
			out = append(out, v.Type())
		}
		return out
	}
	return []types.Type{tv.Type}
}

func tupleHasError(tuple *types.Tuple) bool {
	if tuple == nil {
		return false
	}
	for v := range tuple.Variables() {
		if isErrorType(v.Type()) {
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

func (b *builder) nextID(prefix string) string {
	b.seq++
	return fmt.Sprintf("%s-%d", prefix, b.seq)
}

func (b *builder) buildSpecs(spec ast.Spec) []Declaration {
	switch s := spec.(type) {
	case *ast.TypeSpec:
		decl := b.newDeclaration(s.Name, KindType)
		b.appendFieldDeclarations(&decl, s.TypeParams, KindParameter)
		if iface, ok := s.Type.(*ast.InterfaceType); ok && iface.Methods != nil {
			b.appendFieldDeclarations(&decl, iface.Methods, KindFunction)
		}
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
	b.appendFunctionBody(&decl, fn.Body)
	return decl
}

func (b *builder) buildFuncLit(fn *ast.FuncLit) Declaration {
	p := b.fset.Position(fn.Type.Func)
	decl := Declaration{ID: b.nextID(KindFunction), Kind: KindFunction, Location: Location{b.file, p.Line, p.Column}}
	if fn.Type != nil {
		b.appendFieldDeclarations(&decl, fn.Type.TypeParams, KindParameter)
		b.appendFieldDeclarations(&decl, fn.Type.Params, KindParameter)
		b.collectReferences(fn.Type, &decl)
	}
	b.appendFunctionBody(&decl, fn.Body)
	return decl
}

func (b *builder) appendFunctionBody(decl *Declaration, body *ast.BlockStmt) {
	if decl == nil || body == nil {
		return
	}
	cfb := controlFlowBuilder{fset: b.fset, file: b.file, returnErrors: b.returnErrors, labels: map[string]*ControlFlowBlock{}}
	decl.ControlFlow = cfb.buildBlocks(body.List)
	b.appendBodyDeclarations(decl, body)
	b.collectReferences(body, decl)
	sortDeclarations(decl.Declarations)
}

func (b *builder) appendBodyDeclarations(decl *Declaration, body *ast.BlockStmt) {
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			decl.Declarations = append(decl.Declarations, b.buildFuncLit(x))
			return false
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
					decl.Declarations = append(decl.Declarations, b.newDeclaration(id, KindVariable))
				}
			}
		case *ast.RangeStmt:
			for _, expr := range []ast.Expr{x.Key, x.Value} {
				id, ok := expr.(*ast.Ident)
				if ok && id.Name != "_" {
					decl.Declarations = append(decl.Declarations, b.newDeclaration(id, KindVariable))
				}
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
				if !ok || b.info.Implicits[clause] == nil {
					continue
				}
				b.declByObj[b.info.Implicits[clause]] = child.ID
				b.kindByObj[b.info.Implicits[clause]] = KindVariable
			}
		}
		return true
	})
}

type controlFlowBuilder struct {
	fset         *token.FileSet
	file         string
	returnErrors map[token.Pos]bool
	labels       map[string]*ControlFlowBlock
	breakStack   []*ControlFlowBlock
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
	block := ControlFlowBlock{Kind: BlockKindBase, Location: Location{b.file, pos.Line, pos.Column}}
	switch s := stmt.(type) {
	case *ast.BlockStmt:
		block.Blocks = b.buildBlocks(s.List)
	case *ast.IfStmt:
		block = b.buildIfChain(s, BlockKindIf, s.If)
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
	block.HasTerminalControlFlowStatement = controlFlowBlockHasTerminalStatement(block)
	return block
}

func (b *controlFlowBuilder) buildIfChain(stmt *ast.IfStmt, kind string, keywordPos token.Pos) ControlFlowBlock {
	pos := b.fset.Position(keywordPos)
	block := ControlFlowBlock{Kind: kind, Location: Location{b.file, pos.Line, pos.Column}}
	block.Statements = b.collectControlFlowStatements(stmt.Init, stmt.Cond)
	if stmt.Body != nil {
		setBlockBracesFromStmt(b.fset, &block, stmt.Body)
		block.Blocks = append(block.Blocks, b.buildBlocks(stmt.Body.List)...)
	}
	if stmt.Else != nil {
		elsePos := stmt.Else.Pos()
		switch e := stmt.Else.(type) {
		case *ast.IfStmt:
			block.Blocks = append(block.Blocks, b.buildIfChain(e, BlockKindElseIf, elsePos))
		case *ast.BlockStmt:
			elseLoc := b.fset.Position(elsePos)
			elseBlock := ControlFlowBlock{Kind: BlockKindElse, Location: Location{b.file, elseLoc.Line, elseLoc.Column}}
			setBlockBracesFromStmt(b.fset, &elseBlock, e)
			elseBlock.Blocks = b.buildBlocks(e.List)
			elseBlock.HasTerminalControlFlowStatement = controlFlowBlockHasTerminalStatement(elseBlock)
			block.Blocks = append(block.Blocks, elseBlock)
		default:
			block.Blocks = append(block.Blocks, b.buildBlock(e))
		}
	}
	block.HasTerminalControlFlowStatement = controlFlowBlockHasTerminalStatement(block)
	return block
}

func (b *controlFlowBuilder) buildForBlock(pos token.Pos, body *ast.BlockStmt, label string) ControlFlowBlock {
	p := b.fset.Position(pos)
	block := ControlFlowBlock{Kind: BlockKindFor, Location: Location{b.file, p.Line, p.Column}}
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
	block.HasTerminalControlFlowStatement = controlFlowBlockHasTerminalStatement(block)
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
	if blockUsesDirectTerminalStatement(block.Kind) {
		return controlFlowBlockHasDirectTerminalStatement(block)
	}
	return controlFlowBlockHasNestedTerminalStatement(block)
}

func controlFlowBlockHasDirectTerminalStatement(block ControlFlowBlock) bool {
	for _, stmt := range block.Statements {
		if isTerminalControlFlowKind(stmt.Kind) {
			return true
		}
	}
	for _, child := range block.Blocks {
		for _, stmt := range child.Statements {
			if isTerminalControlFlowKind(stmt.Kind) {
				return true
			}
		}
	}
	return false
}

func controlFlowBlockHasNestedTerminalStatement(block ControlFlowBlock) bool {
	for _, stmt := range block.Statements {
		if isTerminalControlFlowKind(stmt.Kind) {
			return true
		}
	}
	for _, child := range block.Blocks {
		if controlFlowBlockHasNestedTerminalStatement(child) {
			return true
		}
	}
	return false
}

func blockUsesDirectTerminalStatement(kind string) bool {
	switch scan.BlockConstructKind(kind) {
	case scan.ConstructKindBranch, scan.ConstructKindBranchAlternative, scan.ConstructKindCase:
		return true
	default:
		return false
	}
}

func isTerminalControlFlowKind(kind string) bool {
	switch kind {
	case "return", "throw", "continue", "break", "goto", "panic":
		return true
	default:
		return false
	}
}

func (b *controlFlowBuilder) buildSwitchBlock(pos token.Pos, body *ast.BlockStmt) ControlFlowBlock {
	p := b.fset.Position(pos)
	block := ControlFlowBlock{Kind: BlockKindSwitch, Location: Location{b.file, p.Line, p.Column}}
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
	block.HasTerminalControlFlowStatement = controlFlowBlockHasTerminalStatement(block)
	return block
}

func (b *controlFlowBuilder) buildSelectBlock(stmt *ast.SelectStmt) ControlFlowBlock {
	p := b.fset.Position(stmt.Select)
	block := ControlFlowBlock{Kind: BlockKindSelect, Location: Location{b.file, p.Line, p.Column}}
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
	block.HasTerminalControlFlowStatement = controlFlowBlockHasTerminalStatement(block)
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
	p := b.fset.Position(casePos)
	caseBlock := ControlFlowBlock{Kind: BlockKindCase, Location: Location{b.file, p.Line, p.Column}, HasDefault: hasDefault}
	caseNodes := make([]ast.Node, 0, len(body))
	for _, stmt := range body {
		caseNodes = append(caseNodes, stmt)
	}
	caseBlock.Statements = b.collectControlFlowStatements(caseNodes...)
	caseBlock.Blocks = b.buildBlocks(body)
	caseBlock.HasTerminalControlFlowStatement = controlFlowBlockHasTerminalStatement(caseBlock)
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
				out = append(out, ControlFlowStatement{Kind: "return", Location: Location{b.file, p.Line, p.Column}, ReturnsError: b.returnErrors[s.Return]})
			case *ast.BranchStmt:
				if s.Tok == token.BREAK {
					b.markBreakTarget(s)
				}
				kind := strings.ToLower(s.Tok.String())
				p := b.fset.Position(s.TokPos)
				out = append(out, ControlFlowStatement{Kind: kind, Location: Location{b.file, p.Line, p.Column}})
			case *ast.CallExpr:
				id, ok := s.Fun.(*ast.Ident)
				if !ok || id.Name != StatementKindPanic {
					return true
				}
				p := b.fset.Position(id.NamePos)
				out = append(out, ControlFlowStatement{Kind: StatementKindPanic, Location: Location{b.file, p.Line, p.Column}})
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
	structFieldsByType := map[string]map[string]namedFieldInfo{}
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

func (b *builder) structFieldTypes(fields *ast.FieldList) map[string]namedFieldInfo {
	byName := map[string]namedFieldInfo{}
	if fields == nil {
		return byName
	}
	for _, field := range fields.List {
		info := namedFieldInfo{
			ReferenceType:    b.isReferenceTypeExpr(field.Type),
			TypeDeclarations: b.namedFieldTypeDeclarations(field.Type),
		}
		for _, name := range field.Names {
			if name != nil {
				byName[name.Name] = info
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
			typeDeclarations := b.namedFieldTypeDeclarations(field.Type)
			var loc NamedFieldTypeDeclaration
			if len(typeDeclarations) > 0 {
				loc = typeDeclarations[0]
			}
			*out = append(*out, NamedField{
				Location:         Location{pos.Filename, pos.Line, pos.Column},
				ReferenceType:    b.isReferenceTypeExpr(field.Type),
				TypeDeclarations: typeDeclarations,
				Declaration:      loc,
				Text:             name.Name,
				Inline:           inline,
			})
		}
	}
}

func (b *builder) collectTypedStructLiteralFields(lit *ast.CompositeLit, out *[]NamedField) bool {
	if b.info == nil {
		return false
	}
	tv, ok := b.info.Types[lit.Type]
	if !ok || tv.Type == nil {
		return false
	}
	t := tv.Type
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	if named, ok := t.(*types.Named); ok {
		t = named.Underlying()
	}
	st, ok := t.(*types.Struct)
	if !ok {
		return false
	}
	byName := map[string]namedFieldInfo{}
	for field := range st.Fields() {
		if field != nil {
			byName[field.Name()] = namedFieldInfo{
				ReferenceType:    isReferenceType(field.Type()),
				TypeDeclarations: b.namedFieldTypeDeclarationsForType(field.Type()),
			}
		}
	}
	structTypeLoc, hasStructTypeLoc := definitionLocation{}, false
	if ptr, ok := tv.Type.(*types.Pointer); ok {
		tv.Type = ptr.Elem()
	}
	if named, ok := tv.Type.(*types.Named); ok {
		structTypeLoc, hasStructTypeLoc = b.typeNameLocation(named.Obj())
	}
	return b.collectStructLiteralFields(lit, byName, structTypeLoc, hasStructTypeLoc, out)
}

func (b *builder) collectInlineStructLiteralFields(lit *ast.CompositeLit, out *[]NamedField) bool {
	st, ok := lit.Type.(*ast.StructType)
	if !ok || st.Fields == nil {
		return false
	}
	byName := b.structFieldTypes(st.Fields)
	return b.collectStructLiteralFields(lit, byName, definitionLocation{}, false, out)
}

func (b *builder) collectNamedStructLiteralFields(lit *ast.CompositeLit, structFieldsByType map[string]map[string]namedFieldInfo, out *[]NamedField) bool {
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

func (b *builder) collectStructLiteralFields(lit *ast.CompositeLit, byName map[string]namedFieldInfo, typeLoc definitionLocation, hasTypeLoc bool, out *[]NamedField) bool {
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
		info, ok := byName[key.Name]
		if !ok {
			continue
		}
		pos := b.fset.Position(key.Pos())
		if pos.Line < 1 || pos.Column < 1 {
			continue
		}
		named := NamedField{Location: Location{pos.Filename, pos.Line, pos.Column}, Text: key.Name, Inline: true, ReferenceType: info.ReferenceType, TypeDeclarations: info.TypeDeclarations}
		if hasTypeLoc {
			named.StructDecl = typeLoc
		}
		*out = append(*out, named)
		collected = true
	}
	return collected
}

func (b *builder) namedFieldTypeDeclarations(expr ast.Expr) []NamedFieldTypeDeclaration {
	if b.info == nil {
		return nil
	}
	var out []NamedFieldTypeDeclaration
	for _, loc := range b.typeDeclarationsFor(expr) {
		out = append(out, NamedFieldTypeDeclaration{Location: Location{loc.File, loc.Line, loc.Column}})
	}
	return out
}

func (b *builder) isReferenceTypeExpr(expr ast.Expr) bool {
	if b.info == nil || expr == nil {
		return false
	}
	tv, ok := b.info.Types[expr]
	return ok && isReferenceType(tv.Type)
}

func (b *builder) namedFieldTypeDeclarationsForType(t types.Type) []NamedFieldTypeDeclaration {
	var out []NamedFieldTypeDeclaration
	b.appendTypeDeclarations(t, &out)
	return out
}

func (b *builder) appendTypeDeclarations(t types.Type, out *[]NamedFieldTypeDeclaration) {
	switch t := t.(type) {
	case nil, *types.Basic:
		return
	case *types.Named:
		if loc, ok := b.typeNameLocation(t.Obj()); ok {
			b.appendNamedFieldTypeDeclaration(out, loc)
		}
		if typeArgs := t.TypeArgs(); typeArgs != nil {
			for i := 0; i < typeArgs.Len(); i++ {
				b.appendTypeDeclarations(typeArgs.At(i), out)
			}
		}
	case *types.Pointer:
		b.appendTypeDeclarations(t.Elem(), out)
	case *types.Slice:
		b.appendTypeDeclarations(t.Elem(), out)
	case *types.Array:
		b.appendTypeDeclarations(t.Elem(), out)
	case *types.Map:
		b.appendTypeDeclarations(t.Key(), out)
		b.appendTypeDeclarations(t.Elem(), out)
	case *types.Chan:
		b.appendTypeDeclarations(t.Elem(), out)
	case *types.Signature:
		b.appendTupleTypeDeclarations(t.Params(), out)
		b.appendTupleTypeDeclarations(t.Results(), out)
	case *types.Struct:
		for field := range t.Fields() {
			b.appendTypeDeclarations(field.Type(), out)
		}
	case *types.Interface:
		for etyp := range t.EmbeddedTypes() {
			b.appendTypeDeclarations(etyp, out)
		}
	}
}

func (b *builder) appendTupleTypeDeclarations(tuple *types.Tuple, out *[]NamedFieldTypeDeclaration) {
	if tuple == nil {
		return
	}
	for v := range tuple.Variables() {
		b.appendTypeDeclarations(v.Type(), out)
	}
}

func (b *builder) typeNameLocation(obj *types.TypeName) (definitionLocation, bool) {
	if obj == nil {
		return definitionLocation{}, false
	}
	if obj.Pkg() == nil {
		return definitionLocation{File: "", Line: 1, Column: 1}, true
	}
	if obj.Pkg().Path() != filepath.Dir(b.file) {
		if loc, ok := b.packageTypeDefinition(obj.Pkg().Path(), obj.Name()); ok {
			return loc, true
		}
	}
	if obj.Pos() != token.NoPos {
		pos := b.fset.Position(obj.Pos())
		if pos.Filename != "" && pos.Line > 0 && pos.Column > 0 {
			return definitionLocation{File: pos.Filename, Line: pos.Line, Column: pos.Column}, true
		}
	}
	return b.packageTypeDefinition(obj.Pkg().Path(), obj.Name())
}

func (b *builder) packageTypeDefinition(importPath, name string) (definitionLocation, bool) {
	for _, file := range loadPackageFiles(importPath) {
		for _, decl := range file.Declarations {
			if decl.Kind == KindType && decl.Name == name {
				return definitionLocation{File: decl.File, Line: decl.Line, Column: decl.Column}, true
			}
		}
	}
	return definitionLocation{}, false
}

func (b *builder) appendNamedFieldTypeDeclaration(out *[]NamedFieldTypeDeclaration, loc definitionLocation) {
	key := fmt.Sprintf("%s:%d:%d", loc.File, loc.Line, loc.Column)
	if _, ok := b.seen[key]; ok {
		return
	}
	b.seen[key] = struct{}{}
	*out = append(*out, NamedFieldTypeDeclaration{Location: loc})
}

func (b *builder) typeDeclarationsFor(expr ast.Expr) []definitionLocation {
	var ids []*ast.Ident
	var walk func(ast.Expr)
	walk = func(expr ast.Expr) {
		switch n := expr.(type) {
		case *ast.Ident:
			ids = append(ids, n)
		case *ast.SelectorExpr:
			ids = append(ids, n.Sel)
		case *ast.StarExpr:
			walk(n.X)
		case *ast.ArrayType:
			walk(n.Elt)
		case *ast.MapType:
			walk(n.Key)
			walk(n.Value)
		case *ast.ChanType:
			walk(n.Value)
		case *ast.IndexExpr:
			walk(n.X)
			walk(n.Index)
		case *ast.IndexListExpr:
			walk(n.X)
			for _, idx := range n.Indices {
				walk(idx)
			}
		case *ast.ParenExpr:
			walk(n.X)
		case *ast.FuncType:
			for _, fields := range []*ast.FieldList{n.Params, n.Results} {
				if fields == nil {
					continue
				}
				for _, field := range fields.List {
					walk(field.Type)
				}
			}
		case *ast.InterfaceType:
			if n.Methods != nil {
				for _, field := range n.Methods.List {
					walk(field.Type)
				}
			}
		case *ast.StructType:
			if n.Fields != nil {
				for _, field := range n.Fields.List {
					walk(field.Type)
				}
			}
		}
	}
	walk(expr)
	out := make([]definitionLocation, 0, len(ids))
	seen := map[string]struct{}{}
	for _, id := range ids {
		if id == nil || id.Pos() == token.NoPos {
			continue
		}
		loc, ok := b.typeDeclarationForIdent(id)
		if !ok {
			continue
		}
		key := fmt.Sprintf("%s:%d:%d", loc.File, loc.Line, loc.Column)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, loc)
	}
	return out
}

func (b *builder) typeDeclarationForIdent(id *ast.Ident) (definitionLocation, bool) {
	if b.definitionForPos != nil {
		if loc, ok := b.definitionForPos(id.Pos()); ok {
			return loc, true
		}
	}
	if obj := b.info.Uses[id]; obj != nil {
		if obj.Parent() == types.Universe {
			return definitionLocation{File: "", Line: 1, Column: 1}, true
		}
		objPos := b.fset.Position(obj.Pos())
		if objPos.Filename != "" && objPos.Line > 0 && objPos.Column > 0 {
			return definitionLocation{File: objPos.Filename, Line: objPos.Line, Column: objPos.Column}, true
		}
	}
	return definitionLocation{}, false
}

func (b *builder) newDeclaration(id *ast.Ident, kind string) Declaration {
	pos := b.fset.Position(id.Pos())
	decl := Declaration{
		ID:   b.nextID(kind),
		Name: id.Name,
		Kind: kind,
		Location: Location{
			File:   b.file,
			Line:   pos.Line,
			Column: pos.Column,
		},
	}
	if obj := b.info.Defs[id]; obj != nil {
		b.declByObj[obj] = decl.ID
		b.kindByObj[obj] = kind
		decl.ReferenceType = isReferenceType(obj.Type())
	}
	return decl
}

func isReferenceType(t types.Type) bool {
	return isReferenceTypeSeen(types.Unalias(t), map[types.Type]bool{})
}

func isReferenceTypeSeen(t types.Type, seen map[types.Type]bool) bool {
	t = types.Unalias(t)
	switch t := t.(type) {
	case nil:
		return false
	case *types.Pointer, *types.Slice, *types.Map, *types.Chan, *types.Interface, *types.Signature:
		return true
	case *types.Array:
		return isReferenceTypeSeen(t.Elem(), seen)
	case *types.Named:
		if seen[t] {
			return false
		}
		seen[t] = true
		return isReferenceTypeSeen(t.Underlying(), seen)
	case *types.Struct:
		for field := range t.Fields() {
			if field != nil && isReferenceTypeSeen(field.Type(), seen) {
				return true
			}
		}
	}
	return false
}

func (b *builder) collectReferences(node ast.Node, decl *Declaration) {
	ast.Inspect(node, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok && n != node {
			return false
		}
		if field, ok := n.(*ast.Field); ok {
			b.collectReferences(field.Type, decl)
			return false
		}
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
	ref := Reference{
		Text: id.Name,
		Location: Location{
			File:   b.file,
			Line:   pos.Line,
			Column: pos.Column,
		},
		Kind: b.classifyObject(b.info.Uses[id]),
	}
	if importPath != "" {
		ref.Kind = KindPackage
		if loc, ok := b.packageDefinitionForImportPath(importPath); ok {
			ref.Declaration = loc
		}
	} else if obj := b.info.Uses[id]; obj != nil {
		ref.ReferenceType = isReferenceType(obj.Type())
		ref.DeclarationID = b.declByObj[obj]
		if pkgName, ok := obj.(*types.PkgName); ok && pkgName.Imported() != nil {
			if loc, ok := b.packageDefinitionForImportPath(pkgName.Imported().Path()); ok {
				ref.Declaration = loc
			}
		}
	}
	decl.References = append(decl.References, ref)
}

func importedPackageName(imp *ast.ImportSpec) string {
	if imp == nil {
		return ""
	}
	path := strings.Trim(imp.Path.Value, "\"")
	name := importedPathSegment(path)
	if imp.Name != nil {
		name = imp.Name.Name
	}
	return name
}

func importedPathSegment(path string) string {
	if path == "" {
		return ""
	}
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		return path[idx+1:]
	}
	return path
}

func importPathSegmentLocation(pathPos token.Position, path string) Location {
	segment := importedPathSegment(path)
	if pathPos.Line < 1 || pathPos.Column < 1 || segment == "" {
		return Location{}
	}
	startIndex := len(path) - len(segment)
	return Location{File: pathPos.Filename, Line: pathPos.Line, Column: pathPos.Column + 1 + startIndex}
}

func importStringCommentNodes(pathPos token.Position, path string) []scan.Node {
	segment := importedPathSegment(path)
	if pathPos.Line < 1 || pathPos.Column < 1 || path == "" || segment == "" {
		return nil
	}
	segmentStart := pathPos.Column + 1 + len(path) - len(segment)
	segmentEnd := segmentStart + len(segment)
	quoteEndExclusive := pathPos.Column + len(path) + 2
	var out []scan.Node
	if prefixLen := segmentStart - pathPos.Column; prefixLen > 0 {
		out = append(out, scan.CommentNode{NodeSpans: []scan.Span{{Line: pathPos.Line, Column: pathPos.Column, Length: prefixLen}}})
	}
	if suffixLen := quoteEndExclusive - segmentEnd; suffixLen > 0 {
		out = append(out, scan.CommentNode{NodeSpans: []scan.Span{{Line: pathPos.Line, Column: segmentEnd, Length: suffixLen}}})
	}
	return out
}

func commentNodes(fset *token.FileSet, parsed *ast.File) []scan.Node {
	if fset == nil || parsed == nil {
		return nil
	}
	var out []scan.Node
	for _, group := range parsed.Comments {
		for _, comment := range group.List {
			if comment == nil {
				continue
			}
			start := fset.Position(comment.Pos())
			if start.Line < 1 || start.Column < 1 || comment.Text == "" {
				continue
			}
			spans := scan.SpansForText(start.Line, start.Column, comment.Text)
			if len(spans) == 0 {
				continue
			}
			out = append(out, scan.CommentNode{NodeSpans: spans})
		}
	}
	return out
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
		pf := PackageFile{Location: Location{File: file, Line: 1, Column: 1}}
		for _, decl := range parsed.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				pos := fset.Position(d.Name.Pos())
				pf.Declarations = append(pf.Declarations, DeclarationSummary{Name: d.Name.Name, Kind: KindFunction, Location: Location{file, pos.Line, pos.Column}})
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						pos := fset.Position(s.Name.Pos())
						pf.Declarations = append(pf.Declarations, DeclarationSummary{Name: s.Name.Name, Kind: KindType, Location: Location{file, pos.Line, pos.Column}})
					case *ast.ValueSpec:
						for _, name := range s.Names {
							pos := fset.Position(name.Pos())
							pf.Declarations = append(pf.Declarations, DeclarationSummary{Name: name.Name, Kind: KindVariable, Location: Location{file, pos.Line, pos.Column}})
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

func isIndirectCall(fun ast.Expr, info *types.Info, lookup *goplsLookupCache, pos token.Position) bool {
	if indirect, ok := lookup.indirectCall(pos); ok {
		return indirect
	}
	indirect := isIndirectCallUncached(fun, info, lookup, pos)
	lookup.storeIndirectCall(pos, indirect)
	return indirect
}

func isIndirectCallUncached(fun ast.Expr, info *types.Info, lookup *goplsLookupCache, pos token.Position) bool {
	switch expr := fun.(type) {
	case *ast.Ident:
		if indirect, ok := indirectObject(info.Uses[expr]); ok {
			return indirect
		}
		if indirect, ok := isIndirectCallByDefinition(lookup, pos); ok {
			return indirect
		}
		return isIndirectCallByHover(lookup, pos)
	case *ast.SelectorExpr:
		if isPackageQualifier(expr.X, info) {
			return false
		}
		if selection := info.Selections[expr]; selection != nil {
			if indirect, ok := indirectObject(selection.Obj()); ok && !indirect {
				if isInterfaceType(selection.Recv()) {
					if indirect, ok := isIndirectCallByDefinition(lookup, pos); ok {
						return indirect
					}
					return true
				}
				return false
			}
			if indirect, ok := isIndirectCallByDefinition(lookup, pos); ok {
				return indirect
			}
			return isIndirectCallByHover(lookup, pos)
		}
		if indirect, ok := isIndirectCallByDefinition(lookup, pos); ok {
			return indirect
		}
		return isIndirectCallByHover(lookup, pos)
	case *ast.FuncLit:
		return false
	case *ast.IndexExpr, *ast.IndexListExpr:
		return true
	case *ast.ParenExpr:
		return isIndirectCall(expr.X, info, lookup, pos)
	}

	if indirect, ok := isIndirectCallByDefinition(lookup, pos); ok {
		return indirect
	}
	return isIndirectCallByHover(lookup, pos)
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
	if ok {
		_, ok = info.Uses[id].(*types.PkgName)
	}
	return ok
}

func isInterfaceType(t types.Type) bool {
	if t == nil {
		return false
	}
	_, ok := types.Unalias(t).Underlying().(*types.Interface)
	return ok
}

func isIndirectCallByHover(lookup *goplsLookupCache, pos token.Position) bool {
	if lookup == nil {
		return false
	}
	hoverRaw, err := lookup.HoverForPosition(pos)
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

func isIndirectCallByDefinition(lookup *goplsLookupCache, pos token.Position) (bool, bool) {
	if lookup == nil {
		return false, false
	}
	if indirect, ok := lookup.indirectDefinition(pos); ok {
		return indirect.indirect, indirect.ok
	}
	loc, ok, err := lookup.DefinitionForPosition(pos)
	if err != nil || !ok || loc.File == "" || loc.Line < 1 {
		lookup.storeIndirectDefinition(pos, indirectLookup{})
		return false, false
	}
	lines, err := lookup.FileLines(loc.File)
	if err != nil {
		lookup.storeIndirectDefinition(pos, indirectLookup{})
		return false, false
	}
	if len(lines) < loc.Line {
		lookup.storeIndirectDefinition(pos, indirectLookup{})
		return false, false
	}
	line := strings.TrimSpace(lines[loc.Line-1])
	result := indirectLookup{indirect: !strings.HasPrefix(line, "func "), ok: true}
	lookup.storeIndirectDefinition(pos, result)
	return result.indirect, result.ok
}

func finalizeGoResult(res *Result, parsed *ast.File, info *types.Info, b *builder, pendingIndirectCalls []pendingIndirectCall, client *goplsclient.Client, source []byte) error {
	if res == nil || client == nil {
		return nil
	}
	if err := client.SyncDocumentContent(b.file, string(source)); err != nil {
		return err
	}
	lookup := newGoplsLookupCache(client)
	finalizeReferenceDeclarations(res.Declarations, lookup)
	res.NamedFields = finalizeNamedFields(parsed, b, lookup)
	finalizeIndirectCalls(res, pendingIndirectCalls, info, lookup)
	return nil
}

func finalizeReferenceDeclarations(decls []Declaration, lookup *goplsLookupCache) {
	for i := range decls {
		for j := range decls[i].References {
			ref := &decls[i].References[j]
			if ref.DeclarationID != "" || scan.HasLocation(ref.Declaration) {
				continue
			}
			loc, ok, err := lookup.DefinitionForPosition(token.Position{Filename: ref.File, Line: ref.Line, Column: ref.Column})
			if err == nil && ok {
				ref.Declaration = definitionLocation(loc)
			}
		}
		finalizeReferenceDeclarations(decls[i].Declarations, lookup)
	}
}

func finalizeNamedFields(parsed *ast.File, base *builder, lookup *goplsLookupCache) []NamedField {
	if parsed == nil || base == nil {
		return nil
	}
	b := *base
	b.seen = map[string]struct{}{}
	b.definitionForPos = func(pos token.Pos) (definitionLocation, bool) {
		loc, ok, err := lookup.DefinitionForPosition(b.fset.Position(pos))
		if err != nil {
			return definitionLocation{}, false
		}
		return definitionLocation(loc), ok
	}
	return b.collectTopLevelNamedFields(parsed)
}

func finalizeIndirectCalls(res *Result, pending []pendingIndirectCall, info *types.Info, lookup *goplsLookupCache) {
	for _, call := range pending {
		if call.text == "" || call.position.Line < 1 || call.position.Column < 1 {
			continue
		}
		if !isIndirectCall(call.fun, info, lookup, call.position) {
			continue
		}
		res.IndirectCalls = append(res.IndirectCalls, IndirectCall{Location: Location{File: call.position.Filename, Line: call.position.Line, Column: call.position.Column}, Text: call.text})
	}
}

func newGoplsLookupCache(client *goplsclient.Client) *goplsLookupCache {
	return &goplsLookupCache{
		client:              client,
		definitions:         map[positionKey]definitionLookup{},
		hovers:              map[positionKey]string{},
		indirectDefinitions: map[positionKey]indirectLookup{},
		indirectCalls:       map[positionKey]bool{},
		fileLines:           map[string][]string{},
	}
}

func (c *goplsLookupCache) DefinitionForPosition(pos token.Position) (goplsclient.Location, bool, error) {
	if c == nil || c.client == nil {
		return goplsclient.Location{}, false, nil
	}
	key, ok := lookupKey(pos)
	if !ok {
		return goplsclient.Location{}, false, nil
	}
	if cached, ok := c.definitions[key]; ok {
		return cached.loc, cached.ok, nil
	}
	loc, found, err := c.client.DefinitionForPosition(pos)
	if err != nil {
		return goplsclient.Location{}, false, err
	}
	c.definitions[key] = definitionLookup{loc: loc, ok: found}
	return loc, found, nil
}

func (c *goplsLookupCache) HoverForPosition(pos token.Position) (string, error) {
	if c == nil || c.client == nil {
		return "", nil
	}
	key, ok := lookupKey(pos)
	if !ok {
		return "", nil
	}
	if hover, ok := c.hovers[key]; ok {
		return hover, nil
	}
	hover, err := c.client.HoverForPosition(pos)
	if err != nil {
		return "", err
	}
	c.hovers[key] = hover
	return hover, nil
}

func (c *goplsLookupCache) FileLines(path string) ([]string, error) {
	if c == nil {
		return nil, nil
	}
	if lines, ok := c.fileLines[path]; ok {
		return lines, nil
	}
	targetSrc, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(targetSrc), "\n")
	c.fileLines[path] = lines
	return lines, nil
}

func (c *goplsLookupCache) indirectDefinition(pos token.Position) (indirectLookup, bool) {
	if c == nil {
		return indirectLookup{}, false
	}
	key, ok := lookupKey(pos)
	if !ok {
		return indirectLookup{}, false
	}
	result, ok := c.indirectDefinitions[key]
	return result, ok
}

func (c *goplsLookupCache) storeIndirectDefinition(pos token.Position, result indirectLookup) {
	if c == nil {
		return
	}
	key, ok := lookupKey(pos)
	if !ok {
		return
	}
	c.indirectDefinitions[key] = result
}

func (c *goplsLookupCache) indirectCall(pos token.Position) (bool, bool) {
	if c == nil {
		return false, false
	}
	key, ok := lookupKey(pos)
	if !ok {
		return false, false
	}
	indirect, ok := c.indirectCalls[key]
	return indirect, ok
}

func (c *goplsLookupCache) storeIndirectCall(pos token.Position, indirect bool) {
	if c == nil {
		return
	}
	key, ok := lookupKey(pos)
	if !ok {
		return
	}
	c.indirectCalls[key] = indirect
}

func lookupKey(pos token.Position) (positionKey, bool) {
	if pos.Filename == "" || pos.Line < 1 || pos.Column < 1 {
		return positionKey{}, false
	}
	return positionKey{file: pos.Filename, line: pos.Line, column: pos.Column}, true
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
		return cached, scan.HasLocation(cached)
	}
	files := loadPackageFiles(importPath)
	if len(files) == 0 {
		b.pkgDefByPath[importPath] = definitionLocation{}
		return definitionLocation{}, false
	}
	loc := definitionLocation{File: files[0].File, Line: files[0].Line, Column: files[0].Column}
	b.pkgDefByPath[importPath] = loc
	return loc, scan.HasLocation(loc)
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
	return (&builder{fset: fset, seen: map[string]struct{}{}}).collectTopLevelNamedFields(node)
}
