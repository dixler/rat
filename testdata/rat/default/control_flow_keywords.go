package sample

func controlFlowKeywords() int {
loop:
	for {
		if false {
			continue
		}
		break loop
	}
	goto done
done:
	panic("boom")
	return 1
}
