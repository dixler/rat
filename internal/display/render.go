package display

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"notectl/internal/file"
)

const (
	reset  = "\x1b[0m"
	bold   = "\x1b[1m"
	gray   = "\x1b[90m"
	orange = "\x1b[38;5;208m"
	green  = "\x1b[32m"
	cyan   = "\x1b[36m"
	blue   = "\x1b[34m"
	purple = "\x1b[35m"
	white  = "\x1b[97m"
)

type span struct {
	start int
	end   int
	color string
	isDef bool
}

type refGroup struct {
	decl  file.Declaration
	color string
	refs  []file.Reference
}

func RenderFile(f file.File) {
	root := projectRoot(f.Name())
	printHeader(f)
	printTree(root, f.Tree(), 0)
	printImports(f.PackageReferences())
	printSource(root, f.Source(), f.Declarations())
}

func printHeader(f file.File) {
	fmt.Printf("%s%s%s\n", bold, f.Name(), reset)
}

func printTree(root string, d file.Declaration, depth int) {
	indent := strings.Repeat("  ", depth)
	fmt.Printf("%s%s%s%s %s%s:%d:%d%s\n", indent, colorFor(string(d.Kind())), d.Kind(), reset, gray, d.Location().File(), d.Location().Line(), d.Location().Column(), reset)
	for _, group := range groupReferences(root, d) {
		fmt.Printf("%s  %s%s%s %s%s:%d:%d%s\n", indent, group.color, group.decl.Name(), reset, gray, group.decl.Location().File(), group.decl.Location().Line(), group.decl.Location().Column(), reset)
		for _, ref := range group.refs {
			color, ok := relationshipColor(root, ref.Parent(), ref.Declaration(), ref.Kind())
			if !ok {
				continue
			}
			fmt.Printf("%s    %s%s:%d:%d%s\n", indent, color, ref.Location().File(), ref.Location().Line(), ref.Location().Column(), reset)
		}
	}
	for _, child := range d.Declarations() {
		printTree(root, child, depth+1)
	}
}

func printImports(refs []file.PackageReference) {
	if len(refs) == 0 {
		return
	}
	fmt.Printf("%sImports%s\n", bold, reset)
	for _, ref := range refs {
		fmt.Printf("- %s%s%s -> %s\n", purple, ref.Text(), reset, ref.Package().Name())
	}
}

func printSource(root, src string, decls []file.Declaration) {
	if src == "" {
		return
	}
	fmt.Printf("%sSource%s\n", bold, reset)
	lines := strings.Split(src, "\n")
	spansByLine := collectSpans(root, decls)
	for i, line := range lines {
		fmt.Printf("%4d  %s\n", i+1, colorLine(line, spansByLine[i+1]))
	}
}

func collectSpans(root string, decls []file.Declaration) map[int][]span {
	out := map[int][]span{}
	for _, decl := range decls {
		collectDeclarationSpans(root, out, decl)
	}
	for line := range out {
		sort.Slice(out[line], func(i, j int) bool {
			if out[line][i].start != out[line][j].start {
				return out[line][i].start < out[line][j].start
			}
			if out[line][i].isDef != out[line][j].isDef {
				return out[line][i].isDef
			}
			return out[line][i].end < out[line][j].end
		})
	}
	return out
}

func collectDeclarationSpans(root string, out map[int][]span, decl file.Declaration) {
	addSpan(out, decl.Location(), decl.Name(), colorFor(string(decl.Kind())), true)
	for _, ref := range decl.References() {
		color, ok := relationshipColor(root, ref.Parent(), ref.Declaration(), ref.Kind())
		if !ok {
			continue
		}
		addSpan(out, ref.Location(), ref.Text(), color, false)
	}
	for _, child := range decl.Declarations() {
		collectDeclarationSpans(root, out, child)
	}
}

func addSpan(out map[int][]span, loc file.Location, text, color string, isDef bool) {
	if loc == nil || text == "" {
		return
	}
	line := loc.Line()
	col := loc.Column()
	if line < 1 || col < 1 {
		return
	}
	out[line] = append(out[line], span{start: col - 1, end: col - 1 + len(text), color: color, isDef: isDef})
}

func colorLine(line string, spans []span) string {
	if len(spans) == 0 {
		return line
	}
	var b strings.Builder
	idx := 0
	for _, s := range spans {
		if s.start < idx || s.start >= len(line) {
			continue
		}
		if s.end > len(line) {
			s.end = len(line)
		}
		if s.end <= s.start {
			continue
		}
		b.WriteString(line[idx:s.start])
		if s.isDef {
			b.WriteString(s.color)
			b.WriteString(line[s.start:s.end])
			b.WriteString(reset)
		} else {
			b.WriteString(white)
			b.WriteString(bgFor(s.color))
			b.WriteString(line[s.start:s.end])
			b.WriteString(reset)
		}
		idx = s.end
	}
	b.WriteString(line[idx:])
	return b.String()
}

func bgFor(color string) string {
	switch color {
	case orange:
		return "\x1b[48;5;208m"
	case green:
		return "\x1b[42m"
	case cyan:
		return "\x1b[46m"
	case blue:
		return "\x1b[44m"
	case purple:
		return "\x1b[45m"
	default:
		return "\x1b[100m"
	}
}

func colorFor(kind string) string {
	switch kind {
	case "type":
		return cyan
	case "function":
		return green
	case "package":
		return purple
	default:
		return orange
	}
}

func groupReferences(root string, decl file.Declaration) []refGroup {
	byKey := map[string]*refGroup{}
	for _, ref := range decl.References() {
		color, ok := relationshipColor(root, ref.Parent(), ref.Declaration(), ref.Kind())
		if !ok || ref.Declaration() == nil {
			continue
		}
		target := ref.Declaration()
		key := locationKey(target.Location())
		if byKey[key] == nil {
			byKey[key] = &refGroup{decl: target, color: color}
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

func relationshipColor(root string, parent, target file.Declaration, kind file.Kind) (string, bool) {
	if kind == file.KindPackage {
		return purple, true
	}
	if target == nil || target.Location() == nil {
		return "", false
	}
	path := filepath.Clean(target.Location().File())
	if path == "" || strings.Contains(path, "/src/builtin") {
		return "", false
	}
	if sameFunction(parent, target) {
		return orange, true
	}
	if sameFile(parent, target) {
		return green, true
	}
	if samePackage(parent, target) {
		return cyan, true
	}
	if sameProject(root, parent, target) {
		return blue, true
	}
	return purple, true
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
	if !sameFile(left, right) {
		if left == nil || right == nil || left.Location() == nil || right.Location() == nil {
			return false
		}
		return filepath.Dir(filepath.Clean(left.Location().File())) == filepath.Dir(filepath.Clean(right.Location().File()))
	}
	return false
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
