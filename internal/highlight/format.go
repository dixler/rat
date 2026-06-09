package highlight

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"rat/internal/display"
	"rat/internal/file"
	"rat/internal/file/scan"
)

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
	file.KindVariable:  display.VibrantOrange,
	file.KindParameter: display.HotMagenta,
	file.KindFunction:  display.Yellow,
	file.KindPackage:   display.Purple,
	file.KindFile:      display.VibrantOrange,
}

var _relationStyles = map[relation]display.BasicStyle{
	_relSameFunction: display.VibrantOrange,
	_relSameFile:     display.LightGreen,
	_relSamePackage:  display.Green,
	_relSameProject:  display.Blue,
	_relExternal:     display.Purple,
}

type ParseResult struct {
	Source      string
	SourceSpans map[int][]Span
}

type controlFlowMark struct {
	loc       file.Location
	span      scan.Span
	textStyle display.Style
	lineStyle display.Style
}

func declarationStyle(d file.Declaration) display.Style {
	switch {
	case d == nil:
		return _relationStyles[_relSameFile].Invert()
	case usesTopLevelSameFileStyle(d):
		return _relationStyles[_relSameFile].Invert()
	case isTopLevelDeclaration(d):
		return _relationStyles[_relSameFile].Invert()
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

func sortSpans(spans []Span) {
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

func collectIndirectCallSpans(out map[int][]Span, call file.IndirectCall) {
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

	out[line] = append(out[line], Span{
		Start:    col - 1,
		End:      col - 1 + len(text),
		Style:    display.White,
		Priority: 2,
	})
}

func collectDeclarationSpans(root string, out map[int][]Span, sourceLines []string, decl file.Declaration) {
	declStyle := declarationStyle(decl)
	if decl.ReferenceType() {
		declStyle = frameStyle(declStyle)
	}
	addSpan(out, sourceLines, decl.Location(), decl.Name(), Span{Style: declStyle, Priority: 1})
	for _, ref := range decl.References() {
		ref := reference{Reference: ref}
		span := ref.relationshipStyle(root)
		if ref.ReferenceType() {
			span.Style = frameStyle(span.Style)
		}
		addSpan(out, sourceLines, ref.Location(), ref.Text(), span)
	}
	for _, child := range decl.Declarations() {
		collectDeclarationSpans(root, out, sourceLines, child)
	}
}

func frameStyle(style display.Style) display.Style {
	if basic, ok := style.(display.BasicStyle); ok {
		return basic.Frame()
	}
	return style
}

func addSpan(out map[int][]Span, sourceLines []string, loc file.Location, text string, span Span) {
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

func addScanSpan(out map[int][]Span, sourceLines []string, src scan.Span, span Span) {
	if src.Line < 1 || src.Line > len(sourceLines) || src.Column < 1 || src.Length < 1 {
		return
	}
	line := sourceLines[src.Line-1]
	start := src.Column - 1
	if start < 0 {
		start = 0
	}
	if start > len(line) {
		start = len(line)
	}
	end := start + src.Length
	if end > len(line) {
		end = len(line)
	}
	if end <= start {
		return
	}
	span.Start = start
	span.End = end
	out[src.Line] = append(out[src.Line], span)
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

type reference struct {
	file.Reference
}

func (r *reference) relationshipStyle(root string) Span {
	switch r.Kind() {
	case file.KindParameter:
		return Span{Style: kindStyle(file.KindParameter)}
	case file.KindPackage:
		return Span{Style: packageDeclarationStyle(root, r.Declaration())}
	default:
		target, parent := r.Declaration(), r.Parent()
		switch {
		case target == nil || target.Location() == nil:
			return Span{Style: _relationStyles[_relExternal]}
		case isBuiltin(target):
			return Span{Style: display.MutedOrange}
		case sameFunction(parent, target):
			return Span{Style: _relationStyles[_relSameFunction], Priority: 3}
		case sameFile(parent, target):
			return Span{Style: _relationStyles[_relSameFile]}
		case samePackage(parent, target):
			return Span{Style: _relationStyles[_relSamePackage]}
		case sameProject(root, parent, target):
			return Span{Style: _relationStyles[_relSameProject]}
		default:
			return Span{Style: _relationStyles[_relExternal]}
		}
	}
}

func packageDeclarationStyle(root string, target interface{ Location() file.Location }) display.BasicStyle {
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
	return scan.IsBuiltinFile(target.Location().File())
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

func Analyze(path string) (ParseResult, error) {
	f, err := file.New(path)
	if err != nil {
		return ParseResult{}, err
	}
	res := ParseFormats(f)
	return res, nil
}

func flattenSpans(line string, spans []Span) []Span {
	if len(spans) == 0 {
		return nil
	}
	out := make([]Span, 0, len(spans))
	idx := 0
	for _, s := range spans {
		if s.Start < idx || s.Start >= len(line) {
			continue
		}
		if s.End > len(line) {
			s.End = len(line)
		}
		if s.End <= s.Start {
			continue
		}
		out = append(out, s)
		idx = s.End
	}
	return out
}

func ParseFormats(f file.File) ParseResult {
	result := ParseResult{
		Source:      f.Source(),
		SourceSpans: map[int][]Span{},
	}
	sourceLines := strings.Split(f.Source(), "\n")
	root := file.ProjectRoot(f.Name())
	controlFlowMarks := collectNodeControlFlowMarks(f.Nodes())
	collectLexicalNodeSpans(result.SourceSpans, sourceLines, f.Nodes(), loopStyleByLocation(controlFlowMarks))
	addTopLevelStructFieldDeclarationSpans(root, result.SourceSpans, sourceLines, f)
	collectPackageReferenceSpans(root, result.SourceSpans, sourceLines, f)

	for _, call := range f.IndirectCalls() {
		collectIndirectCallSpans(result.SourceSpans, call)
	}

	for _, decl := range f.Declarations() {
		collectDeclarationSpans(root, result.SourceSpans, sourceLines, decl)
	}

	for _, mark := range controlFlowMarks {
		if mark.loc == nil || mark.loc.Line() < 1 {
			continue
		}
		addScanSpan(result.SourceSpans, sourceLines, mark.span, Span{Style: mark.textStyle, Priority: 2})
	}

	for line := range result.SourceSpans {
		sortSpans(result.SourceSpans[line])
		if line < 1 || line > len(sourceLines) {
			delete(result.SourceSpans, line)
			continue
		}
		result.SourceSpans[line] = flattenSpans(sourceLines[line-1], result.SourceSpans[line])
	}
	return result
}

func loopStyleByLocation(marks []controlFlowMark) map[string]display.Style {
	out := map[string]display.Style{}
	for _, mark := range marks {
		if mark.loc == nil {
			continue
		}
		out[locationMapKey(mark.loc.Line(), mark.loc.Column())] = mark.textStyle
	}
	return out
}

func collectPackageReferenceSpans(root string, out map[int][]Span, sourceLines []string, f file.File) {
	for _, ref := range f.PackageReferences() {
		addSpan(out, sourceLines, ref.Location(), ref.Text(), Span{Style: packageDeclarationStyle(root, ref.Package()).Invert()})
	}
}

func collectNodeControlFlowMarks(nodes []scan.Node) []controlFlowMark {
	marks := make([]controlFlowMark, 0, len(nodes))
	plain := display.MutedOrange
	blue := display.Blue
	exhaustive := display.Green

	for _, node := range nodes {
		var style display.Style
		switch n := node.(type) {
		case scan.CondNode:
			style = blue
			if !n.IsGuard {
				style = plain
			}
		case scan.MatchNode:
			style = plain
			if n.HasDefault {
				style = exhaustive
			}
		case scan.LoopNode:
			style = blue
			if n.HasExit {
				style = plain
			}
		case scan.JumpNode:
			switch n.Kind {
			case scan.JumpKindExit:
				style = blue
			case scan.JumpKindErrorExit:
				style = plain
			case scan.JumpKindContinue:
				style = blue
			case scan.JumpKindBreak:
				style = plain
			case scan.JumpKindEscape:
				style = display.LightRed
			case scan.JumpKindFallthrough:
				style = blue
			}
		default:
			continue
		}
		if style == nil {
			continue
		}
		for _, span := range node.Spans() {
			if span.Line < 1 || span.Column < 1 || span.Length < 1 {
				continue
			}
			loc := scanNodeLocation{line: span.Line, column: span.Column}
			marks = append(marks, controlFlowMark{loc: loc, span: span, textStyle: style, lineStyle: style})
		}
	}
	sort.Slice(marks, func(i, j int) bool {
		if marks[i].loc.Line() != marks[j].loc.Line() {
			return marks[i].loc.Line() < marks[j].loc.Line()
		}
		return marks[i].loc.Column() < marks[j].loc.Column()
	})
	return marks
}

type scanNodeLocation struct {
	line   int
	column int
}

func (l scanNodeLocation) File() string { return "" }
func (l scanNodeLocation) Line() int    { return l.line }
func (l scanNodeLocation) Column() int  { return l.column }

func addTopLevelStructFieldDeclarationSpans(root string, out map[int][]Span, sourceLines []string, f file.File) {
	for _, named := range file.TopLevelNamedFields(f) {
		distanceLoc := named.DistanceLocation()
		externalStructInstantiation := distanceLoc != nil && !samePackageLocation(named.Location(), distanceLoc)
		if distanceLoc == nil {
			distanceLoc = named.Location()
		}
		style := fieldTypeDistanceStyle(root, distanceLoc, named.DeclarationLocations(), !named.Inline(), externalStructInstantiation)
		if named.ReferenceType() {
			style = frameStyle(style)
		}
		addSpan(out, sourceLines, named.Location(), named.Text(), Span{Style: style, Priority: 2})
	}
}

func fieldTypeDistanceStyle(root string, source file.Location, targets []file.Location, invert bool, packageResolution bool) display.Style {
	rank := fieldTypeDistanceBuiltin
	if source == nil {
		rank = fieldTypeDistanceExternal
	} else {
		for _, target := range targets {
			rank = max(rank, fieldTypeDistanceRank(root, source, target, packageResolution))
		}
	}

	switch rank {
	case fieldTypeDistanceBuiltin:
		return fieldStyle(display.MutedOrange, invert)
	case fieldTypeDistanceSameFile:
		return fieldStyle(_relationStyles[_relSameFile], invert)
	case fieldTypeDistanceSamePackage:
		return fieldStyle(_relationStyles[_relSamePackage], invert)
	case fieldTypeDistanceSameProject:
		return fieldStyle(_relationStyles[_relSameProject], invert)
	default:
		return fieldStyle(display.Purple, invert)
	}
}

func collectLexicalNodeSpans(out map[int][]Span, sourceLines []string, nodes []scan.Node, loopStyles map[string]display.Style) {
	for _, node := range nodes {
		style := lexicalNodeStyle(node, loopStyles)
		if style == nil {
			continue
		}
		for _, span := range node.Spans() {
			addScanSpan(out, sourceLines, span, Span{Style: style, Priority: lexicalNodePriority(node)})
		}
	}
}

func lexicalNodeStyle(node scan.Node, loopStyles map[string]display.Style) display.Style {
	switch n := node.(type) {
	case scan.DeclarationSyntaxNode:
		return display.MutedOrange
	case scan.MutableTypeSyntaxNode:
		return display.MutedOrange.Frame()
	case scan.FunctionSyntaxNode:
		if n.ReturnsError {
			return display.MutedOrange
		}
		return display.Blue
	case scan.InlineFunctionIndentNode:
		return display.White.Invert()
	case scan.ProgramSyntaxNode:
		return display.Blue
	case scan.EscapeSyntaxNode:
		return display.LightRed
	case scan.LiteralNode:
		return display.LightPink
	case scan.PackageNameNode:
		return _relationStyles[_relSamePackage]
	case scan.BuiltinNode:
		return display.MutedOrange
	case scan.CommentNode:
		return display.Gray
	case scan.LoopOperatorNode:
		anchor := n.Anchor
		if anchor.Line < 1 || anchor.Column < 1 {
			anchor = n.Span
		}
		return loopStyles[locationMapKey(anchor.Line, anchor.Column)]
	default:
		return nil
	}
}

func lexicalNodePriority(node scan.Node) int {
	switch node.(type) {
	case scan.FunctionSyntaxNode, scan.InlineFunctionIndentNode:
		return 2
	default:
		return 0
	}
}

func locationMapKey(line, col int) string {
	return fmt.Sprintf("%d:%d", line, col)
}

func fieldStyle(style display.BasicStyle, invert bool) display.Style {
	if invert {
		return style.Invert()
	}
	return style
}

type fieldTypeDistance int

const (
	fieldTypeDistanceBuiltin fieldTypeDistance = iota
	fieldTypeDistanceSameFile
	fieldTypeDistanceSamePackage
	fieldTypeDistanceSameProject
	fieldTypeDistanceExternal
)

func fieldTypeDistanceRank(root string, source, target file.Location, packageResolution bool) fieldTypeDistance {
	switch {
	case target == nil:
		return fieldTypeDistanceExternal
	case isBuiltinLocation(target):
		return fieldTypeDistanceBuiltin
	case !packageResolution && sameFileLocation(source, target):
		return fieldTypeDistanceSameFile
	case samePackageLocation(source, target):
		return fieldTypeDistanceSamePackage
	case sameProjectLocation(root, source, target):
		return fieldTypeDistanceSameProject
	default:
		return fieldTypeDistanceExternal
	}
}

func isBuiltinLocation(loc file.Location) bool {
	if loc == nil {
		return false
	}
	return scan.IsBuiltinFile(loc.File())
}

func sameFileLocation(left, right file.Location) bool {
	if left == nil || right == nil {
		return false
	}
	return filepath.Clean(left.File()) == filepath.Clean(right.File())
}

func samePackageLocation(left, right file.Location) bool {
	if left == nil || right == nil {
		return false
	}
	return filepath.Dir(filepath.Clean(left.File())) == filepath.Dir(filepath.Clean(right.File()))
}

func sameProjectLocation(root string, left, right file.Location) bool {
	if root == "" || left == nil || right == nil {
		return false
	}
	lfile := filepath.Clean(left.File())
	rfile := filepath.Clean(right.File())
	root = filepath.Clean(root)
	return strings.HasPrefix(lfile, root+string(filepath.Separator)) && strings.HasPrefix(rfile, root+string(filepath.Separator))
}
