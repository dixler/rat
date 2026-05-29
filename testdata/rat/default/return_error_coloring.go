package sample

func returnErrorColoring(flag bool, err error) (string, error) {
	if flag {
		return "", err
	}
	return "", nil
}

func returnNoErrorColoring() int {
	return 1
}
