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

func nestedErrors(seed int, err error) error {
	wrap := func(delta int) (int, error) {
		if delta > seed {
			return 0, err
		}
		return func(extra int) (int, error) {
			return seed + delta + extra, nil
		}(1)
	}
	_, got := wrap(2)
	return got
}
