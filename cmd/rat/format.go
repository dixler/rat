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
	file.KindType:      display.Cyan,
	file.KindVariable:  display.Orange,
	file.KindParameter: display.Orange,
	file.KindFunction:  display.Green,
	file.KindPackage:   display.Purple,
	file.KindFile:      display.Orange,
}

var relationStyles = map[relation]display.Style{
	relSameFunction: display.Yellow,
	relSameFile:     display.LightGreen,
	relSamePackage:  display.Cyan,
	relSameProject:  display.Blue,
	relExternal:     display.Purple,
}

type refGroup struct {
	decl  file.Declaration
	Style display.Style
	refs  []file.Reference
}

type ParseResult struct {
	SourceSpans map[int][]display.Span
	LineSpans   map[int]display.Style
	LineMarkers map[int]string
}

type controlFlowMark struct {
	loc       file.Location
	kind      string
	gutter    string
	depth     int
	text      string
	textStyle display.Style
	lineStyle display.Style
}

var controlFlowLineStyle = display.Bold + display.Orange

var controlFlowTextStyles = map[string]display.Style{
	"return":   display.Bold + display.Orange,
	"if":       display.Bold + display.Orange,
	"else":     display.Bold + display.Orange,
	"switch":   display.Bold + display.Orange,
	"select":   display.Bold + display.Orange,
	"break":    display.Bold + display.Amber,
	"continue": display.Bold + display.Lime,
	"panic":    display.Bold + display.CoralRed,
	"goto":     display.Bold + display.HotMagenta,
}

func (r *Renderer) printHeader(f file.File) {
	headerStyle := display.Bold
	fmt.Fprintf(&r.b, "%s\n", headerStyle.Format(f.Name()))
}

func (r *Renderer) printTree(root string, d file.Declaration, depth int) {
	indent := strings.Repeat("  ", depth)
	declStyle := declarationStyle(d)
	locStyle := display.Gray
	fmt.Fprintf(&r.b, "%s%s %s\n", indent, declStyle.Format(treeLabel(d)), locStyle.Format(fmt.Sprintf("%s:%d:%d", d.Location().File(), d.Location().Line(), d.Location().Column())))
	for _, group := range groupReferences(root, d) {
		fmt.Fprintf(&r.b, "%s  %s %s\n", indent, group.Style.Format(group.decl.Name()), locStyle.Format(fmt.Sprintf("%s:%d:%d", group.decl.Location().File(), group.decl.Location().Line(), group.decl.Location().Column())))
		for _, ref := range group.refs {
			sty, ok := relationshipStyle(root, ref.Parent(), ref.Declaration(), ref.Kind())
			if !ok {
				continue
			}
			fmt.Fprintf(&r.b, "%s    %s\n", indent, sty.Format(fmt.Sprintf("%s:%d:%d", ref.Location().File(), ref.Location().Line(), ref.Location().Column())))
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
	if isTopLevelDeclaration(d) {
		return relationStyles[relSameFunction]
	}
	if isTopLevelStructField(d) {
		return relationStyles[relSameFunction]
	}
	if d != nil && d.Kind() == file.KindVariable && enclosingFunction(d) != nil {
		return relationStyles[relSameFunction]
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
	headerStyle := display.Bold
	fmt.Fprintf(&r.b, "%s\n", headerStyle.Format("Imports"))
	root := ""
	if refs[0].Location() != nil {
		root = projectRoot(refs[0].Location().File())
	}
	for _, ref := range refs {
		importStyle := packageDeclarationStyle(root, ref.Package())
		fmt.Fprintf(&r.b, "- %s -> %s\n", importStyle.Format(ref.Text()), ref.Package().Name())
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
		charStyle := display.HotMagenta
		out[line] = append(out[line], display.Span{
			Start: col - 1 + i,
			End:   col - 1 + i + 1,
			Style: charStyle,
			IsDef: false,
		})
	}
}

func collectDeclarationSpans(root string, out map[int][]display.Span, sourceLines []string, decl file.Declaration) {
	addSpan(out, sourceLines, decl.Location(), decl.Name(), declarationStyle(decl), true)
	for _, ref := range decl.References() {
		if sty, ok := relationshipStyle(root, ref.Parent(), ref.Declaration(), ref.Kind()); ok {
			addSpan(out, sourceLines, ref.Location(), ref.Text(), sty, false)
		}
	}
	for _, child := range decl.Declarations() {
		collectDeclarationSpans(root, out, sourceLines, child)
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
	if kind == file.KindParameter {
		return kindStyle(kind), true
	}
	if target == nil || target.Location() == nil {
		if kind == file.KindPackage {
			return relationStyles[relExternal], true
		}
		return "", false
	}
	if kind == file.KindPackage {
		return packageDeclarationStyle(root, target), true
	}
	path := filepath.Clean(target.Location().File())
	if path == "" || strings.Contains(path, "/src/builtin") {
		return "", false
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

func packageDeclarationStyle(root string, target interface{ Location() file.Location }) display.Style {
	if target == nil || target.Location() == nil {
		return relationStyles[relExternal]
	}
	targetFile := filepath.Clean(target.Location().File())
	if targetFile == "" {
		return relationStyles[relExternal]
	}
	if root != "" {
		cleanRoot := filepath.Clean(root)
		if strings.HasPrefix(targetFile, cleanRoot+string(filepath.Separator)) {
			return relationStyles[relSameProject]
		}
	}
	return relationStyles[relExternal]
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

func ParseFormats(f file.File) ParseResult {
	result := ParseResult{
		SourceSpans: map[int][]display.Span{},
		LineSpans:   map[int]display.Style{},
		LineMarkers: map[int]string{},
	}
	sourceLines := strings.Split(f.Source(), "\n")
	addTopLevelStructFieldDeclarationSpans(result.SourceSpans, sourceLines, f)
	collectPackageReferenceSpans(result.SourceSpans, sourceLines, f)

	for _, call := range f.IndirectCalls() {
		collectIndirectCallSpans(result.SourceSpans, call)
	}

	root := projectRoot(f.Name())
	for _, decl := range f.Declarations() {
		collectDeclarationSpans(root, result.SourceSpans, sourceLines, decl)
	}

	for _, mark := range collectControlFlowMarks(f) {
		if mark.loc == nil || mark.loc.Line() < 1 {
			continue
		}
		result.LineSpans[mark.loc.Line()] = mark.lineStyle
		if mark.gutter != "" {
			if existing := result.LineMarkers[mark.loc.Line()]; existing != "" {
				result.LineMarkers[mark.loc.Line()] = existing + "/" + mark.gutter
			} else {
				result.LineMarkers[mark.loc.Line()] = mark.gutter
			}
		}
		addSpan(result.SourceSpans, sourceLines, mark.loc, mark.text, mark.textStyle, false)
	}

	for line := range result.SourceSpans {
		sortSpans(result.SourceSpans[line])
	}
	return result
}

func collectPackageReferenceSpans(out map[int][]display.Span, sourceLines []string, f file.File) {
	root := projectRoot(f.Name())
	for _, ref := range f.PackageReferences() {
		addImportReferenceSpan(out, sourceLines, ref, packageDeclarationStyle(root, ref.Package()))
	}
}

func addImportReferenceSpan(out map[int][]display.Span, sourceLines []string, ref file.PackageReference, style display.Style) {
	loc := ref.Location()
	if loc == nil || loc.Line() < 1 || loc.Line() > len(sourceLines) || ref.Text() == "" {
		return
	}
	line := sourceLines[loc.Line()-1]
	start := strings.Index(line, ref.Text())
	if start < 0 {
		return
	}
	out[loc.Line()] = append(out[loc.Line()], display.Span{Start: start, End: start + len(ref.Text()), Style: style, IsDef: false})
}

func collectControlFlowMarks(f file.File) []controlFlowMark {
	marks := make([]controlFlowMark, 0)
	for _, decl := range f.Declarations() {
		collectDeclarationControlFlowMarks(decl, &marks)
	}
	sort.Slice(marks, func(i, j int) bool {
		if marks[i].loc.Line() != marks[j].loc.Line() {
			return marks[i].loc.Line() < marks[j].loc.Line()
		}
		return marks[i].loc.Column() < marks[j].loc.Column()
	})
	return marks
}

func newControlFlowMark(loc file.Location, kind, text, gutter string) controlFlowMark {
	textStyle := controlFlowTextStyles[kind]
	if textStyle == "" {
		textStyle = display.Bold + display.Orange
	}
	return controlFlowMark{
		loc:       loc,
		kind:      kind,
		gutter:    gutter,
		depth:     0,
		text:      text,
		textStyle: textStyle,
		lineStyle: controlFlowLineStyle,
	}
}

func collectDeclarationControlFlowMarks(decl file.Declaration, marks *[]controlFlowMark) {
	if decl == nil {
		return
	}
	if decl.Kind() == file.KindFunction {
		collectFunctionControlFlowMarks(decl, marks)
	}
	for _, child := range decl.Declarations() {
		collectDeclarationControlFlowMarks(child, marks)
	}
}

func collectFunctionControlFlowMarks(decl file.Declaration, marks *[]controlFlowMark) {
	blocks := decl.Blocks()
	if len(blocks) == 0 {
		return
	}
	returnLocs := collectFunctionStatementLocations(blocks, "return")
	returnTotal := len(returnLocs)
	ifChainSizes := collectIfChainSizes(blocks)
	collectBlockMarks(blocks, marks, returnTotal, ifChainSizes, 0)
}

func collectBlockMarks(blocks []file.Block, marks *[]controlFlowMark, returnTotal int, ifChainSizes map[string]int, depth int) {
	for _, block := range blocks {
		if block == nil || block.Location() == nil {
			continue
		}
		gutterPrefix := strings.Repeat("  ", depth)
		switch b := block.(type) {
		case file.IfBlock:
			for _, branch := range b.Branches() {
				step := branch.Step()
				if step < 1 {
					step = 1
				}
				gutter := ")"
				if ifChainSizes[b.IfChainID()] > 1 {
					gutter = fmt.Sprintf("%d)", step)
				}
				keyword := "if"
				kind := "if"
				if branch.Kind() == "else" {
					keyword = "else"
					kind = "else"
				} else if branch.Kind() == "elseif" {
					keyword = "else if"
				}
				mark := newControlFlowMark(branch.Location(), kind, keyword, gutterPrefix+gutter)
				mark.depth = depth
				*marks = append(*marks, mark)
			}
		case file.LoopBlock:
			gutter := ">>"
			if b.HasBreak() {
				gutter = ">>?"
			}
			mark := newControlFlowMark(block.Location(), "break", b.LoopKind(), gutterPrefix+gutter)
			mark.depth = depth
			*marks = append(*marks, mark)
		case file.SwitchBlock:
			prefix := "#"
			if b.SwitchKind() == "select" {
				prefix = "|"
			}
			gutter := fmt.Sprintf("%s%d", prefix, b.CaseCount())
			if b.HasDefault() {
				gutter += "*"
			}
			mark := newControlFlowMark(block.Location(), b.SwitchKind(), b.SwitchKind(), gutterPrefix+gutter)
			mark.depth = depth
			*marks = append(*marks, mark)
		}
		for _, stmt := range block.Statements() {
			if stmt == nil || stmt.Location() == nil {
				continue
			}
			if stmt.Kind() == "return" {
				gutter := "<<"
				if returnTotal > 1 {
					gutter = fmt.Sprintf("<<%d", returnTotal)
				}
				mark := newControlFlowMark(stmt.Location(), "return", "return", gutterPrefix+gutter)
				mark.depth = depth
				*marks = append(*marks, mark)
			}
		}
		for _, child := range block.Blocks() {
			if child == nil {
				continue
			}
			childDepth := depth + 1
			collectBlockMarks([]file.Block{child}, marks, returnTotal, ifChainSizes, childDepth)
		}
	}
}

func collectIfChainSizes(blocks []file.Block) map[string]int {
	out := map[string]int{}
	var visit func([]file.Block)
	visit = func(items []file.Block) {
		for _, block := range items {
			if block == nil {
				continue
			}
			if ifBlock, ok := block.(interface {
				IfChainID() string
				Branches() []file.IfBranch
			}); ok {
				id := ifBlock.IfChainID()
				if id == "" {
					visit(block.Blocks())
					continue
				}
				for _, branch := range ifBlock.Branches() {
					step := branch.Step()
					if step > out[id] {
						out[id] = step
					}
				}
			}
			visit(block.Blocks())
		}
	}
	visit(blocks)
	return out
}

func collectFunctionStatementLocations(blocks []file.Block, kind string) []file.Location {
	out := make([]file.Location, 0)
	for _, block := range blocks {
		if block == nil {
			continue
		}
		for _, stmt := range block.ControlFlowStatements() {
			if stmt == nil || stmt.Location() == nil || stmt.Kind() != kind {
				continue
			}
			out = append(out, stmt.Location())
		}
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
			if ok {
				addStructFieldSpansFromAST(out, sourceLines, fset, st)
				continue
			}
			iface, ok := ts.Type.(*ast.InterfaceType)
			if ok {
				addNamedFieldSpansFromAST(out, sourceLines, fset, iface.Methods)
			}
		}
	}
}

func addStructFieldSpansFromAST(out map[int][]display.Span, sourceLines []string, fset *token.FileSet, st *ast.StructType) {
	if st == nil || st.Fields == nil {
		return
	}
	addNamedFieldSpansFromAST(out, sourceLines, fset, st.Fields)
	for _, field := range st.Fields.List {
		addStructFieldSpansFromExpr(out, sourceLines, fset, field.Type)
	}
}

func addNamedFieldSpansFromAST(out map[int][]display.Span, sourceLines []string, fset *token.FileSet, fields *ast.FieldList) {
	if fields == nil {
		return
	}
	for _, field := range fields.List {
		for _, name := range field.Names {
			pos := fset.Position(name.Pos())
			if pos.Line < 1 || pos.Column < 1 {
				continue
			}
			loc := &astLocation{file: pos.Filename, line: pos.Line, column: pos.Column}
			addSpan(out, sourceLines, loc, name.Name, relationStyles[relSameFunction], true)
		}
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
