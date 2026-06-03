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

func RenderSource(src string, spans map[int][]Span, lineNumberStyles map[int]display.Style, lineMarkers map[int]string) string {
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
			fmt.Fprintf(&b, " %s%s %s\n", display.Reset, pad, marker)
		}
		lineNumber := fmt.Sprintf("%*d", lineNumberWidth, lineNo)
		fmt.Fprintf(&b, " %s%s %s\n", display.Reset, lineNumber, colorLine(line, spans[lineNo]))
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
