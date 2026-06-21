package sample

import (
	_ "errors"
	. "fmt"
	alias "rat/internal/display"
	"strings"
)

func importStrings() {
	_ = alias.Blue
	Println("ok")
	_ = strings.Builder
}
