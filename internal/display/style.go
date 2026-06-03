package display

const (
	Reset         BasicStyle = "\x1b[0m"
	Bold          BasicStyle = "\x1b[1m"
	Invert        BasicStyle = "\x1b[7m"
	Blink         BasicStyle = "\x1b[5m"
	Underline     BasicStyle = "\x1b[4m"
	Overline      BasicStyle = "\x1b[53m"
	Strikethrough BasicStyle = "\x1b[9m"
	Gray          BasicStyle = "\x1b[90m"
	LightGray     BasicStyle = "\x1b[38;5;250m"
	Red           BasicStyle = "\x1b[31m"
	BrightRed     BasicStyle = "\x1b[91m"
	LightRed      BasicStyle = "\x1b[38;5;203m"
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
	LightPink     BasicStyle = "\x1b[38;5;218m"
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

func (s BasicStyle) Frame() BasicStyle {
	return s + Underline
}
