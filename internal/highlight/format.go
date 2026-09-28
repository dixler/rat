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

var _kindStyles = map[file.Kind]display.BasicStyle{
	file.KindType:      display.LightGreen,
	file.KindVariable:  display.VibrantOrange,
	file.KindParameter: display.HotMagenta,
	file.KindPackage:   display.Purple,
	file.KindFile:      display.VibrantOrange,
}

var (
	builtinStyle      = display.MutedOrange
	sameFunctionStyle = display.VibrantOrange
	sameFileStyle     = display.LightGreen
	samePackageStyle  = display.Green
	sameProjectStyle  = display.Blue
	externalStyle     = display.Purple
	unknownStyle      = display.White
)

type ParseResult struct {
	Source      string
	SourceSpans map[int][]Span
}

type spanCollector struct {
	root  string
	lines []string
	spans map[int][]Span
}

type controlFlowMark struct {
	span      scan.Span
	textStyle display.BasicStyle
}

func declarationStyle(d file.Declaration) display.BasicStyle {
	switch {
	case d == nil || usesTopLevelSameFileStyle(d) || isTopLevelDeclaration(d) || d.Kind() == file.KindFunction:
		return sameFileStyle.Invert()
	case enclosingFunction(d) != nil && d.Kind() == file.KindVariable:
		return sameFunctionStyle.Invert()
	default:
		return kindStyle(d.Kind()).Invert()
	}
}

func isTopLevelDeclaration(d file.Declaration) bool {
	return d != nil && d.Parent() != nil && d.Parent().Kind() == file.KindFile
}

func usesTopLevelSameFileStyle(d file.Declaration) bool {
	if d == nil || d.Kind() == file.KindParameter {
		return false
	}
	if d.Kind() == file.KindFunction && isTopLevelDeclaration(d) {
		return true
	}
	hasTypeAncestor := false
	for curr := d; curr != nil; curr = curr.Parent() {
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

func (c *spanCollector) declaration(decl file.Declaration) {
	declStyle := declarationStyle(decl)
	if decl.ReferenceType() {
		declStyle = declStyle.Frame()
	}
	c.add(decl.Location(), decl.Name(), Span{Style: declStyle, Priority: 1})
	for _, ref := range decl.References() {
		span := relationshipStyle(c.root, ref)
		if ref.ReferenceType() {
			span.Style = span.Style.Frame()
		}
		c.add(ref.Location(), ref.Text(), span)
	}
	for _, child := range decl.Declarations() {
		c.declaration(child)
	}
}

func (c *spanCollector) add(loc file.Location, text string, span Span) {
	if loc == nil || text == "" {
		return
	}
	line := loc.Line()
	col := loc.Column()
	if line < 1 || col < 1 {
		return
	}
	start := col - 1
	if line <= len(c.lines) {
		lineText := c.lines[line-1]
		start = min(start, len(lineText))
		if !strings.HasPrefix(lineText[start:], text) {
			if idx := closestOccurrenceIndex(lineText, text, start); idx >= 0 {
				start = idx
			}
		}
	}
	span.Start = start
	span.End = start + len(text)
	c.spans[line] = append(c.spans[line], span)
}

func (c *spanCollector) addScan(src scan.Span, span Span) {
	if src.Line < 1 || src.Line > len(c.lines) || src.Column < 1 || src.Length < 1 {
		return
	}
	line := c.lines[src.Line-1]
	start := min(src.Column-1, len(line))
	end := min(start+src.Length, len(line))
	if end <= start {
		return
	}
	span.Start = start
	span.End = end
	c.spans[src.Line] = append(c.spans[src.Line], span)
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

func relationshipStyle(root string, r file.Reference) Span {
	switch r.Kind() {
	case file.KindParameter:
		return Span{Style: kindStyle(file.KindParameter)}
	case file.KindPackage:
		return Span{Style: packageDeclarationStyle(root, declarationLocation(r.Declaration()))}
	default:
		targetLoc := declarationLocation(r.Declaration())
		switch {
		case targetLoc == nil:
			return Span{Style: unknownStyle}
		case isBuiltinLocation(targetLoc):
			return Span{Style: builtinStyle}
		case sameFunction(r.Parent(), r.Declaration()):
			return Span{Style: sameFunctionStyle, Priority: 3}
		default:
			return Span{Style: distanceStyle(fieldTypeDistanceRank(root, declarationLocation(r.Parent()), targetLoc))}
		}
	}
}

func packageDeclarationStyle(root string, loc file.Location) display.BasicStyle {
	if !inProject(root, loc) {
		return externalStyle
	}
	return sameProjectStyle
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
	return ParseFormats(f), nil
}

func AnalyzeContent(path string, src []byte) (ParseResult, error) {
	f, err := file.NewContent(path, src)
	if err != nil {
		return ParseResult{}, err
	}
	return ParseFormats(f), nil
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
		s.End = min(s.End, len(line))
		if s.End <= s.Start {
			continue
		}
		out = append(out, s)
		idx = s.End
	}
	return out
}

func ParseFormats(f file.File) ParseResult {
	c := spanCollector{root: f.ProjectRoot(), lines: f.SourceLines(), spans: map[int][]Span{}}
	controlFlowMarks := collectNodeControlFlowMarks(f.Nodes())
	c.lexicalNodes(f.Nodes(), loopStyleByLocation(controlFlowMarks))
	for _, named := range f.TopLevelNamedFields() {
		style := fieldTypeDistanceStyle(c.root, named)
		if named.ReferenceType() {
			style = style.Frame()
		}
		c.add(named.Location(), named.Text(), Span{Style: style, Priority: 2})
	}
	for _, ref := range f.PackageReferences() {
		c.add(ref.Location(), ref.Text(), Span{Style: packageDeclarationStyle(c.root, ref.Package().Location()).Invert()})
	}

	for _, decl := range f.Declarations() {
		c.declaration(decl)
	}

	for _, mark := range controlFlowMarks {
		c.addScan(mark.span, Span{Style: mark.textStyle, Priority: 2})
	}

	for line, spans := range c.spans {
		sortSpans(spans)
		if line < 1 || line > len(c.lines) {
			delete(c.spans, line)
			continue
		}
		c.spans[line] = flattenSpans(c.lines[line-1], spans)
	}
	return ParseResult{Source: f.Source(), SourceSpans: c.spans}
}

func loopStyleByLocation(marks []controlFlowMark) map[string]display.BasicStyle {
	out := map[string]display.BasicStyle{}
	for _, mark := range marks {
		out[locationMapKey(mark.span.Line, mark.span.Column)] = mark.textStyle
	}
	return out
}

func collectNodeControlFlowMarks(nodes []scan.Node) []controlFlowMark {
	marks := make([]controlFlowMark, 0, len(nodes))
	incomplete := display.MutedOrange
	blue := display.Blue

	for _, node := range nodes {
		var style display.BasicStyle
		switch n := node.(type) {
		case scan.CondNode:
			style = blue
			if !n.IsGuard {
				style = incomplete
			}
		case scan.PartialNode:
			style = partialStyle(n.IsComplete)
		case scan.LoopNode:
			style = blue
			if n.HasExit {
				style = incomplete
			}
		case scan.JumpNode:
			switch n.Kind {
			case scan.JumpKindExit, scan.JumpKindContinue, scan.JumpKindFallthrough:
				style = blue
			case scan.JumpKindErrorExit, scan.JumpKindBreak:
				style = incomplete
			case scan.JumpKindEscape:
				style = display.LightRed
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

func fieldTypeDistanceStyle(root string, field file.NamedLocation) display.BasicStyle {
	distanceLoc := field.DistanceLocation()
	if distanceLoc == nil {
		distanceLoc = field.Location()
	}

	var target file.Location
	if distanceLoc != nil {
		maxRank := fieldTypeDistanceUnknown
		for _, t := range field.DeclarationLocations() {
			rank := fieldTypeDistanceRank(root, distanceLoc, t)
			if rank >= maxRank {
				target = t
				maxRank = rank
			}
		}
	}

	style := externalStyle
	switch {
	case isBuiltinLocation(target):
		style = builtinStyle
	case samePackageLocation(field.Location(), target) && sameFileLocation(distanceLoc, target):
		style = sameFileStyle
	case samePackageLocation(field.Location(), target) && samePackageLocation(distanceLoc, target):
		style = samePackageStyle
	case sameProjectLocation(root, distanceLoc, target):
		style = sameProjectStyle
	case target == nil:
		style = unknownStyle
	}
	if !field.Inline() {
		style = style.Invert()
	}
	return style
}

func (c *spanCollector) lexicalNodes(nodes []scan.Node, loopStyles map[string]display.BasicStyle) {
	for _, node := range nodes {
		style := lexicalNodeStyle(node, loopStyles)
		if style == "" {
			continue
		}
		for _, span := range node.Spans() {
			c.addScan(span, Span{Style: style, Priority: lexicalNodePriority(node)})
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
	case scan.PartialNode:
		return partialStyle(n.IsComplete)
	case scan.PackageNameNode:
		return samePackageStyle
	case scan.CommentNode:
		return display.Gray
	case scan.LoopOperatorNode:
		anchor := n.Anchor
		if anchor.Line < 1 || anchor.Column < 1 {
			anchor = n.Span
		}
		return loopStyles[locationMapKey(anchor.Line, anchor.Column)]
	case scan.CallParenNode:
		if n.Indirect {
			return kindStyle(file.KindParameter)
		}
		return display.VibrantOrange
	default:
		return ""
	}
}

func partialStyle(isComplete bool) display.BasicStyle {
	if isComplete {
		return display.Green
	}
	return display.MutedOrange
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

type fieldTypeDistance int

const (
	fieldTypeDistanceUnknown fieldTypeDistance = iota
	fieldTypeDistanceBuiltin
	fieldTypeDistanceSameFile
	fieldTypeDistanceSamePackage
	fieldTypeDistanceSameProject
	fieldTypeDistanceExternal
)

func distanceStyle(distance fieldTypeDistance) display.BasicStyle {
	return [...]display.BasicStyle{unknownStyle, builtinStyle, sameFileStyle, samePackageStyle, sameProjectStyle, externalStyle}[distance]
}

func fieldTypeDistanceRank(root string, source, target file.Location) fieldTypeDistance {
	switch {
	case target == nil:
		return fieldTypeDistanceUnknown
	case isBuiltinLocation(target):
		return fieldTypeDistanceBuiltin
	case sameFileLocation(source, target):
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
	return loc != nil && scan.IsBuiltinFile(loc.File())
}

func sameFileLocation(left, right file.Location) bool {
	return left != nil && right != nil && filepath.Clean(left.File()) == filepath.Clean(right.File())
}

func samePackageLocation(left, right file.Location) bool {
	return left != nil && right != nil && filepath.Dir(filepath.Clean(left.File())) == filepath.Dir(filepath.Clean(right.File()))
}

func sameProjectLocation(root string, left, right file.Location) bool {
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
