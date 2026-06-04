package sample

func ifReturnPermutations(flag bool, override string) string {
	value := "default"
	if flag {
		return value
	} else if override == "skip" {
		return "skip"
	} else {
		value = "from-else"
	}
	if override != "" {
		value = override
	}
	switch value {
	case "a":
		return "A"
	case "b":
		value = "B"
	default:
		return "fallback"
	}
	if value == "default" {
		return "still-default"
	}
	if value == "x" {
		value = "y"
	}
	return value
}

func ifReturnPermutationsMultiline(flag bool, override string) string {
	value := "default"
	if flag {
		value = "flag"
	} else if override == "ok" {
		value = "ok"
	} else if override == "ret" {
		return "ret"
	} else {
		return value
	}
	select {
	case <-make(chan struct{}):
		return "never"
	default:
		value = "selected"
	}
	return value
}
