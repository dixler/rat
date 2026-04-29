package sample

type absurd struct {
	outer  int
	nested struct {
		inner  int
		deeper struct {
			core int
		}
	}
	build func(seed int) struct {
		result int
	}
}

func absurdNestedStructFields(a absurd) int {
	v := a.nested.deeper.core
	if v > 0 {
		v := a.outer + a.build(v).result
		return v
	}
	return a.nested.inner
}
