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
	span      scan.Span
	textStyle display.BasicStyle
}

func declarationStyle(d file.Declaration) display.BasicStyle {
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

func collectIndirectCallSpans(out map[int][]Span, sourceLines []string, call file.IndirectCall) {
	addSpan(out, sourceLines, call.Location(), call.Text(), Span{Style: display.White, Priority: 2})
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

func frameStyle(style display.BasicStyle) display.BasicStyle {
	return style.Frame()
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
		return Span{Style: packageDeclarationStyle(root, declarationLocation(r.Declaration()))}
	default:
		targetLoc := declarationLocation(r.Declaration())
		parentLoc := declarationLocation(r.Parent())
		switch {
		case targetLoc == nil:
			return Span{Style: _relationStyles[_relExternal]}
		case isBuiltinLocation(targetLoc):
			return Span{Style: display.MutedOrange}
		case sameFunction(r.Parent(), r.Declaration()):
			return Span{Style: _relationStyles[_relSameFunction], Priority: 3}
		case sameFileLocation(parentLoc, targetLoc):
			return Span{Style: _relationStyles[_relSameFile]}
		case samePackageLocation(parentLoc, targetLoc):
			return Span{Style: _relationStyles[_relSamePackage]}
		case sameProjectLocation(root, parentLoc, targetLoc):
			return Span{Style: _relationStyles[_relSameProject]}
		default:
			return Span{Style: _relationStyles[_relExternal]}
		}
	}
}

func packageDeclarationStyle(root string, loc file.Location) display.BasicStyle {
	if !inProject(root, loc) {
		return _relationStyles[_relExternal]
	}
	return _relationStyles[_relSameProject]
}

func sameFunction(left, right file.Declaration) bool {
	lfn := enclosingFunction(left)
	rfn := enclosingFunction(right)
	return lfn != nil && rfn != nil && locationKey(lfn.Location()) == locationKey(rfn.Location())
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

func declarationLocation(decl file.Declaration) file.Location {
	if decl == nil {
		return nil
	}
	return decl.Location()
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
	sourceLines := f.SourceLines()
	root := f.ProjectRoot()
	controlFlowMarks := collectNodeControlFlowMarks(f.Nodes())
	collectLexicalNodeSpans(result.SourceSpans, sourceLines, f.Nodes(), loopStyleByLocation(controlFlowMarks))
	addTopLevelStructFieldDeclarationSpans(root, result.SourceSpans, sourceLines, f)
	collectPackageReferenceSpans(root, result.SourceSpans, sourceLines, f)

	for _, call := range f.IndirectCalls() {
		collectIndirectCallSpans(result.SourceSpans, sourceLines, call)
	}

	for _, decl := range f.Declarations() {
		collectDeclarationSpans(root, result.SourceSpans, sourceLines, decl)
	}

	for _, mark := range controlFlowMarks {
		if mark.span.Line < 1 {
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

func loopStyleByLocation(marks []controlFlowMark) map[string]display.BasicStyle {
	out := map[string]display.BasicStyle{}
	for _, mark := range marks {
		out[locationMapKey(mark.span.Line, mark.span.Column)] = mark.textStyle
	}
	return out
}

func collectPackageReferenceSpans(root string, out map[int][]Span, sourceLines []string, f file.File) {
	for _, ref := range f.PackageReferences() {
		addSpan(out, sourceLines, ref.Location(), ref.Text(), Span{Style: packageDeclarationStyle(root, ref.Package().Location()).Invert()})
	}
}

func collectNodeControlFlowMarks(nodes []scan.Node) []controlFlowMark {
	marks := make([]controlFlowMark, 0, len(nodes))
	plain := display.MutedOrange
	blue := display.Blue
	exhaustive := display.Green

	for _, node := range nodes {
		var style display.BasicStyle
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
		if style == "" {
			continue
		}
		for _, span := range node.Spans() {
			if span.Line < 1 || span.Column < 1 || span.Length < 1 {
				continue
			}
			marks = append(marks, controlFlowMark{span: span, textStyle: style})
		}
	}
	sort.Slice(marks, func(i, j int) bool {
		if marks[i].span.Line != marks[j].span.Line {
			return marks[i].span.Line < marks[j].span.Line
		}
		return marks[i].span.Column < marks[j].span.Column
	})
	return marks
}

func addTopLevelStructFieldDeclarationSpans(root string, out map[int][]Span, sourceLines []string, f file.File) {
	for _, named := range f.TopLevelNamedFields() {
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

func fieldTypeDistanceStyle(root string, source file.Location, targets []file.Location, invert bool, packageResolution bool) display.BasicStyle {
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

func collectLexicalNodeSpans(out map[int][]Span, sourceLines []string, nodes []scan.Node, loopStyles map[string]display.BasicStyle) {
	for _, node := range nodes {
		style := lexicalNodeStyle(node, loopStyles)
		if style == "" {
			continue
		}
		for _, span := range node.Spans() {
			addScanSpan(out, sourceLines, span, Span{Style: style, Priority: lexicalNodePriority(node)})
		}
	}
}

func lexicalNodeStyle(node scan.Node, loopStyles map[string]display.BasicStyle) display.BasicStyle {
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
	case scan.CommentNode:
		return display.Gray
	case scan.LoopOperatorNode:
		anchor := n.Anchor
		if anchor.Line < 1 || anchor.Column < 1 {
			anchor = n.Span
		}
		return loopStyles[locationMapKey(anchor.Line, anchor.Column)]
	default:
		return ""
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

func fieldStyle(style display.BasicStyle, invert bool) display.BasicStyle {
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
	if left == nil || right == nil {
		return false
	}
	return inProject(root, left) && inProject(root, right)
}

func inProject(root string, loc file.Location) bool {
	if root == "" || loc == nil {
		return false
	}
	file := filepath.Clean(loc.File())
	root = filepath.Clean(root)
	return file != "" && strings.HasPrefix(file, root+string(filepath.Separator))
}
