package file

import (
	"fmt"
	"path/filepath"

	"notectl/internal/file/scan"
)

func buildTree(raw *scan.Result) (*declaration, []PackageReference, []Declaration, error) {
	root := &declaration{name: filepath.Base(raw.File), kind: KindFile, location: newLocation(raw.File, 1, 1)}
	declsByID := map[string]*declaration{"file": root}
	for _, d := range raw.Declarations {
		decl, err := buildDeclaration(d, root, declsByID)
		if err != nil {
			return nil, nil, nil, err
		}
		root.declarations = append(root.declarations, decl)
	}
	pkgDecls := map[string]*packageDeclaration{}
	for _, p := range raw.Packages {
		pkgDecls[p.ID] = buildPackageDeclaration(p)
	}
	pkgRefs := make([]PackageReference, 0, len(raw.PackageReferences))
	for _, p := range raw.PackageReferences {
		parent, ok := declsByID[p.ParentID]
		if !ok {
			return nil, nil, nil, fmt.Errorf("missing package parent %q", p.ParentID)
		}
		pkgRef := &packageReference{reference: &reference{
			parent:   parent,
			location: newLocation(p.File, p.Line, p.Column),
			text:     p.Text,
			kind:     KindPackage,
		}, pkg: pkgDecls[p.PackageID]}
		pkgRefs = append(pkgRefs, pkgRef)
	}
	attachReferences(raw.Declarations, declsByID)
	decls := append([]Declaration(nil), root.declarations...)
	return root, pkgRefs, decls, nil
}

func buildDeclaration(raw scan.Declaration, parent Declaration, declsByID map[string]*declaration) (*declaration, error) {
	d := &declaration{
		name:     raw.Name,
		kind:     mapKind(raw.Kind),
		location: newLocation(raw.File, raw.Line, raw.Column),
		parent:   parent,
	}
	declsByID[raw.ID] = d
	for _, child := range raw.Declarations {
		childDecl, err := buildDeclaration(child, d, declsByID)
		if err != nil {
			return nil, err
		}
		d.declarations = append(d.declarations, childDecl)
	}
	return d, nil
}

func attachReferences(rawDecls []scan.Declaration, declsByID map[string]*declaration) {
	for _, raw := range rawDecls {
		attachDeclarationReferences(raw, declsByID)
	}
}

func attachDeclarationReferences(raw scan.Declaration, declsByID map[string]*declaration) {
	decl := declsByID[raw.ID]
	for _, rr := range raw.References {
		ref := &reference{
			parent:   decl,
			location: newLocation(rr.File, rr.Line, rr.Column),
			text:     rr.Text,
			kind:     mapKind(rr.Kind),
		}
		if rr.DeclarationID != "" {
			ref.declaration = declsByID[rr.DeclarationID]
		} else if rr.DeclarationFile != "" && rr.DeclarationLine > 0 && rr.DeclarationColumn > 0 {
			ref.declaration = externalDeclaration(rr, declsByID)
		}
		decl.references = append(decl.references, ref)
	}
	for _, child := range raw.Declarations {
		attachDeclarationReferences(child, declsByID)
	}
}

func externalDeclaration(raw scan.Reference, declsByID map[string]*declaration) *declaration {
	key := fmt.Sprintf("external:%s:%d:%d:%s", raw.DeclarationFile, raw.DeclarationLine, raw.DeclarationColumn, raw.Kind)
	if decl := declsByID[key]; decl != nil {
		return decl
	}
	decl := &declaration{
		name:     raw.Text,
		kind:     mapKind(raw.Kind),
		location: newLocation(raw.DeclarationFile, raw.DeclarationLine, raw.DeclarationColumn),
	}
	declsByID[key] = decl
	return decl
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
	default:
		return KindVariable
	}
}

func newLocation(file string, line, column int) location {
	return location{file: file, line: line, column: column}
}
