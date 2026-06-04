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

	var tokens []Token
	for _, t := range raw.Tokens {
		var anchor *location
		if t.AnchorLine > 0 && t.AnchorColumn > 0 {
			anchor = &location{t.File, t.AnchorLine, t.AnchorColumn}
		}
		tokens = append(tokens, lexicalToken{
			location:       location{t.File, t.Line, t.Column},
			anchorLocation: anchor,
			text:           t.Text,
			kind:           TokenKind(t.Kind),
		})
	}

	return &file{
		name:          abs,
		source:        src,
		root:          root,
		nodes:         append([]scan.Node(nil), raw.Nodes...),
		packageRefs:   pkgRefs,
		decls:         decls,
		namedFields:   buildNamedFields(raw.NamedFields),
		returns:       returns,
		indirectCalls: indirectCalls,
		comments:      comments,
		tokens:        tokens,
	}, nil
}

func toDeclaration(src scan.Declaration, parent Declaration, declMap map[string]*declaration) *declaration {
	blocks := make([]Block, 0, len(src.ControlFlow))
	for _, block := range src.ControlFlow {
		blocks = append(blocks, buildBlock(block))
	}
	d := &declaration{
		name:          src.Name,
		kind:          Kind(src.Kind),
		location:      location{src.File, src.Line, src.Column},
		referenceType: src.ReferenceType,
		parent:        parent,
		blocks:        blocks,
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
			parent:        decl,
			location:      location{rr.File, rr.Line, rr.Column},
			text:          rr.Text,
			kind:          Kind(rr.Kind),
			referenceType: rr.ReferenceType,
		}
		if rr.DeclarationID != "" {
			ref.declaration = declMap[rr.DeclarationID]
		} else if loc := rr.Declaration.Location(); loc.File != "" && loc.Line > 0 && loc.Column > 0 {
			ref.declaration = externalDeclaration(rr, declMap)
		}
		decl.references = append(decl.references, ref)
	}
	for _, child := range raw.Declarations {
		attachDeclarationReferences(child, declMap)
	}
}

func externalDeclaration(raw scan.Reference, declMap map[string]*declaration) *declaration {
	loc := raw.Declaration.Location()
	key := fmt.Sprintf("external:%s:%d:%d:%s", loc.File, loc.Line, loc.Column, raw.Kind)
	if decl := declMap[key]; decl != nil {
		return decl
	}
	decl := &declaration{name: raw.Text, kind: Kind(raw.Kind), location: location{loc.File, loc.Line, loc.Column}, referenceType: raw.ReferenceType}
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

func buildBlock(raw scan.ControlFlowBlock) Block {
	blockBase := buildBlockBase(raw)
	switch scan.BlockConstructKind(raw.Kind) {
	case scan.ConstructKindBranch:
		return buildIfBlock(raw)
	case scan.ConstructKindBranchAlternative:
		return &anonymousBlock{blockBase: blockBase}
	case scan.ConstructKindLoop:
		return &loopBlock{blockBase: blockBase, kind: raw.Kind, mayBreak: raw.MayBreak, mayReturn: raw.MayReturn}
	case scan.ConstructKindExhaustiveMatch:
		return &switchBlock{blockBase: blockBase, kind: raw.Kind, caseCount: raw.CaseCount, hasDefault: raw.HasDefault}
	case scan.ConstructKindCase:
		return &caseBlock{blockBase: blockBase, isDefault: raw.HasDefault}
	default:
		return &anonymousBlock{blockBase: blockBase}
	}
}

func buildIfBlock(raw scan.ControlFlowBlock) Block {
	ifb := &ifBlock{ifChainID: raw.IfChainID}
	ifb.collectIfBranches(raw)
	// If statement has to have at least 1 branch.
	ifb.location = ifb.branches[0].Location()
	for _, branch := range ifb.branches {
		ifb.blocks = append(ifb.blocks, branch.Blocks()...)
	}
	return ifb
}

func buildBlockBase(raw scan.ControlFlowBlock) blockBase {
	var openBrace, closeBrace *location
	if raw.OpenBraceLine > 0 && raw.OpenBraceColumn > 0 {
		openBrace = &location{raw.File, raw.OpenBraceLine, raw.OpenBraceColumn}
	}
	if raw.CloseBraceLine > 0 && raw.CloseBraceColumn > 0 {
		closeBrace = &location{raw.File, raw.CloseBraceLine, raw.CloseBraceColumn}
	}
	statements := make([]ControlFlowStatement, 0, len(raw.Statements))
	for _, stmt := range raw.Statements {
		statements = append(statements, &controlFlowStatement{
			kind:         stmt.Kind,
			location:     location{stmt.File, stmt.Line, stmt.Column},
			returnsError: stmt.ReturnsError,
		})
	}
	blocks := make([]Block, 0, len(raw.Blocks))
	for _, child := range raw.Blocks {
		blocks = append(blocks, buildBlock(child))
	}
	return blockBase{
		location:                        location{raw.File, raw.Line, raw.Column},
		openBrace:                       openBrace,
		closeBrace:                      closeBrace,
		hasTerminalControlFlowStatement: raw.HasTerminalControlFlowStatement,
		statements:                      statements,
		blocks:                          blocks,
	}
}

func buildBranch(raw scan.ControlFlowBlock) (IfBranch, *ifBranchBase) {
	base := ifBranchBase{
		blockBase: buildBlockBase(raw),
		step:      raw.IfStep,
	}
	switch raw.Kind {
	case scan.BlockKindIf, scan.BlockKindTry:
		branch := &ifBranch{ifBranchBase: base, keyword: raw.Kind}
		return branch, &branch.ifBranchBase
	case scan.BlockKindElseIf:
		branch := &ifBranch{ifBranchBase: base, elseIf: true, keyword: "else if"}
		return branch, &branch.ifBranchBase
	case scan.BlockKindElse, scan.BlockKindCatch, scan.BlockKindFinally:
		branch := &elseBranch{ifBranchBase: base, keyword: raw.Kind}
		return branch, &branch.ifBranchBase
	default:
		panic("unexpected control flow block in collectIfBranches")
	}
}

func (ifb *ifBlock) collectIfBranches(raw scan.ControlFlowBlock) {
	branch, base := buildBranch(raw)
	for _, child := range raw.Blocks {
		switch child.Kind {
		case scan.BlockKindElseIf, scan.BlockKindElse, scan.BlockKindCatch, scan.BlockKindFinally:
			ifb.collectIfBranches(child)
		default:
			base.blocks = append(base.blocks, buildBlock(child))
		}
	}
	ifb.branches = append(ifb.branches, branch)
}
