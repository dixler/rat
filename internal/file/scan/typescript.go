package scan

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	treesitter "github.com/tree-sitter/go-tree-sitter"
	tstypescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"

	"rat/internal/lspclient"
	"rat/internal/tsgobin"
)

var (
	typescriptMu      sync.Mutex
	typescriptClients = map[string]*lspclient.Client{}
)

type typescriptBuilder struct {
	file       string
	source     []byte
	client     *lspclient.Client
	result     *Result
	declByKey  map[string]string
	declByNode map[uintptr]string
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

	client, err := defaultTypeScriptLSP(languageIDForTypeScriptFile(abs))
	if err != nil {
		return nil, err
	}
	if err := client.SyncDocumentContent(abs, string(source)); err != nil {
		return nil, err
	}
	b := &typescriptBuilder{
		file:       abs,
		source:     source,
		client:     client,
		result:     &Result{File: abs, Tokens: collectTypeScriptTokens(abs, source)},
		declByKey:  map[string]string{},
		declByNode: map[uintptr]string{},
		declNames:  map[string]struct{}{},
		seenRef:    map[string]struct{}{},
	}
	root := tree.RootNode()
	b.collectCommentsAndReturns(root)
	b.collectDeclarations(root, nil)
	b.collectReferences(root, nil)
	return b.result, nil
}

func defaultTypeScriptLSP(languageID string) (*lspclient.Client, error) {
	typescriptMu.Lock()
	defer typescriptMu.Unlock()
	if client := typescriptClients[languageID]; client != nil {
		return client, nil
	}
	cmd := strings.TrimSpace(os.Getenv("TSGO_BIN"))
	if cmd == "" {
		if path, err := tsgobin.Path(); err == nil {
			cmd = path
		}
	}
	if cmd == "" {
		if _, err := os.Stat("./tsgo"); err == nil {
			cmd = "./tsgo"
		}
	}
	if cmd == "" {
		var err error
		cmd, err = exec.LookPath("tsgo")
		if err != nil {
			return nil, fmt.Errorf("tsgo not found; set TSGO_BIN, build the embedded tsgo artifact, or include tsgo in PATH")
		}
	}
	client, err := lspclient.Start(lspclient.Config{Name: "tsgo", Command: cmd, Args: []string{"--lsp", "--stdio"}, LanguageID: languageID})
	if err != nil {
		return nil, err
	}
	typescriptClients[languageID] = client
	return client, nil
}

func languageIDForTypeScriptFile(file string) string {
	if strings.EqualFold(filepath.Ext(file), ".tsx") {
		return "typescriptreact"
	}
	return "typescript"
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
	b.declByKey[locationKey(decl.File, decl.Line, decl.Column)] = decl.ID
	b.declNames[decl.Name] = struct{}{}
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
	loc, ok, err := b.client.DefinitionInSyncedDocument(b.file, line, col)
	if err != nil || !ok {
		return
	}
	ref := Reference{Location: Location{File: b.file, Line: line, Column: col}, Text: text, Kind: KindVariable}
	if id := b.declByKey[locationKey(loc.File, loc.Line, loc.Column)]; id != "" {
		ref.DeclarationID = id
		if decl := b.findDeclaration(id, b.result.Declarations); decl != nil {
			ref.Kind = decl.Kind
		}
	} else {
		ref.Declaration = definitionLocation{file: loc.File, line: loc.Line, column: loc.Column}
	}
	parent.References = append(parent.References, ref)
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
	name := parent.ChildByFieldName("name")
	return name != nil && name.Id() == node.Id()
}

func locationKey(file string, line, column int) string {
	return fmt.Sprintf("%s:%d:%d", filepath.Clean(file), line, column)
}
