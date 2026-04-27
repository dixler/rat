package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"notectl/internal/display"
	"notectl/internal/file"
)

type Renderer struct {
	b strings.Builder
}

type relation string

const (
	relSameFunction relation = "same-function"
	relSameFile     relation = "same-file"
	relSamePackage  relation = "same-package"
	relSameProject  relation = "same-project"
	relExternal     relation = "external"
)

var kindStyles = map[file.Kind]display.Style{
	file.KindType:      {Fg: display.Cyan, Bg: "\x1b[46m", RefText: display.Black},
	file.KindVariable:  {Fg: display.Orange, Bg: "\x1b[48;5;208m", RefText: display.Black},
	file.KindParameter: {Fg: display.Orange, Bg: "\x1b[48;5;208m", RefText: display.Black},
	file.KindFunction:  {Fg: display.Green, Bg: "\x1b[42m", RefText: display.Black},
	file.KindPackage:   {Fg: display.Purple, Bg: "\x1b[45m", RefText: display.White},
	file.KindFile:      {Fg: display.Orange, Bg: "\x1b[48;5;208m", RefText: display.Black},
}

var relationStyles = map[relation]display.Style{
	relSameFunction: {Fg: display.Black, Bg: "\x1b[47m", RefText: display.Black},
	relSameFile:     {Fg: display.Green, Bg: "\x1b[42m", RefText: display.Black},
	relSamePackage:  {Fg: display.Cyan, Bg: "\x1b[46m", RefText: display.Black},
	relSameProject:  {Fg: display.Blue, Bg: "\x1b[44m", RefText: display.White},
	relExternal:     {Fg: display.Purple, Bg: "\x1b[45m", RefText: display.White},
}

type refGroup struct {
	decl  file.Declaration
	Style display.Style
	refs  []file.Reference
}

func (r *Renderer) printHeader(f file.File) {
	fmt.Fprintf(&r.b, "%s%s%s\n", display.Bold, f.Name(), display.Reset)
}

func (r *Renderer) printTree(root string, d file.Declaration, depth int) {
	indent := strings.Repeat("  ", depth)
	fmt.Fprintf(&r.b, "%s%s%s%s %s%s:%d:%d%s\n", indent, declarationStyle(d).Fg, treeLabel(d), display.Reset, display.Gray, d.Location().File(), d.Location().Line(), d.Location().Column(), display.Reset)
	for _, group := range groupReferences(root, d) {
		fmt.Fprintf(&r.b, "%s  %s%s%s %s%s:%d:%d%s\n", indent, group.Style.Fg, group.decl.Name(), display.Reset, display.Gray, group.decl.Location().File(), group.decl.Location().Line(), group.decl.Location().Column(), display.Reset)
		for _, ref := range group.refs {
			sty, ok := relationshipStyle(root, ref.Parent(), ref.Declaration(), ref.Kind())
			if !ok {
				continue
			}
			fmt.Fprintf(&r.b, "%s    %s%s:%d:%d%s\n", indent, sty.Fg, ref.Location().File(), ref.Location().Line(), ref.Location().Column(), display.Reset)
		}
	}
	for _, child := range d.Declarations() {
		if d.Kind() == file.KindFunction && child.Kind() == file.KindVariable {
			continue
		}
		r.printTree(root, child, depth+1)
	}
}

func treeLabel(d file.Declaration) string {
	if d == nil {
		return ""
	}
	if d.Kind() == file.KindFile {
		return string(d.Kind())
	}
	return d.Name()
}

func declarationStyle(d file.Declaration) display.Style {
	if d != nil && d.Kind() == file.KindVariable && enclosingFunction(d) != nil {
		return display.Style{Fg: display.White}
	}
	return kindStyle(d.Kind())
}

func (r *Renderer) printImports(refs []file.PackageReference) {
	if len(refs) == 0 {
		return
	}
	fmt.Fprintf(&r.b, "%sImports%s\n", display.Bold, display.Reset)
	for _, ref := range refs {
		fmt.Fprintf(&r.b, "- %s%s%s -> %s\n", display.Purple, ref.Text(), display.Reset, ref.Package().Name())
	}
}

func collectSpans(root string, decls []file.Declaration) map[int][]display.Span {
	out := map[int][]display.Span{}
	for _, decl := range decls {
		collectDeclarationSpans(root, out, decl)
	}
	for line := range out {
		sort.Slice(out[line], func(i, j int) bool {
			if out[line][i].Start != out[line][j].Start {
				return out[line][i].Start < out[line][j].Start
			}
			if out[line][i].IsDef != out[line][j].IsDef {
				return out[line][i].IsDef
			}
			return out[line][i].End < out[line][j].End
		})
	}
	return out
}

func collectDeclarationSpans(root string, out map[int][]display.Span, decl file.Declaration) {
	addSpan(out, decl.Location(), decl.Name(), declarationStyle(decl), true)
	for _, ref := range decl.References() {
		sty, ok := relationshipStyle(root, ref.Parent(), ref.Declaration(), ref.Kind())
		if !ok {
			continue
		}
		addSpan(out, ref.Location(), ref.Text(), sty, false)
	}
	for _, child := range decl.Declarations() {
		collectDeclarationSpans(root, out, child)
	}
}

func addSpan(out map[int][]display.Span, loc file.Location, text string, Style display.Style, IsDef bool) {
	if loc == nil || text == "" {
		return
	}
	line := loc.Line()
	col := loc.Column()
	if line < 1 || col < 1 {
		return
	}
	out[line] = append(out[line], display.Span{Start: col - 1, End: col - 1 + len(text), Style: Style, IsDef: IsDef})
}

func kindStyle(kind file.Kind) display.Style {
	if sty, ok := kindStyles[kind]; ok {
		return sty
	}
	return kindStyles[file.KindVariable]
}

func groupReferences(root string, decl file.Declaration) []refGroup {
	byKey := map[string]*refGroup{}
	for _, ref := range decl.References() {
		sty, ok := relationshipStyle(root, ref.Parent(), ref.Declaration(), ref.Kind())
		if !ok || ref.Declaration() == nil {
			continue
		}
		target := ref.Declaration()
		key := locationKey(target.Location())
		if byKey[key] == nil {
			byKey[key] = &refGroup{decl: target, Style: sty}
		}
		byKey[key].refs = append(byKey[key].refs, ref)
	}
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]refGroup, 0, len(keys))
	for _, key := range keys {
		group := byKey[key]
		sort.Slice(group.refs, func(i, j int) bool {
			return locationKey(group.refs[i].Location()) < locationKey(group.refs[j].Location())
		})
		out = append(out, *group)
	}
	return out
}

func relationshipStyle(root string, parent, target file.Declaration, kind file.Kind) (display.Style, bool) {
	if kind == file.KindPackage {
		return kindStyle(kind), true
	}
	if kind == file.KindParameter {
		return kindStyle(kind), true
	}
	if target == nil || target.Location() == nil {
		return display.Style{}, false
	}
	path := filepath.Clean(target.Location().File())
	if path == "" || strings.Contains(path, "/src/builtin") {
		return display.Style{}, false
	}
	if sameFunction(parent, target) {
		return relationStyles[relSameFunction], true
	}
	if sameFile(parent, target) {
		return relationStyles[relSameFile], true
	}
	if samePackage(parent, target) {
		return relationStyles[relSamePackage], true
	}
	if sameProject(root, parent, target) {
		return relationStyles[relSameProject], true
	}
	return relationStyles[relExternal], true
}

func sameFunction(left, right file.Declaration) bool {
	lfn := enclosingFunction(left)
	rfn := enclosingFunction(right)
	return lfn != nil && rfn != nil && locationKey(lfn.Location()) == locationKey(rfn.Location())
}

func sameFile(left, right file.Declaration) bool {
	if left == nil || right == nil || left.Location() == nil || right.Location() == nil {
		return false
	}
	return filepath.Clean(left.Location().File()) == filepath.Clean(right.Location().File())
}

func samePackage(left, right file.Declaration) bool {
	if sameFile(left, right) || left == nil || right == nil || left.Location() == nil || right.Location() == nil {
		return false
	}
	return filepath.Dir(filepath.Clean(left.Location().File())) == filepath.Dir(filepath.Clean(right.Location().File()))
}

func sameProject(root string, left, right file.Declaration) bool {
	if root == "" || left == nil || right == nil || left.Location() == nil || right.Location() == nil {
		return false
	}
	lfile := filepath.Clean(left.Location().File())
	rfile := filepath.Clean(right.Location().File())
	root = filepath.Clean(root)
	return strings.HasPrefix(lfile, root+string(filepath.Separator)) && strings.HasPrefix(rfile, root+string(filepath.Separator))
}

func enclosingFunction(decl file.Declaration) file.Declaration {
	for curr := decl; curr != nil; curr = curr.Parent() {
		if curr.Kind() == file.KindFunction {
			return curr
		}
	}
	return nil
}

func locationKey(loc file.Location) string {
	if loc == nil {
		return ""
	}
	return fmt.Sprintf("%s:%d:%d", filepath.Clean(loc.File()), loc.Line(), loc.Column())
}

func projectRoot(path string) string {
	path = filepath.Clean(path)
	info, err := filepath.Abs(path)
	if err == nil {
		path = info
	}
	dir := path
	if filepath.Ext(dir) != "" {
		dir = filepath.Dir(dir)
	}
	for {
		if exists(filepath.Join(dir, ".git")) || exists(filepath.Join(dir, "go.mod")) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func exists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func ParseFormats(f file.File) map[int][]display.Span {
	root := projectRoot(f.Name())
	return collectSpans(root, f.Declarations())
}
