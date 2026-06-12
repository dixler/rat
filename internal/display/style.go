package display

const (
	Reset         BasicStyle = "\x1b[0m"
	Invert        BasicStyle = "\x1b[7m"
	Underline     BasicStyle = "\x1b[4m"
	Gray          BasicStyle = "\x1b[90m"
	LightRed      BasicStyle = "\x1b[38;5;203m"
	MutedOrange   BasicStyle = "\x1b[38;5;130m"
	VibrantOrange BasicStyle = "\x1b[38;5;215m"
	Yellow        BasicStyle = "\x1b[38;5;226m"
	LightGreen    BasicStyle = "\x1b[38;5;120m"
	Green         BasicStyle = "\x1b[32m"
	Blue          BasicStyle = "\x1b[94m"
	Purple        BasicStyle = "\x1b[35m"
	White         BasicStyle = "\x1b[97m"
	LightPink     BasicStyle = "\x1b[38;5;218m"
	HotMagenta    BasicStyle = "\x1b[38;5;198m"
)

type BasicStyle string

func (s BasicStyle) Format(str string) string {
	return string(s) + str + string(Reset)
}

func (s BasicStyle) Invert() BasicStyle {
	return s + Invert
}

func (s BasicStyle) Frame() BasicStyle {
	return s + Underline
}
