package getrefs

import (
	"bufio"

	"notectl/internal/getrefs/astrefs"
	"notectl/internal/getrefs/colors"
	lspclient "notectl/internal/getrefs/lsp"
	"notectl/internal/getrefs/refs"
	"notectl/internal/getrefs/view"
)

func pathToURI(path string) string                  { return refs.PathToURI(path) }
func locToFileLine(loc Location) (string, int, int) { return refs.ToFileLine(loc) }
func locFile(loc Location) string                   { return refs.File(loc) }
func locLine(loc Location) int                      { return refs.Line(loc) }
func locKey(loc Location) string                    { return refs.Key(loc) }
func lineText(file string, line int) string         { return refs.LineText(file, line) }
func uniqLocs(in []Location) []Location             { return refs.UniqLocs(in) }
func sortLocs(locs []Location)                      { refs.SortLocs(locs) }
func classifyExternal(root string, d, r Location, s, e int) (string, string, bool) {
	return astrefs.ClassifyExternal(root, d, r, s, e)
}
func readMsg(r *bufio.Reader) ([]byte, error) { return lspclient.ReadMsg(r) }

func filterLocs(in []Location, keep func(string) bool) []Location {
	out := in[:0]
	for _, l := range in {
		if keep(refs.File(l)) {
			out = append(out, l)
		}
	}
	return out
}
func containsLoc(locs []Location, target Location) bool {
	k := refs.Key(target)
	for _, l := range locs {
		if refs.Key(l) == k {
			return true
		}
	}
	return false
}

func groupRefsByReassign(rs, reassigns []Location) ([]Location, map[int][]Location) {
	return viewGroup(rs, reassigns)
}

func viewGroup(rs, reassigns []Location) ([]Location, map[int][]Location) {
	// keep test-facing behavior while delegating display behavior package.
	return viewGroupImpl(rs, reassigns)
}

func viewGroupImpl(rs, reassigns []Location) ([]Location, map[int][]Location) {
	byAssign := map[int][]Location{}
	if len(reassigns) == 0 {
		return rs, byAssign
	}
	var base []Location
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

const clrGreen = colors.Green

var clrReset = colors.Reset

type mark struct {
	line       int
	start, end int
	open       string
}

func printFileWithMarks(file string, marks []mark) error {
	out := make([]view.ColorMark, 0, len(marks))
	for _, m := range marks {
		out = append(out, view.ColorMark{Line: m.line, Start: m.start, End: m.end, Open: m.open})
	}
	return view.PrintFileWithMarks(file, out)
}
