package main

import (
	"fmt"
	"path"
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
	_relSameFunction relation = "same-function"
	_relSameFile     relation = "same-file"
	_relSamePackage  relation = "same-package"
	_relSameProject  relation = "same-project"
	_relExternal     relation = "external"
)

var _kindStyles = map[file.Kind]display.BasicStyle{
	file.KindType:      display.LightGreen,
	file.KindVariable:  display.Yellow,
	file.KindParameter: display.VibrantOrange,
	file.KindFunction:  display.LightGreen,
	file.KindPackage:   display.Lavender,
	file.KindFile:      display.Yellow,
}

var _relationStyles = map[relation]display.BasicStyle{
	_relSameFunction: display.Yellow,
	_relSameFile:     display.LightGreen,
	_relSamePackage:  display.Cyan,
	_relSameProject:  display.Blue,
	_relExternal:     display.Lavender,
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
			sty := relationshipStyle(root, ref.Parent(), ref.Declaration(), ref.Kind())
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
	switch {
	case d == nil:
		return _relationStyles[_relSameFile].Invert()
	case usesTopLevelSameFileStyle(d):
		return _relationStyles[_relSameFile].Invert()
	case isTopLevelDeclaration(d):
		return _relationStyles[_relSameFunction].Invert()
	case enclosingFunction(d) != nil && d.Kind() == file.KindVariable:
		return _relationStyles[_relSameFunction].Invert()
	default:
		return kindStyle(d.Kind()).Invert()
	}
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
		if spans[i].Priority != spans[j].Priority {
			return spans[i].Priority > spans[j].Priority
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
		})
	}
}

func collectDeclarationSpans(root string, out map[int][]display.Span, sourceLines []string, decl file.Declaration) {
	addSpan(out, sourceLines, decl.Location(), decl.Name(), display.Span{Style: declarationStyle(decl), Priority: 1})
	for _, ref := range decl.References() {
		addSpan(out, sourceLines, ref.Location(), ref.Text(), display.Span{Style: relationshipStyle(root, ref.Parent(), ref.Declaration(), ref.Kind())})
	}
	for _, child := range decl.Declarations() {
		collectDeclarationSpans(root, out, sourceLines, child)
	}
}

func addSpan(out map[int][]display.Span, sourceLines []string, loc file.Location, text string, span display.Span) {
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
	span.Start = start
	span.End = start + len(text)
	out[line] = append(out[line], span)
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

func kindStyle(kind file.Kind) display.BasicStyle {
	if sty, ok := _kindStyles[kind]; ok {
		return sty
	}
	panic(fmt.Sprintf("kind %s has no style", kind))
}

func groupReferences(root string, decl file.Declaration) []refGroup {
	byKey := map[string]*refGroup{}
	for _, ref := range decl.References() {
		if ref.Declaration() == nil {
			continue
		}
		target := ref.Declaration()
		key := locationKey(target.Location())
		if byKey[key] == nil {
			sty := relationshipStyle(root, ref.Parent(), ref.Declaration(), ref.Kind())
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

func relationshipStyle(root string, parent, target file.Declaration, kind file.Kind) display.Style {
	switch kind {
	case file.KindParameter:
		return kindStyle(file.KindParameter)
	case file.KindPackage:
		return packageDeclarationStyle(root, target)
	default:
		switch {
		case target == nil || target.Location() == nil:
			return _relationStyles[_relExternal]
		case isBuiltin(target):
			return display.White
		case sameFunction(parent, target):
			return _relationStyles[_relSameFunction]
		case sameFile(parent, target):
			return _relationStyles[_relSameFile]
		case samePackage(parent, target):
			return _relationStyles[_relSamePackage]
		case sameProject(root, parent, target):
			return _relationStyles[_relSameProject]
		default:
			return _relationStyles[_relExternal]
		}
	}
}

func packageDeclarationStyle(root string, target interface{ Location() file.Location }) display.Style {
	if target == nil || target.Location() == nil {
		return _relationStyles[_relExternal]
	}
	targetFile := filepath.Clean(target.Location().File())
	if targetFile == "" {
		return _relationStyles[_relExternal]
	}
	if root != "" {
		cleanRoot := filepath.Clean(root)
		if strings.HasPrefix(targetFile, cleanRoot+string(filepath.Separator)) {
			return _relationStyles[_relSameProject]
		}
	}
	return _relationStyles[_relExternal]
}

func isBuiltin(target file.Declaration) bool {
	p := path.Clean(target.Location().File())
	return p == "" || strings.Contains(p, "/src/builtin")
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
	collectCommentSpans(result.SourceSpans, sourceLines, f)
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
		addSpan(result.SourceSpans, sourceLines, mark.loc, mark.text, display.Span{Style: mark.textStyle})
	}

	for line := range result.SourceSpans {
		sortSpans(result.SourceSpans[line])
	}
	return result
}

func collectPackageReferenceSpans(out map[int][]display.Span, sourceLines []string, f file.File) {
	for _, ref := range f.PackageReferences() {
		addImportReferenceSpan(out, sourceLines, ref, display.Lavender)
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
	out[loc.Line()] = append(out[loc.Line()], display.Span{Start: start, End: start + len(ref.Text()), Style: style})
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

	plain := display.MutedOrange
	blue := display.Blue
	exhaustive := display.Green

	for _, block := range blocks {
		switch b := block.(type) {
		case file.IfBlock:
			for _, branch := range b.Branches() {
				addBranchMark := func(style display.Style) {
					*marks = append(*marks, newControlFlowMark(branch.Location(), branch.Keyword(), style))
					appendBraceMarks(marks, branch.OpenBrace(), branch.CloseBrace(), style)
				}
				switch {
				case branch.HasTerminalControlFlowStatement():
					addBranchMark(plain)
				default:
					addBranchMark(blue)
				}
			}
		case file.LoopBlock:
			addLoopMark := func(style display.Style) {
				*marks = append(*marks, newControlFlowMark(block.Location(), b.LoopKind(), style))
				appendBraceMarks(marks, block.OpenBrace(), block.CloseBrace(), style)
			}
			switch {
			case b.HasEscapingControlFlow():
				addLoopMark(plain)
			default:
				addLoopMark(blue)
			}
		case file.SwitchBlock:
			addSwitchMark := func(style display.Style) {
				*marks = append(*marks, newControlFlowMark(b.Location(), b.SwitchKind(), style))
				appendBraceMarks(marks, block.OpenBrace(), block.CloseBrace(), style)
			}
			switch {
			case b.IsExhaustive():
				addSwitchMark(exhaustive)
			default:
				addSwitchMark(plain)
			}
			for _, child := range b.Blocks() {
				if caseBlock, ok := child.(file.CaseBlock); ok {
					addCaseMark := func(keyword string, style display.Style) {
						*marks = append(*marks, newControlFlowMark(child.Location(), keyword, style))
					}
					switch {
					case caseBlock.IsDefault():
						addCaseMark("default", exhaustive)
					case caseBlock.HasFallthrough():
						addCaseMark("case", display.Blue)
					default:
						addCaseMark("case", plain)
					}
				}
			}
		}
		collectBlockMarks(block.Blocks(), marks)
		for _, stmt := range block.Statements() {
			addMark := func(style display.Style) {
				*marks = append(*marks, newControlFlowMark(stmt.Location(), stmt.Kind(), style))
			}
			switch stmt.Kind() {
			case "return":
				addMark(plain)
			case "fallthrough":
				addMark(blue)
			}
		}
		for _, stmt := range block.ControlFlowStatements() {
			addMark := func(style display.Style) {
				*marks = append(*marks, newControlFlowMark(stmt.Location(), stmt.Kind(), style))
			}
			switch stmt.Kind() {
			case "break":
				addMark(plain)
			case "continue":
				addMark(blue)
			}
		}
	}
}

func appendBraceMarks(marks *[]controlFlowMark, open, close file.Location, style display.Style) {
	if marks == nil || style == nil {
		return
	}
	if open != nil {
		*marks = append(*marks, newControlFlowMark(open, "{", style))
	}
	if close != nil {
		*marks = append(*marks, newControlFlowMark(close, "}", style))
	}
}

func addTopLevelStructFieldDeclarationSpans(out map[int][]display.Span, sourceLines []string, f file.File) {
	for _, named := range file.TopLevelNamedFields(f) {
		addSpan(out, sourceLines, named.Location(), named.Text(), display.Span{Style: declarationStyle(nil), Priority: 1})
	}
}

func collectCommentSpans(out map[int][]display.Span, sourceLines []string, f file.File) {
	for _, comment := range f.Comments() {
		start := comment.Start()
		end := comment.End()
		if start == nil || end == nil {
			continue
		}
		for line := start.Line(); line <= end.Line(); line++ {
			getStart := func(lineText string) int {
				spanStart := 0
				if line == start.Line() {
					spanStart = max(start.Column()-1, 0)
				}
				if spanStart > len(lineText) {
					spanStart = len(lineText)
				}
				return spanStart
			}
			getEnd := func(lineText string) int {
				spanEnd := len(lineText)
				if line == end.Line() {
					spanEnd = max(end.Column()-1, 0)
				}
				if spanEnd > len(lineText) {
					spanEnd = len(lineText)
				}
				return spanEnd
			}
			if line < 1 || line > len(sourceLines) {
				continue
			}
			lineText := sourceLines[line-1]
			start, end := getStart(lineText), getEnd(lineText)
			if end <= start {
				continue
			}
			out[line] = append(out[line], display.Span{Start: start, End: end, Style: display.Gray})
		}
	}
}
