package sample

var globalCount = 1

func reassignGlobal() int {
	globalCount = globalCount + 1
	return globalCount
}
