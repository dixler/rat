package main

import (
	"fmt"
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
	file.KindParameter: display.VibrantOrange,
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
	text      string
	textStyle display.Style
	lineStyle display.Style
}

var (
	controlFlowGreen  = display.Green
	controlFlowOrange = display.MutedOrange
	controlFlowReturn = display.Orange
	controlFlowBlock  = display.Blue
)

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
	if usesTopLevelSameFileStyle(d) {
		return relationStyles[relSameFile]
	}
	if isTopLevelDeclaration(d) {
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

func usesTopLevelSameFileStyle(d file.Declaration) bool {
	if d == nil {
		return false
	}
	if d.Kind() == file.KindFunction && isTopLevelDeclaration(d) {
		return true
	}
	hasTypeAncestor := false
	for curr := d; curr != nil; curr = curr.Parent() {
		if curr.Kind() == file.KindFunction {
			return false
		}
		if curr.Kind() == file.KindType {
			hasTypeAncestor = true
		}
		if curr.Parent() != nil && curr.Parent().Kind() == file.KindFile {
			return hasTypeAncestor
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
		root = file.ProjectRoot(refs[0].Location().File())
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
	if line <= len(sourceLines) {
		lineText := sourceLines[line-1]
		if start < 0 {
			start = 0
		}
		if start > len(lineText) {
			start = len(lineText)
		}
		if !strings.HasPrefix(lineText[start:], text) {
			if idx := closestOccurrenceIndex(lineText, text, start); idx >= 0 {
				start = idx
			}
		}
	}
	out[line] = append(out[line], display.Span{Start: start, End: start + len(text), Style: Style, IsDef: IsDef})
}

func closestOccurrenceIndex(line, text string, anchor int) int {
	if text == "" || line == "" {
		return -1
	}
	best := -1
	bestDist := 0
	for i := 0; i+len(text) <= len(line); {
		idx := strings.Index(line[i:], text)
		if idx < 0 {
			break
		}
		absIdx := i + idx
		dist := absInt(absIdx - anchor)
		if best < 0 || dist < bestDist {
			best = absIdx
			bestDist = dist
		}
		i = absIdx + 1
	}
	return best
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
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

	root := file.ProjectRoot(f.Name())
	for _, decl := range f.Declarations() {
		collectDeclarationSpans(root, result.SourceSpans, sourceLines, decl)
	}

	for _, mark := range collectControlFlowMarks(f) {
		if mark.loc == nil || mark.loc.Line() < 1 {
			continue
		}
		result.LineSpans[mark.loc.Line()] = mark.lineStyle
		addSpan(result.SourceSpans, sourceLines, mark.loc, mark.text, mark.textStyle, false)
	}

	for line := range result.SourceSpans {
		sortSpans(result.SourceSpans[line])
	}
	return result
}

func collectPackageReferenceSpans(out map[int][]display.Span, sourceLines []string, f file.File) {
	for _, ref := range f.PackageReferences() {
		addImportReferenceSpan(out, sourceLines, ref, display.HotMagenta)
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

func newControlFlowMark(loc file.Location, text string, style display.Style) controlFlowMark {
	return controlFlowMark{
		loc:       loc,
		text:      text,
		textStyle: style,
		lineStyle: style,
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
	collectBlockMarks(blocks, marks)
}

func collectBlockMarks(blocks []file.Block, marks *[]controlFlowMark) {
	for _, block := range blocks {
		if block == nil || block.Location() == nil {
			continue
		}
		switch b := block.(type) {
		case file.IfBlock:
			for _, branch := range b.Branches() {
				keyword := ifBranchKeyword(branch)
				style := styleForReturnPresence(ifBranchHasDirectReturn(branch))
				mark := newControlFlowMark(branch.Location(), keyword, style)
				*marks = append(*marks, mark)
			}
		case file.LoopBlock:
			style := controlFlowGreen
			if b.HasBreak() {
				style = controlFlowOrange
			}
			keyword := b.LoopKind()
			mark := newControlFlowMark(block.Location(), keyword, style)
			*marks = append(*marks, mark)
			if b.HasBreak() {
				for _, stmt := range block.Statements() {
					if stmt == nil || stmt.Location() == nil || stmt.Kind() != "break" {
						continue
					}
					*marks = append(*marks, newControlFlowMark(stmt.Location(), "break", controlFlowOrange))
				}
			}
		case file.SwitchBlock:
			style := controlFlowOrange
			if b.HasDefault() {
				style = controlFlowGreen
			}
			mark := newControlFlowMark(block.Location(), b.SwitchKind(), style)
			*marks = append(*marks, mark)
			for _, child := range block.Blocks() {
				caseBlock, ok := child.(file.CaseBlock)
				if !ok {
					continue
				}
				caseStyle := blockKeywordStyle(child)
				if caseBlock.IsDefault() {
					*marks = append(*marks, newControlFlowMark(child.Location(), "default", caseStyle))
					continue
				}
				*marks = append(*marks, newControlFlowMark(child.Location(), "case", caseStyle))
			}
		}
		collectBlockMarks(block.Blocks(), marks)
		appendReturnMarks(block.Statements(), marks)
	}
}

func blockKeywordStyle(block file.Block) display.Style {
	return styleForReturnPresence(blockHasDirectReturn(block))
}

func blockHasDirectReturn(block file.Block) bool {
	if block == nil {
		return false
	}
	return hasReturnInStatements(block.Statements())
}

func hasReturnInStatements(statements []file.ControlFlowStatement) bool {
	for _, stmt := range statements {
		if stmt != nil && stmt.Kind() == "return" {
			return true
		}
	}
	return false
}

func appendReturnMarks(statements []file.ControlFlowStatement, marks *[]controlFlowMark) {
	for _, stmt := range statements {
		if stmt == nil || stmt.Location() == nil || stmt.Kind() != "return" {
			continue
		}
		*marks = append(*marks, newControlFlowMark(stmt.Location(), "return", controlFlowReturn))
	}
}

func ifBranchKeyword(branch file.IfBranch) string {
	if branch == nil {
		return "if"
	}
	if typed, ok := branch.(file.ElseBranch); ok && typed.IsElse() {
		return "else"
	}
	if typed, ok := branch.(file.ConditionalBranch); ok && typed.IsElseIf() {
		return "else if"
	}
	return "if"
}

func styleForReturnPresence(hasReturn bool) display.Style {
	if hasReturn {
		return controlFlowReturn
	}
	return controlFlowBlock
}

func ifBranchHasDirectReturn(branch file.IfBranch) bool {
	if branch == nil {
		return false
	}
	if hasReturnInStatements(branch.Statements()) {
		return true
	}
	for _, child := range branch.Blocks() {
		if child == nil {
			continue
		}
		if hasReturnInStatements(child.Statements()) {
			return true
		}
	}
	return false
}

func addTopLevelStructFieldDeclarationSpans(out map[int][]display.Span, sourceLines []string, f file.File) {
	for _, named := range file.TopLevelNamedFields(f) {
		addSpan(out, sourceLines, named.Location(), named.Text(), relationStyles[relSameFile], true)
	}
}
