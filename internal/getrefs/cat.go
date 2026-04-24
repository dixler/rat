package getrefs

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const clrPlain = "\x1b[97m"

type lineSpan struct {
	start int
	end   int
	color string
	isDef bool
}

type funcScope struct{ start, end int }
type localResolver struct {
	defs     map[string]Location
	eligible map[string]bool
}

func newLocalResolver(file string) defResolver {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		return localResolver{defs: map[string]Location{}, eligible: map[string]bool{}}
	}
	info := &types.Info{
		Defs: map[*ast.Ident]types.Object{},
		Uses: map[*ast.Ident]types.Object{},
	}
	cfg := &types.Config{Importer: importer.Default(), Error: func(error) {}}
	_, _ = cfg.Check(filepath.Dir(file), fset, []*ast.File{f}, info)
	defLocByObj := map[types.Object]Location{}
	for id, obj := range info.Defs {
		if id == nil || obj == nil {
			continue
		}
		defLocByObj[obj] = identLoc(file, fset, id)
	}
	defs := map[string]Location{}
	eligible := map[string]bool{}
	for id, obj := range info.Defs {
		if id == nil || obj == nil {
			continue
		}
		loc := identLoc(file, fset, id)
		defs[locKey(loc)] = defLocByObj[obj]
		if sameFunctionEligible(obj) {
			eligible[locKey(loc)] = true
		}
	}
	for id, obj := range info.Uses {
		if id == nil || obj == nil {
			continue
		}
		loc := identLoc(file, fset, id)
		if def, ok := defLocByObj[obj]; ok {
			defs[locKey(loc)] = def
			if sameFunctionEligible(obj) {
				eligible[locKey(loc)] = true
			}
		}
	}
	return localResolver{defs: defs, eligible: eligible}
}

func (r localResolver) definitionAt(loc Location) Location {
	if def, ok := r.defs[locKey(loc)]; ok {
		return def
	}
	return loc
}

func (r localResolver) sameFunctionEligible(loc Location) bool { return r.eligible[locKey(loc)] }

func sameFunctionEligible(obj types.Object) bool {
	v, ok := obj.(*types.Var)
	return ok && !v.IsField()
}

func RenderFileCat(r defResolver, repoRoot, file string) error {
	b, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	lines := strings.Split(string(b), "\n")
	spansByLine := collectColoredSpans(r, repoRoot, file)
	printFileLegend()
	for i, line := range lines {
		fmt.Printf("%4d  %s\n", i+1, colorLine(line, spansByLine[i+1]))
	}
	return nil
}

func collectColoredSpans(r defResolver, repoRoot, file string) map[int][]lineSpan {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		return nil
	}
	bounds := functionScopes(fset, f)
	spansByLine := map[int][]lineSpan{}
	ast.Inspect(f, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || id.Name == "_" {
			return true
		}
		start := fset.Position(id.Pos())
		end := fset.Position(id.End())
		if start.Line != end.Line || start.Line < 1 || start.Column < 1 || end.Column <= start.Column {
			return true
		}
		loc := identLoc(file, fset, id)
		def := r.definitionAt(loc)
		eligible := true
		if lr, ok := r.(localResolver); ok {
			eligible = lr.sameFunctionEligible(loc)
		}
		cat, color := classifyColor(repoRoot, def, loc, findFuncScope(bounds, start.Line), eligible)
		if cat == "" {
			return true
		}
		spansByLine[start.Line] = append(spansByLine[start.Line], lineSpan{
			start: start.Column - 1,
			end:   end.Column - 1,
			color: color,
			isDef: locKey(def) == locKey(loc),
		})
		return true
	})
	for line := range spansByLine {
		sort.Slice(spansByLine[line], func(i, j int) bool {
			if spansByLine[line][i].start != spansByLine[line][j].start {
				return spansByLine[line][i].start < spansByLine[line][j].start
			}
			return spansByLine[line][i].end < spansByLine[line][j].end
		})
	}
	return spansByLine
}

func functionScopes(fset *token.FileSet, root ast.Node) []funcScope {
	var out []funcScope
	ast.Inspect(root, func(n ast.Node) bool {
		switch fn := n.(type) {
		case *ast.FuncDecl:
			out = append(out, funcScope{start: fset.Position(fn.Pos()).Line, end: fset.Position(fn.End()).Line})
		case *ast.FuncLit:
			out = append(out, funcScope{start: fset.Position(fn.Pos()).Line, end: fset.Position(fn.End()).Line})
		}
		return true
	})
	return out
}

func findFuncScope(scopes []funcScope, line int) funcScope {
	best := funcScope{}
	for _, s := range scopes {
		if line < s.start || line > s.end {
			continue
		}
		if best.start == 0 || (s.end-s.start) < (best.end-best.start) {
			best = s
		}
	}
	return best
}

func classifyColor(repoRoot string, def, ref Location, scope funcScope, sameFuncEligible bool) (string, string) {
	df, dl, _ := locToFileLine(def)
	rf, _, _ := locToFileLine(ref)
	if strings.HasPrefix(df, "/usr/lib/go/src/builtin") {
		return "", ""
	}
	if sameFuncEligible && df == rf && scope.start > 0 && dl >= scope.start && dl <= scope.end {
		return "same function", clrYellow
	}
	fs, fe := scope.start, scope.end
	if !sameFuncEligible {
		fs, fe = 0, 0
	}
	cat, _, ok := classifyExternal(repoRoot, def, ref, fs, fe)
	if !ok {
		return "", ""
	}
	switch cat {
	case "same file":
		return cat, clrGreen
	case "same package":
		return cat, clrCyan
	case "same repository":
		return cat, clrBlue
	default:
		return cat, clrPurple
	}
}

func colorLine(line string, spans []lineSpan) string {
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
			b.WriteString(clrReset)
			idx = s.end
			continue
		}
		b.WriteString(clrPlain)
		b.WriteString(bgFor(s.color))
		b.WriteString(line[s.start:s.end])
		b.WriteString(clrReset)
		idx = s.end
	}
	b.WriteString(line[idx:])
	return b.String()
}

func printFileLegend() {
	fmt.Printf("%sLegend%s %sSame function%s %sSame file%s %sSame package%s %sSame repository%s %sExternal repositories%s\n",
		clrBold, clrReset,
		clrYellow, clrReset,
		clrGreen, clrReset,
		clrCyan, clrReset,
		clrBlue, clrReset,
		clrPurple, clrReset,
	)
	fmt.Printf("%sDefinitions are colored text; references are highlighted with plain text.%s\n", clrGray, clrReset)
}

func bgFor(color string) string {
	switch color {
	case clrYellow:
		return "\x1b[43m"
	case clrGreen:
		return "\x1b[42m"
	case clrCyan:
		return "\x1b[46m"
	case clrBlue:
		return "\x1b[44m"
	case clrPurple:
		return "\x1b[45m"
	default:
		return "\x1b[100m"
	}
}
