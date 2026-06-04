package sample

import "fmt"

type Namer interface {
	Name() string
}

type person struct {
	name string
}

func (p person) Name() string { return p.name }

func narrowByTypeSwitch(v interface{}) string {
	switch x := v.(type) {
	case Namer:
		return x.Name()
	case fmt.Stringer:
		return x.String()
	default:
		return "unknown"
	}
}
