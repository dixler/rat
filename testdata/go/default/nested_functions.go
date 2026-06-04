package sample

func nested(seed int) int {
	adder := func(seed int) int {
		inner := func(delta int) int {
			return seed + delta
		}
		return inner(2)
	}
	return adder(seed)
}
