package sample

func anonymousScopes(flag bool) int {
	count := 0
	{
		count := 2
		_ = count
	}
	if flag {
		count := 3
		_ = count
	}
	return count
}
