package sample

func controlFlowDefs(values []int, ch <-chan int) int {
	total := 0
	if v := len(values); v > 0 {
		total += v
	}
	for i := 0; i < len(values); i++ {
		total += values[i]
	}
	switch mode := total % 3; mode {
	case 0:
		total += mode
	default:
		total += 1
	}
	select {
	case n := <-ch:
		total += n
	default:
		total += 0
	}
	return total
}
