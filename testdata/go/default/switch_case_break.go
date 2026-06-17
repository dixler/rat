package sample

func switchCaseBreak(v int, x any, ch <-chan int) int {
	total := 0
	switch v {
	case 0:
		for i := 0; i < v; i++ {
			if i > 1 {
				break
			}
			total += i
		}
		total++
		break
	case 1:
		return total + 1
	case 2:
		for i := 0; i < v; i++ {
			switch i {
			case 0:
				break
			default:
				total += i
			}
			total++
		}
	case 3:
		for i := 0; i < v; i++ {
			if i > 2 {
				break
			}
		}
	case 4:
		for i := 0; i < v; i++ {
			switch value := x.(type) {
			case int:
				total += value
				break
			default:
				total += i
			}
			total++
		}
	case 5:
		for i := 0; i < v; i++ {
			select {
			case got := <-ch:
				total += got
				break
			default:
				total += i
			}
			total++
		}
	default:
		total += 2
	}

	for i := 0; i < v; i++ {
	switchLabel:
		switch i {
		case 0:
			break switchLabel
		default:
			total += i
		}
	}

	for i := 0; i < v; i++ {
	selectLabel:
		select {
		case got := <-ch:
			total += got
			break selectLabel
		default:
			total += i
		}
	}

outer:
	for i := 0; i < v; i++ {
		switch i {
		case 0:
			break
		case 1:
			break outer
		default:
			total += i
		}
	}
	return total
}
