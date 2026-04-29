package sample

var packageValue = 10

func plusOne(v int) int {
	return v + 1
}

func useSameFileRefs() int {
	local := plusOne(packageValue)
	return local
}
