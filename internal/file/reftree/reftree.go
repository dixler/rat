package reftree

import (
	"fmt"
	"path/filepath"

	"rat/internal/file/scan"
)

type Tree struct {
	Root             *Declaration
	Declarations     []*Declaration
	DeclarationsByID map[string]*Declaration
	PackageRefs      []PackageReference
}

type Declaration struct {
	ID           string
	Name         string
	Kind         string
	File         string
	Line         int
	Column       int
	Escapes      bool
	References   []Reference
	Declarations []*Declaration
	Parent       *Declaration
	ControlFlow  []scan.ControlFlowBlock
}

type Reference struct {
	Declaration *Declaration
	File        string
	Line        int
	Column      int
	Text        string
	Kind        string
	Escapes     bool
}

type PackageReference struct {
	Parent    *Declaration
	PackageID string
	Text      string
	File      string
	Line      int
	Column    int
}

func Build(raw *scan.Result) (*Tree, error) {
	root := &Declaration{ID: "file", Name: filepath.Base(raw.File), Kind: "file", File: raw.File, Line: 1, Column: 1}
	declsByID := map[string]*Declaration{"file": root}
	for _, d := range raw.Declarations {
		decl, err := buildDeclaration(d, root, declsByID)
		if err != nil {
			return nil, err
		}
		root.Declarations = append(root.Declarations, decl)
	}
	attachReferences(raw.Declarations, declsByID)

	pkgRefs := make([]PackageReference, 0, len(raw.PackageReferences))
	for _, p := range raw.PackageReferences {
		parent, ok := declsByID[p.ParentID]
		if !ok {
			return nil, fmt.Errorf("missing package parent %q", p.ParentID)
		}
		pkgRefs = append(pkgRefs, PackageReference{
			Parent:    parent,
			PackageID: p.PackageID,
			Text:      p.Text,
			File:      p.File,
			Line:      p.Line,
			Column:    p.Column,
		})
	}

	decls := make([]*Declaration, 0, len(root.Declarations))
	decls = append(decls, root.Declarations...)
	return &Tree{Root: root, Declarations: decls, DeclarationsByID: declsByID, PackageRefs: pkgRefs}, nil
}

func buildDeclaration(raw scan.Declaration, parent *Declaration, declsByID map[string]*Declaration) (*Declaration, error) {
	d := &Declaration{
		ID:          raw.ID,
		Name:        raw.Name,
		Kind:        raw.Kind,
		File:        raw.File,
		Line:        raw.Line,
		Column:      raw.Column,
		Escapes:     raw.Escapes,
		Parent:      parent,
		ControlFlow: raw.ControlFlow,
	}
	declsByID[raw.ID] = d
	for _, child := range raw.Declarations {
		childDecl, err := buildDeclaration(child, d, declsByID)
		if err != nil {
			return nil, err
		}
		d.Declarations = append(d.Declarations, childDecl)
	}
	return d, nil
}

func attachReferences(rawDecls []scan.Declaration, declsByID map[string]*Declaration) {
	for _, raw := range rawDecls {
		attachDeclarationReferences(raw, declsByID)
	}
}

func attachDeclarationReferences(raw scan.Declaration, declsByID map[string]*Declaration) {
	decl := declsByID[raw.ID]
	for _, rr := range raw.References {
		ref := Reference{File: rr.File, Line: rr.Line, Column: rr.Column, Text: rr.Text, Kind: rr.Kind, Escapes: rr.Escapes}
		if rr.DeclarationID != "" {
			ref.Declaration = declsByID[rr.DeclarationID]
		} else if rr.DeclarationFile != "" && rr.DeclarationLine > 0 && rr.DeclarationColumn > 0 {
			ref.Declaration = externalDeclaration(rr, declsByID)
		}
		decl.References = append(decl.References, ref)
	}
	for _, child := range raw.Declarations {
		attachDeclarationReferences(child, declsByID)
	}
}

func externalDeclaration(raw scan.Reference, declsByID map[string]*Declaration) *Declaration {
	key := fmt.Sprintf("external:%s:%d:%d:%s", raw.DeclarationFile, raw.DeclarationLine, raw.DeclarationColumn, raw.Kind)
	if decl := declsByID[key]; decl != nil {
		return decl
	}
	decl := &Declaration{ID: key, Name: raw.Text, Kind: raw.Kind, File: raw.DeclarationFile, Line: raw.DeclarationLine, Column: raw.DeclarationColumn}
	declsByID[key] = decl
	return decl
}
