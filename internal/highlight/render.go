package highlight

import (
	"fmt"
	"strconv"
	"strings"

	"rat/internal/display"
)

type Span struct {
	Line     int           `json:"line,omitempty"`
	Start    int           `json:"start"`
	End      int           `json:"end"`
	Style    display.Style `json:"style"`
	Priority int           `json:"priority,omitempty"`
}

func RenderSource(program ParseResult) string {
	if program.Source == "" {
		return ""
	}
	var b strings.Builder
	lines := strings.Split(program.Source, "\n")
	lineNumberWidth := len(strconv.Itoa(len(lines)))
	for i, line := range lines {
		lineNo := i + 1
		if program.LineMarkers != nil {
			if marker := program.LineMarkers[lineNo]; marker != "" {
				pad := strings.Repeat(" ", lineNumberWidth)
				fmt.Fprintf(&b, " %s%s %s\n", display.Reset, pad, marker)
			}
		}
		lineNumber := fmt.Sprintf("%*d", lineNumberWidth, lineNo)
		fmt.Fprintf(&b, " %s%s %s\n", display.Reset, lineNumber, colorLine(line, program.SourceSpans[lineNo]))
	}
	return strings.ReplaceAll(b.String(), "\t", strings.Repeat(" ", 4))
}

func colorLine(line string, spans []Span) string {
	defaultStyle := display.White
	if len(spans) == 0 {
		return defaultStyle.Format(line)
	}
	var b strings.Builder
	idx := 0
	for _, s := range spans {
		b.WriteString(defaultStyle.Format(line[idx:s.Start]))
		b.WriteString(s.Style.Format(line[s.Start:s.End]))
		idx = s.End
	}
	b.WriteString(defaultStyle.Format(line[idx:]))
	return b.String()
}
