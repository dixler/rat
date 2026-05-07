package ansihtml

import (
	"fmt"
	"html"
	"strconv"
	"strings"
)

type state struct {
	fg        string
	bg        string
	bold      bool
	underline bool
	invert    bool
	blink     bool
}

func Convert(input string) string {
	var out strings.Builder
	var text strings.Builder
	st := state{}

	flush := func() {
		if text.Len() == 0 {
			return
		}
		content := html.EscapeString(text.String())
		text.Reset()
		style := styleString(st)
		if style == "" {
			out.WriteString(content)
			return
		}
		out.WriteString(`<span style="`)
		out.WriteString(style)
		out.WriteString(`">`)
		out.WriteString(content)
		out.WriteString(`</span>`)
	}

	for i := 0; i < len(input); i++ {
		if input[i] != '\x1b' || i+1 >= len(input) || input[i+1] != '[' {
			text.WriteByte(input[i])
			continue
		}
		j := i + 2
		for j < len(input) && input[j] != 'm' {
			j++
		}
		if j >= len(input) {
			text.WriteString(input[i:])
			break
		}

		flush()
		applyCode(&st, input[i+2:j])
		i = j
	}
	flush()

	return strings.ReplaceAll(out.String(), "\n", "<br>")
}

func styleString(st state) string {
	parts := make([]string, 0, 6)
	fg := st.fg
	bg := st.bg
	if st.invert {
		fg, bg = bg, fg
		if bg != "" {
			fg = contrastTextColor(bg)
		}
	}
	if fg != "" {
		parts = append(parts, "color:"+fg)
	}
	if bg != "" {
		parts = append(parts, "background-color:"+bg)
	}
	if st.bold {
		parts = append(parts, "font-weight:700")
	}
	if st.underline {
		parts = append(parts, "text-decoration:underline")
	}
	if st.blink {
		parts = append(parts, "opacity:0.95")
	}
	return strings.Join(parts, ";")
}

func contrastTextColor(hex string) string {
	if len(hex) != 7 || hex[0] != '#' {
		return "#ffffff"
	}
	r, okR := parseHexByte(hex[1:3])
	g, okG := parseHexByte(hex[3:5])
	b, okB := parseHexByte(hex[5:7])
	if !okR || !okG || !okB {
		return "#ffffff"
	}
	luma := 0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)
	if luma >= 140 {
		return "#000000"
	}
	return "#ffffff"
}

func parseHexByte(v string) (int64, bool) {
	n, err := strconv.ParseInt(v, 16, 32)
	if err != nil {
		return 0, false
	}
	return n, true
}

func applyCode(st *state, code string) {
	if code == "" {
		*st = state{}
		return
	}
	parts := strings.Split(code, ";")
	for i := 0; i < len(parts); i++ {
		v, err := strconv.Atoi(parts[i])
		if err != nil {
			continue
		}
		switch {
		case v == 0:
			*st = state{}
		case v == 1:
			st.bold = true
		case v == 4:
			st.underline = true
		case v == 5:
			st.blink = true
		case v == 7:
			st.invert = true
		case v == 22:
			st.bold = false
		case v == 24:
			st.underline = false
		case v == 25:
			st.blink = false
		case v == 27:
			st.invert = false
		case v == 39:
			st.fg = ""
		case v == 49:
			st.bg = ""
		case 30 <= v && v <= 37:
			st.fg = ansiBasicColor(v - 30)
		case 90 <= v && v <= 97:
			st.fg = ansiBrightColor(v - 90)
		case 40 <= v && v <= 47:
			st.bg = ansiBasicColor(v - 40)
		case 100 <= v && v <= 107:
			st.bg = ansiBrightColor(v - 100)
		case v == 38 || v == 48:
			if i+2 < len(parts) && parts[i+1] == "5" {
				idx, err := strconv.Atoi(parts[i+2])
				if err == nil {
					color := xterm256(idx)
					if v == 38 {
						st.fg = color
					} else {
						st.bg = color
					}
				}
				i += 2
			}
		}
	}
}

func ansiBasicColor(i int) string {
	colors := []string{"#000000", "#b22222", "#2e8b57", "#b8860b", "#1d4ed8", "#8b5cf6", "#0891b2", "#d1d5db"}
	if i < 0 || i >= len(colors) {
		return ""
	}
	return colors[i]
}

func ansiBrightColor(i int) string {
	colors := []string{"#6b7280", "#ef4444", "#22c55e", "#fde047", "#60a5fa", "#c084fc", "#67e8f9", "#ffffff"}
	if i < 0 || i >= len(colors) {
		return ""
	}
	return colors[i]
}

func xterm256(idx int) string {
	if idx < 0 {
		idx = 0
	}
	if idx > 255 {
		idx = 255
	}
	if idx < 16 {
		return []string{
			"#000000", "#800000", "#008000", "#808000", "#000080", "#800080", "#008080", "#c0c0c0",
			"#808080", "#ff0000", "#00ff00", "#ffff00", "#0000ff", "#ff00ff", "#00ffff", "#ffffff",
		}[idx]
	}
	if idx <= 231 {
		n := idx - 16
		r := n / 36
		g := (n % 36) / 6
		b := n % 6
		return fmt.Sprintf("#%02x%02x%02x", cubeValue(r), cubeValue(g), cubeValue(b))
	}
	v := 8 + (idx-232)*10
	return fmt.Sprintf("#%02x%02x%02x", v, v, v)
}

func cubeValue(v int) int {
	if v == 0 {
		return 0
	}
	return 55 + v*40
}
