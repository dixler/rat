package main

import (
	"fmt"
	"os"

	"notectl/internal/getrefs"
)

func main() {
	if len(os.Args) == 3 && os.Args[1] == "cat" {
		if err := getrefs.Cat(os.Args[2]); err != nil {
			die(err.Error())
		}
		return
	}
	if len(os.Args) != 2 {
		die("usage: getrefs [<file|dir>:]<identifierName>\n       getrefs cat <file.go>")
	}
	if err := getrefs.Run(os.Args[1]); err != nil {
		die(err.Error())
	}
}

func die(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}
