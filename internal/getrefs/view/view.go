package view

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"notectl/internal/getrefs/astrefs"
	"notectl/internal/getrefs/colors"
	"notectl/internal/getrefs/refs"
)

type DefResolver interface {
	DefinitionAt(refs.Location) refs.Location
}

type FuncAnalyzer interface {
	CapturedRefsInFunction(refs.Location) ([]*astrefs.FuncRef, []astrefs.NamedLoc)
}

func Render(r DefResolver, a FuncAnalyzer, repoRoot, name string, matches []refs.Match) error {
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
			printCapturedHierarchy(r, a, repoRoot, anchor)
		}
		fmt.Println()
	}
	return nil
}

func printCapturedHierarchy(r DefResolver, a FuncAnalyzer, repoRoot string, ref refs.Location) {
	roots, externals := a.CapturedRefsInFunction(ref)
	if len(roots) == 0 && len(externals) == 0 {
		return
	}
	printExternalGroups(r, repoRoot, externals)
	hasReassign := false
	for _, root := range roots {
		hasReassign = hasReassign || len(root.Reassign) > 0
	}
	printSectionHeader("  ", "Same-function definitions:", colors.Yellow)
	if hasReassign {
		fmt.Printf("  %sWARNING%s: one or more declarations are reassigned\n", colors.Orange, colors.Reset)
	}
	for _, root := range roots {
		if root.Def == nil {
			continue
		}
		refs.SortLocs(root.Reassign)
		refs.SortLocs(root.Refs)
		printLocIndented("  ", root.Name, *root.Def, root.Name, colors.Yellow, colors.Yellow)
		base, byAssign := groupRefsByReassign(root.Refs, root.Reassign)
		for _, l := range refs.UniqLocs(base) {
			printLocIndented("    ", "", l, root.Name, "", colors.Yellow)
		}
		for i, rs := range root.Reassign {
			printLocIndented("    ", fmt.Sprintf("Reassign %d", i+1), rs, root.Name, colors.Orange, colors.Yellow)
			for _, l := range refs.UniqLocs(byAssign[i]) {
				printLocIndented("      ", "", l, root.Name, "", colors.Yellow)
			}
		}
		printChildRefs(root, 3)
	}
}

func printExternalGroups(r DefResolver, repoRoot string, ext []astrefs.NamedLoc) {
	if len(ext) == 0 {
		return
	}
	type defGroup struct {
		name string
		def  refs.Location
		refs []refs.Location
	}
	categories := map[string]map[string]map[string]*defGroup{"external repositories": {}, "same repository": {}, "same package": {}, "same file": {}}
	for _, ref := range ext {
		def := r.DefinitionAt(ref.Loc)
		cat, grp, ok := astrefs.ClassifyExternal(repoRoot, def, ref.Loc, ref.FuncStart, ref.FuncEnd)
		if !ok {
			continue
		}
		if categories[cat][grp] == nil {
			categories[cat][grp] = map[string]*defGroup{}
		}
		k := refs.Key(def)
		if categories[cat][grp][k] == nil {
			categories[cat][grp][k] = &defGroup{name: ref.Name, def: def}
		}
		categories[cat][grp][k].refs = append(categories[cat][grp][k].refs, ref.Loc)
	}
	order := []string{"external repositories", "same repository", "same package", "same file"}
	categoryColors := map[string]string{"external repositories": colors.Purple, "same repository": colors.Blue, "same package": colors.Cyan, "same file": colors.Green}
	hasAny := false
	for _, c := range order {
		hasAny = hasAny || len(categories[c]) > 0
	}
	if !hasAny {
		return
	}
	printSectionHeader("  ", "In-function external references:", colors.Purple)
	for _, cat := range order {
		if len(categories[cat]) == 0 {
			continue
		}
		printSectionHeader("  ", cat, categoryColors[cat])
		names := make([]string, 0, len(categories[cat]))
		for n := range categories[cat] {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, name := range names {
			fmt.Printf("    %s%s%s%s\n", colors.Bold, categoryColors[cat], name, colors.Reset)
			defs := categories[cat][name]
			keys := make([]string, 0, len(defs))
			for k := range defs {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				d := defs[k]
				printNamedLocBrief("      ", d.name, d.def, categoryColors[cat])
				for _, l := range refs.UniqLocs(d.refs) {
					printLocBriefRaw("        ", l)
				}
			}
		}
	}
}

func printChildRefs(n *astrefs.FuncRef, depth int) {
	names := make([]string, 0, len(n.Children))
	for k := range n.Children {
		names = append(names, k)
	}
	sort.Strings(names)
	indent := strings.Repeat("  ", depth)
	for _, name := range names {
		c := n.Children[name]
		fmt.Printf("%s%s%s%s\n", indent, colors.White, name, colors.Reset)
		for _, rl := range refs.UniqLocs(c.Refs) {
			printLocIndented(indent+"  ", "", rl, c.Name, "", colors.Yellow)
		}
		for _, rl := range refs.UniqLocs(c.Reassign) {
			printLocIndented(indent+"  ", "Reassign", rl, c.Name, colors.Orange, colors.Yellow)
		}
		printChildRefs(c, depth+1)
	}
}

func groupRefsByReassign(rs, reassigns []refs.Location) ([]refs.Location, map[int][]refs.Location) {
	byAssign := map[int][]refs.Location{}
	if len(reassigns) == 0 {
		return rs, byAssign
	}
	var base []refs.Location
	for _, ref := range rs {
		idx := -1
		for i, as := range reassigns {
			if refs.Line(as) <= refs.Line(ref) {
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

func printLoc(label string, loc refs.Location, focus string) {
	printLocIndented("  ", label, loc, focus, "", colors.Ref)
}

func printLocIndented(indent, label string, loc refs.Location, focus, color, focusColor string) {
	file, line, col := refs.ToFileLine(loc)
	if color == "" {
		color = colors.White
	}
	if focusColor == "" {
		focusColor = colors.Ref
	}
	if label == "" {
		fmt.Printf("%s%s%s%s: %d:%d%s\n", indent, color, colors.Reset, file, line, col, colors.Reset)
	} else {
		fmt.Printf("%s%s%s%s: %s:%d:%d%s\n", indent, color, label, colors.Reset, file, line, col, colors.Reset)
	}
	fmt.Printf("%s  %s%s%s\n", indent, colors.White, colorizeIdentifier(refs.LineText(file, line), focus, focusColor), colors.Reset)
}

func printLocBriefRaw(indent string, loc refs.Location) {
	file, line, _ := refs.ToFileLine(loc)
	fmt.Printf("%s%s%s:%d%s\n", indent, colors.Gray, file, line, colors.Reset)
}
func printNamedLocBrief(indent, name string, loc refs.Location, color string) {
	file, line, _ := refs.ToFileLine(loc)
	fmt.Printf("%s%s%s%s - %s%s:%d%s\n", indent, color, name, colors.Reset, colors.Gray, file, line, colors.Reset)
}
func printSectionHeader(indent, text, color string) {
	fmt.Printf("%s%s%s%s%s\n", indent, colors.Bold, color, text, colors.Reset)
}

func colorizeIdentifier(line, ident, color string) string {
	if ident == "" {
		return line
	}
	loc := regexp.MustCompile(`\b` + regexp.QuoteMeta(ident) + `\b`).FindStringIndex(line)
	if len(loc) != 2 {
		return line
	}
	return line[:loc[0]] + color + line[loc[0]:loc[1]] + colors.White + line[loc[1]:]
}

func PrintFileWithMarks(file string, marks []ColorMark) error {
	b, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	byLine := map[int][]ColorMark{}
	for _, m := range marks {
		byLine[m.Line] = append(byLine[m.Line], m)
	}
	lines := strings.Split(string(b), "\n")
	for i, line := range lines {
		ms := byLine[i+1]
		sort.Slice(ms, func(a, b int) bool { return ms[a].Start > ms[b].Start })
		for _, m := range ms {
			if m.Start < 0 || m.End > len(line) || m.Start >= m.End {
				continue
			}
			line = line[:m.Start] + m.Open + line[m.Start:m.End] + colors.Reset + line[m.End:]
		}
		if i == len(lines)-1 {
			fmt.Print(line)
		} else {
			fmt.Println(line)
		}
	}
	return nil
}

type ColorMark struct {
	Line, Start, End int
	Open             string
}
