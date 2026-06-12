package golang

import (
	"go/ast"
	"go/token"

	"rat/internal/file/scan"
)

func arrayTypeNode(fset *token.FileSet, arrayType *ast.ArrayType) scan.Node {
	if fset == nil || arrayType == nil || arrayType.Len != nil {
		return nil
	}
	pos := fset.Position(arrayType.Lbrack)
	if pos.Line < 1 || pos.Column < 1 {
		return nil
	}
	return scan.MutableTypeSyntaxNode{NodeSpans: []scan.Span{{Line: pos.Line, Column: pos.Column, Length: len("[]")}}}
}
