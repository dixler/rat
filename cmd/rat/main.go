package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"rat/internal/ansihtml"
	"rat/internal/file/scan/golang/goplsclient"
	"rat/internal/highlight"
)

type OutputMode string

const (
	ModeANSI OutputMode = "ansi"
	ModeHTML OutputMode = "html"
)

func ProcessPipeline(filepath string, mode OutputMode) (string, error) {
	program, err := highlight.Analyze(filepath)
	if err != nil {
		return "", err
	}

	ansi := highlight.RenderSource(program)
	if mode == ModeHTML {
		return ansihtml.Convert(ansi), nil
	}
	return ansi, nil
}

func main() {
	if err := run(); err != nil {
		die(err.Error())
	}
}

func run() (err error) {
	defer func() {
		err = errors.Join(err, goplsclient.CloseDefault())
	}()

	profiler := &profiler{}
	if err := profiler.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "%v", err)
	}
	defer func() {
		if err := profiler.Stop(); err != nil {
			fmt.Fprintf(os.Stderr, "%v", err)
		}
	}()

	serve := flag.Bool("serve", false, "run HTTP server")
	addr := flag.String("addr", ":8081", "server listen addr")
	mode := flag.String("format", string(ModeANSI), "output format: ansi or html")
	flag.Parse()

	if *serve {
		runServer(*addr)
		return nil
	}

	if len(flag.Args()) != 1 {
		return fmt.Errorf("usage: rat [-format ansi|html] <file>")
	}
	outputMode := OutputMode(*mode)
	if outputMode != ModeANSI && outputMode != ModeHTML {
		return fmt.Errorf("invalid format: expected ansi or html")
	}

	path := flag.Args()[0]
	out, err := ProcessPipeline(path, outputMode)
	if err != nil {
		return err
	}
	fmt.Print(out)
	return nil
}

func die(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}
