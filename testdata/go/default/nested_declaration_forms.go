package sample

func nestedDeclarationForms(nums []int, in <-chan int, v interface{}) int {
	total := 0

	if n := len(nums); n > 0 {
		total += n
	}

	for i := 0; i < len(nums); i++ {
		total += nums[i]
	}

	switch mode := total % 2; mode {
	case 0:
		total += mode
	default:
		total += 1
	}

	switch x := v.(type) {
	case int:
		total += x
	case interface{ value() int }:
		total += x.value()
	}

	for idx, val := range nums {
		total += idx + val
	}

	select {
	case n := <-in:
		total += n
	default:
		total += 0
	}

	select {
	case n, ok := <-in:
		if ok {
			total += n
		}
	default:
		total += 0
	}

	return total
}
