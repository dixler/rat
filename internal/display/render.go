package display

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	Reset        Style = "\x1b[0m"
	Bold         Style = "\x1b[1m"
	Invert       Style = "\x1b[7m"
	Blink        Style = "\x1b[5m"
	Underline    Style = "\x1b[4m"
	Gray         Style = "\x1b[90m"
	Red          Style = "\x1b[31m"
	Orange       Style = "\x1b[38;5;208m"
	Yellow       Style = "\x1b[38;5;226m"
	LightGreen   Style = "\x1b[38;5;120m"
	LightGreenBg Style = "\x1b[48;5;120m"
	Green        Style = "\x1b[32m"
	Cyan         Style = "\x1b[36m"
	Blue         Style = "\x1b[34m"
	Black        Style = "\x1b[30m"
	Purple       Style = "\x1b[35m"
	White        Style = "\x1b[97m"
	Lavender     Style = "\x1b[38;5;183m"
	Amber        Style = "\x1b[38;5;214m"
	Lime         Style = "\x1b[38;5;118m"
	CoralRed     Style = "\x1b[38;5;203m"
	HotMagenta   Style = "\x1b[38;5;198m"
)

type Style string

func (s Style) Format(str string) string {
	return string(s) + str + string(Reset)
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
	defaultLineNumberStyle := White
	for i, line := range lines {
		lineNumberStyle, ok := lineNumberStyles[i+1]
		if !ok || lineNumberStyle == "" {
			lineNumberStyle = defaultLineNumberStyle
		}
		lineNumber := fmt.Sprintf("%*d", lineNumberWidth, i+1)
		fmt.Fprintf(&b, " %s  %s\n", lineNumberStyle.Format(lineNumber), ColorLine(line, spans[i+1]))
	}
	return b.String()
}

func ColorLine(line string, spans []Span) string {
	defaultStyle := White
	if len(spans) == 0 {
		return defaultStyle.Format(line)
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
		b.WriteString(defaultStyle.Format(line[idx:s.Start]))
		if s.UseFg || !s.IsDef {
			b.WriteString(s.Style.Format(line[s.Start:s.End]))
		} else {
			spanStyle := s.Style + Invert
			b.WriteString(spanStyle.Format(line[s.Start:s.End]))
		}
		idx = s.End
	}
	b.WriteString(defaultStyle.Format(line[idx:]))
	return b.String()
}
