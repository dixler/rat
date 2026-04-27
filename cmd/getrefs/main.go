package main

import (
	"fmt"
	"os"

	"notectl/internal/display"
	"notectl/internal/file"
)

func ProcessPipeline(filepath string) (string, error) {
	f, err := file.Analyze(filepath)
	if err != nil {
		return "", err
	}
	spans := ParseFormats(f)

	r := &Renderer{}
	r.printHeader(f)
	r.printTree(projectRoot(f.Name()), f.Tree(), 0)
	r.printImports(f.PackageReferences())

	srcRender := display.RenderSource(f.Source(), spans)
	r.b.WriteString(srcRender)

	return r.b.String(), nil
}

func main() {
	if len(os.Args) != 2 {
		die("usage: getrefs <file.go>")
	}
	out, err := ProcessPipeline(os.Args[1])
	if err != nil {
		die(err.Error())
	}
	fmt.Print(out)
}

func die(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}
