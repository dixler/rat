package sample

func returnErrorColoring(flag bool, err error) (string, error) {
	if flag {
		return "", err
	}
	return "", nil
}

func returnOnlyError(err error) error {
	return err
}

func returnNamedError(flag bool) (err error) {
	if flag {
		return
	}
	return nil
}

func returnErrorNotLast(err error) (error, string) {
	return err, ""
}

func returnNoErrorColoring() int {
	return 1
}
