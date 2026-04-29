package sample

var n = 1

func shadowing(n int) int {
	value := n
	{
		n := value + 1
		value := n
		_ = value
	}
	return value
}
