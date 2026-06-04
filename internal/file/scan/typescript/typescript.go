package typescript

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"rat/internal/file/scan"
	"rat/internal/lspclient"

	treesitter "github.com/tree-sitter/go-tree-sitter"
	tstypescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

type Result = scan.Result
type Location = scan.Location
type Comment = scan.Comment
type Token = scan.Token
type Return = scan.Return
type Declaration = scan.Declaration
type ControlFlowStatement = scan.ControlFlowStatement
type ControlFlowBlock = scan.ControlFlowBlock
type Reference = scan.Reference
type NamedFieldTypeDeclaration = scan.NamedFieldTypeDeclaration
type definitionLocation = scan.DefinitionLocation

const (
	KindType      = scan.KindType
	KindVariable  = scan.KindVariable
	KindParameter = scan.KindParameter
	KindFunction  = scan.KindFunction

	BlockKindBase    = scan.BlockKindBase
	BlockKindIf      = scan.BlockKindIf
	BlockKindElseIf  = scan.BlockKindElseIf
	BlockKindElse    = scan.BlockKindElse
	BlockKindFor     = scan.BlockKindFor
	BlockKindWhile   = scan.BlockKindWhile
	BlockKindDo      = scan.BlockKindDo
	BlockKindSwitch  = scan.BlockKindSwitch
	BlockKindCase    = scan.BlockKindCase
	BlockKindTry     = scan.BlockKindTry
	BlockKindCatch   = scan.BlockKindCatch
	BlockKindFinally = scan.BlockKindFinally
)

type Scanner struct{}

func init() {
	scan.Register(Scanner{})
}

func (Scanner) Extensions() []string { return []string{".ts", ".tsx", ".js", ".jsx"} }

func (Scanner) Build(file string) (*scan.Result, error) { return buildTypeScript(file) }

type typescriptBuilder struct {
	file        string
	source      []byte
	result      *Result
	declByNode  map[uintptr]string
	scopeByNode map[uintptr]*typescriptScope
	declNames   map[string]struct{}
	seenRef     map[string]struct{}
	rootScope   *typescriptScope
	client      *lspclient.Client
	defsByPos   map[string]definitionLocation
	next        int
}

type typescriptScope struct {
	parent *typescriptScope
	decls  []string
}

func buildTypeScript(file string) (*Result, error) {
	abs, err := filepath.Abs(file)
	if err != nil {
		return nil, err
	}
	source, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	parser := treesitter.NewParser()
	defer parser.Close()
	language := treesitter.NewLanguage(tstypescript.LanguageTypescript())
	if strings.EqualFold(filepath.Ext(abs), ".tsx") {
		language = treesitter.NewLanguage(tstypescript.LanguageTSX())
	}
	if err := parser.SetLanguage(language); err != nil {
		return nil, err
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, fmt.Errorf("failed to parse TypeScript")
	}
	defer tree.Close()

	b := &typescriptBuilder{
		file:        abs,
		source:      source,
		result:      &Result{File: abs, Tokens: collectTypeScriptTokens(abs, source, tree.RootNode())},
		declByNode:  map[uintptr]string{},
		scopeByNode: map[uintptr]*typescriptScope{},
		declNames:   map[string]struct{}{},
		seenRef:     map[string]struct{}{},
		client:      defaultLSPClient(abs),
		defsByPos:   map[string]definitionLocation{},
	}
	b.rootScope = &typescriptScope{}
	root := tree.RootNode()
	b.collectCommentsAndReturns(root)
	b.collectDeclarations(root, nil, b.rootScope)
	b.collectReferences(root, nil, b.rootScope)
	b.collectControlFlow(root)
	return b.result, nil
}

func (b *typescriptBuilder) collectCommentsAndReturns(node *treesitter.Node) {
	if node == nil {
		return
	}
	switch node.Kind() {
	case "comment":
		start := node.StartPosition()
		end := node.EndPosition()
		b.result.Comments = append(b.result.Comments, Comment{StartLine: int(start.Row) + 1, StartColumn: int(start.Column) + 1, EndLine: int(end.Row) + 1, EndColumn: int(end.Column) + 1})
	case "return_statement", "throw_statement":
		pos := node.StartPosition()
		b.result.Returns = append(b.result.Returns, Return{Location: Location{File: b.file, Line: int(pos.Row) + 1, Column: int(pos.Column) + 1}})
	}
	for i := uint(0); i < node.ChildCount(); i++ {
		b.collectCommentsAndReturns(node.Child(i))
	}
}

func (b *typescriptBuilder) collectControlFlow(node *treesitter.Node) {
	if node == nil {
		return
	}
	if declID := b.declByNode[node.Id()]; declID != "" && (b.isFunctionLikeDeclaration(node) || b.hasFunctionLikeExpression(node)) {
		if decl := b.findDeclaration(declID, b.result.Declarations); decl != nil {
			if body := b.functionBody(node); body != nil {
				decl.ControlFlow = b.buildTypeScriptBlocks(body)
			}
		}
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		b.collectControlFlow(node.NamedChild(i))
	}
}

func (b *typescriptBuilder) functionBody(node *treesitter.Node) *treesitter.Node {
	if body := node.ChildByFieldName("body"); body != nil {
		return body
	}
	if value := node.ChildByFieldName("value"); value != nil {
		return value.ChildByFieldName("body")
	}
	return nil
}

func (b *typescriptBuilder) buildTypeScriptBlocks(container *treesitter.Node) []ControlFlowBlock {
	if container == nil {
		return nil
	}
	var out []ControlFlowBlock
	for i := uint(0); i < container.NamedChildCount(); i++ {
		if block, ok := b.buildTypeScriptBlock(container.NamedChild(i)); ok {
			out = append(out, block)
		}
	}
	return out
}

func (b *typescriptBuilder) buildTypeScriptBlock(node *treesitter.Node) (ControlFlowBlock, bool) {
	if node == nil {
		return ControlFlowBlock{}, false
	}
	switch node.Kind() {
	case "if_statement":
		return b.buildTypeScriptIfBlock(node, BlockKindIf, b.ifChainID(node), 0), true
	case "for_statement", "for_in_statement":
		return b.buildTypeScriptLoopBlock(node, BlockKindFor), true
	case "while_statement":
		return b.buildTypeScriptLoopBlock(node, BlockKindWhile), true
	case "do_statement":
		return b.buildTypeScriptLoopBlock(node, BlockKindDo), true
	case "switch_statement":
		return b.buildTypeScriptSwitchBlock(node), true
	case "try_statement":
		return b.buildTypeScriptTryBlock(node), true
	default:
		statements := b.collectTypeScriptControlFlowStatements(node)
		blocks := b.buildTypeScriptBlocks(node)
		if len(statements) == 0 && len(blocks) == 0 {
			return ControlFlowBlock{}, false
		}
		block := ControlFlowBlock{Kind: BlockKindBase, Location: b.nodeLocation(node), Statements: statements, Blocks: blocks}
		block.HasTerminalControlFlowStatement = controlFlowBlockHasTerminalStatement(block)
		return block, true
	}
}

func (b *typescriptBuilder) buildTypeScriptTryBlock(node *treesitter.Node) ControlFlowBlock {
	body := firstChildKind(node, "statement_block")
	block := ControlFlowBlock{Kind: BlockKindTry, Location: b.nodeLocation(node), IfChainID: b.tryChainID(node), IfStep: 0}
	b.setTypeScriptBlockBraces(&block, body)
	block.Statements = b.collectTypeScriptControlFlowStatements(body)
	block.Blocks = b.buildTypeScriptBlocks(body)
	step := 1
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}
		switch child.Kind() {
		case "catch_clause":
			block.Blocks = append(block.Blocks, b.buildTypeScriptTryBranch(child, BlockKindCatch, block.IfChainID, step))
			step++
		case "finally_clause":
			block.Blocks = append(block.Blocks, b.buildTypeScriptTryBranch(child, BlockKindFinally, block.IfChainID, step))
			step++
		}
	}
	block.HasTerminalControlFlowStatement = controlFlowBlockHasTerminalStatement(block)
	return block
}

func (b *typescriptBuilder) buildTypeScriptTryBranch(node *treesitter.Node, kind, chainID string, step int) ControlFlowBlock {
	body := firstChildKind(node, "statement_block")
	block := ControlFlowBlock{Kind: kind, Location: b.nodeLocation(node), IfChainID: chainID, IfStep: step}
	b.setTypeScriptBlockBraces(&block, body)
	block.Statements = b.collectTypeScriptControlFlowStatements(body)
	block.Blocks = b.buildTypeScriptBlocks(body)
	block.HasTerminalControlFlowStatement = controlFlowBlockHasTerminalStatement(block)
	return block
}

func (b *typescriptBuilder) buildTypeScriptIfBlock(node *treesitter.Node, kind, chainID string, step int) ControlFlowBlock {
	body := node.ChildByFieldName("consequence")
	block := ControlFlowBlock{Kind: kind, Location: b.nodeLocation(node), IfChainID: chainID, IfStep: step}
	b.setTypeScriptBlockBraces(&block, body)
	block.Statements = b.collectTypeScriptControlFlowStatements(body)
	block.Blocks = b.buildTypeScriptBlocks(body)
	if alt := node.ChildByFieldName("alternative"); alt != nil {
		if alt.Kind() == "if_statement" {
			block.Blocks = append(block.Blocks, b.buildTypeScriptIfBlock(alt, BlockKindElseIf, chainID, step+1))
		} else {
			elseBlock := ControlFlowBlock{Kind: BlockKindElse, Location: b.nodeLocation(alt), IfChainID: chainID, IfStep: step + 1}
			b.setTypeScriptBlockBraces(&elseBlock, alt)
			elseBlock.Statements = b.collectTypeScriptControlFlowStatements(alt)
			elseBlock.Blocks = b.buildTypeScriptBlocks(alt)
			elseBlock.HasTerminalControlFlowStatement = controlFlowBlockHasTerminalStatement(elseBlock)
			block.Blocks = append(block.Blocks, elseBlock)
		}
	}
	block.HasTerminalControlFlowStatement = controlFlowBlockHasTerminalStatement(block)
	return block
}

func (b *typescriptBuilder) buildTypeScriptLoopBlock(node *treesitter.Node, kind string) ControlFlowBlock {
	body := node.ChildByFieldName("body")
	block := ControlFlowBlock{Kind: kind, Location: b.nodeLocation(node)}
	b.setTypeScriptBlockBraces(&block, body)
	block.Statements = b.collectTypeScriptControlFlowStatements(body)
	block.Blocks = b.buildTypeScriptBlocks(body)
	block.MayBreak = controlFlowBlockHasStatementKind(block, "break")
	block.MayReturn = controlFlowBlockHasStatementKind(block, "return") || controlFlowBlockHasStatementKind(block, "throw")
	block.HasTerminalControlFlowStatement = controlFlowBlockHasTerminalStatement(block)
	return block
}

func (b *typescriptBuilder) buildTypeScriptSwitchBlock(node *treesitter.Node) ControlFlowBlock {
	body := node.ChildByFieldName("body")
	block := ControlFlowBlock{Kind: BlockKindSwitch, Location: b.nodeLocation(node)}
	b.setTypeScriptBlockBraces(&block, body)
	if body != nil {
		for i := uint(0); i < body.NamedChildCount(); i++ {
			child := body.NamedChild(i)
			if child == nil || child.Kind() != "switch_case" && child.Kind() != "switch_default" {
				continue
			}
			caseBlock := ControlFlowBlock{Kind: BlockKindCase, Location: b.nodeLocation(child), HasDefault: child.Kind() == "switch_default"}
			caseBlock.Statements = b.collectTypeScriptControlFlowStatements(child)
			caseBlock.Blocks = b.buildTypeScriptBlocks(child)
			caseBlock.HasTerminalControlFlowStatement = controlFlowBlockHasTerminalStatement(caseBlock)
			block.Blocks = append(block.Blocks, caseBlock)
			block.CaseCount++
			block.HasDefault = block.HasDefault || caseBlock.HasDefault
		}
	}
	block.HasTerminalControlFlowStatement = controlFlowBlockHasTerminalStatement(block)
	return block
}

func (b *typescriptBuilder) collectTypeScriptControlFlowStatements(node *treesitter.Node) []ControlFlowStatement {
	if node == nil {
		return nil
	}
	var out []ControlFlowStatement
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}
		kind := ""
		returnsError := false
		switch child.Kind() {
		case "return_statement":
			kind = "return"
		case "throw_statement":
			kind = "throw"
			returnsError = true
		case "break_statement":
			kind = "break"
		case "continue_statement":
			kind = "continue"
		}
		if kind != "" {
			out = append(out, ControlFlowStatement{Location: b.nodeLocation(child), Kind: kind, ReturnsError: returnsError})
		}
	}
	return out
}

func (b *typescriptBuilder) setTypeScriptBlockBraces(block *ControlFlowBlock, node *treesitter.Node) {
	if block == nil || node == nil || node.Kind() != "statement_block" && node.Kind() != "switch_body" {
		return
	}
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		switch child.Kind() {
		case "{":
			loc := b.nodeLocation(child)
			block.OpenBraceLine, block.OpenBraceColumn = loc.Line, loc.Column
		case "}":
			loc := b.nodeLocation(child)
			block.CloseBraceLine, block.CloseBraceColumn = loc.Line, loc.Column
		}
	}
}

func (b *typescriptBuilder) nodeLocation(node *treesitter.Node) Location {
	pos := node.StartPosition()
	return Location{File: b.file, Line: int(pos.Row) + 1, Column: int(pos.Column) + 1}
}

func (b *typescriptBuilder) ifChainID(node *treesitter.Node) string {
	loc := b.nodeLocation(node)
	return fmt.Sprintf("ts-if:%d:%d", loc.Line, loc.Column)
}

func (b *typescriptBuilder) tryChainID(node *treesitter.Node) string {
	loc := b.nodeLocation(node)
	return fmt.Sprintf("ts-try:%d:%d", loc.Line, loc.Column)
}

func (b *typescriptBuilder) collectDeclarations(node *treesitter.Node, parent *Declaration, scope *typescriptScope) {
	if node == nil {
		return
	}
	if b.startsLexicalScope(node) {
		scope = &typescriptScope{parent: scope}
		b.scopeByNode[node.Id()] = scope
	}
	if declarations := b.declarationNames(node); len(declarations) > 0 {
		if b.isFunctionLikeDeclaration(node) {
			name := declarations[0]
			decl := b.appendDeclaration(name.node, name.kind, parent, scope)
			b.declByNode[node.Id()] = decl.ID
			parent = b.findDeclaration(decl.ID, b.result.Declarations)
			scope = &typescriptScope{parent: scope}
			b.scopeByNode[node.Id()] = scope
		} else if b.isTypeLikeDeclaration(node) {
			name := declarations[0]
			decl := b.appendDeclaration(name.node, name.kind, parent, scope)
			b.declByNode[node.Id()] = decl.ID
			parent = b.findDeclaration(decl.ID, b.result.Declarations)
			scope = &typescriptScope{parent: scope}
			b.scopeByNode[node.Id()] = scope
		} else {
			for _, name := range declarations {
				decl := b.appendDeclaration(name.node, name.kind, parent, scope)
				if len(declarations) == 1 {
					b.declByNode[node.Id()] = decl.ID
					if b.hasFunctionLikeExpression(node) {
						parent = b.findDeclaration(decl.ID, b.result.Declarations)
						scope = &typescriptScope{parent: scope}
						b.scopeByNode[node.Id()] = scope
					}
				}
			}
		}
	}
	if node.Kind() == "catch_clause" {
		if param := node.ChildByFieldName("parameter"); param != nil {
			for _, nameNode := range patternIdentifiers(param) {
				b.appendDeclaration(nameNode, KindParameter, parent, scope)
			}
		}
	}
	if child := node.ChildByFieldName("parameters"); child != nil && isParameterList(child) {
		b.collectParameters(child, parent, scope)
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}
		if child.Kind() == "formal_parameters" || nodeFieldHasID(node, "parameters", child.Id()) || nodeFieldHasID(node, "parameter", child.Id()) {
			continue
		}
		b.collectDeclarations(child, parent, scope)
	}
}

func (b *typescriptBuilder) collectParameters(node *treesitter.Node, parent *Declaration, scope *typescriptScope) {
	if node == nil {
		return
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		for _, name := range patternIdentifiers(child) {
			b.appendDeclaration(name, KindParameter, parent, scope)
		}
	}
}

type typescriptDeclarationName struct {
	node *treesitter.Node
	kind string
}

func (b *typescriptBuilder) declarationNames(node *treesitter.Node) []typescriptDeclarationName {
	switch node.Kind() {
	case "function_declaration", "method_definition", "method_signature", "abstract_method_signature", "generator_function_declaration":
		return oneDeclaration(node.ChildByFieldName("name"), KindFunction)
	case "class_declaration", "interface_declaration", "type_alias_declaration", "enum_declaration":
		return oneDeclaration(node.ChildByFieldName("name"), KindType)
	case "variable_declarator", "required_parameter", "optional_parameter", "public_field_definition", "property_signature":
		name := node.ChildByFieldName("name")
		if name == nil {
			name = node.ChildByFieldName("pattern")
		}
		return patternDeclarations(name, KindVariable)
	case "for_in_statement":
		for i := uint(0); i < node.NamedChildCount(); i++ {
			child := node.NamedChild(i)
			if child != nil && isPatternNode(child) {
				return patternDeclarations(child, KindVariable)
			}
		}
	case "import_specifier", "namespace_import":
		name := node.ChildByFieldName("alias")
		if name == nil {
			name = node.ChildByFieldName("name")
		}
		if name == nil {
			name = firstIdentifier(node)
		}
		return oneDeclaration(name, KindVariable)
	case "import_clause":
		name := node.ChildByFieldName("name")
		if name == nil {
			name = firstIdentifier(node)
		}
		return oneDeclaration(name, KindVariable)
	}
	return nil
}

func (b *typescriptBuilder) appendDeclaration(nameNode *treesitter.Node, kind string, parent *Declaration, scope *typescriptScope) Declaration {
	decl := b.newDeclaration(nameNode, kind, parent)
	if parent == nil {
		b.result.Declarations = append(b.result.Declarations, decl)
	} else {
		parent.Declarations = append(parent.Declarations, decl)
	}
	if scope != nil {
		scope.decls = append(scope.decls, decl.ID)
	}
	return decl
}

func (b *typescriptBuilder) newDeclaration(nameNode *treesitter.Node, kind string, parent *Declaration) Declaration {
	b.next++
	pos := nameNode.StartPosition()
	decl := Declaration{
		ID:       fmt.Sprintf("ts%d", b.next),
		Name:     b.nodeText(nameNode),
		Kind:     kind,
		Location: Location{File: b.file, Line: int(pos.Row) + 1, Column: int(pos.Column) + 1},
	}
	b.declNames[decl.Name] = struct{}{}
	return decl
}

func (b *typescriptBuilder) collectReferences(node *treesitter.Node, parent *Declaration, scope *typescriptScope) {
	if node == nil {
		return
	}
	if nodeScope := b.scopeByNode[node.Id()]; nodeScope != nil {
		scope = nodeScope
	}
	if declID := b.declByNode[node.Id()]; declID != "" {
		parent = b.findDeclaration(declID, b.result.Declarations)
	}
	if isIdentifierNode(node) && !isDeclarationIdentifier(node) && !isPropertyReferenceIdentifier(node) {
		b.addReference(node, parent, scope)
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		b.collectReferences(node.NamedChild(i), parent, scope)
	}
}

func (b *typescriptBuilder) addReference(node *treesitter.Node, parent *Declaration, scope *typescriptScope) {
	text := b.nodeText(node)
	if text == "" {
		return
	}
	_, knownDeclarationName := b.declNames[text]
	_, knownBuiltinName := builtinNames[text]
	if !knownDeclarationName && !knownBuiltinName {
		return
	}
	pos := node.StartPosition()
	line, col := int(pos.Row)+1, int(pos.Column)+1
	key := fmt.Sprintf("%d:%d:%s", line, col, text)
	if _, ok := b.seenRef[key]; ok {
		return
	}
	b.seenRef[key] = struct{}{}
	decl := b.findVisibleDeclaration(text, scope, line, col)
	if decl == nil && knownBuiltinName {
		b.addBuiltinReference(parent, text, line, col)
		return
	}
	if decl == nil {
		return
	}
	if parent == nil {
		parent = decl
	}
	ref := Reference{Location: Location{File: b.file, Line: line, Column: col}, Text: text, Kind: decl.Kind, DeclarationID: decl.ID}
	parent.References = append(parent.References, ref)
}

func (b *typescriptBuilder) addBuiltinReference(parent *Declaration, text string, line, col int) {
	if parent == nil {
		return
	}
	decl := scan.BuiltinDefinitionLocation("typescript")
	if loc, ok := b.definitionFor(line, col); ok {
		decl = loc
	}
	ref := Reference{
		Location:    Location{File: b.file, Line: line, Column: col},
		Text:        text,
		Kind:        KindVariable,
		Declaration: decl,
	}
	parent.References = append(parent.References, ref)
}

func (b *typescriptBuilder) findVisibleDeclaration(name string, scope *typescriptScope, line, col int) *Declaration {
	var best *Declaration
	for curr := scope; curr != nil; curr = curr.parent {
		for _, id := range curr.decls {
			decl := b.findDeclaration(id, b.result.Declarations)
			if decl != nil && decl.Name == name && beforeOrAt(decl.Location, line, col) {
				best = newerDeclaration(best, decl)
			}
		}
		if best != nil {
			return best
		}
	}
	return best
}

func newerDeclaration(best, candidate *Declaration) *Declaration {
	if best == nil || beforeOrAt(best.Location, candidate.Line, candidate.Column) {
		return candidate
	}
	return best
}

func beforeOrAt(loc Location, line, col int) bool {
	return loc.Line < line || loc.Line == line && loc.Column <= col
}

func (b *typescriptBuilder) findDeclaration(id string, decls []Declaration) *Declaration {
	for i := range decls {
		if decls[i].ID == id {
			return &decls[i]
		}
		if found := b.findDeclaration(id, decls[i].Declarations); found != nil {
			return found
		}
	}
	return nil
}

func (b *typescriptBuilder) nodeText(node *treesitter.Node) string {
	if node == nil {
		return ""
	}
	start, end := node.StartByte(), node.EndByte()
	if end > uint(len(b.source)) || start > end {
		return ""
	}
	return string(b.source[start:end])
}

func firstIdentifier(node *treesitter.Node) *treesitter.Node {
	if node == nil {
		return nil
	}
	if node.Kind() == "identifier" {
		return node
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		if found := firstIdentifier(node.NamedChild(i)); found != nil {
			return found
		}
	}
	return nil
}

func firstChildKind(node *treesitter.Node, kind string) *treesitter.Node {
	if node == nil {
		return nil
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child != nil && child.Kind() == kind {
			return child
		}
	}
	return nil
}

func isIdentifierNode(node *treesitter.Node) bool {
	return node.Kind() == "identifier" || node.Kind() == "type_identifier" || node.Kind() == "property_identifier" || node.Kind() == "shorthand_property_identifier"
}

func isDeclarationIdentifier(node *treesitter.Node) bool {
	parent := node.Parent()
	if parent == nil {
		return false
	}
	if isPatternNode(parent) || parent.Kind() == "pair_pattern" {
		return true
	}
	return fieldContainsNode(parent, "name", node) || fieldContainsNode(parent, "pattern", node)
}

func fieldContainsNode(parent *treesitter.Node, field string, node *treesitter.Node) bool {
	fieldNode := parent.ChildByFieldName(field)
	if fieldNode == nil {
		return false
	}
	return containsNode(fieldNode, node)
}

func containsNode(root *treesitter.Node, target *treesitter.Node) bool {
	if root == nil || target == nil {
		return false
	}
	if root.Id() == target.Id() {
		return true
	}
	for i := uint(0); i < root.NamedChildCount(); i++ {
		if containsNode(root.NamedChild(i), target) {
			return true
		}
	}
	return false
}

func (b *typescriptBuilder) startsLexicalScope(node *treesitter.Node) bool {
	switch node.Kind() {
	case "statement_block", "class_body", "enum_body", "switch_body", "for_statement", "for_in_statement", "catch_clause":
		return true
	default:
		return false
	}
}

func (b *typescriptBuilder) isFunctionLikeDeclaration(node *treesitter.Node) bool {
	switch node.Kind() {
	case "function_declaration", "method_definition", "method_signature", "abstract_method_signature", "generator_function_declaration":
		return true
	default:
		return false
	}
}

func (b *typescriptBuilder) isTypeLikeDeclaration(node *treesitter.Node) bool {
	switch node.Kind() {
	case "class_declaration", "interface_declaration", "type_alias_declaration", "enum_declaration":
		return true
	default:
		return false
	}
}

func (b *typescriptBuilder) hasFunctionLikeExpression(node *treesitter.Node) bool {
	if node.Kind() != "variable_declarator" {
		return false
	}
	value := node.ChildByFieldName("value")
	if value == nil {
		return false
	}
	switch value.Kind() {
	case "arrow_function", "function", "function_expression", "generator_function":
		return true
	default:
		return false
	}
}

func isParameterList(node *treesitter.Node) bool {
	if node == nil {
		return false
	}
	switch node.Kind() {
	case "formal_parameters", "call_signature", "construct_signature":
		return true
	default:
		return strings.Contains(node.Kind(), "parameters")
	}
}

func oneDeclaration(node *treesitter.Node, kind string) []typescriptDeclarationName {
	if node == nil || !isIdentifierNode(node) {
		return nil
	}
	return []typescriptDeclarationName{{node: node, kind: kind}}
}

func patternDeclarations(node *treesitter.Node, kind string) []typescriptDeclarationName {
	idents := patternIdentifiers(node)
	out := make([]typescriptDeclarationName, 0, len(idents))
	for _, ident := range idents {
		out = append(out, typescriptDeclarationName{node: ident, kind: kind})
	}
	return out
}

func patternIdentifiers(node *treesitter.Node) []*treesitter.Node {
	if node == nil {
		return nil
	}
	if isIdentifierNode(node) || node.Kind() == "shorthand_property_identifier_pattern" {
		return []*treesitter.Node{node}
	}
	if node.Kind() == "pair" || node.Kind() == "pair_pattern" {
		if value := node.ChildByFieldName("value"); value != nil {
			return patternIdentifiers(value)
		}
		return patternIdentifiers(node.ChildByFieldName("key"))
	}
	var out []*treesitter.Node
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child == nil || child.Kind() == "type_annotation" {
			continue
		}
		out = append(out, patternIdentifiers(child)...)
	}
	return out
}

func isPatternNode(node *treesitter.Node) bool {
	if node == nil {
		return false
	}
	switch node.Kind() {
	case "array_pattern", "object_pattern", "assignment_pattern", "rest_pattern":
		return true
	default:
		return false
	}
}

func nodeFieldHasID(node *treesitter.Node, field string, id uintptr) bool {
	fieldNode := node.ChildByFieldName(field)
	return fieldNode != nil && fieldNode.Id() == id
}

func isPropertyReferenceIdentifier(node *treesitter.Node) bool {
	parent := node.Parent()
	if parent == nil {
		return false
	}
	switch parent.Kind() {
	case "member_expression", "subscript_expression":
		return nodeFieldHasID(parent, "property", node.Id())
	case "pair", "property_assignment":
		return nodeFieldHasID(parent, "key", node.Id())
	}
	return false
}
