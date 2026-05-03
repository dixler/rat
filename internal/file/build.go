package file

import (
	"path/filepath"

	"rat/internal/file/controlflow"
	"rat/internal/file/reftree"
	"rat/internal/file/scan"
)

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
		name:        src.Name,
		kind:        mapKind(src.Kind),
		location:    newLocation(src.File, src.Line, src.Column),
		parent:      parent,
		escapes:     src.Escapes,
		controlFlow: buildControlFlowBlocks(controlflow.FromScan(src.ControlFlow)),
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
	switch kind {
	case "type":
		return KindType
	case "variable":
		return KindVariable
	case "parameter":
		return KindParameter
	case "function":
		return KindFunction
	case "package":
		return KindPackage
	case "file":
		return KindFile
	default:
		return KindVariable
	}
}

func newLocation(file string, line, column int) location {
	return location{file: file, line: line, column: column}
}

func buildControlFlowBlocks(raw []controlflow.Block) []ControlFlowBlock {
	out := make([]ControlFlowBlock, 0, len(raw))
	for _, block := range raw {
		out = append(out, buildControlFlowBlock(block))
	}
	return out
}

func buildControlFlowBlock(raw controlflow.Block) ControlFlowBlock {
	block := &controlFlowBlock{
		kind:       raw.Kind,
		location:   newLocation(raw.File, raw.Line, raw.Column),
		ifChainID:  raw.IfChainID,
		ifStep:     raw.IfStep,
		caseCount:  raw.CaseCount,
		hasDefault: raw.HasDefault,
		hasBreak:   raw.HasBreak,
	}
	for _, stmt := range raw.Statements {
		block.statements = append(block.statements, &controlFlowStatement{
			kind:     stmt.Kind,
			location: newLocation(stmt.File, stmt.Line, stmt.Column),
		})
	}
	for _, child := range raw.Blocks {
		block.blocks = append(block.blocks, buildControlFlowBlock(child))
	}
	return block
}
