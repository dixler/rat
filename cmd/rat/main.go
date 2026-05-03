package main

import (
	"flag"
	"fmt"
	"os"

	"rat/internal/display"
	"rat/internal/file"
)

type ProcessOptions struct {
}

func ProcessPipeline(filepath string, opts ProcessOptions) (string, error) {
	f, err := file.Analyze(filepath)
	if err != nil {
		return "", err
	}

	parsed := ParseFormats(f)
	return display.RenderSource(f.Source(), parsed.SourceSpans, parsed.LineSpans), nil
}

func main() {
	flag.Parse()

	if len(flag.Args()) != 1 {
		die("usage: rat <file.go>")
	}

	path := flag.Args()[0]
	out, err := ProcessPipeline(path, ProcessOptions{})
	if err != nil {
		die(err.Error())
	}
	fmt.Print(out)
}

func die(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}
