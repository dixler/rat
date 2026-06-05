package sample

func switchCaseFallthrough(v int) int {
	switch v {
	case 0:
		return 10
	case 1:
		v++
		fallthrough
	case 2:
		return 20
	default:
		return 30
	}
}
