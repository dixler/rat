package sample

func ifElseFallthroughMisclassified() error {
	var root string
	if root == "" {
		var err error
		if err != nil {
			return err
		}
	} else {
		var err error
		if err != nil {
			return err
		}
	}
	return nil
}
