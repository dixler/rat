package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type req struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id,omitempty"`
	Method  string      `json:"method,omitempty"`
	Params  interface{} `json:"params,omitempty"`
}

type resp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcErr         `json:"error,omitempty"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type location struct {
	URI   string `json:"uri"`
	Range struct {
		Start pos `json:"start"`
		End   pos `json:"end"`
	} `json:"range"`
}

type pos struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type symbolInfo struct {
	Name     string   `json:"name"`
	Location location `json:"location"`
}

type match struct {
	Def  location
	Refs []location
}

type query struct {
	scopeArg string
	name     string
	inScope  func(string) bool
}

type funcRef struct {
	Name     string
	Def      *location
	Refs     []location
	Reassign []location
	Children map[string]*funcRef
}

type namedLoc struct {
	Name      string
	Loc       location
	FuncStart int
	FuncEnd   int
}

type lsp struct {
	cmd    *exec.Cmd
	in     io.WriteCloser
	out    *bufio.Reader
	nextID int
	mu     sync.Mutex
}

func main() {
	if len(os.Args) != 2 {
		die("usage: getrefs [<file|dir>:]<identifierName>")
	}
	root, err := repoRoot()
	if err != nil {
		die(err.Error())
	}
	q, err := parseQuery(root, os.Args[1])
	if err != nil {
		die(err.Error())
	}
	c, err := startLSP(root)
	if err != nil {
		die(err.Error())
	}
	defer c.close()
	if err := c.init(root); err != nil {
		die(err.Error())
	}
	ms, err := c.find(q)
	if err != nil {
		die(err.Error())
	}
	if len(ms) == 0 {
		fmt.Printf("no identifier matches for %q\n", q.name)
		return
	}
	for i, m := range ms {
		if len(ms) > 1 {
			fmt.Printf("Match %d\n", i+1)
		}
		printLoc("Definition", m.Def, q.name)
		for j, r := range m.Refs {
			printLoc(fmt.Sprintf("Ref %d", j+1), r, q.name)
		}
		if len(ms) == 1 {
			anchor := m.Def
			if len(m.Refs) > 0 {
				anchor = m.Refs[0]
			}
			printCapturedHierarchy(c, root, anchor)
		}
		fmt.Println()
	}
}

func repoRoot() (string, error) {
	c := exec.Command("git", "rev-parse", "--show-toplevel")
	c.Stderr = io.Discard
	b, err := c.Output()
	if err != nil {
		return "", errors.New("run inside a git repo")
	}
	return strings.TrimSpace(string(b)), nil
}

func startLSP(root string) (*lsp, error) {
	cmd := exec.Command("gopls", "serve")
	cmd.Dir = root
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, errors.New("failed to start gopls; install it with `go install golang.org/x/tools/gopls@latest`")
	}
	return &lsp{cmd: cmd, in: in, out: bufio.NewReader(out), nextID: 1}, nil
}

func (c *lsp) close() {
	_, _ = c.call("shutdown", map[string]any{})
	_ = c.notify("exit", map[string]any{})
	_ = c.in.Close()
	_ = c.cmd.Process.Kill()
	_, _ = c.cmd.Process.Wait()
}

func (c *lsp) init(root string) error {
	uri := pathToURI(root)
	_, err := c.call("initialize", map[string]any{
		"processId": os.Getpid(),
		"rootUri":   uri,
		"capabilities": map[string]any{
			"textDocument": map[string]any{},
			"workspace":    map[string]any{},
		},
	})
	if err != nil {
		return err
	}
	return c.notify("initialized", map[string]any{})
}

func (c *lsp) find(q query) ([]match, error) {
	res, err := c.call("workspace/symbol", map[string]any{"query": q.name})
	if err != nil {
		return nil, err
	}
	var syms []symbolInfo
	if err := json.Unmarshal(res, &syms); err != nil {
		return nil, err
	}
	var symbols []symbolInfo
	for _, s := range syms {
		file, _, _ := locToFileLine(s.Location)
		if s.Name == q.name && q.inScope(file) {
			symbols = append(symbols, s)
		}
	}
	outCh := make(chan match, len(symbols))
	var wg sync.WaitGroup
	for _, s := range symbols {
		wg.Add(1)
		go func(s symbolInfo) {
			defer wg.Done()
			def, refs, err := c.defAndRefs(s.Location)
			if err != nil {
				return
			}
			if !q.inScope(locFile(def)) {
				return
			}
			refs = filterLocs(refs, q.inScope)
			groups := c.groupByDefinition(def, refs)
			for _, g := range groups {
				if !q.inScope(locFile(g.Def)) {
					continue
				}
				sort.Slice(g.Refs, func(i, j int) bool { return locKey(g.Refs[i]) < locKey(g.Refs[j]) })
				outCh <- g
			}
		}(s)
	}
	wg.Wait()
	close(outCh)
	byDef := map[string]match{}
	for m := range outCh {
		k := locKey(m.Def)
		curr := byDef[k]
		if curr.Def.URI == "" {
			curr.Def = m.Def
		}
		curr.Refs = append(curr.Refs, m.Refs...)
		byDef[k] = curr
	}
	var out []match
	for _, m := range byDef {
		m.Refs = uniqLocs(m.Refs)
		sort.Slice(m.Refs, func(i, j int) bool { return locKey(m.Refs[i]) < locKey(m.Refs[j]) })
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return locKey(out[i].Def) < locKey(out[j].Def) })
	return out, nil
}

func (c *lsp) defAndRefs(loc location) (location, []location, error) {
	def := c.definitionAt(loc)
	refsRaw, err := c.call("textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": def.URI},
		"position":     def.Range.Start,
		"context":      map[string]any{"includeDeclaration": false},
	})
	if err != nil {
		return location{}, nil, err
	}
	var refs []location
	if len(refsRaw) > 0 && string(refsRaw) != "null" {
		if err := json.Unmarshal(refsRaw, &refs); err != nil {
			return location{}, nil, err
		}
	}
	uniq := map[string]location{}
	defKey := locKey(def)
	for _, r := range refs {
		k := locKey(r)
		if k == defKey {
			continue
		}
		uniq[k] = r
	}
	refs = refs[:0]
	for _, r := range uniq {
		refs = append(refs, r)
	}
	return def, refs, nil
}

func (c *lsp) definitionAt(loc location) location {
	p := map[string]any{"textDocument": map[string]any{"uri": loc.URI}, "position": loc.Range.Start}
	defRaw, err := c.call("textDocument/definition", p)
	if err != nil || len(defRaw) == 0 || string(defRaw) == "null" {
		return loc
	}
	if defRaw[0] == '{' {
		var one location
		if json.Unmarshal(defRaw, &one) == nil {
			return one
		}
		return loc
	}
	var defs []location
	if json.Unmarshal(defRaw, &defs) == nil && len(defs) > 0 {
		return defs[0]
	}
	return loc
}

func (c *lsp) groupByDefinition(def location, refs []location) []match {
	groups := map[string]match{}
	add := func(d, r location) {
		k := locKey(d)
		m := groups[k]
		if m.Def.URI == "" {
			m.Def = d
		}
		if !containsLoc(m.Refs, r) {
			m.Refs = append(m.Refs, r)
		}
		groups[k] = m
	}
	if len(refs) == 0 {
		groups[locKey(def)] = match{Def: def}
	}
	for _, r := range refs {
		add(c.definitionAt(r), r)
	}
	var out []match
	for _, g := range groups {
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return locKey(out[i].Def) < locKey(out[j].Def) })
	return out
}

func containsLoc(locs []location, target location) bool {
	k := locKey(target)
	for _, l := range locs {
		if locKey(l) == k {
			return true
		}
	}
	return false
}

func uniqLocs(locs []location) []location {
	seen := map[string]bool{}
	out := locs[:0]
	for _, l := range locs {
		k := locKey(l)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, l)
	}
	return out
}

func sortLocs(locs []location) {
	sort.Slice(locs, func(i, j int) bool {
		fi, li, ci := locToFileLine(locs[i])
		fj, lj, cj := locToFileLine(locs[j])
		if fi != fj {
			return fi < fj
		}
		if li != lj {
			return li < lj
		}
		return ci < cj
	})
}

func groupRefsByReassign(refs, reassigns []location) ([]location, map[int][]location) {
	byAssign := map[int][]location{}
	if len(reassigns) == 0 {
		return refs, byAssign
	}
	var base []location
	for _, ref := range refs {
		refLine := locLine(ref)
		idx := -1
		for i, rs := range reassigns {
			if locLine(rs) <= refLine {
				idx = i
			} else {
				break
			}
		}
		if idx == -1 {
			base = append(base, ref)
			continue
		}
		byAssign[idx] = append(byAssign[idx], ref)
	}
	return base, byAssign
}

func locLine(loc location) int {
	_, line, _ := locToFileLine(loc)
	return line
}

func printLoc(label string, loc location, focus string) {
	printLocIndented("  ", label, loc, focus, "", clrRef)
}

func printLocIndented(indent, label string, loc location, focus, color, focusColor string) {
	file, line, col := locToFileLine(loc)
	c := color
	if c == "" {
		c = clrWhite
	}
	h := focusColor
	if h == "" {
		h = clrRef
	}
	if label == "" {
		fmt.Printf("%s%s%s%s: %d:%d%s\n", indent, c, clrReset, file, line, col, clrReset)
	} else {
		fmt.Printf("%s%s%s%s: %s:%d:%d%s\n", indent, c, label, clrReset, file, line, col, clrReset)
	}
	fmt.Printf("%s  %s%s%s\n", indent, clrWhite, colorizeIdentifier(lineText(file, line), focus, h), clrReset)
}

func printLocBriefIndented(indent, label string, loc location, color string) {
	file, line, _ := locToFileLine(loc)
	c := color
	if c == "" {
		c = clrWhite
	}
	fmt.Printf("%s%s%s%s: %s:%d%s\n", indent, c, label, clrReset, file, line, clrReset)
}

func printLocBriefRaw(indent string, loc location) {
	file, line, _ := locToFileLine(loc)
	fmt.Printf("%s%s%s:%d%s\n", indent, clrGray, file, line, clrReset)
}

func printNamedLocBrief(indent, name string, loc location, nameColor string) {
	file, line, _ := locToFileLine(loc)
	fmt.Printf("%s%s%s%s - %s%s:%d%s\n", indent, nameColor, name, clrReset, clrGray, file, line, clrReset)
}

func locToFileLine(loc location) (string, int, int) {
	u, _ := url.Parse(loc.URI)
	p := filepath.FromSlash(u.Path)
	return p, loc.Range.Start.Line + 1, loc.Range.Start.Character + 1
}

func lineText(file string, line int) string {
	b, err := os.ReadFile(file)
	if err != nil {
		return ""
	}
	ls := bytes.Split(b, []byte("\n"))
	if line < 1 || line > len(ls) {
		return ""
	}
	return strings.TrimSpace(string(ls[line-1]))
}

func locKey(l location) string {
	f, ln, c := locToFileLine(l)
	return f + ":" + strconv.Itoa(ln) + ":" + strconv.Itoa(c)
}

func locFile(l location) string {
	f, _, _ := locToFileLine(l)
	return f
}

func filterLocs(in []location, keep func(string) bool) []location {
	out := in[:0]
	for _, l := range in {
		if keep(locFile(l)) {
			out = append(out, l)
		}
	}
	return out
}

func parseQuery(root, arg string) (query, error) {
	if !strings.Contains(arg, ":") {
		return query{name: arg, inScope: func(string) bool { return true }}, nil
	}
	scopeArg, name, ok := strings.Cut(arg, ":")
	if !ok || strings.TrimSpace(name) == "" || strings.TrimSpace(scopeArg) == "" {
		return query{}, errors.New("usage: getrefs [<file|dir>:]<identifierName>")
	}
	scopePath := scopeArg
	if !filepath.IsAbs(scopePath) {
		scopePath = filepath.Join(root, scopePath)
	}
	scopePath = filepath.Clean(scopePath)
	info, err := os.Stat(scopePath)
	if err != nil {
		return query{}, fmt.Errorf("invalid scope %q: %w", scopeArg, err)
	}
	if info.IsDir() {
		prefix := scopePath + string(os.PathSeparator)
		return query{
			scopeArg: scopeArg,
			name:     name,
			inScope: func(file string) bool {
				f := filepath.Clean(file)
				return f == scopePath || strings.HasPrefix(f, prefix)
			},
		}, nil
	}
	return query{
		scopeArg: scopeArg,
		name:     name,
		inScope: func(file string) bool {
			return filepath.Clean(file) == scopePath
		},
	}, nil
}

func printCapturedHierarchy(c *lsp, repoRoot string, ref location) {
	roots, externals := capturedRefsInFunction(ref)
	if len(roots) == 0 && len(externals) == 0 {
		return
	}
	printExternalGroups(c, repoRoot, externals)
	hasReassign := false
	for _, r := range roots {
		if len(r.Reassign) > 0 {
			hasReassign = true
			break
		}
	}
	printSectionHeader("  ", "Same-function definitions:", clrYellow)
	if hasReassign {
		fmt.Printf("  %sWARNING%s: one or more declarations are reassigned\n", clrOrange, clrReset)
	}
	for _, r := range roots {
		if r.Def == nil {
			continue
		}
		sortLocs(r.Reassign)
		sortLocs(r.Refs)
		printLocIndented("  ", r.Name, *r.Def, r.Name, clrYellow, clrYellow)
		base, byAssign := groupRefsByReassign(r.Refs, r.Reassign)
		for _, rl := range uniqLocs(base) {
			printLocIndented("    ", "", rl, r.Name, "", clrYellow)
		}
		for i, rs := range r.Reassign {
			printLocIndented("    ", fmt.Sprintf("Reassign %d", i+1), rs, r.Name, clrOrange, clrYellow)
			for _, rl := range uniqLocs(byAssign[i]) {
				printLocIndented("      ", "", rl, r.Name, "", clrYellow)
			}
		}
		printChildRefs(r, 3)
	}
}

func printChildRefs(n *funcRef, depth int) {
	names := make([]string, 0, len(n.Children))
	for k := range n.Children {
		names = append(names, k)
	}
	sort.Strings(names)
	indent := strings.Repeat("  ", depth)
	for _, name := range names {
		c := n.Children[name]
		fmt.Printf("%s%s%s%s\n", indent, clrWhite, name, clrReset)
		for _, rl := range uniqLocs(c.Refs) {
			printLocIndented(indent+"  ", "", rl, c.Name, "", clrYellow)
		}
		for _, rl := range uniqLocs(c.Reassign) {
			printLocIndented(indent+"  ", "Reassign", rl, c.Name, clrOrange, clrYellow)
		}
		printChildRefs(c, depth+1)
	}
}

func capturedRefsInFunction(ref location) ([]*funcRef, []namedLoc) {
	file, line, _ := locToFileLine(ref)
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		return nil, nil
	}
	var target ast.Node
	funcStart, funcEnd := 0, 0
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok {
			start := fset.Position(fd.Pos()).Line
			end := fset.Position(fd.End()).Line
			if line >= start && line <= end {
				target = fd
				funcStart, funcEnd = start, end
				break
			}
		}
	}
	if target == nil {
		return nil, nil
	}
	roots := map[string]*funcRef{}
	declPos := map[token.Pos]bool{}
	var externals []namedLoc
	addRoot := func(name string) *funcRef {
		if roots[name] == nil {
			roots[name] = &funcRef{Name: name, Children: map[string]*funcRef{}}
		}
		return roots[name]
	}
	ast.Inspect(target, func(n ast.Node) bool {
		switch t := n.(type) {
		case *ast.AssignStmt:
			for _, lhs := range t.Lhs {
				id, ok := lhs.(*ast.Ident)
				if !ok || id.Name == "_" {
					continue
				}
				r := addRoot(id.Name)
				loc := identLoc(file, fset, id)
				if t.Tok == token.DEFINE && r.Def == nil {
					r.Def = &loc
					declPos[id.Pos()] = true
					continue
				}
				r.Reassign = append(r.Reassign, loc)
			}
		case *ast.ValueSpec:
			for _, id := range t.Names {
				if id.Name == "_" {
					continue
				}
				r := addRoot(id.Name)
				loc := identLoc(file, fset, id)
				r.Def = &loc
				declPos[id.Pos()] = true
			}
		case *ast.Field:
			for _, id := range t.Names {
				if id.Name == "_" {
					continue
				}
				r := addRoot(id.Name)
				loc := identLoc(file, fset, id)
				r.Def = &loc
				declPos[id.Pos()] = true
			}
		}
		return true
	})
	ast.Inspect(target, func(n ast.Node) bool {
		switch t := n.(type) {
		case *ast.SelectorExpr:
			parts := selectorChain(t)
			if len(parts) < 2 {
				return true
			}
			root := addRoot(parts[0])
			curr := root
			for _, p := range parts[1:] {
				if curr.Children[p] == nil {
					curr.Children[p] = &funcRef{Name: p, Children: map[string]*funcRef{}}
				}
				curr = curr.Children[p]
			}
			curr.Refs = append(curr.Refs, identLoc(file, fset, t.Sel))
			return true
		case *ast.Ident:
			if t.Name == "_" || declPos[t.Pos()] {
				return true
			}
			if _, ok := roots[t.Name]; ok {
				roots[t.Name].Refs = append(roots[t.Name].Refs, identLoc(file, fset, t))
				return true
			}
			externals = append(externals, namedLoc{
				Name:      t.Name,
				Loc:       identLoc(file, fset, t),
				FuncStart: funcStart,
				FuncEnd:   funcEnd,
			})
		}
		return true
	})
	var out []*funcRef
	for _, r := range roots {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, externals
}

func printExternalGroups(c *lsp, repoRoot string, refs []namedLoc) {
	if len(refs) == 0 {
		return
	}
	type defGroup struct {
		name string
		def  location
		refs []location
	}
	type packageGroup map[string]*defGroup
	categories := map[string]map[string]packageGroup{
		"external repositories": {},
		"same repository":       {},
		"same package":          {},
		"same file":             {},
	}
	for _, r := range refs {
		def := c.definitionAt(r.Loc)
		cat, grp, ok := classifyExternal(repoRoot, def, r.Loc, r.FuncStart, r.FuncEnd)
		if !ok {
			continue
		}
		if categories[cat][grp] == nil {
			categories[cat][grp] = packageGroup{}
		}
		k := locKey(def)
		g := categories[cat][grp][k]
		if g == nil {
			g = &defGroup{name: r.Name, def: def}
			categories[cat][grp][k] = g
		}
		g.refs = append(g.refs, r.Loc)
	}
	if len(categories["external repositories"]) == 0 &&
		len(categories["same repository"]) == 0 &&
		len(categories["same package"]) == 0 &&
		len(categories["same file"]) == 0 {
		return
	}
	printSectionHeader("  ", "In-function external references:", clrPurple)
	order := []string{
		"external repositories",
		"same repository",
		"same package",
		"same file",
	}
	catColor := map[string]string{
		"external repositories": clrPurple,
		"same repository":       clrBlue,
		"same package":          clrCyan,
		"same file":             clrGreen,
	}
	for _, cat := range order {
		groupMap := categories[cat]
		if len(groupMap) == 0 {
			continue
		}
		printSectionHeader("  ", cat, catColor[cat])
		groupNames := make([]string, 0, len(groupMap))
		for name := range groupMap {
			groupNames = append(groupNames, name)
		}
		sort.Strings(groupNames)
		for _, name := range groupNames {
			fmt.Printf("    %s%s%s%s\n", clrBold, catColor[cat], name, clrReset)
			defs := groupMap[name]
			defKeys := make([]string, 0, len(defs))
			for k := range defs {
				defKeys = append(defKeys, k)
			}
			sort.Strings(defKeys)
			for _, dk := range defKeys {
				d := defs[dk]
				printNamedLocBrief("      ", d.name, d.def, catColor[cat])
				for _, l := range uniqLocs(d.refs) {
					printLocBriefRaw("        ", l)
				}
			}
		}
	}
}

func classifyExternal(repoRoot string, def, ref location, funcStart, funcEnd int) (string, string, bool) {
	df, dl, _ := locToFileLine(def)
	if strings.HasPrefix(filepath.Clean(df), "/usr/lib/go/src/builtin") {
		return "", "", false
	}
	rf, _, _ := locToFileLine(ref)
	if df == rf {
		if dl >= funcStart && dl <= funcEnd {
			return "", "", false
		}
		rel, _ := filepath.Rel(repoRoot, df)
		return "same file", rel, true
	}
	cleanRoot := filepath.Clean(repoRoot) + string(os.PathSeparator)
	inRepoDef := strings.HasPrefix(filepath.Clean(df), cleanRoot)
	inRepoRef := strings.HasPrefix(filepath.Clean(rf), cleanRoot)
	if inRepoDef && inRepoRef {
		pkg, _ := filepath.Rel(repoRoot, filepath.Dir(df))
		if filepath.Dir(df) == filepath.Dir(rf) {
			rel, _ := filepath.Rel(repoRoot, df)
			return "same package", rel, true
		}
		return "same repository", pkg, true
	}
	return "external repositories", filepath.Dir(df), true
}

func printSectionHeader(indent, text, color string) {
	fmt.Printf("%s%s%s%s%s\n", indent, clrBold, color, text, clrReset)
}

func selectorChain(s *ast.SelectorExpr) []string {
	var out []string
	var walk func(ast.Expr)
	walk = func(e ast.Expr) {
		switch t := e.(type) {
		case *ast.Ident:
			out = append(out, t.Name)
		case *ast.SelectorExpr:
			walk(t.X)
			out = append(out, t.Sel.Name)
		}
	}
	walk(s)
	return out
}

var (
	clrReset  = "\x1b[0m"
	clrBold   = "\x1b[1m"
	clrWhite  = "\x1b[97m"
	clrGray   = "\x1b[90m"
	clrYellow = "\x1b[33m"
	clrGreen  = "\x1b[32m"
	clrCyan   = "\x1b[36m"
	clrBlue   = "\x1b[34m"
	clrPurple = "\x1b[35m"
	clrRef    = "\x1b[96m"
	clrOrange = "\x1b[38;5;208m"
)

func colorizeIdentifier(line, ident, color string) string {
	if ident == "" {
		return line
	}
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(ident) + `\b`)
	loc := re.FindStringIndex(line)
	if len(loc) != 2 {
		return line
	}
	return line[:loc[0]] + color + line[loc[0]:loc[1]] + clrWhite + line[loc[1]:]
}

func identLoc(file string, fset *token.FileSet, id *ast.Ident) location {
	p := fset.Position(id.Pos())
	l := location{URI: pathToURI(file)}
	l.Range.Start.Line = p.Line - 1
	l.Range.Start.Character = p.Column - 1
	l.Range.End = l.Range.Start
	return l
}

func (c *lsp) notify(method string, params interface{}) error {
	b, err := json.Marshal(req{JSONRPC: "2.0", Method: method, Params: params})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(c.in, "Content-Length: %d\r\n\r\n%s", len(b), b)
	return err
}

func (c *lsp) call(method string, params interface{}) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.nextID
	c.nextID++
	b, err := json.Marshal(req{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintf(c.in, "Content-Length: %d\r\n\r\n%s", len(b), b); err != nil {
		return nil, err
	}
	for {
		msg, err := readMsg(c.out)
		if err != nil {
			return nil, err
		}
		var r resp
		if err := json.Unmarshal(msg, &r); err != nil {
			continue
		}
		if r.ID == 0 || r.ID != id {
			continue
		}
		if r.Error != nil {
			return nil, errors.New(r.Error.Message)
		}
		return r.Result, nil
	}
}

func readMsg(r *bufio.Reader) ([]byte, error) {
	headers := map[string]string{}
	for {
		ln, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		ln = strings.TrimRight(ln, "\r\n")
		if ln == "" {
			break
		}
		parts := strings.SplitN(ln, ":", 2)
		if len(parts) == 2 {
			headers[strings.TrimSpace(strings.ToLower(parts[0]))] = strings.TrimSpace(parts[1])
		}
	}
	n, err := strconv.Atoi(headers["content-length"])
	if err != nil || n <= 0 {
		return nil, errors.New("invalid lsp content-length")
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func pathToURI(path string) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return "file://" + p
}

func die(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}
