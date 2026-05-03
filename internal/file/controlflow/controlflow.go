package controlflow

import "rat/internal/file/scan"

type Statement struct {
	Kind   string
	File   string
	Line   int
	Column int
}

type Block struct {
	Kind       string
	File       string
	Line       int
	Column     int
	IfChainID  string
	IfStep     int
	Statements []Statement
	Blocks     []Block
	CaseCount  int
	HasDefault bool
	HasBreak   bool
}

func FromScan(raw []scan.ControlFlowBlock) []Block {
	out := make([]Block, 0, len(raw))
	for _, b := range raw {
		out = append(out, fromScanBlock(b))
	}
	return out
}

func fromScanBlock(raw scan.ControlFlowBlock) Block {
	b := Block{
		Kind:       raw.Kind,
		File:       raw.File,
		Line:       raw.Line,
		Column:     raw.Column,
		IfChainID:  raw.IfChainID,
		IfStep:     raw.IfStep,
		CaseCount:  raw.CaseCount,
		HasDefault: raw.HasDefault,
		HasBreak:   raw.HasBreak,
	}
	for _, s := range raw.Statements {
		b.Statements = append(b.Statements, Statement{Kind: s.Kind, File: s.File, Line: s.Line, Column: s.Column})
	}
	for _, child := range raw.Blocks {
		b.Blocks = append(b.Blocks, fromScanBlock(child))
	}
	return b
}
