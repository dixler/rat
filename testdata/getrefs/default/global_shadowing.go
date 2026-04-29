package sample

type item struct {
	name string
}

func makeItem() item {
	return item{name: "global"}
}

func globalShadowing() item {
	makeItem := func() item {
		return item{name: "local"}
	}
	return makeItem()
}
