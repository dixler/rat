package typescript

func controlFlowBlockHasStatementKind(block ControlFlowBlock, kind string) bool {
	for _, stmt := range block.Statements {
		if stmt.Kind == kind {
			return true
		}
	}
	for _, child := range block.Blocks {
		if controlFlowBlockHasStatementKind(child, kind) {
			return true
		}
	}
	return false
}

func controlFlowBlockHasTerminalStatement(block ControlFlowBlock) bool {
	for _, stmt := range block.Statements {
		if isTerminalControlFlowKind(stmt.Kind) {
			return true
		}
	}
	for _, child := range block.Blocks {
		if child.Kind == BlockKindElseIf || child.Kind == BlockKindElse {
			continue
		}
		if controlFlowBlockHasTerminalStatement(child) {
			return true
		}
	}
	return false
}

func isTerminalControlFlowKind(kind string) bool {
	switch kind {
	case "return", "throw", "continue", "break", "goto", "panic":
		return true
	default:
		return false
	}
}
