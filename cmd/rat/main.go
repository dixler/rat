package main

import (
	"flag"
	"fmt"
	"os"

	"rat/internal/ansihtml"
	"rat/internal/display"
	"rat/internal/file"
)

type OutputMode string

const (
	ModeANSI OutputMode = "ansi"
	ModeHTML OutputMode = "html"
)

func ProcessPipeline(filepath string, mode OutputMode) (string, error) {
	f, err := file.Analyze(filepath)
	if err != nil {
		return "", err
	}

	parsed := ParseFormats(f)
	ansi := display.RenderSource(f.Source(), parsed.SourceSpans, parsed.LineSpans, parsed.LineMarkers)
	if mode == ModeHTML {
		return ansihtml.Convert(ansi), nil
	}
	return ansi, nil
}

func main() {
	serve := flag.Bool("serve", false, "run HTTP server")
	addr := flag.String("addr", ":8081", "server listen addr")
	mode := flag.String("format", string(ModeANSI), "output format: ansi or html")
	flag.Parse()

	if *serve {
		runServer(*addr)
		return
	}

	if len(flag.Args()) != 1 {
		die("usage: rat [-format ansi|html] <file.go>")
	}
	outputMode := OutputMode(*mode)
	if outputMode != ModeANSI && outputMode != ModeHTML {
		die("invalid format: expected ansi or html")
	}

	path := flag.Args()[0]
	out, err := ProcessPipeline(path, outputMode)
	if err != nil {
		die(err.Error())
	}
	fmt.Print(out)
}

func die(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}
