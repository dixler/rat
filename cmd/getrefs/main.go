package main

import (
	"flag"
	"fmt"
	"os"

	"notectl/internal/display"
	"notectl/internal/file"
)

var escapeMode bool

func init() {
	flag.BoolVar(&escapeMode, "escapes", false, "Render escape analysis information")
}

func ProcessPipeline(filepath string) (string, error) {
	f, err := file.Analyze(filepath)
	if err != nil {
		return "", err
	}

	var provider StyleProvider
	if escapeMode {
		provider = EscapeStyleProvider{}
	} else {
		provider = DefaultStyleProvider{}
	}

	spans := ParseFormats(f, provider)

	r := &Renderer{}
	r.printHeader(f)
	r.printTree(projectRoot(f.Name()), f.Tree(), 0, provider)
	r.printImports(f.PackageReferences())

	srcRender := display.RenderSource(f.Source(), spans)
	r.b.WriteString(srcRender)

	return r.b.String(), nil
}

func main() {
	flag.Parse()

	if len(flag.Args()) != 1 {
		die("usage: getrefs [-escapes] <file.go>")
	}

	out, err := ProcessPipeline(flag.Args()[0])
	if err != nil {
		die(err.Error())
	}
	fmt.Print(out)
}

func die(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}
