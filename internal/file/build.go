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

	var indirectCalls []IndirectCall
	for _, c := range raw.IndirectCalls {
		indirectCalls = append(indirectCalls, &indirectCall{
			location: location{c.File, c.Line, c.Column},
			text:     c.Text,
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
		indirectCalls: indirectCalls,
	}, nil
}

func toDeclaration(src scan.Declaration, parent Declaration, declMap map[string]*declaration) *declaration {
	d := &declaration{
		name:          src.Name,
		kind:          Kind(src.Kind),
		location:      location{src.File, src.Line, src.Column},
		referenceType: src.ReferenceType,
		parent:        parent,
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
