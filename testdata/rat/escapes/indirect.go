package p

type I interface{ M() }

func test(i I) {
	i.M()
}
