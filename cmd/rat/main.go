package main

import (
	"flag"
	"fmt"
	"os"

	"rat/internal/display"
	"rat/internal/file"
)

type ProcessOptions struct {
	EscapeMode bool
}

func ProcessPipeline(filepath string, opts ProcessOptions) (string, error) {
	f, err := file.Analyze(filepath)
	if err != nil {
		return "", err
	}

	var provider StyleProvider
	if opts.EscapeMode {
		provider = EscapeStyleProvider{}
	} else {
		provider = DefaultStyleProvider{}
	}

	spans := ParseFormats(f, provider)
	return display.RenderSource(f.Source(), spans), nil
}

func main() {
	var escapeMode bool
	flag.BoolVar(&escapeMode, "escapes", false, "Render escape analysis information")
	flag.Parse()

	if len(flag.Args()) != 1 {
		die("usage: rat [-escapes] <file.go>")
	}

	path := flag.Args()[0]
	out, err := ProcessPipeline(path, ProcessOptions{EscapeMode: escapeMode})
	if err != nil {
		die(err.Error())
	}
	fmt.Print(out)
}

func die(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}
