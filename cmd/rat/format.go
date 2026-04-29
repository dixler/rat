package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"rat/internal/display"
	"rat/internal/file"
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

type StyleProvider interface {
	Style(d file.Declaration) display.Style
	ReferenceStyle(root string, ref file.Reference) (display.Style, bool)
}

type DefaultStyleProvider struct{}

func (DefaultStyleProvider) Style(d file.Declaration) display.Style {
	return declarationStyle(d)
}

func (DefaultStyleProvider) ReferenceStyle(root string, ref file.Reference) (display.Style, bool) {
	return relationshipStyle(root, ref.Parent(), ref.Declaration(), ref.Kind())
}

type EscapeStyleProvider struct{}

func (EscapeStyleProvider) Style(d file.Declaration) display.Style {
	if d.Escapes() {
		return display.Style{Fg: display.Red}
	}
	if d.Kind() == file.KindParameter {
		return display.Style{Fg: display.Orange}
	}
	if d.Kind() == file.KindFunction {
		return display.Style{Fg: display.Green}
	}
	return display.Style{}
}

func (EscapeStyleProvider) ReferenceStyle(root string, ref file.Reference) (display.Style, bool) {
	if ref.Escapes() || (ref.Declaration() != nil && ref.Declaration().Escapes()) {
		return display.Style{Fg: display.White, Bg: "\x1b[41m", RefText: display.White}, true
	}
	if ref.Kind() == file.KindParameter {
		return kindStyles[file.KindParameter], true
	}
	return display.Style{}, false
}

func (r *Renderer) printTree(root string, d file.Declaration, depth int, provider StyleProvider) {
	indent := strings.Repeat("  ", depth)
	fmt.Fprintf(&r.b, "%s%s%s%s %s%s:%d:%d%s\n", indent, provider.Style(d).Fg, treeLabel(d), display.Reset, display.Gray, d.Location().File(), d.Location().Line(), d.Location().Column(), display.Reset)
	for _, group := range groupReferences(root, d, provider) {
		fmt.Fprintf(&r.b, "%s  %s%s%s %s%s:%d:%d%s\n", indent, group.Style.Fg, group.decl.Name(), display.Reset, display.Gray, group.decl.Location().File(), group.decl.Location().Line(), group.decl.Location().Column(), display.Reset)
		for _, ref := range group.refs {
			sty, ok := provider.ReferenceStyle(root, ref)
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
		r.printTree(root, child, depth+1, provider)
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
	if isTopLevelDeclaration(d) {
		return display.Style{Fg: display.Green}
	}
	if isTopLevelStructField(d) {
		return display.Style{Fg: display.Green}
	}
	if d != nil && d.Kind() == file.KindVariable && enclosingFunction(d) != nil {
		return display.Style{Fg: display.White}
	}
	return kindStyle(d.Kind())
}

func isTopLevelDeclaration(d file.Declaration) bool {
	if d == nil || d.Parent() == nil {
		return false
	}
	return d.Parent().Kind() == file.KindFile
}

func isTopLevelStructField(d file.Declaration) bool {
	if d == nil || d.Kind() != file.KindVariable {
		return false
	}
	for parent := d.Parent(); parent != nil; parent = parent.Parent() {
		if parent.Kind() == file.KindFunction {
			return false
		}
		if parent.Kind() == file.KindType {
			grandparent := parent.Parent()
			return grandparent != nil && grandparent.Kind() == file.KindFile
		}
	}
	return false
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

func sortSpans(spans []display.Span) {
	sort.Slice(spans, func(i, j int) bool {
		if spans[i].Start != spans[j].Start {
			return spans[i].Start < spans[j].Start
		}
		if spans[i].IsDef != spans[j].IsDef {
			return spans[i].IsDef
		}
		return spans[i].End < spans[j].End
	})
}

func collectIndirectCallSpans(out map[int][]display.Span, call file.IndirectCall) {
	text := call.Text()
	loc := call.Location()
	if loc == nil || text == "" {
		return
	}

	line := loc.Line()
	col := loc.Column()
	if line < 1 || col < 1 {
		return
	}

	for i := 0; i < len(text); i++ {
		charStyle := display.Style{Fg: "\x1b[97;41m"}
		out[line] = append(out[line], display.Span{
			Start: col - 1 + i,
			End:   col - 1 + i + 1,
			Style: charStyle,
			IsDef: true, // we highlight it as def to apply fg color directly
		})
	}
}

func collectDeclarationSpans(root string, out map[int][]display.Span, sourceLines []string, decl file.Declaration, provider StyleProvider) {
	addSpan(out, sourceLines, decl.Location(), decl.Name(), provider.Style(decl), true)
	for _, ref := range decl.References() {
		if sty, ok := provider.ReferenceStyle(root, ref); ok {
			addSpan(out, sourceLines, ref.Location(), ref.Text(), sty, false)
		}
	}
	for _, child := range decl.Declarations() {
		collectDeclarationSpans(root, out, sourceLines, child, provider)
	}
}

func addSpan(out map[int][]display.Span, sourceLines []string, loc file.Location, text string, Style display.Style, IsDef bool) {
	if loc == nil || text == "" {
		return
	}
	line := loc.Line()
	col := loc.Column()
	if line < 1 || col < 1 {
		return
	}
	start := col - 1
	if IsDef && line <= len(sourceLines) {
		lineText := sourceLines[line-1]
		if start < 0 {
			start = 0
		}
		if start < len(lineText) && !strings.HasPrefix(lineText[start:], text) {
			if idx := strings.Index(lineText[start:], text); idx >= 0 {
				start += idx
			}
		}
	}
	out[line] = append(out[line], display.Span{Start: start, End: start + len(text), Style: Style, IsDef: IsDef})
}

func kindStyle(kind file.Kind) display.Style {
	if sty, ok := kindStyles[kind]; ok {
		return sty
	}
	return kindStyles[file.KindVariable]
}

func groupReferences(root string, decl file.Declaration, provider StyleProvider) []refGroup {
	byKey := map[string]*refGroup{}
	for _, ref := range decl.References() {
		sty, ok := provider.ReferenceStyle(root, ref)
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

func ParseFormats(f file.File, provider StyleProvider) map[int][]display.Span {
	out := map[int][]display.Span{}
	sourceLines := strings.Split(f.Source(), "\n")
	addTopLevelStructFieldDeclarationSpans(out, sourceLines, f)

	for _, call := range f.IndirectCalls() {
		collectIndirectCallSpans(out, call)
	}

	root := projectRoot(f.Name())
	for _, decl := range f.Declarations() {
		collectDeclarationSpans(root, out, sourceLines, decl, provider)
	}

	for _, loc := range f.Returns() {
		addSpan(out, sourceLines, loc, "return", display.Style{Fg: display.Bold + display.Purple}, true)
	}

	for line := range out {
		sortSpans(out[line])
	}
	return out
}

func addTopLevelStructFieldDeclarationSpans(out map[int][]display.Span, sourceLines []string, f file.File) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, f.Name(), f.Source(), parser.SkipObjectResolution)
	if err != nil || node == nil {
		return
	}
	for _, decl := range node.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}
			addStructFieldSpansFromAST(out, sourceLines, fset, st)
		}
	}
}

func addStructFieldSpansFromAST(out map[int][]display.Span, sourceLines []string, fset *token.FileSet, st *ast.StructType) {
	if st == nil || st.Fields == nil {
		return
	}
	for _, field := range st.Fields.List {
		for _, name := range field.Names {
			pos := fset.Position(name.Pos())
			if pos.Line < 1 || pos.Column < 1 {
				continue
			}
			loc := &astLocation{file: pos.Filename, line: pos.Line, column: pos.Column}
			addSpan(out, sourceLines, loc, name.Name, display.Style{Fg: display.Green}, true)
		}
		addStructFieldSpansFromExpr(out, sourceLines, fset, field.Type)
	}
}

func addStructFieldSpansFromExpr(out map[int][]display.Span, sourceLines []string, fset *token.FileSet, expr ast.Expr) {
	switch n := expr.(type) {
	case *ast.StructType:
		addStructFieldSpansFromAST(out, sourceLines, fset, n)
	case *ast.FuncType:
		if n.Params != nil {
			for _, p := range n.Params.List {
				addStructFieldSpansFromExpr(out, sourceLines, fset, p.Type)
			}
		}
		if n.Results != nil {
			for _, r := range n.Results.List {
				addStructFieldSpansFromExpr(out, sourceLines, fset, r.Type)
			}
		}
	case *ast.ArrayType:
		addStructFieldSpansFromExpr(out, sourceLines, fset, n.Elt)
	case *ast.MapType:
		addStructFieldSpansFromExpr(out, sourceLines, fset, n.Key)
		addStructFieldSpansFromExpr(out, sourceLines, fset, n.Value)
	case *ast.StarExpr:
		addStructFieldSpansFromExpr(out, sourceLines, fset, n.X)
	case *ast.ChanType:
		addStructFieldSpansFromExpr(out, sourceLines, fset, n.Value)
	}
}

type astLocation struct {
	file   string
	line   int
	column int
}

func (l *astLocation) File() string { return l.file }
func (l *astLocation) Line() int    { return l.line }
func (l *astLocation) Column() int  { return l.column }
