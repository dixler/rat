package file

import (
	"fmt"
	"path/filepath"
	"strings"

	"rat/internal/file/scan"
)

func buildTree(abs string, src string, raw *scan.Result) *file {
	root := &declaration{raw: scan.Declaration{Name: filepath.Base(raw.File), Kind: scan.KindFile, Location: scan.Location{File: raw.File, Line: 1, Column: 1}}}
	declMap := map[string]*declaration{"file": root}

	for _, d := range raw.Declarations {
		decl := toDeclaration(d, root, declMap)
		root.declarations = append(root.declarations, decl)
	}

	for _, rawDecl := range raw.Declarations {
		attachDeclarationReferences(rawDecl, declMap)
	}

	pkgDecls := map[string]*packageDeclaration{}
	for _, p := range raw.Packages {
		pkgDecls[p.ID] = buildPackageDeclaration(p)
	}
	pkgRefs := make([]PackageReference, 0, len(raw.PackageReferences))
	for _, p := range raw.PackageReferences {
		pkgRef := &packageReference{reference: &reference{
			parent: declMap[p.ParentID],
			raw:    scan.Reference{Location: p.Location, Text: p.Text, Kind: scan.KindPackage},
		}, pkg: pkgDecls[p.PackageID]}
		pkgRefs = append(pkgRefs, pkgRef)
	}

	var indirectCalls []IndirectCall
	for _, c := range raw.IndirectCalls {
		indirectCalls = append(indirectCalls, &indirectCall{raw: c})
	}

	return &file{
		name:          abs,
		source:        src,
		sourceLines:   strings.Split(src, "\n"),
		root:          root,
		nodes:         clone(raw.Nodes),
		packageRefs:   pkgRefs,
		namedFields:   buildNamedFields(raw.NamedFields),
		indirectCalls: indirectCalls,
	}
}

func toDeclaration(src scan.Declaration, parent Declaration, declMap map[string]*declaration) *declaration {
	d := &declaration{raw: src, parent: parent}
	declMap[src.ID] = d
	for _, child := range src.Declarations {
		d.declarations = append(d.declarations, toDeclaration(child, d, declMap))
	}
	return d
}

func attachDeclarationReferences(raw scan.Declaration, declMap map[string]*declaration) {
	decl := declMap[raw.ID]
	for _, rr := range raw.References {
		ref := &reference{raw: rr, parent: decl}
		if rr.DeclarationID != "" {
			ref.declaration = declMap[rr.DeclarationID]
		} else if scan.HasLocation(rr.Declaration) {
			ref.declaration = externalDeclaration(rr, declMap)
		}
		decl.references = append(decl.references, ref)
	}
	for _, child := range raw.Declarations {
		attachDeclarationReferences(child, declMap)
	}
}

func externalDeclaration(raw scan.Reference, declMap map[string]*declaration) *declaration {
	loc := raw.Declaration
	key := fmt.Sprintf("external:%s:%d:%d:%s", loc.File, loc.Line, loc.Column, raw.Kind)
	if decl := declMap[key]; decl != nil {
		return decl
	}
	decl := &declaration{raw: scan.Declaration{Name: raw.Text, Kind: raw.Kind, Location: loc, ReferenceType: raw.ReferenceType}}
	declMap[key] = decl
	return decl
}

func buildPackageDeclaration(raw scan.Package) *packageDeclaration {
	p := &packageDeclaration{raw: raw}
	for _, f := range raw.Files {
		fd := &declaration{raw: scan.Declaration{Name: filepath.Base(f.File), Kind: scan.KindFile, Location: f.Location}}
		for _, d := range f.Declarations {
			fd.declarations = append(fd.declarations, &declaration{raw: scan.Declaration{Name: d.Name, Kind: d.Kind, Location: d.Location}, parent: fd})
		}
		p.files = append(p.files, fd)
	}
	return p
}
