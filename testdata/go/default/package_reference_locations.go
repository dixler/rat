package sample

import (
	"fmt"

	"rat/internal/display"
)

type greeter struct{}

func (greeter) message() string {
	return "hello"
}

func samePackageFn() string {
	return "world"
}

func exercise() {
	g := greeter{}
	fmt.Println(samePackageFn())
	fmt.Println(g.message())
	fmt.Println(display.Blue.Format("!"))
}
