package file

import (
	"path/filepath"

	"rat/internal/file/controlflow"
	"rat/internal/file/reftree"
	"rat/internal/file/scan"
)

var kindMap = map[string]Kind{
	scan.KindType:      KindType,
	scan.KindVariable:  KindVariable,
	scan.KindParameter: KindParameter,
	scan.KindFunction:  KindFunction,
	scan.KindPackage:   KindPackage,
	scan.KindFile:      KindFile,
}

func buildTree(raw *scan.Result) (*declaration, []PackageReference, []Declaration, []Location, []IndirectCall, error) {
	tree, err := reftree.Build(raw)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}

	declMap := map[*reftree.Declaration]*declaration{}
	root := toDeclaration(tree.Root, nil, declMap)
	decls := make([]Declaration, 0, len(root.declarations))
	decls = append(decls, root.declarations...)
	attachReferencesFromRefTree(declMap)

	pkgDecls := map[string]*packageDeclaration{}
	for _, p := range raw.Packages {
		pkgDecls[p.ID] = buildPackageDeclaration(p)
	}
	pkgRefs := make([]PackageReference, 0, len(tree.PackageRefs))
	for _, p := range tree.PackageRefs {
		parent := declMap[p.Parent]
		pkgRef := &packageReference{reference: &reference{
			parent:   parent,
			location: newLocation(p.File, p.Line, p.Column),
			text:     p.Text,
			kind:     KindPackage,
		}, pkg: pkgDecls[p.PackageID]}
		pkgRefs = append(pkgRefs, pkgRef)
	}

	var returns []Location
	for _, r := range raw.Returns {
		returns = append(returns, newLocation(r.File, r.Line, r.Column))
	}

	var indirectCalls []IndirectCall
	for _, c := range raw.IndirectCalls {
		indirectCalls = append(indirectCalls, &indirectCall{
			location: newLocation(c.File, c.Line, c.Column),
			text:     c.Text,
		})
	}

	return root, pkgRefs, decls, returns, indirectCalls, nil
}

func toDeclaration(src *reftree.Declaration, parent Declaration, declMap map[*reftree.Declaration]*declaration) *declaration {
	if src == nil {
		return nil
	}
	d := &declaration{
		name:     src.Name,
		kind:     mapKind(src.Kind),
		location: newLocation(src.File, src.Line, src.Column),
		parent:   parent,
		escapes:  src.Escapes,
		blocks:   buildBlocks(controlflow.FromScan(src.ControlFlow)),
	}
	declMap[src] = d
	for _, child := range src.Declarations {
		d.declarations = append(d.declarations, toDeclaration(child, d, declMap))
	}
	return d
}

func attachReferencesFromRefTree(declMap map[*reftree.Declaration]*declaration) {
	for src, dst := range declMap {
		for _, rr := range src.References {
			ref := &reference{
				parent:   dst,
				location: newLocation(rr.File, rr.Line, rr.Column),
				text:     rr.Text,
				kind:     mapKind(rr.Kind),
				escapes:  rr.Escapes,
			}
			if rr.Declaration != nil {
				ref.declaration = ensureMappedDeclaration(rr.Declaration, declMap)
			}
			dst.references = append(dst.references, ref)
		}
	}
}

func ensureMappedDeclaration(src *reftree.Declaration, declMap map[*reftree.Declaration]*declaration) Declaration {
	if src == nil {
		return nil
	}
	if mapped, ok := declMap[src]; ok && mapped != nil {
		return mapped
	}
	mapped := &declaration{
		name:     src.Name,
		kind:     mapKind(src.Kind),
		location: newLocation(src.File, src.Line, src.Column),
		escapes:  src.Escapes,
	}
	declMap[src] = mapped
	return mapped
}

func buildPackageDeclaration(raw scan.Package) *packageDeclaration {
	p := &packageDeclaration{name: raw.Name, location: newLocation(raw.File, raw.Line, raw.Column)}
	for _, f := range raw.Files {
		fd := &declaration{name: filepath.Base(f.File), kind: KindFile, location: newLocation(f.File, f.Line, f.Column)}
		for _, d := range f.Declarations {
			fd.declarations = append(fd.declarations, &declaration{name: d.Name, kind: mapKind(d.Kind), location: newLocation(d.File, d.Line, d.Column), parent: fd})
		}
		p.files = append(p.files, fd)
	}
	return p
}

func mapKind(kind string) Kind {
	if mapped, ok := kindMap[kind]; ok {
		return mapped
	}
	return KindVariable
}

func newLocation(file string, line, column int) location {
	return location{file: file, line: line, column: column}
}

func buildBlocks(raw []controlflow.Block) []Block {
	out := make([]Block, 0, len(raw))
	for _, block := range raw {
		out = append(out, buildBlock(block))
	}
	return out
}

func buildBlock(raw controlflow.Block) Block {
	base := blockBase{location: newLocation(raw.File, raw.Line, raw.Column)}
	var block Block
	switch raw.Kind {
	case scan.BlockKindIf:
		return buildIfBlock(raw)
	case scan.BlockKindElseIf, scan.BlockKindElse:
		block = &anonymousBlock{blockBase: base}
	case scan.BlockKindFor:
		block = &loopBlock{blockBase: base, kind: raw.Kind, hasBreak: raw.HasBreak}
	case scan.BlockKindSwitch, scan.BlockKindSelect:
		block = &switchBlock{blockBase: base, kind: raw.Kind, caseCount: raw.CaseCount, hasDefault: raw.HasDefault}
	case scan.BlockKindCase:
		block = &caseBlock{blockBase: base}
	default:
		block = &anonymousBlock{blockBase: base}
	}

	basePtr := baseOf(block)
	if basePtr == nil {
		return block
	}
	for _, stmt := range raw.Statements {
		basePtr.statements = append(basePtr.statements, &controlFlowStatement{
			kind:     stmt.Kind,
			location: newLocation(stmt.File, stmt.Line, stmt.Column),
		})
	}
	for _, child := range raw.Blocks {
		basePtr.blocks = append(basePtr.blocks, buildBlock(child))
	}
	return block
}

func buildIfBlock(raw controlflow.Block) Block {
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

func collectIfBranches(raw controlflow.Block, dst *ifBlock) {
	if dst == nil {
		return
	}
	kind := raw.Kind
	if !isIfBranchKind(kind) {
		return
	}
	var branch IfBranch
	if kind == scan.BlockKindElse {
		branch = &elseBranch{ifBranchBase: ifBranchBase{location: newLocation(raw.File, raw.Line, raw.Column), step: raw.IfStep}}
	} else {
		branch = &ifBranch{ifBranchBase: ifBranchBase{location: newLocation(raw.File, raw.Line, raw.Column), step: raw.IfStep}, elseIf: kind == scan.BlockKindElseIf}
	}
	for _, stmt := range raw.Statements {
		dst.statements = append(dst.statements, &controlFlowStatement{kind: stmt.Kind, location: newLocation(stmt.File, stmt.Line, stmt.Column)})
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

func baseOf(block Block) *blockBase {
	switch b := block.(type) {
	case *ifBlock:
		return &b.blockBase
	case *loopBlock:
		return &b.blockBase
	case *switchBlock:
		return &b.blockBase
	case *caseBlock:
		return &b.blockBase
	case *anonymousBlock:
		return &b.blockBase
	default:
		return nil
	}
}
