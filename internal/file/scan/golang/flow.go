package golang

import (
	"go/ast"
	"go/token"
	"go/types"

	"rat/internal/file/scan"
)

type flowContext struct {
	breakTarget *scan.ControlFlowBlock
	loops       []*scan.ControlFlowBlock
	labels      map[string]*scan.ControlFlowBlock
	label       string
}

func (b *build) flowList(stmts []ast.Stmt, fn *ast.FuncType, ctx flowContext) []scan.ControlFlowBlock {
	var blocks []scan.ControlFlowBlock
	for _, stmt := range stmts {
		blocks = append(blocks, b.flow(stmt, fn, ctx))
	}
	return blocks
}

func (b *build) flow(stmt ast.Stmt, fn *ast.FuncType, ctx flowContext) scan.ControlFlowBlock {
	if labeled, ok := stmt.(*ast.LabeledStmt); ok {
		ctx.label = labeled.Label.Name
		return b.flow(labeled.Stmt, fn, ctx)
	}
	block := scan.ControlFlowBlock{Location: b.location(stmt.Pos()), Kind: scan.BlockKindBase}
	if ctx.label != "" {
		labels := map[string]*scan.ControlFlowBlock{}
		for k, v := range ctx.labels {
			labels[k] = v
		}
		labels[ctx.label] = &block
		ctx.labels = labels
		ctx.label = ""
	}
	body := func(kind string, body *ast.BlockStmt, childCtx flowContext) {
		block.Kind = kind
		open, close := b.location(body.Lbrace), b.location(body.Rbrace)
		block.OpenBraceLine, block.OpenBraceColumn = open.Line, open.Column
		block.CloseBraceLine, block.CloseBraceColumn = close.Line, close.Column
		switch kind {
		case scan.BlockKindFor:
			childCtx.loops = append(childCtx.loops, &block)
			childCtx.breakTarget = &block
		case scan.BlockKindSwitch, scan.BlockKindSelect:
			childCtx.breakTarget = &block
		}
		block.Blocks = b.flowList(body.List, fn, childCtx)
	}
	switch n := stmt.(type) {
	case *ast.IfStmt:
		body(scan.BlockKindIf, n.Body, ctx)
		block.HasAbort = directAbort(block.Blocks)
		if n.Else != nil {
			alternative := b.flow(n.Else, fn, ctx)
			if _, ok := n.Else.(*ast.IfStmt); ok {
				alternative.Kind = scan.BlockKindElseIf
			} else {
				alternative.Kind = scan.BlockKindElse
				body := n.Else.(*ast.BlockStmt)
				open, close := b.location(body.Lbrace), b.location(body.Rbrace)
				alternative.OpenBraceLine, alternative.OpenBraceColumn = open.Line, open.Column
				alternative.CloseBraceLine, alternative.CloseBraceColumn = close.Line, close.Column
				alternative.HasAbort = directAbort(alternative.Blocks)
			}
			block.Blocks = append(block.Blocks, alternative)
		}
	case *ast.BlockStmt:
		block.Blocks = b.flowList(n.List, fn, ctx)
		block.HasAbort = block.HasAbortStmt(true)
	case *ast.ForStmt:
		body(scan.BlockKindFor, n.Body, ctx)
		block.HasAbort = block.HasAbortStmt(true)
	case *ast.RangeStmt:
		body(scan.BlockKindFor, n.Body, ctx)
		block.HasAbort = block.HasAbortStmt(true)
	case *ast.SwitchStmt:
		body(scan.BlockKindSwitch, n.Body, ctx)
	case *ast.TypeSwitchStmt:
		body(scan.BlockKindSwitch, n.Body, ctx)
	case *ast.SelectStmt:
		body(scan.BlockKindSelect, n.Body, ctx)
	case *ast.CaseClause:
		block.Kind = scan.BlockKindCase
		block.HasDefault = n.List == nil
		block.Blocks = b.flowList(n.Body, fn, ctx)
		for _, child := range block.Blocks {
			block.Statements = append(block.Statements, child.Statements...)
		}
		block.HasAbort = block.HasAbortStmt(false)
	case *ast.CommClause:
		block.Kind = scan.BlockKindCase
		block.HasDefault = n.Comm == nil
		block.Blocks = b.flowList(n.Body, fn, ctx)
		for _, child := range block.Blocks {
			block.Statements = append(block.Statements, child.Statements...)
		}
		block.HasAbort = block.HasAbortStmt(false)
	case *ast.ReturnStmt:
		block.Statements = []scan.ControlFlowStatement{{Location: block.Location, Kind: "return", ReturnsError: b.errorReturn(n, fn)}}
		block.HasAbort = true
		for _, loop := range ctx.loops {
			loop.MayReturn = true
		}
	case *ast.BranchStmt:
		target := ctx.breakTarget
		if n.Label != nil {
			target = ctx.labels[n.Label.Name]
		}
		isAbort := n.Tok == token.CONTINUE || n.Tok == token.BREAK && target != nil && (target.Kind == scan.BlockKindSwitch || target.Kind == scan.BlockKindSelect)
		if n.Tok == token.BREAK && target != nil && target.Kind == scan.BlockKindFor {
			target.MayBreak = true
		}
		block.Statements = []scan.ControlFlowStatement{{Location: block.Location, Kind: n.Tok.String(), IsAbort: isAbort}}
		block.HasAbort = block.HasAbortStmt(false)
	case *ast.ExprStmt:
		if call, ok := n.X.(*ast.CallExpr); ok {
			if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "panic" {
				block.Statements = []scan.ControlFlowStatement{{Location: block.Location, Kind: scan.StatementKindPanic}}
				block.HasAbort = true
			}
		}
	}
	if block.Kind == scan.BlockKindSwitch || block.Kind == scan.BlockKindSelect {
		for _, child := range block.Blocks {
			block.HasDefault = block.HasDefault || child.HasDefault
		}
		block.HasAbort = block.HasAbortStmt(true)
	}
	return block
}

func directAbort(blocks []scan.ControlFlowBlock) bool {
	for _, block := range blocks {
		if block.HasAbortStmt(false) {
			return true
		}
	}
	return false
}

func (b *build) returnsError(fn *ast.FuncType) bool {
	if fn.Results == nil {
		return false
	}
	fields := fn.Results.List
	return len(fields) > 0 && errorType(b.info.TypeOf(fields[len(fields)-1].Type))
}

func errorType(t types.Type) bool {
	return t != nil && t != types.Typ[types.Invalid] && types.AssignableTo(t, types.Universe.Lookup("error").Type())
}

func (b *build) errorReturn(stmt *ast.ReturnStmt, fn *ast.FuncType) bool {
	if fn.Results == nil {
		return false
	}
	index := 0
	for _, field := range fn.Results.List {
		t := b.info.TypeOf(field.Type)
		for range max(1, len(field.Names)) {
			if errorType(t) {
				if index >= len(stmt.Results) {
					return true
				}
				if id, ok := stmt.Results[index].(*ast.Ident); !ok || id.Name != "nil" {
					return true
				}
			}
			index++
		}
	}
	return false
}
