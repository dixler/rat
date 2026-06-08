package sample

func conditionalDirectTerminalOnly(flag bool, values []int) int {
	if flag {
		{
			return 1
		}
	}

	if len(values) > 0 {
		for _, v := range values {
			if v > 10 {
				return v
			}
		}
	}

	if len(values) == 1 {
		return values[0]
	}

	return 0
}
