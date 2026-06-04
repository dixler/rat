package scan

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	treesitter "github.com/tree-sitter/go-tree-sitter"
	tstypescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

type typescriptBuilder struct {
	file       string
	source     []byte
	result     *Result
	declByNode map[uintptr]string
	declParent map[string]string
	declNames  map[string]struct{}
	seenRef    map[string]struct{}
	next       int
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
		file:       abs,
		source:     source,
		result:     &Result{File: abs, Tokens: collectTypeScriptTokens(abs, source)},
		declByNode: map[uintptr]string{},
		declParent: map[string]string{},
		declNames:  map[string]struct{}{},
		seenRef:    map[string]struct{}{},
	}
	root := tree.RootNode()
	b.collectCommentsAndReturns(root)
	b.collectDeclarations(root, nil)
	b.collectReferences(root, nil)
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
	case "return_statement":
		pos := node.StartPosition()
		b.result.Returns = append(b.result.Returns, Return{Location: Location{File: b.file, Line: int(pos.Row) + 1, Column: int(pos.Column) + 1}})
	}
	for i := uint(0); i < node.ChildCount(); i++ {
		b.collectCommentsAndReturns(node.Child(i))
	}
}

func (b *typescriptBuilder) collectDeclarations(node *treesitter.Node, parent *Declaration) {
	if node == nil {
		return
	}
	if nameNode, kind := b.declarationName(node); nameNode != nil {
		decl := b.newDeclaration(nameNode, kind, parent)
		b.declByNode[node.Id()] = decl.ID
		if parent == nil {
			b.result.Declarations = append(b.result.Declarations, decl)
			parent = &b.result.Declarations[len(b.result.Declarations)-1]
		} else {
			parent.Declarations = append(parent.Declarations, decl)
			parent = &parent.Declarations[len(parent.Declarations)-1]
		}
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child != nil && child.Kind() == "formal_parameters" {
			b.collectParameters(child, parent)
			continue
		}
		b.collectDeclarations(child, parent)
	}
}

func (b *typescriptBuilder) collectParameters(node *treesitter.Node, parent *Declaration) {
	if parent == nil || node == nil {
		return
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		name := firstIdentifier(child)
		if name == nil {
			continue
		}
		param := b.newDeclaration(name, KindParameter, parent)
		parent.Declarations = append(parent.Declarations, param)
	}
}

func (b *typescriptBuilder) declarationName(node *treesitter.Node) (*treesitter.Node, string) {
	switch node.Kind() {
	case "function_declaration", "method_definition":
		return node.ChildByFieldName("name"), KindFunction
	case "class_declaration", "interface_declaration", "type_alias_declaration", "enum_declaration":
		return node.ChildByFieldName("name"), KindType
	case "variable_declarator":
		name := node.ChildByFieldName("name")
		if name != nil && name.Kind() == "identifier" {
			return name, KindVariable
		}
	}
	return nil, ""
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
	if parent != nil {
		b.declParent[decl.ID] = parent.ID
	}
	return decl
}

func (b *typescriptBuilder) collectReferences(node *treesitter.Node, parent *Declaration) {
	if node == nil {
		return
	}
	if declID := b.declByNode[node.Id()]; declID != "" {
		parent = b.findDeclaration(declID, b.result.Declarations)
	}
	if isIdentifierNode(node) && !isDeclarationIdentifier(node) && parent != nil {
		b.addReference(node, parent)
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		b.collectReferences(node.NamedChild(i), parent)
	}
}

func (b *typescriptBuilder) addReference(node *treesitter.Node, parent *Declaration) {
	text := b.nodeText(node)
	if text == "" {
		return
	}
	if _, ok := b.declNames[text]; !ok {
		return
	}
	pos := node.StartPosition()
	line, col := int(pos.Row)+1, int(pos.Column)+1
	key := fmt.Sprintf("%d:%d:%s", line, col, text)
	if _, ok := b.seenRef[key]; ok {
		return
	}
	b.seenRef[key] = struct{}{}
	decl := b.findVisibleDeclaration(text, parent, line, col)
	if decl == nil {
		return
	}
	ref := Reference{Location: Location{File: b.file, Line: line, Column: col}, Text: text, Kind: decl.Kind, DeclarationID: decl.ID}
	parent.References = append(parent.References, ref)
}

func (b *typescriptBuilder) findVisibleDeclaration(name string, parent *Declaration, line, col int) *Declaration {
	var best *Declaration
	for scope := parent; scope != nil; scope = b.parentDeclaration(scope.ID) {
		if scope.Name == name && beforeOrAt(scope.Location, line, col) {
			best = newerDeclaration(best, scope)
		}
		best = b.newerNamedChild(best, scope.Declarations, name, line, col)
	}
	best = b.newerNamedChild(best, b.result.Declarations, name, line, col)
	return best
}

func (b *typescriptBuilder) parentDeclaration(id string) *Declaration {
	parentID := b.declParent[id]
	if parentID == "" {
		return nil
	}
	return b.findDeclaration(parentID, b.result.Declarations)
}

func (b *typescriptBuilder) newerNamedChild(best *Declaration, decls []Declaration, name string, line, col int) *Declaration {
	for i := range decls {
		decl := &decls[i]
		if decl.Name == name && beforeOrAt(decl.Location, line, col) {
			best = newerDeclaration(best, decl)
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

func isIdentifierNode(node *treesitter.Node) bool {
	return node.Kind() == "identifier" || node.Kind() == "property_identifier" || node.Kind() == "shorthand_property_identifier"
}

func isDeclarationIdentifier(node *treesitter.Node) bool {
	parent := node.Parent()
	if parent == nil {
		return false
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
