package golang

import (
	"go/ast"
	goscan "go/scanner"
	"go/token"
	"go/types"
	"strings"

	"rat/internal/file/scan"
)

func (b *build) syntax() {
	add := func(node scan.Node) { b.result.Nodes = append(b.result.Nodes, node) }
	imports := map[token.Pos]bool{}
	ranges := map[token.Pos]token.Pos{}
	for _, imp := range b.ast.Imports {
		imports[imp.Path.Pos()] = true
	}
	ast.Inspect(b.ast, func(node ast.Node) bool {
		if n, ok := node.(*ast.RangeStmt); ok {
			ranges[n.Range] = n.For
		}
		return true
	})
	var lexer goscan.Scanner
	lexer.Init(b.fset.File(b.ast.Pos()), b.source, func(token.Position, string) {}, goscan.ScanComments)
	for {
		pos, tok, text := lexer.Scan()
		if tok == token.EOF {
			break
		}
		p := b.location(pos)
		spans := scan.NodeSpans(scan.SpansForText(p.Line, p.Column, text))
		switch tok {
		case token.PACKAGE, token.IMPORT, token.TYPE, token.VAR, token.STRUCT, token.INTERFACE:
			add(scan.DeclarationSyntaxNode{NodeSpans: b.word(pos, tok.String())})
		case token.CONST, token.GO, token.DEFER:
			add(scan.ProgramSyntaxNode{NodeSpans: b.word(pos, tok.String())})
		case token.CHAN, token.MAP:
			add(scan.MutableTypeSyntaxNode{NodeSpans: b.word(pos, tok.String())})
		case token.COMMENT:
		case token.GOTO:
			add(scan.EscapeSyntaxNode{NodeSpans: b.word(pos, tok.String())})
		case token.INT, token.FLOAT, token.IMAG, token.CHAR, token.STRING:
			if !imports[pos] {
				add(scan.LiteralNode{NodeSpans: spans})
			}
		case token.IDENT:
			if pos == b.ast.Name.Pos() {
				add(scan.PackageNameNode{NodeSpans: spans})
			}
		case token.RANGE:
			add(scan.LoopOperatorNode{Span: b.span(pos, 5), Anchor: b.span(ranges[pos], 3)})
		}
	}
	ast.Inspect(b.ast, func(node ast.Node) bool {
		var fn *ast.FuncType
		var body *ast.BlockStmt
		inline := false
		switch n := node.(type) {
		case *ast.FuncDecl:
			fn, body = n.Type, n.Body
		case *ast.FuncLit:
			fn, body, inline = n.Type, n.Body, true
		case *ast.ArrayType:
			if n.Len == nil {
				add(scan.MutableTypeSyntaxNode{NodeSpans: scan.NodeSpans{b.span(n.Lbrack, 2)}})
			}
		case *ast.CompositeLit:
			if t := b.info.TypeOf(n); t != nil {
				if s, ok := t.Underlying().(*types.Struct); ok {
					add(scan.PartialNode{NodeSpans: b.spans(n.Lbrace, n.Rbrace), IsComplete: len(n.Elts) == s.NumFields()})
				}
			}
		}
		if fn != nil && body != nil {
			spans := append(b.word(fn.Func, "func"), b.spans(body.Lbrace, body.Rbrace)...)
			add(scan.FunctionSyntaxNode{NodeSpans: spans, ReturnsError: b.returnsError(fn)})
			if inline {
				open, close := b.location(body.Lbrace), b.location(body.Rbrace)
				var indent scan.NodeSpans
				lines := strings.Split(string(b.source), "\n")
				for line := open.Line + 1; line < close.Line; line++ {
					if line <= len(lines) && len(lines[line-1]) >= close.Column && strings.TrimSpace(lines[line-1]) != "" {
						indent = append(indent, scan.Span{Line: line, Column: close.Column, Length: 1})
					}
				}
				if len(indent) > 0 {
					add(scan.InlineFunctionIndentNode{NodeSpans: indent})
				}
			}
		}
		return true
	})
	for _, imp := range b.ast.Imports {
		text := imp.Path.Value
		end := strings.LastIndex(text, "/") + 1
		if end == 0 {
			end = 1
		}
		add(scan.CommentNode{NodeSpans: b.word(imp.Path.Pos(), text[:end])})
		add(scan.CommentNode{NodeSpans: b.word(imp.Path.End()-1, text[len(text)-1:])})
	}
	for _, group := range b.ast.Comments {
		for _, comment := range group.List {
			p := b.location(comment.Pos())
			add(scan.CommentNode{NodeSpans: scan.SpansForText(p.Line, p.Column, comment.Text)})
		}
	}
	ast.Inspect(b.ast, func(node ast.Node) bool {
		if n, ok := node.(*ast.CallExpr); ok {
			add(scan.CallParenNode{NodeSpans: b.spans(n.Lparen, n.Rparen), Indirect: b.indirect[n]})
		}
		return true
	})
}

func (b *build) word(pos token.Pos, text string) scan.NodeSpans {
	return scan.NodeSpans{b.span(pos, len(text))}
}
