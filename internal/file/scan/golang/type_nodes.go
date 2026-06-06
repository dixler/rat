package golang

import (
	"go/ast"
	"go/token"

	"rat/internal/file/scan"
)

func collectGoTypeNodes(fset *token.FileSet, parsed *ast.File) []scan.Node {
	var out []scan.Node
	ast.Inspect(parsed, func(n ast.Node) bool {
		arrayType, ok := n.(*ast.ArrayType)
		if !ok || arrayType.Len != nil {
			return true
		}
		pos := fset.Position(arrayType.Lbrack)
		out = append(out, scan.MutableTypeSyntaxNode{NodeSpans: []scan.Span{{Line: pos.Line, Column: pos.Column, Length: len("[]")}}})
		return true
	})
	return out
}
