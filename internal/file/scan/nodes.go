package scan

import (
	"os"
	"strings"
)

func BuildNodes(result *Result) []Node {
	if result == nil {
		return nil
	}
	sourceLines := readSourceLines(result.File)
	var nodes []Node
	for _, decl := range result.Declarations {
		appendDeclarationNodes(&nodes, decl, sourceLines)
	}
	return nodes
}

func readSourceLines(file string) []string {
	if file == "" {
		return nil
	}
	source, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	return strings.Split(string(source), "\n")
}

func appendDeclarationNodes(nodes *[]Node, decl Declaration, sourceLines []string) {
	for _, block := range decl.ControlFlow {
		appendControlFlowNodes(nodes, block, sourceLines)
	}
	for _, child := range decl.Declarations {
		appendDeclarationNodes(nodes, child, sourceLines)
	}
}

func appendControlFlowNodes(nodes *[]Node, block ControlFlowBlock, sourceLines []string) {
	spans := blockSpans(block, sourceLines)
	switch BlockConstructKind(block.Kind) {
	case ConstructKindBranch, ConstructKindBranchAlternative, ConstructKindCase:
		*nodes = append(*nodes, CondNode{NodeSpans: spans, IsGuard: !block.HasTerminalControlFlowStatement})
	case ConstructKindLoop:
		*nodes = append(*nodes, LoopNode{NodeSpans: spans, HasExit: block.MayBreak || block.MayReturn})
	case ConstructKindExhaustiveMatch:
		*nodes = append(*nodes, MatchNode{NodeSpans: spans, HasDefault: block.HasDefault})
	}
	for _, stmt := range block.Statements {
		if jump := jumpNode(stmt); jump != nil {
			*nodes = append(*nodes, *jump)
		}
	}
	for _, child := range block.Blocks {
		appendControlFlowNodes(nodes, child, sourceLines)
	}
}

func blockSpans(block ControlFlowBlock, sourceLines []string) []Span {
	spans := oneSpan(blockKeywordSpan(block, sourceLines))
	spans = append(spans, oneSpan(Span{Line: block.OpenBraceLine, Column: block.OpenBraceColumn, Length: 1})...)
	spans = append(spans, oneSpan(Span{Line: block.CloseBraceLine, Column: block.CloseBraceColumn, Length: 1})...)
	return spans
}

func blockKeywordSpan(block ControlFlowBlock, sourceLines []string) Span {
	span := Span{Line: block.Line, Column: block.Column, Length: blockKeywordLength(block)}
	if block.Line < 1 || block.Line > len(sourceLines) || block.Column < 1 {
		return span
	}
	line := sourceLines[block.Line-1]
	if block.Kind == BlockKindElse {
		start := min(block.Column-1, len(line))
		if strings.HasPrefix(line[start:], "else") {
			return span
		}
		if idx := strings.LastIndex(line[:start], "else"); idx >= 0 {
			span.Column = idx + 1
		}
		return span
	}
	if block.Kind != BlockKindElseIf {
		return span
	}
	ifColumn := min(block.Column-1, len(line))
	end := min(ifColumn+len("if"), len(line))
	if idx := strings.LastIndex(line[:end], "else if"); idx >= 0 {
		span.Column = idx + 1
	}
	return span
}

func blockKeywordLength(block ControlFlowBlock) int {
	switch block.Kind {
	case BlockKindElseIf:
		return len("else if")
	case BlockKindCase:
		if block.HasDefault {
			return len("default")
		}
		return len("case")
	case BlockKindBase:
		return 0
	default:
		return len(block.Kind)
	}
}

func jumpNode(stmt ControlFlowStatement) *JumpNode {
	if stmt.Line < 1 || stmt.Column < 1 {
		return nil
	}
	kind := JumpKind(0)
	switch stmt.Kind {
	case "return", "throw":
		kind = JumpKindExit
		if stmt.ReturnsError {
			kind = JumpKindErrorExit
		}
	case "continue":
		kind = JumpKindContinue
	case "break":
		kind = JumpKindBreak
	case "goto", "panic":
		kind = JumpKindEscape
	case "fallthrough":
		kind = JumpKindFallthrough
	}
	if kind == 0 {
		return nil
	}
	return &JumpNode{Span: Span{Line: stmt.Line, Column: stmt.Column, Length: len(stmt.Kind)}, Kind: kind}
}
