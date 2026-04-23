package getrefs

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

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

type defResolver interface{ definitionAt(Location) Location }

func Render(r defResolver, repoRoot, name string, matches []Match) error {
	if len(matches) == 0 {
		fmt.Printf("no identifier matches for %q\n", name)
		return nil
	}
	for i, m := range matches {
		if len(matches) > 1 {
			fmt.Printf("Match %d\n", i+1)
		}
		printLoc("Definition", m.Def, name)
		for j, ref := range m.Refs {
			printLoc(fmt.Sprintf("Ref %d", j+1), ref, name)
		}
		if len(matches) == 1 {
			anchor := m.Def
			if len(m.Refs) > 0 {
				anchor = m.Refs[0]
			}
			printCapturedHierarchy(r, repoRoot, anchor)
		}
		fmt.Println()
	}
	return nil
}

func printCapturedHierarchy(r defResolver, repoRoot string, ref Location) {
	roots, externals := (analysisClient{}).capturedRefsInFunction(ref)
	if len(roots) == 0 && len(externals) == 0 {
		return
	}
	printExternalGroups(r, repoRoot, externals)
	hasReassign := false
	for _, root := range roots {
		hasReassign = hasReassign || len(root.Reassign) > 0
	}
	printSectionHeader("  ", "Same-function definitions:", clrYellow)
	if hasReassign {
		fmt.Printf("  %sWARNING%s: one or more declarations are reassigned\n", clrOrange, clrReset)
	}
	for _, root := range roots {
		if root.Def == nil {
			continue
		}
		sortLocs(root.Reassign)
		sortLocs(root.Refs)
		printLocIndented("  ", root.Name, *root.Def, root.Name, clrYellow, clrYellow)
		base, byAssign := groupRefsByReassign(root.Refs, root.Reassign)
		for _, l := range uniqLocs(base) {
			printLocIndented("    ", "", l, root.Name, "", clrYellow)
		}
		for i, rs := range root.Reassign {
			printLocIndented("    ", fmt.Sprintf("Reassign %d", i+1), rs, root.Name, clrOrange, clrYellow)
			for _, l := range uniqLocs(byAssign[i]) {
				printLocIndented("      ", "", l, root.Name, "", clrYellow)
			}
		}
		printChildRefs(root, 3)
	}
}

func printExternalGroups(r defResolver, repoRoot string, refs []namedLoc) {
	if len(refs) == 0 {
		return
	}
	type defGroup struct {
		name string
		def  Location
		refs []Location
	}
	categories := map[string]map[string]map[string]*defGroup{
		"external repositories": {}, "same repository": {}, "same package": {}, "same file": {},
	}
	for _, ref := range refs {
		def := r.definitionAt(ref.Loc)
		cat, grp, ok := classifyExternal(repoRoot, def, ref.Loc, ref.FuncStart, ref.FuncEnd)
		if !ok {
			continue
		}
		if categories[cat][grp] == nil {
			categories[cat][grp] = map[string]*defGroup{}
		}
		k := locKey(def)
		if categories[cat][grp][k] == nil {
			categories[cat][grp][k] = &defGroup{name: ref.Name, def: def}
		}
		categories[cat][grp][k].refs = append(categories[cat][grp][k].refs, ref.Loc)
	}
	order := []string{"external repositories", "same repository", "same package", "same file"}
	colors := map[string]string{"external repositories": clrPurple, "same repository": clrBlue, "same package": clrCyan, "same file": clrGreen}
	hasAny := false
	for _, c := range order {
		hasAny = hasAny || len(categories[c]) > 0
	}
	if !hasAny {
		return
	}
	printSectionHeader("  ", "In-function external references:", clrPurple)
	for _, cat := range order {
		if len(categories[cat]) == 0 {
			continue
		}
		printSectionHeader("  ", cat, colors[cat])
		names := make([]string, 0, len(categories[cat]))
		for n := range categories[cat] {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, name := range names {
			fmt.Printf("    %s%s%s%s\n", clrBold, colors[cat], name, clrReset)
			defs := categories[cat][name]
			keys := make([]string, 0, len(defs))
			for k := range defs {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				d := defs[k]
				printNamedLocBrief("      ", d.name, d.def, colors[cat])
				for _, l := range uniqLocs(d.refs) {
					printLocBriefRaw("        ", l)
				}
			}
		}
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

func groupRefsByReassign(refs, reassigns []Location) ([]Location, map[int][]Location) {
	byAssign := map[int][]Location{}
	if len(reassigns) == 0 {
		return refs, byAssign
	}
	var base []Location
	for _, ref := range refs {
		idx := -1
		for i, rs := range reassigns {
			if locLine(rs) <= locLine(ref) {
				idx = i
			} else {
				break
			}
		}
		if idx == -1 {
			base = append(base, ref)
		} else {
			byAssign[idx] = append(byAssign[idx], ref)
		}
	}
	return base, byAssign
}

func printLoc(label string, loc Location, focus string) {
	printLocIndented("  ", label, loc, focus, "", clrRef)
}

func printLocIndented(indent, label string, loc Location, focus, color, focusColor string) {
	file, line, col := locToFileLine(loc)
	if color == "" {
		color = clrWhite
	}
	if focusColor == "" {
		focusColor = clrRef
	}
	if label == "" {
		fmt.Printf("%s%s%s%s: %d:%d%s\n", indent, color, clrReset, file, line, col, clrReset)
	} else {
		fmt.Printf("%s%s%s%s: %s:%d:%d%s\n", indent, color, label, clrReset, file, line, col, clrReset)
	}
	fmt.Printf("%s  %s%s%s\n", indent, clrWhite, colorizeIdentifier(lineText(file, line), focus, focusColor), clrReset)
}

func printLocBriefRaw(indent string, loc Location) {
	file, line, _ := locToFileLine(loc)
	fmt.Printf("%s%s%s:%d%s\n", indent, clrGray, file, line, clrReset)
}
func printNamedLocBrief(indent, name string, loc Location, color string) {
	file, line, _ := locToFileLine(loc)
	fmt.Printf("%s%s%s%s - %s%s:%d%s\n", indent, color, name, clrReset, clrGray, file, line, clrReset)
}
func printSectionHeader(indent, text, color string) {
	fmt.Printf("%s%s%s%s%s\n", indent, clrBold, color, text, clrReset)
}

func colorizeIdentifier(line, ident, color string) string {
	if ident == "" {
		return line
	}
	loc := regexp.MustCompile(`\b` + regexp.QuoteMeta(ident) + `\b`).FindStringIndex(line)
	if len(loc) != 2 {
		return line
	}
	return line[:loc[0]] + color + line[loc[0]:loc[1]] + clrWhite + line[loc[1]:]
}
