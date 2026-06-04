package sample

import "fmt"

const keywordFixtureConst = 42

var keywordFixtureVar = map[string]int{"one": 1}

type keywordFixtureStruct struct {
	Value string
}

type keywordFixtureInterface interface {
	Handle(map[string]int)
}

func keywordLiteralBuiltins(input []string) {
	defer fmt.Println("done", len(input))
	go func() {
		println("worker")
	}()

	values := make(map[string]int)
	for i, value := range input {
		values[value] = i
	}

	_ = keywordFixtureStruct{Value: `multi
line`}
	_ = append(input, "tail")
}
