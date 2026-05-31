package highlight

import (
	"fmt"
	"go/scanner"
	gotoken "go/token"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"rat/internal/display"
	"rat/internal/file"
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

	out[line] = append(out[line], display.Span{
		Start:    col - 1,
		End:      col - 1 + len(text),
		Style:    display.HotMagenta,
		Priority: 2,
	})
}

func collectDeclarationSpans(root string, out map[int][]display.Span, sourceLines []string, decl file.Declaration) {
	addSpan(out, sourceLines, decl.Location(), decl.Name(), display.Span{Style: declarationStyle(decl), Priority: 1})
	for _, ref := range decl.References() {
		addSpan(out, sourceLines, ref.Location(), ref.Text(), relationshipStyle(root, ref.Parent(), ref.Declaration(), ref.Kind()))
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

func relationshipStyle(root string, parent, target file.Declaration, kind file.Kind) display.Span {
	switch kind {
	case file.KindParameter:
		return display.Span{Style: kindStyle(file.KindParameter)}
	case file.KindPackage:
		return display.Span{Style: packageDeclarationStyle(root, target)}
	default:
		switch {
		case target == nil || target.Location() == nil:
			return display.Span{Style: _relationStyles[_relExternal]}
		case isBuiltin(target):
			return display.Span{Style: display.MutedOrange}
		case sameFunction(parent, target):
			return display.Span{Style: _relationStyles[_relSameFunction], Priority: 3}
		case sameFile(parent, target):
			return display.Span{Style: _relationStyles[_relSameFile]}
		case samePackage(parent, target):
			return display.Span{Style: _relationStyles[_relSamePackage]}
		case sameProject(root, parent, target):
			return display.Span{Style: _relationStyles[_relSameProject]}
		default:
			return display.Span{Style: _relationStyles[_relExternal]}
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

func Analyze(path string) (ParseResult, error) {
	f, err := file.Analyze(path)
	if err != nil {
		return ParseResult{}, err
	}
	res := ParseFormats(f)
	lines := strings.Split(res.Source, "\n")
	for i, line := range lines {
		lineNo := i + 1
		res.SourceSpans[lineNo] = FlattenSpans(line, res.SourceSpans[lineNo])
	}
	return res, nil
}

func FlattenSpans(line string, spans []display.Span) []display.Span {
	if len(spans) == 0 {
		return nil
	}
	out := make([]display.Span, 0, len(spans))
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
		SourceSpans: map[int][]display.Span{},
		LineSpans:   map[int]display.Style{},
		LineMarkers: map[int]string{},
	}
	sourceLines := strings.Split(f.Source(), "\n")
	root := file.ProjectRoot(f.Name())
	controlFlowMarks := collectControlFlowMarks(f)
	collectCommentSpans(result.SourceSpans, sourceLines, f)
	collectLexicalTokenSpans(result.SourceSpans, f.Source(), sourceLines, loopStyleByLocation(controlFlowMarks))
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
		result.LineSpans[mark.loc.Line()] = mark.lineStyle
		addSpan(result.SourceSpans, sourceLines, mark.loc, mark.text, display.Span{Style: mark.textStyle, Priority: 2})
	}

	for line := range result.SourceSpans {
		sortSpans(result.SourceSpans[line])
		if line < 1 || line > len(sourceLines) {
			delete(result.SourceSpans, line)
			continue
		}
		result.SourceSpans[line] = display.FlattenSpans(sourceLines[line-1], result.SourceSpans[line])
	}
	return result
}

func loopStyleByLocation(marks []controlFlowMark) map[string]display.Style {
	out := map[string]display.Style{}
	for _, mark := range marks {
		if mark.loc == nil || mark.text != "for" {
			continue
		}
		out[locationMapKey(mark.loc.Line(), mark.loc.Column())] = mark.textStyle
	}
	return out
}

func collectPackageReferenceSpans(root string, out map[int][]display.Span, sourceLines []string, f file.File) {
	for _, ref := range f.PackageReferences() {
		addImportReferenceSpan(out, sourceLines, ref, packageDeclarationStyle(root, ref.Package()).Invert())
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
				if stmt.ReturnsError() {
					addMark(plain)
				} else {
					addMark(blue)
				}
			case "fallthrough":
				addMark(blue)
			case "panic":
				addMark(display.LightRed)
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
			case "goto", "panic":
				addMark(display.LightRed)
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

func addTopLevelStructFieldDeclarationSpans(root string, out map[int][]display.Span, sourceLines []string, f file.File) {
	for _, named := range file.TopLevelNamedFields(f) {
		distanceLoc := named.DistanceLocation()
		if distanceLoc == nil {
			distanceLoc = named.Location()
		}
		addSpan(out, sourceLines, named.Location(), named.Text(), display.Span{Style: fieldTypeDistanceStyle(root, distanceLoc, named.DeclarationLocations(), !named.Inline()), Priority: 1})
	}
}

func fieldTypeDistanceStyle(root string, source file.Location, targets []file.Location, invert bool) display.Style {
	rank := fieldTypeDistanceBuiltin
	if source == nil {
		rank = fieldTypeDistanceExternal
	} else {
		for _, target := range targets {
			rank = max(rank, fieldTypeDistanceRank(root, source, target))
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

var keywordStyles = map[gotoken.Token]display.Style{
	gotoken.TYPE:      display.MutedOrange,
	gotoken.STRUCT:    display.MutedOrange,
	gotoken.FUNC:      display.MutedOrange,
	gotoken.INTERFACE: display.MutedOrange,
	gotoken.MAP:       display.MutedOrange,
	gotoken.VAR:       display.MutedOrange,
	gotoken.PACKAGE:   display.MutedOrange,
	gotoken.IMPORT:    display.MutedOrange,
	gotoken.DEFER:     display.Blue,
	gotoken.GO:        display.Blue,
	gotoken.CONST:     display.Blue,
	gotoken.GOTO:      display.LightRed,
}

var literalTokens = map[gotoken.Token]bool{
	gotoken.CHAR:   true,
	gotoken.FLOAT:  true,
	gotoken.IMAG:   true,
	gotoken.INT:    true,
	gotoken.STRING: true,
}

func collectLexicalTokenSpans(out map[int][]display.Span, source string, sourceLines []string, loopStyles map[string]display.Style) {
	fset := gotoken.NewFileSet()
	file := fset.AddFile("", fset.Base(), len(source))
	var s scanner.Scanner
	s.Init(file, []byte(source), nil, 0)

	var pendingForStyle display.Style
	pendingPackageName := false
	pendingImportSpec := false
	importBlockDepth := 0
	for {
		pos, tok, lit := s.Scan()
		if tok == gotoken.EOF {
			break
		}

		p := fset.Position(pos)
		text := lit
		if text == "" {
			text = tok.String()
		}

		if tok == gotoken.FOR {
			pendingForStyle = loopStyles[locationMapKey(p.Line, p.Column)]
		}
		if tok == gotoken.PACKAGE {
			pendingPackageName = true
		}
		if tok == gotoken.IMPORT {
			pendingImportSpec = true
		}
		if pendingImportSpec && tok == gotoken.LPAREN {
			importBlockDepth = 1
			pendingImportSpec = false
		} else if importBlockDepth > 0 && tok == gotoken.LPAREN {
			importBlockDepth++
		} else if importBlockDepth > 0 && tok == gotoken.RPAREN {
			importBlockDepth--
		}

		style, ok := keywordStyles[tok]
		if pendingPackageName && tok == gotoken.IDENT {
			style, ok = _relationStyles[_relSamePackage], true
			pendingPackageName = false
		} else if tok == gotoken.RANGE && pendingForStyle != nil {
			style, ok = pendingForStyle, true
			pendingForStyle = nil
		} else if tok == gotoken.STRING && (pendingImportSpec || importBlockDepth > 0) {
			ok = false
		} else if literalTokens[tok] {
			style, ok = display.LightPink, true
		}
		if pendingImportSpec && (tok == gotoken.STRING || tok == gotoken.SEMICOLON) {
			pendingImportSpec = false
		}
		if !ok || text == "" {
			continue
		}
		addTokenSpan(out, sourceLines, p.Line, p.Column, text, display.Span{Style: style})
	}
}

func addTokenSpan(out map[int][]display.Span, sourceLines []string, line, col int, text string, span display.Span) {
	if line < 1 || col < 1 || text == "" {
		return
	}
	parts := strings.Split(text, "\n")
	for i, part := range parts {
		lineNo := line + i
		if lineNo < 1 || lineNo > len(sourceLines) {
			continue
		}
		lineText := sourceLines[lineNo-1]
		start := 0
		if i == 0 {
			start = col - 1
			if start < 0 {
				start = 0
			}
			if start > len(lineText) {
				start = len(lineText)
			}
		}
		end := len(lineText)
		if i == len(parts)-1 {
			end = start + len(part)
			if end > len(lineText) {
				end = len(lineText)
			}
		}
		if end <= start {
			continue
		}
		span.Start = start
		span.End = end
		out[lineNo] = append(out[lineNo], span)
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

func fieldTypeDistanceRank(root string, source, target file.Location) fieldTypeDistance {
	switch {
	case target == nil:
		return fieldTypeDistanceExternal
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
	if loc == nil {
		return false
	}
	p := path.Clean(loc.File())
	return p == "" || strings.Contains(p, "/src/builtin")
}

func sameFileLocation(left, right file.Location) bool {
	if left == nil || right == nil {
		return false
	}
	return filepath.Clean(left.File()) == filepath.Clean(right.File())
}

func samePackageLocation(left, right file.Location) bool {
	if sameFileLocation(left, right) || left == nil || right == nil {
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
