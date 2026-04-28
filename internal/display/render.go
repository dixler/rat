package display

import (
	"fmt"
	"strings"
)

const (
	Reset  = "\x1b[0m"
	Bold   = "\x1b[1m"
	Gray   = "\x1b[90m"
	Red    = "\x1b[31m"
	Orange = "\x1b[38;5;208m"
	Yellow = "\x1b[33m"
	Green  = "\x1b[32m"
	Cyan   = "\x1b[36m"
	Blue   = "\x1b[34m"
	Black  = "\x1b[30m"
	Purple = "\x1b[35m"
	White  = "\x1b[97m"
)

type Style struct {
	Fg      string
	Bg      string
	RefText string
}

type Span struct {
	Start int
	End   int
	Style Style
	IsDef bool
}

func RenderSource(src string, spans map[int][]Span) string {
	if src == "" {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%sSource%s\n", Bold, Reset)
	lines := strings.Split(src, "\n")
	for i, line := range lines {
		fmt.Fprintf(&b, "%4d  %s\n", i+1, ColorLine(line, spans[i+1]))
	}
	return b.String()
}

func ColorLine(line string, spans []Span) string {
	if len(spans) == 0 {
		return Gray + line + Reset
	}
	var b strings.Builder
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
		b.WriteString(Gray)
		b.WriteString(line[idx:s.Start])
		b.WriteString(Reset)
		if s.IsDef {
			b.WriteString(s.Style.Fg)
			b.WriteString(line[s.Start:s.End])
			b.WriteString(Reset)
		} else {
			b.WriteString(s.Style.RefText)
			b.WriteString(s.Style.Bg)
			b.WriteString(line[s.Start:s.End])
			b.WriteString(Reset)
		}
		idx = s.End
	}
	b.WriteString(Gray)
	b.WriteString(line[idx:])
	b.WriteString(Reset)
	return b.String()
}
