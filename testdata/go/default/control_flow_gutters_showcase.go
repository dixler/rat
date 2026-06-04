package sample

func controlFlowGuttersShowcase(a int, ch <-chan int) int {
	if a > 10 {
		a += 10
	} else if a > 0 {
		a += 1
	} else {
		a = 0
	}

	if a == 42 {
		a = 7
	}

	for i := 0; i < a; i++ {
		if i > 4 {
			break
		}
	}

	for j := 0; j < a; j++ {
		a += j
	}

	switch a % 4 {
	case 0:
		a += 100
	case 1:
		a += 200
	default:
		a += 300
	}

	select {
	case v := <-ch:
		a += v
	case <-ch:
		a += 2
	default:
		a += 3
	}

	switch a % 3 {
	case 0:
		a += 11
	case 1:
		a += 22
	case 2:
		a += 33
	}

	select {
	case v := <-ch:
		a += v * 2
	case <-ch:
		a += 4
	}

	if a%2 == 0 {
		return a
	}
	return a + 1
}
