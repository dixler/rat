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

	"rat/internal/goplsclient"
)

type Result struct {
	File              string
	Declarations      []Declaration
	PackageReferences []PackageReference
	Packages          []Package
	Returns           []Return
	IndirectCalls     []IndirectCall
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
	Kind   string
	File   string
	Line   int
	Column int
}

type ControlFlowBlock struct {
	Kind       string
	File       string
	Line       int
	Column     int
	IfChainID  string
	IfStep     int
	Statements []ControlFlowStatement
	Blocks     []ControlFlowBlock
	CaseCount  int
	HasDefault bool
	HasBreak   bool
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
	File   string
	Line   int
	Column int
	Text   string
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
	}
	conf := &types.Config{Importer: importer.Default(), Error: func(error) {}}
	_, _ = conf.Check(filepath.Dir(file), fset, []*ast.File{parsed}, info)
	b := builder{
		file:         file,
		fset:         fset,
		info:         info,
		declByObj:    map[types.Object]string{},
		kindByObj:    map[types.Object]string{},
		pkgByPath:    map[string]string{},
		pkgDefByPath: map[string]definitionLocation{},
		goplsByPos:   map[string]definitionLocation{},
		escapes:      getEscapeAnalysis(file),
	}
	res := &Result{File: file}
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
			var name string
			var startPos token.Pos
			if id, ok := node.Fun.(*ast.Ident); ok {
				name = id.Name
				startPos = id.Pos()
			} else if sel, ok := node.Fun.(*ast.SelectorExpr); ok {
				name = sel.Sel.Name
				startPos = sel.Sel.Pos()
			} else {
				startPos = node.Fun.Pos()
				endPos := node.Fun.End()
				posStart := fset.Position(startPos)
				posEnd := fset.Position(endPos)
				if posStart.Line == posEnd.Line {
					name = strings.Repeat("x", posEnd.Column-posStart.Column)
				} else {
					name = "call"
				}
			}

			pos := fset.Position(startPos)
			hoverRaw, err := client.Hover(pos.Filename, pos.Line, pos.Column)
			if err != nil || hoverRaw == "" {
				break
			}

			var h struct {
				Contents struct {
					Value string `json:"value"`
				} `json:"contents"`
			}
			json.Unmarshal([]byte(hoverRaw), &h)
			val := h.Contents.Value

			isIndirect := false
			if strings.Contains(val, "```go\nvar ") || strings.Contains(val, "```go\nfield ") || (strings.HasPrefix(val, "```go\ntype ") && strings.Contains(val, "interface")) {
				isIndirect = true
			} else if strings.Contains(val, "```go\nfunc (") {
				loc, ok, err := client.Definition(pos.Filename, pos.Line, pos.Column)
				if err == nil && ok && loc.File != "" && loc.Line > 0 {
					targetSrc, err := os.ReadFile(loc.File)
					if err == nil {
						lines := strings.Split(string(targetSrc), "\n")
						if len(lines) >= loc.Line {
							line := strings.TrimSpace(lines[loc.Line-1])
							if !strings.HasPrefix(line, "func ") {
								isIndirect = true
							}
						}
					}
				}
			}

			if isIndirect && name != "" {
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
	for _, imp := range parsed.Imports {
		pkgRef, pkgDecl := b.buildImport(imp)
		if pkgRef.PackageID == "" {
			continue
		}
		res.PackageReferences = append(res.PackageReferences, pkgRef)
		res.Packages = append(res.Packages, pkgDecl)
	}
	sortDeclarations(res.Declarations)
	sort.Slice(res.PackageReferences, func(i, j int) bool { return res.PackageReferences[i].Text < res.PackageReferences[j].Text })
	return res, nil
}

type builder struct {
	file         string
	fset         *token.FileSet
	info         *types.Info
	declByObj    map[types.Object]string
	kindByObj    map[types.Object]string
	pkgByPath    map[string]string
	pkgDefByPath map[string]definitionLocation
	goplsByPos   map[string]definitionLocation
	escapes      map[string]bool
	seq          int
}

func getEscapeAnalysis(file string) map[string]bool {
	dir := filepath.Dir(file)
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
		b.appendTypeParamDeclarations(&decl, s.TypeParams)
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
	}
	if fn.Type != nil {
		b.appendTypeParamDeclarations(&decl, fn.Type.TypeParams)
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
	fset       *token.FileSet
	file       string
	labels     map[string]*ControlFlowBlock
	breakStack []*ControlFlowBlock
	ifChainSeq int
}

func (b *builder) buildControlFlowBlocks(stmts []ast.Stmt) []ControlFlowBlock {
	cfb := controlFlowBuilder{fset: b.fset, file: b.file, labels: map[string]*ControlFlowBlock{}}
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
		block := b.buildBlock(labeled.Stmt)
		if labeled.Label != nil && block.Kind == BlockKindFor {
			b.labels[labeled.Label.Name] = &block
		}
		return block
	}

	pos := b.fset.Position(stmt.Pos())
	block := ControlFlowBlock{Kind: BlockKindBase, File: b.file, Line: pos.Line, Column: pos.Column}
	switch s := stmt.(type) {
	case *ast.BlockStmt:
		block.Blocks = b.buildBlocks(s.List)
	case *ast.IfStmt:
		block = b.buildIfBlock(s)
	case *ast.ForStmt:
		block = b.buildForBlock(s.Pos(), s.Body)
	case *ast.RangeStmt:
		block = b.buildForBlock(s.Pos(), s.Body)
	case *ast.SwitchStmt:
		block = b.buildSwitchBlock(s.Pos(), s.Body.List)
	case *ast.TypeSwitchStmt:
		block = b.buildSwitchBlock(s.Pos(), s.Body.List)
	case *ast.SelectStmt:
		block = b.buildSelectBlock(s)
	default:
		block.Statements = b.collectControlFlowStatements(stmt)
	}
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
			elseBlock.Blocks = b.buildBlocks(e.List)
			block.Blocks = append(block.Blocks, elseBlock)
		default:
			block.Blocks = append(block.Blocks, b.buildBlock(e))
		}
	}
	return block
}

func (b *controlFlowBuilder) buildForBlock(pos token.Pos, body *ast.BlockStmt) ControlFlowBlock {
	p := b.fset.Position(pos)
	block := ControlFlowBlock{Kind: BlockKindFor, File: b.file, Line: p.Line, Column: p.Column}
	b.breakStack = append(b.breakStack, &block)
	if body != nil {
		block.Blocks = b.buildBlocks(body.List)
	}
	b.breakStack = b.breakStack[:len(b.breakStack)-1]
	return block
}

func (b *controlFlowBuilder) buildSwitchBlock(pos token.Pos, clauses []ast.Stmt) ControlFlowBlock {
	p := b.fset.Position(pos)
	block := ControlFlowBlock{Kind: BlockKindSwitch, File: b.file, Line: p.Line, Column: p.Column}
	b.breakStack = append(b.breakStack, &block)
	for _, stmt := range clauses {
		clause, ok := stmt.(*ast.CaseClause)
		if !ok {
			continue
		}
		b.appendCaseBlock(&block, clause.Case, clause.List == nil, clause.Body)
	}
	b.breakStack = b.breakStack[:len(b.breakStack)-1]
	return block
}

func (b *controlFlowBuilder) buildSelectBlock(stmt *ast.SelectStmt) ControlFlowBlock {
	p := b.fset.Position(stmt.Select)
	block := ControlFlowBlock{Kind: BlockKindSelect, File: b.file, Line: p.Line, Column: p.Column}
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
	return block
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
	caseBlock.Blocks = b.buildBlocks(body)
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
			if n != node {
				if _, ok := n.(*ast.FuncLit); ok {
					return false
				}
			}
			switch s := n.(type) {
			case *ast.ReturnStmt:
				p := b.fset.Position(s.Return)
				out = append(out, ControlFlowStatement{Kind: "return", File: b.file, Line: p.Line, Column: p.Column})
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
			target.HasBreak = true
		}
		return
	}
	if len(b.breakStack) == 0 {
		return
	}
	target := b.breakStack[len(b.breakStack)-1]
	if target != nil && target.Kind == BlockKindFor {
		target.HasBreak = true
	}
}

func (b *builder) appendTypeParamDeclarations(parent *Declaration, fields *ast.FieldList) {
	b.appendFieldDeclarations(parent, fields, KindParameter)
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
		id, ok := n.(*ast.Ident)
		if !ok || id.Name == "_" {
			return true
		}
		if b.info.Defs[id] != nil {
			return true
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
		if obj := b.info.Uses[id]; obj != nil {
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
		return true
	})
	sortReferences(decl.References)
}

func (b *builder) definitionFor(pos token.Pos) (definitionLocation, bool) {
	position := b.fset.Position(pos)
	key := fmt.Sprintf("%s:%d:%d", position.Filename, position.Line, position.Column)
	if cached, ok := b.goplsByPos[key]; ok {
		return cached, cached.ok
	}
	client, err := goplsclient.Default()
	if err != nil {
		b.goplsByPos[key] = definitionLocation{}
		return definitionLocation{}, false
	}
	target, ok, err := client.Definition(position.Filename, position.Line, position.Column)
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
	name := filepath.Base(path)
	if imp.Name != nil {
		name = imp.Name.Name
	}
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

func loadPackageFiles(importPath string) []PackageFile {
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
	return files
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
				collectNamedFields(fset, t.Fields, &out)
				for _, field := range t.Fields.List {
					collectNamedFieldsInExpr(fset, field.Type, &out)
				}
			case *ast.InterfaceType:
				collectNamedFields(fset, t.Methods, &out)
			}
		}
	}

	return out
}

func collectNamedFields(fset *token.FileSet, fields *ast.FieldList, out *[]NamedField) {
	if fields == nil {
		return
	}
	for _, field := range fields.List {
		for _, name := range field.Names {
			pos := fset.Position(name.Pos())
			if pos.Line < 1 || pos.Column < 1 {
				continue
			}
			*out = append(*out, NamedField{File: pos.Filename, Line: pos.Line, Column: pos.Column, Text: name.Name})
		}
	}
}

func collectNamedFieldsInExpr(fset *token.FileSet, expr ast.Expr, out *[]NamedField) {
	switch n := expr.(type) {
	case *ast.StructType:
		collectNamedFields(fset, n.Fields, out)
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
