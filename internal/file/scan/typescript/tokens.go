package typescript

import (
	"rat/internal/file/scan"

	treesitter "github.com/tree-sitter/go-tree-sitter"
)

var keywordTokenKinds = map[string]string{
	"class":     scan.TokenKindDeclarationKeyword,
	"interface": scan.TokenKindDeclarationKeyword,
	"type":      scan.TokenKindDeclarationKeyword,
	"enum":      scan.TokenKindDeclarationKeyword,
	"function":  scan.TokenKindDeclarationKeyword,
	"let":       scan.TokenKindDeclarationKeyword,
	"var":       scan.TokenKindDeclarationKeyword,
	"import":    scan.TokenKindDeclarationKeyword,
	"export":    scan.TokenKindDeclarationKeyword,
	"from":      scan.TokenKindDeclarationKeyword,
	"const":     scan.TokenKindControlKeyword,
	"return":    scan.TokenKindControlKeyword,
	"async":     scan.TokenKindControlKeyword,
	"await":     scan.TokenKindControlKeyword,
	"if":        scan.TokenKindControlKeyword,
	"else":      scan.TokenKindControlKeyword,
	"for":       scan.TokenKindControlKeyword,
	"while":     scan.TokenKindControlKeyword,
	"switch":    scan.TokenKindControlKeyword,
	"case":      scan.TokenKindControlKeyword,
	"default":   scan.TokenKindControlKeyword,
	"try":       scan.TokenKindControlKeyword,
	"catch":     scan.TokenKindControlKeyword,
	"finally":   scan.TokenKindControlKeyword,
	"continue":  scan.TokenKindControlKeyword,
	"break":     scan.TokenKindEscapeKeyword,
	"throw":     scan.TokenKindEscapeKeyword,
}

var builtinNames = map[string]struct{}{
	"Error":             {},
	"Array":             {},
	"ArrayBuffer":       {},
	"BigInt":            {},
	"Boolean":           {},
	"Date":              {},
	"Float32Array":      {},
	"Float64Array":      {},
	"Int8Array":         {},
	"Int16Array":        {},
	"Int32Array":        {},
	"Map":               {},
	"Math":              {},
	"Number":            {},
	"Object":            {},
	"Promise":           {},
	"ReadonlyArray":     {},
	"Record":            {},
	"RegExp":            {},
	"Set":               {},
	"String":            {},
	"Symbol":            {},
	"Uint8Array":        {},
	"Uint8ClampedArray": {},
	"Uint16Array":       {},
	"Uint32Array":       {},
	"WeakMap":           {},
	"WeakSet":           {},
	"console":           {},
	"undefined":         {},
	"unknown":           {},
	"never":             {},
	"any":               {},
	"void":              {},
	"string":            {},
	"number":            {},
	"boolean":           {},
	"bigint":            {},
	"symbol":            {},
}

func collectTypeScriptTokens(file string, source []byte, root *treesitter.Node) []Token {
	var out []Token
	collectTypeScriptTokensFromNode(file, source, root, &out)
	return out
}

func collectTypeScriptTokensFromNode(file string, source []byte, node *treesitter.Node, out *[]Token) {
	if node == nil {
		return
	}
	kind, text := typeScriptNodeToken(node, source)
	if kind != "" {
		pos := node.StartPosition()
		*out = append(*out, Token{Location: Location{File: file, Line: int(pos.Row) + 1, Column: int(pos.Column) + 1}, Text: text, Kind: kind})
		if kind == scan.TokenKindLiteral {
			return
		}
	}
	for i := uint(0); i < node.ChildCount(); i++ {
		collectTypeScriptTokensFromNode(file, source, node.Child(i), out)
	}
}

func typeScriptNodeToken(node *treesitter.Node, source []byte) (string, string) {
	if !node.IsNamed() {
		kind := keywordTokenKinds[node.Kind()]
		if kind != "" {
			return kind, node.Kind()
		}
		return "", ""
	}
	if typeScriptLiteralNode(node) {
		return scan.TokenKindLiteral, node.Utf8Text(source)
	}
	if typeScriptBuiltinNode(node) {
		text := node.Utf8Text(source)
		if _, ok := builtinNames[text]; ok {
			return scan.TokenKindBuiltin, text
		}
	}
	return "", ""
}

func typeScriptLiteralNode(node *treesitter.Node) bool {
	switch node.Kind() {
	case "number", "string", "template_string":
		return true
	default:
		return false
	}
}

func typeScriptBuiltinNode(node *treesitter.Node) bool {
	switch node.Kind() {
	case "identifier", "type_identifier", "predefined_type", "property_identifier", "shorthand_property_identifier", "undefined":
		return true
	default:
		return false
	}
}
