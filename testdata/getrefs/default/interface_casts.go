package sample

import "fmt"

type Stringer interface {
	String() string
}

type named struct {
	v string
}

func (n named) String() string {
	return n.v
}

func interfaceCasts(x interface{}) string {
	s, _ := x.(Stringer)
	if n, ok := s.(named); ok {
		return n.String()
	}
	return fmt.Sprint(s)
}
