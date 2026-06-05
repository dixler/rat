package typescript

import (
	"rat/internal/file/scan"

	treesitter "github.com/tree-sitter/go-tree-sitter"
)

var keywordTokenKinds = map[string]string{
	"class":     "declaration",
	"interface": "declaration",
	"type":      "declaration",
	"enum":      "declaration",
	"function":  "declaration",
	"let":       "declaration",
	"var":       "declaration",
	"import":    "declaration",
	"export":    "declaration",
	"from":      "declaration",
	"const":     "program",
	"return":    "program",
	"async":     "program",
	"await":     "program",
	"if":        "program",
	"else":      "program",
	"for":       "program",
	"while":     "program",
	"switch":    "program",
	"case":      "program",
	"default":   "program",
	"try":       "program",
	"catch":     "program",
	"finally":   "program",
	"continue":  "program",
	"break":     "escape",
	"throw":     "escape",
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

func collectTypeScriptTokenNodes(source []byte, root *treesitter.Node) []scan.Node {
	var out []scan.Node
	collectTypeScriptTokenNodesFromNode(source, root, &out)
	return out
}

func collectTypeScriptTokenNodesFromNode(source []byte, node *treesitter.Node, out *[]scan.Node) {
	if node == nil {
		return
	}
	kind, text := typeScriptNodeToken(node, source)
	if kind != "" {
		pos := node.StartPosition()
		spans := scan.SpansForText(int(pos.Row)+1, int(pos.Column)+1, text)
		if len(spans) == 0 {
			return
		}
		switch kind {
		case "declaration":
			*out = append(*out, scan.DeclarationSyntaxNode{NodeSpans: spans})
		case "program":
			*out = append(*out, scan.ProgramSyntaxNode{NodeSpans: spans})
		case "escape":
			*out = append(*out, scan.EscapeSyntaxNode{NodeSpans: spans})
		case "literal":
			*out = append(*out, scan.LiteralNode{NodeSpans: spans})
		case "builtin":
			*out = append(*out, scan.BuiltinNode{NodeSpans: spans})
		}
		if kind == "literal" {
			return
		}
	}
	for i := uint(0); i < node.ChildCount(); i++ {
		collectTypeScriptTokenNodesFromNode(source, node.Child(i), out)
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
		return "literal", node.Utf8Text(source)
	}
	if typeScriptBuiltinNode(node) {
		text := node.Utf8Text(source)
		if _, ok := builtinNames[text]; ok {
			return "builtin", text
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
