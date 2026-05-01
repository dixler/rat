package display

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	Reset        = "\x1b[0m"
	Bold         = "\x1b[1m"
	Invert       = "\x1b[7m"
	Blink        = "\x1b[5m"
	Underline    = "\x1b[4m"
	Gray         = "\x1b[90m"
	Red          = "\x1b[31m"
	Orange       = "\x1b[38;5;208m"
	Yellow       = "\x1b[38;5;226m"
	LightGreen   = "\x1b[38;5;120m"
	LightGreenBg = "\x1b[48;5;120m"
	Green        = "\x1b[32m"
	Cyan         = "\x1b[36m"
	Blue         = "\x1b[34m"
	Black        = "\x1b[30m"
	Purple       = "\x1b[35m"
	White        = "\x1b[97m"
	Lavender     = "\x1b[38;5;183m"
	Amber        = "\x1b[38;5;214m"
	Lime         = "\x1b[38;5;118m"
	CoralRed     = "\x1b[38;5;203m"
	HotMagenta   = "\x1b[38;5;198m"
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
	UseFg bool
}

func RenderSource(src string, spans map[int][]Span, lineNumberStyles map[int]Style) string {
	if src == "" {
		return ""
	}
	var b strings.Builder
	lines := strings.Split(src, "\n")
	lineNumberWidth := len(strconv.Itoa(len(lines)))
	for i, line := range lines {
		fmt.Fprintf(&b, " %s%*d%s  %s\n", lineNumberPrefix(lineNumberStyles[i+1]), lineNumberWidth, i+1, Reset, ColorLine(line, spans[i+1]))
	}
	return b.String()
}

func lineNumberPrefix(sty Style) string {
	if sty.Fg != "" {
		return sty.Fg + Invert
	}
	return White
}

func ColorLine(line string, spans []Span) string {
	if len(spans) == 0 {
		return White + line + Reset
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
		b.WriteString(White)
		b.WriteString(line[idx:s.Start])
		b.WriteString(Reset)
		if s.UseFg || !s.IsDef {
			b.WriteString(s.Style.Fg)
			b.WriteString(line[s.Start:s.End])
			b.WriteString(Reset)
		} else {
			b.WriteString(s.Style.Fg)
			b.WriteString(Invert)
			b.WriteString(line[s.Start:s.End])
			b.WriteString(Reset)
		}
		idx = s.End
	}
	b.WriteString(White)
	b.WriteString(line[idx:])
	b.WriteString(Reset)
	return b.String()
}
