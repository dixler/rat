package golang

import (
	"go/scanner"
	"go/token"

	"rat/internal/file/scan"
)

var goKeywordTokens = map[token.Token]string{
	token.TYPE:      "declaration",
	token.STRUCT:    "declaration",
	token.INTERFACE: "declaration",
	token.MAP:       "mutable-type",
	token.CHAN:      "mutable-type",
	token.VAR:       "declaration",
	token.PACKAGE:   "declaration",
	token.IMPORT:    "declaration",
	token.DEFER:     "program",
	token.GO:        "program",
	token.CONST:     "program",
	token.GOTO:      "escape",
}

var goLiteralTokens = map[token.Token]bool{
	token.CHAR:   true,
	token.FLOAT:  true,
	token.IMAG:   true,
	token.INT:    true,
	token.STRING: true,
}

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

		kind, ok := goKeywordTokens[tok]
		if pendingPackageName && tok == token.IDENT {
			kind, ok = "package-name", true
			pendingPackageName = false
		} else if tok == token.RANGE {
			kind, ok = "loop-operator", true
		} else if tok == token.STRING && (pendingImportSpec || importBlockDepth > 0) {
			ok = false
		} else if goLiteralTokens[tok] {
			kind, ok = "literal", true
		}
		if pendingImportSpec && (tok == token.STRING || tok == token.SEMICOLON) {
			pendingImportSpec = false
		}
		if ok && text != "" {
			spans := scan.SpansForText(p.Line, p.Column, text)
			if len(spans) == 0 {
				continue
			}
			span := spans[0]
			switch kind {
			case "declaration":
				out = append(out, scan.DeclarationSyntaxNode{NodeSpans: spans})
			case "mutable-type":
				out = append(out, scan.MutableTypeSyntaxNode{NodeSpans: spans})
			case "program":
				out = append(out, scan.ProgramSyntaxNode{NodeSpans: spans})
			case "escape":
				out = append(out, scan.EscapeSyntaxNode{NodeSpans: spans})
			case "literal":
				out = append(out, scan.LiteralNode{NodeSpans: spans})
			case "package-name":
				out = append(out, scan.PackageNameNode{NodeSpans: spans})
			case "loop-operator":
				out = append(out, scan.LoopOperatorNode{Span: span, Anchor: pendingLoopAnchor})
				pendingLoopAnchor = scan.Span{}
			}
		}
	}
	return out
}
