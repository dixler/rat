package sample

func loopBreakIfColoring(items []int, stop <-chan struct{}) int {
	total := 0

outer:
	for i := 0; i < len(items); i++ {
		if items[i] < 0 {
			break outer
		}

		for j := 0; j < items[i]; j++ {
			if j == 0 {
				continue
			}
			if j > 3 {
				break
			}
			total += j
		}

		if items[i]%2 == 0 {
			total += items[i]
		} else if items[i] == 7 {
			return total
		} else {
			total += 1
		}
	}

	for {
		select {
		case <-stop:
			break
		default:
			if total > 20 {
				break
			}
			total++
		}
		if total > 30 {
			return total
		}
	}

	for idx, v := range items {
		if v < 0 {
			continue
		}
		if idx > 4 {
			break
		}
		if v == 42 {
			return total + v
		}
		total += v
	}

	for k := 0; k < len(items); k++ {
		if items[k]%3 == 0 {
			continue
		}
		total += items[k]
	}

	return total
}
