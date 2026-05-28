package display

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	Reset         BasicStyle = "\x1b[0m"
	Bold          BasicStyle = "\x1b[1m"
	Invert        BasicStyle = "\x1b[7m"
	Blink         BasicStyle = "\x1b[5m"
	Underline     BasicStyle = "\x1b[4m"
	Gray          BasicStyle = "\x1b[90m"
	Red           BasicStyle = "\x1b[31m"
	BrightRed     BasicStyle = "\x1b[91m"
	MutedOrange   BasicStyle = "\x1b[38;5;130m"
	Orange        BasicStyle = "\x1b[38;5;208m"
	VibrantOrange BasicStyle = "\x1b[38;5;215m"
	Yellow        BasicStyle = "\x1b[38;5;226m"
	LightGreen    BasicStyle = "\x1b[38;5;120m"
	Green         BasicStyle = "\x1b[32m"
	Cyan          BasicStyle = "\x1b[96m"
	Blue          BasicStyle = "\x1b[94m"
	DarkBlue      BasicStyle = "\x1b[34m"
	Black         BasicStyle = "\x1b[30m"
	Purple        BasicStyle = "\x1b[35m"
	White         BasicStyle = "\x1b[97m"
	Amber         BasicStyle = "\x1b[38;5;214m"
	Lime          BasicStyle = "\x1b[38;5;118m"
	CoralRed      BasicStyle = "\x1b[38;5;203m"
	HotMagenta    BasicStyle = "\x1b[38;5;198m"
)

type BasicStyle string

type Style interface {
	Format(str string) string
}

func (s BasicStyle) Format(str string) string {
	return string(s) + str + string(Reset)
}

func (s BasicStyle) Invert() BasicStyle {
	return s + Invert
}

type Span struct {
	Start    int
	End      int
	Style    Style
	Priority int
}

func RenderSource(src string, spans map[int][]Span, lineNumberStyles map[int]Style, lineMarkers map[int]string) string {
	if src == "" {
		return ""
	}
	var b strings.Builder
	lines := strings.Split(src, "\n")
	lineNumberWidth := len(strconv.Itoa(len(lines)))
	for i, line := range lines {
		lineNo := i + 1
		marker := ""
		if lineMarkers != nil {
			marker = lineMarkers[lineNo]
		}
		if marker != "" {
			pad := strings.Repeat(" ", lineNumberWidth)
			fmt.Fprintf(&b, " %s%s %s\n", Reset, pad, marker)
		}
		lineNumber := fmt.Sprintf("%*d", lineNumberWidth, lineNo)
		fmt.Fprintf(&b, " %s%s %s\n", Reset, lineNumber, colorLine(line, spans[lineNo]))
	}
	return strings.ReplaceAll(b.String(), "\t", strings.Repeat(" ", 4))
}

func colorLine(line string, spans []Span) string {
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
		b.WriteString(s.Style.Format(line[s.Start:s.End]))
		idx = s.End
	}
	b.WriteString(defaultStyle.Format(line[idx:]))
	return b.String()
}
