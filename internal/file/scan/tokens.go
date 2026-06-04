package scan

import (
	"go/scanner"
	"go/token"
	"os"
	"unicode"
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

var typeScriptTokenKinds = map[string]string{
	"class":     TokenKindDeclarationKeyword,
	"interface": TokenKindDeclarationKeyword,
	"type":      TokenKindDeclarationKeyword,
	"enum":      TokenKindDeclarationKeyword,
	"function":  TokenKindDeclarationKeyword,
	"let":       TokenKindDeclarationKeyword,
	"var":       TokenKindDeclarationKeyword,
	"import":    TokenKindDeclarationKeyword,
	"export":    TokenKindDeclarationKeyword,
	"from":      TokenKindDeclarationKeyword,
	"const":     TokenKindControlKeyword,
	"return":    TokenKindControlKeyword,
	"async":     TokenKindControlKeyword,
	"await":     TokenKindControlKeyword,
	"if":        TokenKindControlKeyword,
	"else":      TokenKindControlKeyword,
	"for":       TokenKindControlKeyword,
	"while":     TokenKindControlKeyword,
	"switch":    TokenKindControlKeyword,
	"case":      TokenKindControlKeyword,
	"default":   TokenKindControlKeyword,
	"continue":  TokenKindControlKeyword,
	"break":     TokenKindEscapeKeyword,
	"throw":     TokenKindEscapeKeyword,
	"Error":     TokenKindBuiltin,
	"Array":     TokenKindBuiltin,
	"Boolean":   TokenKindBuiltin,
	"Date":      TokenKindBuiltin,
	"Map":       TokenKindBuiltin,
	"Math":      TokenKindBuiltin,
	"Number":    TokenKindBuiltin,
	"Object":    TokenKindBuiltin,
	"Promise":   TokenKindBuiltin,
	"Set":       TokenKindBuiltin,
	"String":    TokenKindBuiltin,
	"Symbol":    TokenKindBuiltin,
	"console":   TokenKindBuiltin,
	"undefined": TokenKindBuiltin,
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

func collectTypeScriptTokens(file string, source []byte) []Token {
	line, col := 1, 1
	var out []Token
	for i := 0; i < len(source); {
		ch := rune(source[i])
		if ch == '\n' {
			line++
			col = 1
			i++
			continue
		}
		if isIdentStart(ch) {
			startLine, startCol, start := line, col, i
			i++
			col++
			for i < len(source) && isIdentPart(rune(source[i])) {
				i++
				col++
			}
			text := string(source[start:i])
			if kind := typeScriptTokenKinds[text]; kind != "" {
				out = append(out, Token{Location: Location{File: file, Line: startLine, Column: startCol}, Text: text, Kind: kind})
			}
			continue
		}
		if ch == '\'' || ch == '"' || ch == '`' {
			quote := ch
			startLine, startCol, start := line, col, i
			i++
			col++
			for i < len(source) {
				curr := rune(source[i])
				if curr == '\\' && i+1 < len(source) {
					i += 2
					col += 2
					continue
				}
				i++
				if curr == '\n' {
					line++
					col = 1
				} else {
					col++
				}
				if curr == quote {
					break
				}
			}
			out = append(out, Token{Location: Location{File: file, Line: startLine, Column: startCol}, Text: string(source[start:i]), Kind: TokenKindLiteral})
			continue
		}
		if unicode.IsDigit(ch) {
			startLine, startCol, start := line, col, i
			i++
			col++
			for i < len(source) && (unicode.IsDigit(rune(source[i])) || source[i] == '.') {
				i++
				col++
			}
			out = append(out, Token{Location: Location{File: file, Line: startLine, Column: startCol}, Text: string(source[start:i]), Kind: TokenKindLiteral})
			continue
		}
		i++
		col++
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

func isIdentStart(ch rune) bool {
	return ch == '_' || ch == '$' || unicode.IsLetter(ch)
}

func isIdentPart(ch rune) bool {
	return isIdentStart(ch) || unicode.IsDigit(ch)
}
