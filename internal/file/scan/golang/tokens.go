package golang

import (
	"go/scanner"
	"go/token"
	"os"
)

var goKeywordTokens = map[token.Token]string{
	token.TYPE:      TokenKindDeclarationKeyword,
	token.STRUCT:    TokenKindDeclarationKeyword,
	token.FUNC:      TokenKindDeclarationKeyword,
	token.INTERFACE: TokenKindDeclarationKeyword,
	token.MAP:       TokenKindDeclarationKeyword,
	token.VAR:       TokenKindDeclarationKeyword,
	token.PACKAGE:   TokenKindDeclarationKeyword,
	token.IMPORT:    TokenKindDeclarationKeyword,
	token.DEFER:     TokenKindControlKeyword,
	token.GO:        TokenKindControlKeyword,
	token.CONST:     TokenKindControlKeyword,
	token.GOTO:      TokenKindEscapeKeyword,
}

var goLiteralTokens = map[token.Token]bool{
	token.CHAR:   true,
	token.FLOAT:  true,
	token.IMAG:   true,
	token.INT:    true,
	token.STRING: true,
}

func collectGoTokens(file string) []Token {
	source, err := readFileString(file)
	if err != nil {
		return nil
	}
	fset := token.NewFileSet()
	f := fset.AddFile(file, fset.Base(), len(source))
	var s scanner.Scanner
	s.Init(f, []byte(source), nil, 0)

	var out []Token
	pendingPackageName := false
	pendingImportSpec := false
	importBlockDepth := 0
	var pendingLoopAnchor Location
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
			pendingLoopAnchor = Location{File: file, Line: p.Line, Column: p.Column}
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
			kind, ok = TokenKindPackageName, true
			pendingPackageName = false
		} else if tok == token.RANGE {
			kind, ok = TokenKindLoopOperator, true
		} else if tok == token.STRING && (pendingImportSpec || importBlockDepth > 0) {
			ok = false
		} else if goLiteralTokens[tok] {
			kind, ok = TokenKindLiteral, true
		}
		if pendingImportSpec && (tok == token.STRING || tok == token.SEMICOLON) {
			pendingImportSpec = false
		}
		if ok && text != "" {
			semanticToken := Token{Location: Location{File: file, Line: p.Line, Column: p.Column}, Text: text, Kind: kind}
			if kind == TokenKindLoopOperator {
				semanticToken.AnchorLine = pendingLoopAnchor.Line
				semanticToken.AnchorColumn = pendingLoopAnchor.Column
				pendingLoopAnchor = Location{}
			}
			out = append(out, semanticToken)
		}
	}
	return out
}

func readFileString(file string) (string, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
