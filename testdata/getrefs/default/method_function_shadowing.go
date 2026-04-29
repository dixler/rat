package sample

type runner struct{}

func runValue(v int) int {
	return v + 1
}

func (runner) runValue(v int) int {
	return v + 2
}

func useRunValue(r runner, runValue int) int {
	local := func(runValue int) int {
		return runValue + 3
	}
	return r.runValue(runValue) + local(runValue)
}
