package file

import (
	"fmt"
	"path/filepath"

	"rat/internal/file/scan"
)

func buildTree(abs string, src string, raw *scan.Result) (*file, error) {
	root := &declaration{name: filepath.Base(raw.File), kind: KindFile, location: location{file: raw.File, line: 1, column: 1}}
	declMap := map[string]*declaration{"file": root}

	for _, d := range raw.Declarations {
		decl := toDeclaration(d, root, declMap)
		root.declarations = append(root.declarations, decl)
	}

	attachReferencesFromScan(raw.Declarations, declMap)

	decls := make([]Declaration, 0, len(root.declarations))
	for _, d := range root.declarations {
		decls = append(decls, d)
	}

	pkgDecls := map[string]*packageDeclaration{}
	for _, p := range raw.Packages {
		pkgDecls[p.ID] = buildPackageDeclaration(p)
	}
	pkgRefs := make([]PackageReference, 0, len(raw.PackageReferences))
	for _, p := range raw.PackageReferences {
		parent, ok := declMap[p.ParentID]
		if !ok {
			return nil, fmt.Errorf("missing package parent %q", p.ParentID)
		}
		pkgRef := &packageReference{reference: &reference{
			parent:   parent,
			location: location{p.File, p.Line, p.Column},
			text:     p.Text,
			kind:     KindPackage,
		}, pkg: pkgDecls[p.PackageID]}
		pkgRefs = append(pkgRefs, pkgRef)
	}

	var returns []Location
	for _, r := range raw.Returns {
		returns = append(returns, location{r.File, r.Line, r.Column})
	}

	var indirectCalls []IndirectCall
	for _, c := range raw.IndirectCalls {
		indirectCalls = append(indirectCalls, &indirectCall{
			location: location{c.File, c.Line, c.Column},
			text:     c.Text,
		})
	}

	var comments []Comment
	for _, c := range raw.Comments {
		comments = append(comments, commentSpan{
			start: location{raw.File, c.StartLine, c.StartColumn},
			end:   location{raw.File, c.EndLine, c.EndColumn},
		})
	}

	return &file{
		name:          abs,
		source:        src,
		root:          root,
		packageRefs:   pkgRefs,
		decls:         decls,
		namedFields:   buildNamedFields(raw.NamedFields),
		returns:       returns,
		indirectCalls: indirectCalls,
		comments:      comments,
	}, nil
}

func toDeclaration(src scan.Declaration, parent Declaration, declMap map[string]*declaration) *declaration {
	d := &declaration{
		name:     src.Name,
		kind:     Kind(src.Kind),
		location: location{src.File, src.Line, src.Column},
		parent:   parent,
		escapes:  src.Escapes,
		blocks:   buildBlocks(src.ControlFlow),
	}
	declMap[src.ID] = d
	for _, child := range src.Declarations {
		d.declarations = append(d.declarations, toDeclaration(child, d, declMap))
	}
	return d
}

func attachReferencesFromScan(rawDecls []scan.Declaration, declMap map[string]*declaration) {
	for _, raw := range rawDecls {
		attachDeclarationReferences(raw, declMap)
	}
}

func attachDeclarationReferences(raw scan.Declaration, declMap map[string]*declaration) {
	decl := declMap[raw.ID]
	for _, rr := range raw.References {
		ref := &reference{
			parent:   decl,
			location: location{rr.File, rr.Line, rr.Column},
			text:     rr.Text,
			kind:     Kind(rr.Kind),
			escapes:  rr.Escapes,
		}
		if rr.DeclarationID != "" {
			ref.declaration = declMap[rr.DeclarationID]
		} else if rr.DeclarationFile != "" && rr.DeclarationLine > 0 && rr.DeclarationColumn > 0 {
			ref.declaration = externalDeclaration(rr, declMap)
		}
		decl.references = append(decl.references, ref)
	}
	for _, child := range raw.Declarations {
		attachDeclarationReferences(child, declMap)
	}
}

func externalDeclaration(raw scan.Reference, declMap map[string]*declaration) *declaration {
	key := fmt.Sprintf("external:%s:%d:%d:%s", raw.DeclarationFile, raw.DeclarationLine, raw.DeclarationColumn, raw.Kind)
	if decl := declMap[key]; decl != nil {
		return decl
	}
	decl := &declaration{name: raw.Text, kind: Kind(raw.Kind), location: location{raw.DeclarationFile, raw.DeclarationLine, raw.DeclarationColumn}}
	declMap[key] = decl
	return decl
}

func buildPackageDeclaration(raw scan.Package) *packageDeclaration {
	p := &packageDeclaration{name: raw.Name, location: location{raw.File, raw.Line, raw.Column}}
	for _, f := range raw.Files {
		fd := &declaration{name: filepath.Base(f.File), kind: KindFile, location: location{f.File, f.Line, f.Column}}
		for _, d := range f.Declarations {
			fd.declarations = append(fd.declarations, &declaration{name: d.Name, kind: Kind(d.Kind), location: location{d.File, d.Line, d.Column}, parent: fd})
		}
		p.files = append(p.files, fd)
	}
	return p
}

func buildBlocks(raw []scan.ControlFlowBlock) []Block {
	out := make([]Block, 0, len(raw))
	for _, block := range raw {
		out = append(out, buildBlock(block))
	}
	return out
}

func buildBlock(raw scan.ControlFlowBlock) Block {
	newBase := func() blockBase {
		base := blockBase{location: location{raw.File, raw.Line, raw.Column}, hasTerminalControlFlowStatement: raw.HasTerminalControlFlowStatement()}
		if raw.OpenBraceLine > 0 && raw.OpenBraceColumn > 0 {
			open := location{raw.File, raw.OpenBraceLine, raw.OpenBraceColumn}
			base.openBrace = &open
		}
		if raw.CloseBraceLine > 0 && raw.CloseBraceColumn > 0 {
			close := location{raw.File, raw.CloseBraceLine, raw.CloseBraceColumn}
			base.closeBrace = &close
		}
		appendControlFlowStatements(&base.statements, raw.Statements)
		for _, child := range raw.Blocks {
			base.blocks = append(base.blocks, buildBlock(child))
		}
		return base
	}
	var block Block
	switch raw.Kind {
	case scan.BlockKindIf:
		return buildIfBlock(raw)
	case scan.BlockKindElseIf, scan.BlockKindElse:
		block = &anonymousBlock{blockBase: newBase()}
	case scan.BlockKindFor:
		block = &loopBlock{blockBase: newBase(), kind: raw.Kind, mayBreak: raw.MayBreak, mayReturn: raw.MayReturn}
	case scan.BlockKindSwitch, scan.BlockKindSelect:
		block = &switchBlock{blockBase: newBase(), kind: raw.Kind, caseCount: raw.CaseCount, hasDefault: raw.HasDefault}
	case scan.BlockKindCase:
		block = &caseBlock{blockBase: newBase(), isDefault: raw.HasDefault}
	default:
		block = &anonymousBlock{blockBase: newBase()}
	}

	return block
}

func buildIfBlock(raw scan.ControlFlowBlock) Block {
	ifb := &ifBlock{ifChainID: raw.IfChainID}
	collectIfBranches(raw, ifb)
	if len(ifb.branches) > 0 {
		if first := ifBranchBaseOf(ifb.branches[0]); first != nil {
			ifb.location = first.location
		}
	}
	for _, branch := range ifb.branches {
		ifb.blocks = append(ifb.blocks, branch.Blocks()...)
	}
	return ifb
}

func collectIfBranches(raw scan.ControlFlowBlock, dst *ifBlock) {
	if dst == nil {
		return
	}
	kind := raw.Kind
	if !isIfBranchKind(kind) {
		return
	}
	loc := location{raw.File, raw.Line, raw.Column}
	var branch IfBranch
	hasTerminal := hasTerminalControlFlowInBranch(raw)
	if kind == scan.BlockKindElse {
		branch = &elseBranch{ifBranchBase: ifBranchBase{location: loc, step: raw.IfStep, hasTerminalControlFlowStatement: hasTerminal}}
	} else {
		branch = &ifBranch{ifBranchBase: ifBranchBase{location: loc, step: raw.IfStep, hasTerminalControlFlowStatement: hasTerminal}, elseIf: kind == scan.BlockKindElseIf}
	}
	if base := ifBranchBaseOf(branch); base != nil {
		if raw.OpenBraceLine > 0 && raw.OpenBraceColumn > 0 {
			open := location{raw.File, raw.OpenBraceLine, raw.OpenBraceColumn}
			base.openBrace = &open
		}
		if raw.CloseBraceLine > 0 && raw.CloseBraceColumn > 0 {
			close := location{raw.File, raw.CloseBraceLine, raw.CloseBraceColumn}
			base.closeBrace = &close
		}
		appendControlFlowStatements(&base.statements, raw.Statements)
	}
	for _, child := range raw.Blocks {
		if child.Kind == scan.BlockKindElseIf || child.Kind == scan.BlockKindElse {
			collectIfBranches(child, dst)
			continue
		}
		if base := ifBranchBaseOf(branch); base != nil {
			base.blocks = append(base.blocks, buildBlock(child))
		}
	}
	dst.branches = append(dst.branches, branch)
}

func hasTerminalControlFlowInBranch(raw scan.ControlFlowBlock) bool {
	for _, stmt := range raw.Statements {
		switch stmt.Kind {
		case "return", "continue", "break", "goto", "panic":
			return true
		}
	}
	for _, child := range raw.Blocks {
		if child.Kind == scan.BlockKindElseIf || child.Kind == scan.BlockKindElse {
			continue
		}
		if hasTerminalControlFlowInBranch(child) {
			return true
		}
	}
	return false
}

func appendControlFlowStatements(dst *[]ControlFlowStatement, raw []scan.ControlFlowStatement) {
	for _, stmt := range raw {
		*dst = append(*dst, &controlFlowStatement{kind: stmt.Kind, location: location{stmt.File, stmt.Line, stmt.Column}, returnsError: stmt.ReturnsError})
	}
}

func isIfBranchKind(kind string) bool {
	return kind == scan.BlockKindIf || kind == scan.BlockKindElseIf || kind == scan.BlockKindElse
}

func ifBranchBaseOf(branch IfBranch) *ifBranchBase {
	switch b := branch.(type) {
	case *ifBranch:
		return &b.ifBranchBase
	case *elseBranch:
		return &b.ifBranchBase
	default:
		return nil
	}
}
