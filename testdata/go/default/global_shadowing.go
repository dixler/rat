package sample

func makeItem() item {
	return item{value: 1}
}

func globalShadowing() item {
	makeItem := func() item {
		return item{value: 2}
	}
	return makeItem()
}
