package main

import (
	"fmt"
	"os"

	"notectl/internal/display"
	"notectl/internal/file"
)

func main() {
	if len(os.Args) != 2 {
		die("usage: getrefs <file.go>")
	}
	f, err := file.New(os.Args[1])
	if err != nil {
		die(err.Error())
	}
	if f == nil {
		die("failed to open file")
	}
	display.RenderFile(f)
}

func die(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}
