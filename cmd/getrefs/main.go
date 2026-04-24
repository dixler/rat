package main

import (
	"fmt"
	"os"

	"notectl/internal/getrefs"
)

func main() {
	if len(os.Args) != 2 {
		die("usage: getrefs <file.go | [<file|dir>:]<identifierName>>")
	}
	if err := getrefs.Run(os.Args[1]); err != nil {
		die(err.Error())
	}
}

func die(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}
