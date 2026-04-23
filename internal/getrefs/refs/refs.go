package refs

import (
	"bytes"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type Location struct {
	URI   string `json:"uri"`
	Range struct {
		Start Pos `json:"start"`
		End   Pos `json:"end"`
	} `json:"range"`
}

type Pos struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type Match struct {
	Def  Location
	Refs []Location
}

func ToFileLine(loc Location) (string, int, int) {
	u, _ := url.Parse(loc.URI)
	p := filepath.FromSlash(u.Path)
	return p, loc.Range.Start.Line + 1, loc.Range.Start.Character + 1
}

func File(loc Location) string { f, _, _ := ToFileLine(loc); return f }
func Line(loc Location) int    { _, l, _ := ToFileLine(loc); return l }

func Key(loc Location) string {
	f, l, c := ToFileLine(loc)
	return f + ":" + strconv.Itoa(l) + ":" + strconv.Itoa(c)
}

func LineText(file string, line int) string {
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

func UniqLocs(in []Location) []Location {
	seen := map[string]bool{}
	out := in[:0]
	for _, l := range in {
		k := Key(l)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, l)
	}
	return out
}

func SortLocs(locs []Location) {
	sort.Slice(locs, func(i, j int) bool {
		fi, li, ci := ToFileLine(locs[i])
		fj, lj, cj := ToFileLine(locs[j])
		if fi != fj {
			return fi < fj
		}
		if li != lj {
			return li < lj
		}
		return ci < cj
	})
}

func PathToURI(path string) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return "file://" + p
}
