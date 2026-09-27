package golang

import (
	"go/scanner"
	"go/token"

	"rat/internal/file/scan"
)

func collectGoTokenNodes(file string, source []byte) []scan.Node {
	fset := token.NewFileSet()
	f := fset.AddFile(file, fset.Base(), len(source))
	var s scanner.Scanner
	s.Init(f, source, nil, 0)

	var out []scan.Node
	pendingPackageName := false
	pendingImportSpec := false
	importBlockDepth := 0
	var pendingLoopAnchor scan.Span
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}

		p := fset.Position(pos)
		text := lit
		if text == "" {
			text = tok.String()
		}

		if tok == token.FOR {
			pendingLoopAnchor = scan.Span{Line: p.Line, Column: p.Column, Length: len(tok.String())}
		}
		if tok == token.PACKAGE {
			pendingPackageName = true
		}
		if tok == token.IMPORT {
			pendingImportSpec = true
		}
		if pendingImportSpec && tok == token.LPAREN {
			importBlockDepth = 1
			pendingImportSpec = false
		} else if importBlockDepth > 0 && tok == token.LPAREN {
			importBlockDepth++
		} else if importBlockDepth > 0 && tok == token.RPAREN {
			importBlockDepth--
		}

		importString := tok == token.STRING && (pendingImportSpec || importBlockDepth > 0)
		if pendingImportSpec && (tok == token.STRING || tok == token.SEMICOLON) {
			pendingImportSpec = false
		}
		if (!tok.IsKeyword() && !tok.IsLiteral()) || (tok == token.IDENT && !pendingPackageName) || importString {
			continue
		}
		spans := scan.SpansForText(p.Line, p.Column, text)
		if len(spans) == 0 {
			continue
		}
		switch tok {
		case token.TYPE, token.STRUCT, token.INTERFACE, token.VAR, token.PACKAGE, token.IMPORT:
			out = append(out, scan.DeclarationSyntaxNode{NodeSpans: spans})
		case token.MAP, token.CHAN:
			out = append(out, scan.MutableTypeSyntaxNode{NodeSpans: spans})
		case token.DEFER, token.GO, token.CONST:
			out = append(out, scan.ProgramSyntaxNode{NodeSpans: spans})
		case token.GOTO:
			out = append(out, scan.EscapeSyntaxNode{NodeSpans: spans})
		case token.CHAR, token.FLOAT, token.IMAG, token.INT, token.STRING:
			out = append(out, scan.LiteralNode{NodeSpans: spans})
		case token.IDENT:
			out = append(out, scan.PackageNameNode{NodeSpans: spans})
			pendingPackageName = false
		case token.RANGE:
			out = append(out, scan.LoopOperatorNode{Span: spans[0], Anchor: pendingLoopAnchor})
			pendingLoopAnchor = scan.Span{}
		}
	}
	return out
}
